#!/usr/bin/env bats

@test "setup-key helper: actual CLI payload and failure contracts" {
  repo="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  run python3 "$repo/tests/test_create_setup_key.py"
  [ "$status" -eq 0 ]
  [[ "$output" == *"OK"* ]]
}
