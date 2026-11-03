#!/usr/bin/env bash
# Prove the compatibility job uses a reliable harness and real 3.2 children.
set -euo pipefail
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
cat > "$scratch/interpreters.bats" <<'BATS'
bats_require_minimum_version 1.14.0
@test "Bats assertions use the modern host interpreter" {
  [ "${BASH_VERSINFO[0]}" -ge 5 ]
}
@test "env-bash children use GNU Bash 3.2.57" {
  run bash -c 'printf "%s:%s:%s" "${BASH_VERSINFO[0]}" "${BASH_VERSINFO[1]}" "${BASH_VERSINFO[2]}"'
  [ "$status" -eq 0 ]
  [ "$output" = "3:2:57" ]
}
@test "the selected subject interpreter is GNU Bash 3.2.57" {
  run "$BASH_UNDER_TEST" -c 'printf "%s:%s:%s" "${BASH_VERSINFO[0]}" "${BASH_VERSINFO[1]}" "${BASH_VERSINFO[2]}"'
  [ "$status" -eq 0 ]
  [ "$output" = "3:2:57" ]
}
@test "the selected subject interpreter retains the empty-array failure" {
  run ! "$BASH_UNDER_TEST" -uc 'a=(); printf "%s\n" "${a[@]}"'
  [ "$status" -ne 0 ]
  [[ "$output" == *"unbound variable"* ]]
}
@test "the Linux-only role interpreter stays modern" {
  run "$HOST_BASH_UNDER_TEST" -c 'test "${BASH_VERSINFO[0]}" -ge 5'
  [ "$status" -eq 0 ]
}
BATS
bats "$scratch/interpreters.bats"
cat > "$scratch/false-assertion.bats" <<'BATS'
bats_require_minimum_version 1.14.0
@test "a false assertion must fail even when the final command succeeds" {
  [[ false == true ]]
  [ 1 -eq 1 ]
}
BATS
echo "==> The following assertion must be rejected"
if bats "$scratch/false-assertion.bats"; then
  echo "FAIL: Bats ignored the deliberately false assertion" >&2
  exit 1
fi
echo "ok: modern assertions, real Bash 3.2 children and host-only interpreter verified"
