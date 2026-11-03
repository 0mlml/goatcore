#!/usr/bin/env bash
# One door per device class — see docs/design/ssh-key-self-service.md.
#
# Bearer gear (NETWORK class) gets its shell through NetBird SSH. Host nodes get
# theirs through authorized_keys. A device must never have both, because two
# grant surfaces means two revoke surfaces: cutting somebody's key would leave
# their NetBird SSH access untouched while the console reported `revoked`.
#
# This guard is cheap and static: it asserts that no goatnet lists hosts under
# the `network-nodes` scope of `operator_ssh_scopes`. A non-empty list is what
# re-opens the second door, and it is a one-line edit somebody could make while
# meaning well.
#
# It deliberately does NOT try to read live `ssh_enabled` flags — CI has no mesh
# credentials, and a check that silently passes when it cannot see the truth is
# worse than no check. The live half is asserted in the design doc and verified
# during deploys.
set -euo pipefail

cd "$(dirname "$0")/.."/..

fail=0

while IFS= read -r f; do
  # Pull the network-nodes entry out of the operator_ssh_scopes block and see
  # whether anything follows it other than an empty list.
  if awk '
    /^operator_ssh_scopes:/ { in_scopes = 1; next }
    in_scopes && /^[a-zA-Z_]/ { in_scopes = 0 }          # left the block
    in_scopes && /^[[:space:]]+network-nodes:[[:space:]]*\[\][[:space:]]*$/ { next }
    in_scopes && /^[[:space:]]+network-nodes:/ { found = 1; next }
    in_scopes && found && /^[[:space:]]+-/ { print; hit = 1 }
    in_scopes && found && /^[[:space:]]+[a-zA-Z-]+:/ { found = 0 }
    END { exit(hit ? 0 : 1) }
  ' "$f" >/dev/null 2>&1; then
    echo "FAIL: $f lists hosts under operator_ssh_scopes.network-nodes"
    echo "      Bearer gear uses NetBird SSH, not authorized_keys."
    echo "      Adding hosts here gives those devices TWO grant surfaces and two"
    echo "      revoke surfaces — a key cut would leave NetBird SSH open while the"
    echo "      console reported 'revoked'."
    echo "      If both are genuinely required, first make the revoke path cut"
    echo "      both in one operation, then update this guard and the design doc."
    fail=1
  fi
done < <(grep -rl "^operator_ssh_scopes:" ops/ansible/group_vars/ 2>/dev/null || true)

if [ "$fail" -eq 0 ]; then
  echo "OK: no goatnet routes bearer gear through authorized_keys."
fi
exit "$fail"
