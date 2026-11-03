#!/usr/bin/env bats

# The shellcheck sweep has to cover the tracked ops shell tree, not a hand list.
#
# #486: `make lint-shell` shellchecked six hand-picked files, so every other
# tracked script under ops/ merged with no shellcheck pass at all, the same
# claimed-scope-versus-actual-scope gap #227 closed for the bash 3.2 gate. The
# sweep derives its list from git and runs at warning severity; these cases pin
# that it goes red on an error- or warning-level finding and that the tree is clean.

REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
SWEEP="$REPO_ROOT/ops/ci/check-shellcheck-sweep.sh"

setup() {
  if ! command -v shellcheck >/dev/null 2>&1; then
    if [ -n "${CI:-}" ]; then
      echo "missing required lint tool: shellcheck (required in CI)" >&2
      return 1
    fi
    skip "shellcheck not installed"
  fi
}

@test "shellcheck sweep: an error-level finding in a named file is refused" {
  bad="$BATS_TEST_TMPDIR/bad.sh"
  # SC2066: a double-quoted list makes the loop run once.
  printf '#!/usr/bin/env bash\nfor f in "$(ls)"; do echo "$f"; done\n' > "$bad"
  run "$SWEEP" "$bad"
  [ "$status" -eq 1 ]
  [[ "$output" == *"SC2066"* ]]
}

@test "shellcheck sweep: a warning-level finding is refused" {
  warn="$BATS_TEST_TMPDIR/warn.sh"
  # SC2034 (unused variable) must fail the warning tier.
  printf '#!/usr/bin/env bash\nunused=1\necho ok\n' > "$warn"
  run "$SWEEP" "$warn"
  [ "$status" -eq 1 ]
  [[ "$output" == *"SC2034"* ]]
}

@test "shellcheck sweep: an explicitly named file that does not exist is an error, not a pass" {
  run "$SWEEP" "$BATS_TEST_TMPDIR/no-such-script.sh"
  [ "$status" -eq 2 ]
}

@test "shellcheck sweep: the tracked ops shell tree is clean at warning severity" {
  cd "$REPO_ROOT"
  run "$SWEEP"
  [ "$status" -eq 0 ]
  [[ "$output" == *"ok:"* ]]
}
