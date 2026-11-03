#!/usr/bin/env bats

@test "PR checks distinguish an unpushed repair from its failed ancestor" {
  run python3 "$BATS_TEST_DIRNAME/../ops/ci/test_pr_head_checks.py"
  [ "$status" -eq 0 ]
}
