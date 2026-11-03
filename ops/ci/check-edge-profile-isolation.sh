#!/usr/bin/env bash
# The sovereign edge profile must stay empty-by-construction (ADR 1157).
#
# The failure this prevents is subtle and one-way: someone "reuses" the core
# profile for an edge, or adds a convenience to the edge profile, and because
# Zenoh's ACL takes the first Allow across matched subjects and breaks
# (access_control.rs:983-1021), the addition can only ever WIDEN. A narrow rule
# added later cannot take it back.
set -uo pipefail
cd "$(dirname "$0")/../.." || exit 1
E="ops/ansible/roles/goat-zenoh-edge/templates/config.json5.j2"
fail=0
chk(){ if eval "$2"; then printf "  ok    %s\n" "$1"; else printf "  FAIL  %s\n" "$1"; fail=1; fi; }

[ -f "$E" ] || { echo "[edge-profile] edge template missing: $E"; exit 1; }
echo "[edge-profile] $E"

# Check CODE, not commentary. The template deliberately explains in comments what
# it does not do ("not link_protocols, not a wildcard"), and a guard that reads
# those would fire on the explanation rather than the configuration.
CODE=$(mktemp); trap 'rm -f "$CODE"' EXIT
sed 's://.*$::' "$E" > "$CODE"
E="$CODE"

chk "default_permission is deny"            'grep -q "default_permission: \"deny\"" "$E"'
chk "no rest plugin"                        '! grep -qE "^\s*rest:" "$E"'
chk "adminspace write is false"             'grep -q "write: false" "$E"'
chk "gossip scouting disabled"              'grep -qE "gossip:\s*\{ enabled: false" "$E"'
chk "multicast scouting disabled"           'grep -qE "multicast:\s*\{ enabled: false" "$E"'
chk "no storages declared"                  'grep -qE "storages:\s*\{\}" "$E"'
chk "close_link_on_expiration is true"      'grep -q "close_link_on_expiration: true" "$E"'
chk "mTLS enforced"                         'grep -q "enable_mtls: true" "$E"'
chk "no wildcard key_expr"                  '! grep -qE "key_exprs:\s*\[\"\*\*\"" "$E"'
chk "no link_protocols subject"             '! grep -q "link_protocols" "$E"'
chk "subject matches an exact CN"           'grep -q "cert_common_names: \[\"{{ goat_zenoh_edge_partner_cn }}\"\]" "$E"'
chk "no trial-peer rule reused from core"   '! grep -q "trial-peer" "$E"'

if [ "$fail" -eq 0 ]; then echo "[edge-profile] PASS"; else
  echo "[edge-profile] FAIL — see docs/adr/1157-cross-sovereign-federation-edge-terminated.md"; fi
exit $fail
