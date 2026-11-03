#!/usr/bin/env bats
# Behavior tests for ops/scripts/pr-readiness.sh — the two checks added for
# goat issue #988 follow-up 1 that the older ops/scripts/pr-readiness_test.sh
# harness does not cover:
#
#   - a Signed-off-by trailer that is PRESENT but does not match the
#     commit's own author. Before this fix, pr-readiness.sh only checked for
#     the trailer's presence, so this case passed locally and was only
#     caught post-push by pr-hygiene.yml's "Developer Certificate of
#     Origin" step.
#   - an agent-tooling / AI-attribution reference in a commit message
#     (claude.ai, Claude-Session:, anthropic.com). Before this fix,
#     pr-readiness.sh did not check commit messages for this at all; it was
#     only caught post-push by agent-references.yml's "Check the commit
#     messages too" step, by which point the message is permanent on a
#     squash-merged main.
#
# Each sandbox repo disables commit.gpgsign locally so a throwaway fixture
# commit never attempts to sign with the operator's real configured key;
# that is orthogonal to what these tests assert (commit MESSAGE content),
# and pr-readiness.sh's own signature check (Check 6) is covered by
# ops/scripts/pr-readiness_test.sh, not here.

REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
SCRIPT="$REPO_ROOT/ops/scripts/pr-readiness.sh"

setup() {
  if ! command -v git >/dev/null; then
    skip "git not available"
  fi
  if ! command -v python3 >/dev/null; then
    skip "python3 not available"
  fi
  if [ ! -x "$SCRIPT" ]; then
    skip "pr-readiness.sh not present at $SCRIPT"
  fi
}

# Helper: bare origin + clone with one seeded, signed-off commit on main, and
# a `gh` stub reporting no open PR (keeps the PR-dependent checks neutral so
# each test exercises only the commit-message check under test).
make_fixture() {
  local base="$BATS_TEST_TMPDIR/fixture-$$-$BATS_TEST_NUMBER"
  local origin="$base/origin.git"
  local clone="$base/clone"
  git init -q --bare -b main "$origin"
  git clone -q "$origin" "$clone"
  (
    cd "$clone" || exit 1
    git config user.email "test@example.com"
    git config user.name "Test User"
    git config commit.gpgsign false
    mkdir -p .git/test-bin
    cat > .git/test-bin/gh <<'GH'
#!/usr/bin/env bash
if [ "$1 $2" = "pr list" ]; then printf '[]\n'; else exit 1; fi
GH
    chmod +x .git/test-bin/gh
    echo seed > README.md
    git add README.md
    git commit -sq -m "seed"
    git push -q origin main
  ) >/dev/null
  echo "$clone"
}

# Helper: run pr-readiness.sh from inside the fixture clone, with its `gh`
# stub ahead of the real PATH (so gpg and everything else stay reachable —
# unlike pr-readiness_test.sh's PATH override, which excludes them).
run_readiness() {
  local clone="$1"
  ( cd "$clone" && PATH="$clone/.git/test-bin:$PATH" "$SCRIPT" )
}

@test "pr-readiness: Signed-off-by trailer present but not matching the commit author is flagged" {
  local clone
  clone="$(make_fixture)"
  (
    cd "$clone" || exit 1
    git checkout -qb fix/mismatched-signoff main
    echo one > one.txt
    git add one.txt
    git -c user.name="Real Author" -c user.email="real@example.com" \
      commit -q -m "fix: one

Signed-off-by: Someone Else <someone-else@example.com>"
  )
  run run_readiness "$clone"
  [ "$status" -eq 1 ]
  [[ "$output" == *"Signed-off-by trailer does not match the commit author"* ]]
}

@test "pr-readiness: an AI-attribution trailer in a commit message is flagged" {
  local clone
  clone="$(make_fixture)"
  (
    cd "$clone" || exit 1
    git checkout -qb fix/ai-attribution main
    echo two > two.txt
    git add two.txt
    git commit -sq -m "fix: two

Co-Authored-By: Claude <noreply@anthropic.com>" # oss-allow: fixture the check must catch
  )
  run run_readiness "$clone"
  [ "$status" -eq 1 ]
  [[ "$output" == *"agent-tooling / AI-attribution reference"* ]]
}
