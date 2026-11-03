#!/usr/bin/env bash
# A retired federated peering is removed from the edge, not merely stopped from
# being re-added.
#
# WHY. `ops/ci/check-spire-federated-trust-files.sh` keeps a partner certificate
# authority off the CORE. `ops/ci/check-per-partner-trust-isolation.sh` keeps
# each partner's authority off every OTHER partner's edge. Both govern what gets
# ADDED. Neither can see the direction that has no code path at all: nothing ever
# REMOVED an authority from a trust file.
#
# spire-trust-sync's append helper adds what is missing and never removes what is
# present — deliberately, because that is what makes a 15-minute timer safe
# against a live trust file. The consequence is that deleting a peering from
# spire_trust_sync_federated_edges stops the partner's authority being added and
# leaves the copy already in the file. Per F-424 the Zenoh ACL matches a Common
# Name and cannot see the issuer, so a partner nobody has peered with for months
# can still mint the names that edge recognises.
#
# The fix is a declared retirement: the entry MOVES to
# spire_trust_sync_federated_edges_retired, whose trust files the trust-sync
# script converges on our own bundle alone. This guard exists because that is a
# rule nobody would follow reliably — the natural edit is to delete the entry,
# and deleting it is exactly the silent non-removal.
#
# WHAT IT PROVES, in three halves:
#
#   1. DECLARATION. Every retired entry has a trust_file; no retired file is
#      also an active edge (added and removed on alternate ticks); no retired
#      file is the core router's (the retired pass writes our own bundle ALONE,
#      which on the core would strip every other CA it carries).
#   2. THE EDIT. For every group_vars/host_vars file this branch changed, a
#      trust_file that was an ACTIVE edge at the merge base and is now in
#      NEITHER list is refused — that is a peering retired by deletion, which
#      leaves the authority in place.
#   3. CODE. The template actually contains the reconciling path, a retired
#      table, and the fetch-failure fallback that keeps a transient failure from
#      reducing trust; and the CORE trust file is never passed to it.
set -uo pipefail
cd "$(dirname "$0")/../.." || exit 2
fail=0
note() { printf "  %s\n" "$1"; }

TPL="ops/ansible/roles/spire/templates/spire-trust-sync.sh.j2"
VARS_GLOBS=('ops/ansible/group_vars/**/*.yml' 'ops/ansible/host_vars/**/*.yml')

# --- Half 1: the declaration -------------------------------------------------
echo "[trust-retirement] retired-edge declarations"
declared_any=0
for f in $(git ls-files "${VARS_GLOBS[@]}" 2>/dev/null); do
  grep -qE '^\s*spire_trust_sync_federated_edges_retired:' "$f" 2>/dev/null || continue
  declared_any=1
  parsed=$(REPO_FILE="$f" python3 - <<'PY'
import os, sys
try:
    import yaml
except ImportError:
    print("SKIP PyYAML not available")
    sys.exit(0)

path = os.environ["REPO_FILE"]
with open(path, encoding="utf-8") as fh:
    try:
        doc = yaml.safe_load(fh) or {}
    except yaml.YAMLError as e:
        print(f"FAIL not valid YAML: {e}")
        sys.exit(0)

core = (doc.get("spire_trust_sync_router_trust_file") or "").strip()
active = doc.get("spire_trust_sync_federated_edges") or []
retired = doc.get("spire_trust_sync_federated_edges_retired") or []
if not isinstance(retired, list):
    print("FAIL spire_trust_sync_federated_edges_retired must be a list of {trust_file, trust_domain}")
    sys.exit(0)

active_files = {
    (e.get("trust_file") or "").strip()
    for e in active if isinstance(e, dict)
}
peers = {
    (e.get("trust_domain") or "").strip().replace("spiffe://", "")
    for e in (doc.get("spire_federates_with") or []) if isinstance(e, dict)
}

for i, e in enumerate(retired):
    if not isinstance(e, dict):
        print(f"FAIL retired entry {i} is not a mapping: {e!r}")
        continue
    tf = (e.get("trust_file") or "").strip()
    td = (e.get("trust_domain") or "").strip()
    if not tf:
        print(f"FAIL retired entry {i} has no trust_file -- nothing to converge")
        continue
    if core and tf == core:
        print(f"FAIL {tf} is the CORE router trust file. The retired pass writes our "
              f"own bundle ALONE; on the core that strips every other CA it carries")
        continue
    if tf in active_files:
        print(f"FAIL {tf} is declared BOTH active and retired -- the two passes would "
              f"add and remove the same authority on alternate ticks, restarting the "
              f"edge router each time")
        continue
    # A retired partner still listed as a federation peer keeps server.conf
    # federating with it and keeps the bootstrap re-importing its bundle, so the
    # authority stays live in SPIRE and one edit puts it back on an edge.
    bare = td.replace("spiffe://", "")
    if bare and bare in peers:
        print(f"FAIL {bare} is retired but still in spire_federates_with -- the server "
              f"keeps federating with it and re-importing its bundle")
        continue
    print(f"OK {tf} retired ({td or 'no trust_domain recorded'})")
PY
)
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    case "$line" in
      OK\ *)   note "ok    $f -> ${line#OK }" ;;
      SKIP\ *) note "skip  $f: ${line#SKIP }" ;;
      *)       note "FAIL  $f"; note "        ${line#FAIL }"; fail=1 ;;
    esac
  done <<< "$parsed"
done
[ "$declared_any" = 1 ] || note "ok    no retired peerings declared anywhere"

# --- Half 2: the edit ---------------------------------------------------------
# A peering removed from the active list must appear in the retired list. Read
# both sides of the change rather than the end state, because the end state of a
# silent deletion and of a peering that never existed are identical.
echo "[trust-retirement] peerings removed from the active list"
BASE=""
for cand in "${GITHUB_BASE_REF:+origin/$GITHUB_BASE_REF}" origin/main main; do
  [ -n "$cand" ] || continue
  if git rev-parse --verify --quiet "$cand^{commit}" >/dev/null; then BASE="$cand"; break; fi
done
if [ -z "$BASE" ]; then
  # A shallow CI clone has no base to diff against. Say so rather than passing
  # silently: half 2 is the half that catches the natural, wrong edit.
  note "skip  no base ref (origin/main) available -- cannot check for deleted peerings"
else
  merge_base="$(git merge-base "$BASE" HEAD 2>/dev/null || echo "$BASE")"
  changed_files="$(git diff --name-only "$merge_base"...HEAD -- 'ops/ansible/group_vars' 'ops/ansible/host_vars' 2>/dev/null | grep -E '\.yml$' || true)"
  if [ -z "$changed_files" ]; then
    note "ok    no group_vars/host_vars changes on this branch"
  fi
  inspected=0
  for f in $changed_files; do
    inspected=$((inspected + 1))
    before="$(git show "$merge_base:$f" 2>/dev/null || true)"
    [ -n "$before" ] || continue          # newly added file: nothing could be removed
    after="$(git show "HEAD:$f" 2>/dev/null || true)"
    parsed=$(BEFORE="$before" AFTER="$after" REPO_FILE="$f" python3 - <<'PY'
import os, sys
try:
    import yaml
except ImportError:
    print("SKIP PyYAML not available")
    sys.exit(0)

def load(text):
    try:
        return yaml.safe_load(text) or {}
    except yaml.YAMLError:
        return {}

def files(doc, key):
    return {
        (e.get("trust_file") or "").strip()
        for e in (doc.get(key) or []) if isinstance(e, dict)
    } - {""}

before, after = load(os.environ["BEFORE"]), load(os.environ["AFTER"])
was_active = files(before, "spire_trust_sync_federated_edges")
now_active = files(after, "spire_trust_sync_federated_edges")
now_retired = files(after, "spire_trust_sync_federated_edges_retired")

for tf in sorted(was_active - now_active - now_retired):
    print(f"FAIL {tf} left spire_trust_sync_federated_edges and is in neither list. "
          f"Deleting the entry only stops the partner authority being ADDED; the copy "
          f"already in that trust file stays trusted. Move the entry to "
          f"spire_trust_sync_federated_edges_retired instead -- that converges the file "
          f"on our own bundle alone and restarts the edge router")
for tf in sorted((was_active - now_active) & now_retired):
    print(f"OK {tf} retired properly (moved to the retired list)")
PY
)
    while IFS= read -r line; do
      [ -n "$line" ] || continue
      case "$line" in
        OK\ *)   note "ok    $f -> ${line#OK }" ;;
        SKIP\ *) note "skip  $f: ${line#SKIP }" ;;
        *)       note "FAIL  $f"; note "        ${line#FAIL }"; fail=1 ;;
      esac
    done <<< "$parsed"
  done
  [ "$inspected" -eq 0 ] || note "ok    $inspected changed vars file(s) inspected against $merge_base"
fi

# --- Half 3: the code path ----------------------------------------------------
echo "[trust-retirement] $TPL"
if [ ! -f "$TPL" ]; then
  note "FAIL  trust-sync template missing: $TPL"
  fail=1
else
  CODE=$(mktemp); trap 'rm -f "$CODE"' EXIT
  # Strip comments: the template explains at length what removal does and does
  # not guarantee, and a guard reading the explanation would pass on the prose.
  sed 's:^[[:space:]]*#.*$::' "$TPL" > "$CODE"

  if grep -q 'reconcile_certs()' "$CODE"; then
    note "ok    a reconciling path exists (reconcile_certs)"
  else
    note "FAIL  no reconcile_certs in the template -- nothing removes a certificate"
    note "        authority, so a retired peering stays trusted on its edge."
    fail=1
  fi

  if grep -q 'retired_edges' "$CODE"; then
    note "ok    the retired peering table is rendered"
  else
    note "FAIL  no retired_edges table -- retirement cannot be declared, so it can"
    note "        only be done by hand on the host."
    fail=1
  fi

  # The property that keeps removal from being dangerous: reconciliation runs
  # only when the partner bundle was actually obtained this tick.
  if grep -q '_partner_ok' "$CODE"; then
    note "ok    reconciliation is gated on the partner bundle being fetched"
  else
    note "FAIL  no fetch-success gate on reconciliation. A transient \`bundle list\`"
    note "        failure would rewrite the edge without the partner's authority --"
    note "        a failure that silently REDUCES trust."
    fail=1
  fi

  # The core is append-only, always. Reconciling it would strip the CAs it
  # carries that this role knows nothing about.
  # shellcheck disable=SC2016  # a literal grep pattern, not a shell expansion
  if grep -q 'reconcile_certs "[^"]*" "\$ROUTER_TRUST_FILE"' "$CODE"; then
    note "FAIL  the CORE router trust file is passed to reconcile_certs. The core"
    note "        legitimately carries CAs this role does not declare; rewriting it"
    note "        from the declared set removes them."
    fail=1
  else
    note "ok    the CORE router trust file is never reconciled (append-only)"
  fi

  if grep -q 'DRY_RUN' "$CODE"; then
    note "ok    the script has a --dry-run that reports removals without making them"
  else
    note "FAIL  no --dry-run. Removing a certificate authority from a live edge must"
    note "        be inspectable before it happens."
    fail=1
  fi
fi

if [ "$fail" -ne 0 ]; then
  echo "[trust-retirement] FAIL — see F-424, roles/spire/defaults/main.yml"
  echo "                   (spire_trust_sync_federated_edges_retired), and"
  echo "                   docs/design/federated-trust-model.md"
  exit 1
fi
echo "[trust-retirement] PASS"
