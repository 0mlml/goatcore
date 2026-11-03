#!/usr/bin/env bats

# A live row that could not run must read UNTESTED, never FAIL and never PASS.
#
# the zagros rows ran on a management host that held no goat
# contexts. `goat admin` answers a missing context with E_NOT_ADMIN, exit 4, so
# onboard-intake-smoke.sh exited 4 and two rows read FAIL on a healthy
# candidate. Graded UNKNOWN instead, the same rows would have let the runner
# exit 0 ("GATE: PASS ... 2 UNKNOWN") and the release would have promoted on
# checks that never ran. The smoke now exits 2 for missing setup, and the
# release drivers pass --require-pass so the runner exits 2 (untested) unless
# every selected row reached PASS.

REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"

setup() {
  TMP="$(mktemp -d)"
  mkdir -p "$TMP/bin"
}

teardown() {
  rm -rf "$TMP"
}

manifest() {
  python3 - "$TMP/manifest.yaml" "$@" <<'PY'
import sys, yaml
rows = {
    "pass": {"id": "pass", "probe": "cmd-assert", "params": {"cmd": "true"}},
    "unknown": {"id": "unknown", "probe": "cmd-assert", "params": {"cmd": "exit 2", "unknown_rc": 2}},
    "fail": {"id": "fail", "probe": "cmd-assert", "params": {"cmd": "exit 4"}},
}
yaml.safe_dump({"tests": [rows[r] for r in sys.argv[2:]]}, open(sys.argv[1], "w"))
PY
}

runner() {
  python3 "$REPO_ROOT/ops/validation/run-acceptance.py" --env bats --manifest "$TMP/manifest.yaml" --json "$TMP/out.jsonl" "$@"
}

@test "runner: a row that did not run still exits 0 without --require-pass (other callers unchanged)" {
  manifest pass unknown
  run runner
  [ "$status" -eq 0 ]
  [[ "$output" == *"GATE: PASS"*"1 UNKNOWN"* ]]
}

@test "runner: --require-pass exits 2 (untested) when a row did not run" {
  manifest pass unknown
  run runner --require-pass
  [ "$status" -eq 2 ]
  [[ "$output" == *"GATE: UNTESTED"*"1 PASS, 1 row(s) did not run"* ]]
}

@test "runner: --require-pass keeps a real failure a failure" {
  manifest pass unknown fail
  run runner --require-pass
  [ "$status" -eq 1 ]
  [[ "$output" == *"GATE: FAIL"* ]]
}

@test "runner: --require-pass exits 0 when every row passed" {
  manifest pass
  run runner --require-pass
  [ "$status" -eq 0 ]
  [[ "$output" == *"GATE: PASS"* ]]
}

@test "smoke: a missing goat context exits 2, before any invite is minted" {
  # A goat whose contexts are absent: profile show fails as the real CLI does
  # (exit 3), and any admin call would answer E_NOT_ADMIN (exit 4).
  cat > "$TMP/bin/goat" <<'SH'
#!/bin/sh
echo "$*" >> "$GOAT_CALLS"
case "$*" in
  *"profile show"*) echo "error: no profile for context" >&2; exit 3 ;;
  *) echo "error: operator-only action" >&2; echo "  code: E_NOT_ADMIN" >&2; exit 4 ;;
esac
SH
  chmod +x "$TMP/bin/goat"
  export GOAT_CALLS="$TMP/calls"
  run env PATH="$TMP/bin:$PATH" "${BASH_UNDER_TEST:-bash}" "$REPO_ROOT/ops/scripts/onboard-intake-smoke.sh" --context absent --env bats
  [ "$status" -eq 2 ] || { printf "status=%s output=%s\n" "$status" "$output" >&3; return 1; }
  [[ "$output" == *"goat context 'absent' is not initialized on this machine"* ]]
  run grep -c "admin" "$TMP/calls"
  [ "$output" = "0" ]
}

@test "smoke: sponsor withdrawal capability is checked with only the sponsor context" {
  mkdir -p "$TMP/repo/ops/scripts" "$TMP/repo/ops/goat-id-console/web/e2e/node_modules"
  cp "$REPO_ROOT/ops/scripts/onboard-intake-smoke.sh" "$TMP/repo/ops/scripts/"
  cat > "$TMP/bin/goat" <<'SH'
#!/bin/sh
echo "$*" >> "$GOAT_CALLS"
case "$*" in
  *"--context sponsor profile show"*) exit 0 ;;
  *"admin invite"*) echo "fixture: invite refused" >&2; exit 4 ;;
  *) exit 99 ;;
esac
SH
  cat > "$TMP/bin/sh" <<'SH'
#!/bin/sh
printf '%s' "$FIXTURE_CAPS"
SH
  chmod +x "$TMP/bin/goat" "$TMP/bin/sh"
  export GOAT_CALLS="$TMP/calls"
  export FIXTURE_CAPS='{"sponsor_withdraw":false}'
  run env PATH="$TMP/bin:$PATH" "${BASH_UNDER_TEST:-bash}" "$TMP/repo/ops/scripts/onboard-intake-smoke.sh" --context sponsor --env bats --stage sponsor
  [ "$status" -eq 2 ]
  [[ "$output" == *"predates ADR 1207 sponsor withdrawal"* ]]
  run grep -c "admin" "$TMP/calls"
  [ "$output" = "0" ]

  export FIXTURE_CAPS='{"sponsor_withdraw":true}'
  run env PATH="$TMP/bin:$PATH" "${BASH_UNDER_TEST:-bash}" "$TMP/repo/ops/scripts/onboard-intake-smoke.sh" --context sponsor --env bats --stage sponsor
  [ "$status" -eq 4 ]
  [[ "$output" == *"fixture: invite refused"* ]]
}

@test "release drivers pass --require-pass to the runner (both avenues, ADR 1201)" {
  run grep -c -- '--require-pass' "$REPO_ROOT/ops/scripts/rc.sh"
  [ "$output" -ge 1 ]
  run grep -c -- '"--require-pass"' "$REPO_ROOT/crates/goatbuilder/src/release.rs"
  [ "$output" -ge 1 ]
}

@test "smoke: unreachable remote context is untested before any invite" {
  cat > "$TMP/bin/ssh" <<'SH'
#!/bin/sh
exit 255
SH
  chmod +x "$TMP/bin/ssh"
  run env PATH="$TMP/bin:$PATH" "${BASH_UNDER_TEST:-bash}" "$REPO_ROOT/ops/scripts/onboard-intake-smoke.sh" --context absent --env bats --goat-via fixture@example.invalid
  [ "$status" -eq 2 ]
  [[ "$output" == *"could not reach fixture@example.invalid over ssh"* ]]
}

@test "smoke: a real invite refusal still exits with its failure status" {
  mkdir -p "$TMP/repo/ops/scripts" "$TMP/repo/ops/goat-id-console/web/e2e/node_modules"
  cp "$REPO_ROOT/ops/scripts/onboard-intake-smoke.sh" "$TMP/repo/ops/scripts/"
  cat > "$TMP/bin/goat" <<'SH'
#!/bin/sh
case "$*" in
  *"profile show"*) exit 0 ;;
  *"admin invite"*) echo "fixture: invite refused" >&2; exit 4 ;;
  *) exit 99 ;;
esac
SH
  chmod +x "$TMP/bin/goat"
  run env PATH="$TMP/bin:$PATH" "${BASH_UNDER_TEST:-bash}" "$TMP/repo/ops/scripts/onboard-intake-smoke.sh" --context present --env bats
  [ "$status" -eq 4 ]
  [[ "$output" == *"fixture: invite refused"* ]]
  [[ "$output" != *"smoke: PASS"* ]]
}
