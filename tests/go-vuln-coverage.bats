#!/usr/bin/env bats

@test "Go vulnerability coverage rejects unclassified modules and ineffective scans" {
  run python3 "$BATS_TEST_DIRNAME/../ops/ci/test_go_vuln_coverage.py"
  [ "$status" -eq 0 ]
}
