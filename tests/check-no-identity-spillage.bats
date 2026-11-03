#!/usr/bin/env bats
# Behavior tests for ops/ci/check-no-identity-spillage.sh.
#
# These guard the three array-usage paths in the shell script:
#   - tree mode over tracked files,
#   - staged mode over an empty index,
#   - explicit path args where every candidate is filtered out.
#
# The tests prove that on the runner's Bash the guard exits 0 and
# prints the success line even when the candidates/filtered arrays
# are empty. They do NOT assert Bash 3.2 compatibility — that is
# provided by the static portability gate in ops/ci/check-bash-portability.sh.

REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
GUARD_SH="$REPO_ROOT/ops/ci/check-no-identity-spillage.sh"

setup() {
  if ! command -v git >/dev/null; then
    skip "git not available"
  fi
  if ! command -v file >/dev/null; then
    skip "file(1) not available"
  fi
  if [ ! -x "$GUARD_SH" ]; then
    skip "identity-spillage guard not present at $GUARD_SH"
  fi
}

# Helper: initialize a sandbox repo under $BATS_TEST_TMPDIR.
init_sandbox_repo() {
  local dir
  dir="$BATS_TEST_TMPDIR/sandbox-$$"
  mkdir -p "$dir"
  git init -q "$dir"
  git -C "$dir" config user.email "test@example.com"
  git -C "$dir" config user.name "test-user"
  echo "$dir"
}

# ── tree mode: benign tracked file ─────────────────────

@test "identity-spillage: tree mode with one benign tracked file passes" {
  local repo safe
  repo="$(init_sandbox_repo)"
  safe="$repo/safe.txt"
  printf 'nothing to see here\n' > "$safe"
  git -C "$repo" add safe.txt

  cd "$repo"
  run bash "$GUARD_SH"
  [ "$status" -eq 0 ]
  [[ "$output" == *"ok: no operator-identity material detected in scanned files"* ]]
}

# ── staged mode: zero candidates (empty index) ─────────

@test "identity-spillage: staged mode with empty index passes" {
  local repo
  repo="$(init_sandbox_repo)"
  cd "$repo"

  run bash "$GUARD_SH" --staged
  [ "$status" -eq 0 ]
  [[ "$output" == *"ok: no operator-identity material detected in scanned files"* ]]
}

# ── explicit paths: all candidates filtered ────────────

@test "identity-spillage: explicit missing paths are treated as empty candidates and pass" {
  local repo
  repo="$(init_sandbox_repo)"
  cd "$repo"

  run bash "$GUARD_SH" missing-one.txt missing-two.bin
  [ "$status" -eq 0 ]
  [[ "$output" == *"ok: no operator-identity material detected in scanned files"* ]]
}

