#!/usr/bin/env bats
# The hold-record fixture must always validate against its schema — the
# console issues build against it (#1041, #1042, #1044) and the sweep writes
# it (#1040). ops/lifecycle/README.md.

REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
V="$REPO_ROOT/ops/lifecycle/validate-hold-records.py"

@test "hold-record fixture validates" {
  run python3 "$V" "$REPO_ROOT/ops/lifecycle/fixtures/hold-records.jsonl"
  [ "$status" -eq 0 ]
}

@test "a dormancy record pointing at QUARANTINED is refused" {
  bad="$BATS_TEST_TMPDIR/bad.jsonl"
  sed 's/ZAGROS-PROBATION-DEVELOPER/ZAGROS-QUARANTINED/' \
    "$REPO_ROOT/ops/lifecycle/fixtures/hold-records.jsonl" > "$bad"
  run python3 "$V" "$bad"
  [ "$status" -eq 1 ]
  [[ "$output" == *"hold_group"* ]]
}

@test "the same peer twice is refused" {
  dup="$BATS_TEST_TMPDIR/dup.jsonl"
  head -1 "$REPO_ROOT/ops/lifecycle/fixtures/hold-records.jsonl" > "$dup"
  head -1 "$REPO_ROOT/ops/lifecycle/fixtures/hold-records.jsonl" >> "$dup"
  run python3 "$V" "$dup"
  [ "$status" -eq 1 ]
  [[ "$output" == *"already recorded"* ]]
}

@test "a hyphenated goatnet id (zagros-prod) validates in both lanes" {
  hy="$BATS_TEST_TMPDIR/hy.jsonl"
  sed -e 's/"site":"zagros"/"site":"zagros-prod"/' -e 's/"ZAGROS-/"ZAGROS-PROD-/g' \
    "$REPO_ROOT/ops/lifecycle/fixtures/hold-records.jsonl" > "$hy"
  run python3 "$V" "$hy"
  [ "$status" -eq 0 ]
}

@test "a hold_group whose prefix is not the site is refused" {
  bad="$BATS_TEST_TMPDIR/prefix.jsonl"
  sed 's/"site":"zagros"/"site":"zagros-prod"/' \
    "$REPO_ROOT/ops/lifecycle/fixtures/hold-records.jsonl" > "$bad"
  run python3 "$V" "$bad"
  [ "$status" -eq 1 ]
  [[ "$output" == *"site prefix"* ]]
}
