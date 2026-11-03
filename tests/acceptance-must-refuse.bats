#!/usr/bin/env bats

# The acceptance runner's must_refuse inversion has to produce a verdict.
#
# Until #1130 every must_refuse row crashed the runner with an AttributeError
# the moment its probe answered PASS or FAIL, and --self-test never drove the
# inversion, so nothing on CI executed that block. The self-test now runs a
# fixture manifest through run() itself; this suite puts it on every CI run.

REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"

@test "acceptance runner: --self-test proves the must_refuse inversion" {
  run python3 "$REPO_ROOT/ops/validation/run-acceptance.py" --self-test
  [ "$status" -eq 0 ]
  [[ "$output" == *"ok   must_refuse refused: got PASS, want PASS"* ]]
  [[ "$output" == *"ok   must_refuse allowed: got FAIL, want FAIL"* ]]
  [[ "$output" == *"ok   must_refuse cannot-tell: got UNKNOWN, want UNKNOWN"* ]]
}

@test "acceptance runner: the inversion reads the verdict's evidence field" {
  run grep -n 'v\.detail' "$REPO_ROOT/ops/validation/run-acceptance.py"
  [ "$status" -eq 1 ]
}
