#!/usr/bin/env bats

# The console's offboard-eligible set and goat-cli's copy must be one set.
#
# #579: the Go set decides which rows the console will offboard, and goat-cli
# carries a Rust copy because it ships apart from the console. Comments on both
# sides named each other and nothing enforced them. This guard does.

REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
GUARD="$REPO_ROOT/ops/ci/check-offboardable-status-parity.py"

@test "offboardable parity: the guard proves red, then green, on its own fixtures" {
  run python3 "$GUARD" --self-test
  [ "$status" -eq 0 ]
}

@test "offboardable parity: the console set and the goat-cli constant match on the tree" {
  run python3 "$GUARD"
  [ "$status" -eq 0 ]
  [[ "$output" == *"archived"* ]]
}

@test "offboardable parity: a state the CLI lacks is refused, and the message says which side" {
  go="$BATS_TEST_TMPDIR/proposals.go"
  rs="$BATS_TEST_TMPDIR/admin.rs"
  cp "$REPO_ROOT/ops/goat-id-console/internal/onboardstore/proposals.go" "$go"
  printf 'const CONSOLE_OFFBOARDABLE_STATUSES: [&str; 2] = ["onboarded", "enrolled"];\n' > "$rs"
  run python3 "$GUARD" "$go" "$rs"
  [ "$status" -eq 1 ]
  [[ "$output" == *"console only"* ]]
  [[ "$output" == *"archived"* ]]
}
