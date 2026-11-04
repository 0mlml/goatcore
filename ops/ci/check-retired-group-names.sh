#!/usr/bin/env bash
# ops/ci/check-retired-group-names.sh — refuse a retired Ory service-permission
# group name anywhere it could be load-bearing.
#
# WHY: a rename is not complete when every current reference is
# updated. It is complete when a NEW reference to the old name cannot be
# introduced. The ADMINS → PLATFORM-ADMIN contract cleared every known site
# and still resurfaced three weeks later as a hardcoded constant copied out of
# another component's Default impl — the sole grant of full access on a
# fail-closed path, and invisible until enforcement was switched on. This
# guard is the property the sweep could not give: the old name is refused at
# PR time, in code and config and the current-state docs, forever.
#
# WHAT IT SCANS: ops/, crates/, .github/, docs/design/, docs/operations/,
# CLAUDE.md — the surfaces that describe or enforce the CURRENT system. History
# is exempt on purpose: docs/findings/, docs/adr/, docs/measurements/,
# docs/project/, docs/claudeoverflow.md record what WAS, and a retired name is
# correct there. Matching is case-insensitive, because the same names exist in
# the lowercase operator-IdP plane (obs-host / minio role defaults).
#
# ADDING A RETIRED NAME: append it to RETIRED below in the contract PR of its
# rename, in the same change that removes the last reference. Never add a name
# whose live identities still carry it.
set -euo pipefail

ROOT="${1:-.}"
cd "$ROOT"

# Retired Ory service-permission group names, and what replaced each.
RETIRED=(
  "ALL-ADMIN=OBS-ADMIN"        # track obs-group-rename, contracted 2026-09
  "ALL-READONLY=OBS-READONLY"  # track obs-group-rename, contracted 2026-09
)

SCAN_PATHS=(ops crates .github docs/design docs/operations CLAUDE.md)

# Files that carry the retired names by necessity: this guard's own list, and
# the rename runbook, which records the migrate commands verbatim. Keep this
# list short; every entry is a place a copy can be seeded from. The public
# copies of the history directories (ops/oss/docs-public/docs/<history>/) are
# history too, exempt for the same reason as the originals.
EXEMPT_RE='ops/ci/check-retired-group-names\.sh|docs/operations/obs-group-rename-cutover\.md|ops/oss/docs-public/docs/(adr|findings|measurements|project)/[^:]*'

fail=0
for pair in "${RETIRED[@]}"; do
  old="${pair%%=*}"; new="${pair##*=}"
  # Word-bounded so OBS-ADMIN does not match inside e.g. "SMALL-ADMIN"; the
  # hyphen is part of the name, so bound on non-name characters.
  hits=$(git grep -n -I -i -E "(^|[^A-Za-z0-9_-])${old}([^A-Za-z0-9_-]|$)" -- "${SCAN_PATHS[@]}" 2>/dev/null \
         | grep -v -E "^(${EXEMPT_RE}):" || true)
  if [[ -n "$hits" ]]; then
    fail=1
    echo "FAIL: retired group name ${old} (replaced by ${new}) is still referenced:"
    echo "$hits" | sed 's/^/    /'
    echo
  fi
done

if [[ "$fail" == 1 ]]; then
  cat >&2 <<'EOF'
A retired Ory service-permission group name appeared in a surface that
describes or enforces the CURRENT system. Use the replacement named above.
If the line is genuinely history, it belongs in docs/findings, docs/adr,
docs/measurements or docs/project, which this guard does not scan.
(SoT for the live vocabulary: ops/permissions/group-permissions.yml.)
EOF
  exit 1
fi
echo "ok: no retired Ory group name in ${SCAN_PATHS[*]} (${#RETIRED[@]} retired names checked)"
