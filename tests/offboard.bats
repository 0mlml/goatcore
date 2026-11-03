#!/usr/bin/env bats

@test "offboard driver fails closed and persists receipts" {
  run python3 ops/scripts/test_offboard_driver.py
  [ "$status" -eq 0 ]
}
