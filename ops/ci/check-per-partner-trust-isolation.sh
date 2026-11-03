#!/usr/bin/env bash
# Each federated EDGE trust file carries exactly ONE partner's authority.
#
# WHY. `ops/ci/check-spire-federated-trust-files.sh` guards the CORE/EDGE
# separation: a partner certificate authority must never land on a core router,
# because per F-424 the Zenoh ACL matches a peer's Common Name and cannot see who
# issued the certificate, so any CA in a router's trust store can mint any name
# that router's ACL recognises.
#
# The SAME argument applies BETWEEN edges, and nothing checked it. The trust-sync
# script used to fold `spire-server bundle list` -- every federated authority --
# into every edge trust file it knew about. With one partner that is
# indistinguishable from correct. With two, the edge facing partner B also trusts
# partner A's authority, so A can mint a certificate carrying B's agreed peering
# CN and it validates at B's edge. A second control still stands in front of it
# (each peering has its own tunnel and its own firewall rule admitting only that
# partner's address), so this is a defence-in-depth failure rather than an open
# door -- but it is invisible with one partner and wrong with two, and two is
# exactly when nobody re-checks an isolation property that has held for months.
#
# WHAT THIS GUARD PROVES, in two halves, because a declaration check alone would
# only prove the config is well-formed and not that the code honours it:
#
#   1. DECLARATION. Every spire_trust_sync_federated_edges entry binds one trust
#      file to one trust domain; no file is shared between two domains; no file
#      is the core router's; the superseded flat list is empty.
#   2. CODE. The rendered trust-sync template fetches PER TRUST DOMAIN
#      (`bundle list -id`) and contains no unqualified `bundle list` that could
#      reach an edge file. Comments are stripped before this half runs, so the
#      guard reads the configuration rather than the prose explaining it.
set -uo pipefail
cd "$(dirname "$0")/../.." || exit 2
fail=0
note() { printf "  %s\n" "$1"; }

TPL="ops/ansible/roles/spire/templates/spire-trust-sync.sh.j2"

echo "[per-partner-trust] scanning group_vars + host_vars"

for f in $(git ls-files 'ops/ansible/group_vars/**/*.yml' 'ops/ansible/host_vars/**/*.yml' 2>/dev/null); do
  grep -qE 'spire_trust_sync_federated_(edges|trust_files)' "$f" 2>/dev/null || continue

  # The superseded flat list. It cannot express which partner belongs in which
  # file, so any non-empty value is refused outright rather than migrated
  # silently -- the role asserts the same thing at deploy time.
  if grep -nE '^\s*spire_trust_sync_federated_trust_files:' "$f" >/dev/null 2>&1; then
    ln=$(grep -nE '^\s*spire_trust_sync_federated_trust_files:' "$f" | head -1 | cut -d: -f1)
    legacy=$(awk -v s="$ln" 'NR>s && /^[[:space:]]*-[[:space:]]*"?/{print; next} NR>s && NF{exit}' "$f")
    if [ -n "$legacy" ]; then
      note "FAIL  $f"
      note "        spire_trust_sync_federated_trust_files is populated and is no longer honoured."
      note "        It was a flat list, so EVERY partner bundle went into EVERY file it named."
      note "        Replace with spire_trust_sync_federated_edges (trust_file + trust_domain)."
      fail=1
    fi
  fi

  grep -qE '^\s*spire_trust_sync_federated_edges:' "$f" || continue

  core=$(grep -E '^\s*spire_trust_sync_router_trust_file:' "$f" 2>/dev/null \
         | head -1 | sed -E 's/.*:[[:space:]]*"?([^"]*)"?[[:space:]]*$/\1/')

  # Walk the block with python so a real YAML parser decides what the entries
  # are; a sed/awk reader of a list-of-maps is how a mistyped key becomes a
  # silent pass.
  parsed=$(REPO_FILE="$f" CORE_TRUST="${core:-}" python3 - <<'PY'
import os, sys
try:
    import yaml
except ImportError:
    print("SKIP PyYAML not available")
    sys.exit(0)

path, core = os.environ["REPO_FILE"], os.environ.get("CORE_TRUST", "")
with open(path, encoding="utf-8") as fh:
    try:
        doc = yaml.safe_load(fh) or {}
    except yaml.YAMLError as e:
        print(f"FAIL not valid YAML: {e}")
        sys.exit(0)

edges = doc.get("spire_trust_sync_federated_edges") or []
if not isinstance(edges, list):
    print("FAIL spire_trust_sync_federated_edges must be a list of {trust_file, trust_domain}")
    sys.exit(0)

by_file = {}
for i, e in enumerate(edges):
    if not isinstance(e, dict):
        print(f"FAIL entry {i} is not a mapping: {e!r}")
        continue
    tf, td = (e.get("trust_file") or "").strip(), (e.get("trust_domain") or "").strip()
    if not tf:
        print(f"FAIL entry {i} has no trust_file")
        continue
    # An edge with no trust domain is the whole defect wearing a new shape: the
    # writer has a file to fill and no statement of whose authority belongs in it.
    if not td:
        print(f"FAIL {tf} declares no trust_domain -- an edge file with no named partner")
        continue
    if core and tf == core:
        print(f"FAIL {tf} is the CORE router trust file; a partner authority there "
              f"can mint any name the core ACL knows")
        continue
    prev = by_file.get(tf)
    if prev and prev != td:
        print(f"FAIL {tf} is claimed by TWO trust domains ({prev} and {td}); "
              f"one edge file carries exactly one partner authority")
        continue
    by_file[tf] = td
    print(f"OK {tf} <- {td}")
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

# --- Half 2: the code path itself -------------------------------------------
echo "[per-partner-trust] $TPL"
if [ ! -f "$TPL" ]; then
  note "FAIL  trust-sync template missing: $TPL"
  fail=1
else
  CODE=$(mktemp); trap 'rm -f "$CODE"' EXIT
  # Strip comments. The template explains at length what it does NOT do, and a
  # guard reading the explanation would fire on the warning rather than the code.
  sed 's:^[[:space:]]*#.*$::' "$TPL" > "$CODE"

  if grep -q 'bundle list -id' "$CODE"; then
    note "ok    edge bundles are fetched per trust domain (bundle list -id)"
  else
    note "FAIL  no per-trust-domain fetch (\`bundle list -id\`) in the template."
    note "        An edge trust file must receive ONE partner's bundle, not all of them."
    fail=1
  fi

  if grep -qE 'bundle list( -format| \|| *$)' "$CODE"; then
    note "FAIL  unqualified \`bundle list\` in the template — that is EVERY federated"
    note "        authority. Only \`bundle list -id <spiffe id>\` may feed an edge file."
    fail=1
  else
    note "ok    no unqualified \`bundle list\` (all-partners fetch) remains"
  fi

  if grep -q 'FEDERATED_TRUST_FILES' "$CODE"; then
    note "FAIL  the superseded flat FEDERATED_TRUST_FILES loop is still rendered"
    fail=1
  else
    note "ok    flat FEDERATED_TRUST_FILES loop is gone"
  fi
fi

if [ "$fail" -ne 0 ]; then
  echo "[per-partner-trust] FAIL — see F-424, roles/spire/defaults/main.yml,"
  echo "                    and docs/design/cross-sovereign-federation.md"
  exit 1
fi
echo "[per-partner-trust] PASS"
