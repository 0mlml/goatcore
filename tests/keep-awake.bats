#!/usr/bin/env bats
# Behavior tests for ops/scripts/lib/keep-awake.sh (#2122).
#
# A fake `caffeinate` on PATH records how it was called and lives exactly as
# long as the pid it was told to wait for, as the real one does. The tests pin:
#   - the assertion starts with the parent and ends when the parent exits,
#   - a live holder further up (GOAT_KEEP_AWAKE) means no second assertion,
#   - a stale holder pid does not suppress it,
#   - GOAT_NO_CAFFEINATE=1 opts out, and off macOS nothing starts,
#   - every long-running driver sources the helper.

REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
HELPER="$REPO_ROOT/ops/scripts/lib/keep-awake.sh"

setup() {
  BIN="$BATS_TEST_TMPDIR/bin"; LOG="$BATS_TEST_TMPDIR/caffeinate.log"
  mkdir -p "$BIN"; : > "$LOG"
  cat > "$BIN/caffeinate" <<FAKE
#!/usr/bin/env bash
echo "start \$*" >> "$LOG"
while [ "\$1" != "-w" ] && [ \$# -gt 0 ]; do shift; done
pid="\$2"
while kill -0 "\$pid" 2>/dev/null; do sleep 0.1; done
echo "exited" >> "$LOG"
FAKE
  chmod +x "$BIN/caffeinate"
}

# Run a short parent script that sources the helper, holds for a moment, exits.
run_parent() {
  env -u GOAT_KEEP_AWAKE PATH="$BIN:$PATH" GOAT_KEEP_AWAKE_ANY_OS=1 "$@" bash -c '
    . "$1"; goat_keep_awake; echo "parent $$ marker=${GOAT_KEEP_AWAKE:-}"; sleep 0.5' _ "$HELPER"
}

# Under a loaded runner the fake may log after the parent has gone; poll for
# each line rather than reading the log once.
wait_for() {
  for _ in $(seq 1 150); do grep -q "$1" "$LOG" && return 0; sleep 0.1; done
  return 1
}
wait_for_exit() { wait_for '^exited'; }

@test "keep-awake: the assertion starts with the parent and ends when it exits" {
  run run_parent
  [ "$status" -eq 0 ]
  pid="$(sed -n 's/^parent \([0-9]*\) .*/\1/p' <<<"$output")"
  [[ "$output" == *"marker=$pid"* ]]
  wait_for "^start -i -w $pid\$"
  wait_for_exit
}

@test "keep-awake: a live holder further up means no second assertion" {
  sleep 30 & holder=$!
  run run_parent GOAT_KEEP_AWAKE="$holder"
  kill "$holder"
  [ "$status" -eq 0 ]
  [ ! -s "$LOG" ]
}

@test "keep-awake: a stale holder pid does not suppress the assertion" {
  run run_parent GOAT_KEEP_AWAKE=999999
  [ "$status" -eq 0 ]
  wait_for '^start -i -w '
  wait_for_exit
}

@test "keep-awake: GOAT_NO_CAFFEINATE=1 opts out" {
  run run_parent GOAT_NO_CAFFEINATE=1
  [ "$status" -eq 0 ]
  [ ! -s "$LOG" ]
}

@test "keep-awake: off macOS nothing starts" {
  [ "$(uname -s)" = Darwin ] && skip "only meaningful off macOS"
  run env -u GOAT_KEEP_AWAKE PATH="$BIN:$PATH" bash -c '. "$1"; goat_keep_awake' _ "$HELPER"
  [ "$status" -eq 0 ]
  [ ! -s "$LOG" ]
}

@test "keep-awake: every long-running driver sources the helper" {
  for s in deploy-component rollback rc fastlane goat-standup goat-teardown peering-standup ansible-preflight; do
    grep -q 'lib/keep-awake.sh"$' "$REPO_ROOT/ops/scripts/$s.sh" && grep -q 'goat_keep_awake; fi$' "$REPO_ROOT/ops/scripts/$s.sh" || { echo "missing in $s.sh"; return 1; }
  done
}
