#!/usr/bin/env bash
# Regions must never gate ACCESS (ADR 1157).
#
# WHY THIS GUARD EXISTS. zenoh's `gateway.south` is a SCALING feature: it keeps a
# router's link-state domain proportional to the number of regions rather than to
# the square of the peer population. It is declared as a list of FILTERS matching
# on modes / interfaces / zids / region_names, which gives it exactly the shape of
# an access control — a list of peer attributes with an allow/deny feel.
#
# It is not one. Two of the four filter axes — `zids` and `region_names` — are
# values THE PEER ASSERTS ABOUT ITSELF (zenoh-config-1.9.0/src/gateway.rs:65-71;
# ZID is a label not a credential, ADR 1041 citing ADR 0844). A peer that lies
# about its region gets its traffic routed differently. It gains no access,
# because access is the TLS trust store plus the ACL, which are independent of
# regions entirely.
#
# The hazard is therefore human: under scale pressure someone reaches for
# region_names to scope a partner, it appears to work, and a routing hint is
# quietly load-bearing for security. This guard refuses that.
set -uo pipefail
cd "$(dirname "$0")/../.." || exit 1
fail=0

# 1. No region/zid axis inside an access_control block of any rendered config.
for f in $(git ls-files 'ops/ansible/roles/*/templates/*.json5.j2' 2>/dev/null); do
  # Extract the access_control section only; region filters elsewhere are fine.
  sec=$(awk '/access_control:/{f=1} f{print} /^  \},$/{if(f)exit}' "$f" 2>/dev/null)
  if grep -qE '(region_names|gateway)' <<<"$sec"; then
    echo "  FAIL  $f — access_control block references regions/gateway."
    echo "        Regions are self-asserted; they route, they do not authorize."
    fail=1
  fi
done

# 2. A partner declaration must not try to scope by region.
# Scan the DIRECTORY, not `git ls-files`: an untracked declaration is exactly the
# one a reviewer has not seen, and skipping it would make this guard weakest
# precisely where it matters most.
for f in ops/federation/partners/*.yaml; do
  [ -e "$f" ] || continue
  if grep -qE '^\s*(region|region_names|zids)\s*:' "$f"; then
    echo "  FAIL  $f — a partner declaration scopes by region or ZID."
    echo "        Both are peer-asserted. Scope by the agreed certificate CN."
    fail=1
  fi
done

if [ "$fail" -eq 0 ]; then
  echo "[no-region-authz] PASS — regions route; the trust store and the ACL authorize."
else
  echo "[no-region-authz] FAIL — see docs/adr/1157-cross-sovereign-federation-edge-terminated.md §8"
fi
exit $fail
