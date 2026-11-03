#!/usr/bin/env bats

setup() {
  REPO="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
}

@test "release command wrapper preserves arguments and adds dry-run only when requested" {
  sed -n '/^run_with_dry_flag()/,/^}/p' "$REPO/ops/scripts/rc.sh" > "$BATS_TEST_TMPDIR/function.sh"
  run "${BASH_UNDER_TEST:-/bin/bash}" -c '
    source "$1"
    run() { printf "<%s>\n" "$@"; }
    DRY=0
    run_with_dry_flag "command with spaces" --goatnet "site one"
    DRY=1
    run_with_dry_flag "command with spaces" --goatnet "site one"
  ' fixture "$BATS_TEST_TMPDIR/function.sh"
  [ "$status" -eq 0 ]
  [ "$output" = $'<command with spaces>\n<--goatnet>\n<site one>\n<command with spaces>\n<--goatnet>\n<site one>\n<--dry-run>' ]
}

@test "fastlane deployment helper preserves site and component arguments in both modes" {
  mkdir -p "$BATS_TEST_TMPDIR/repo/ops/scripts"
  cat > "$BATS_TEST_TMPDIR/repo/ops/scripts/deploy-component.sh" <<'SH'
#!/usr/bin/env bash
printf '<%s>\n' "$@"
SH
  chmod +x "$BATS_TEST_TMPDIR/repo/ops/scripts/deploy-component.sh"
  sed -n '/^deploy_goatnet()/,/^}/p' "$REPO/ops/scripts/fastlane.sh" > "$BATS_TEST_TMPDIR/function.sh"
  run "${BASH_UNDER_TEST:-/bin/bash}" -c '
    source "$1"
    REPO="$2" COMPONENT="component one" DRY=0
    deploy_goatnet "site one"
    DRY=1
    deploy_goatnet "site one"
  ' fixture "$BATS_TEST_TMPDIR/function.sh" "$BATS_TEST_TMPDIR/repo"
  [ "$status" -eq 0 ]
  [ "$output" = $'<component one>\n<--goatnet>\n<site one>\n<component one>\n<--goatnet>\n<site one>\n<--dry-run>' ]
}

@test "smoke measurement refuses a failed clock before running its payload" {
  sed -n '/^measure()/,/^}/p' "$REPO/ops/smoke-test.sh" > "$BATS_TEST_TMPDIR/function.sh"
  run "${BASH_UNDER_TEST:-/bin/bash}" -c '
    source "$1"
    python3() { return 1; }
    measure "printf should-not-run"
  ' fixture "$BATS_TEST_TMPDIR/function.sh"
  [ "$status" -eq 1 ]
  [ -z "$output" ]
}

@test "release runner lookup retains its settings path and passes quoted names as data" {
  sed -n '/^runner_var()/,/^}/p' "$REPO/ops/scripts/rc.sh" > "$BATS_TEST_TMPDIR/function.sh"
  cat > "$BATS_TEST_TMPDIR/runner's settings.yaml" <<'YAML'
goatnets:
  "site'one":
    name: runner one
YAML
  run "${BASH_UNDER_TEST:-/bin/bash}" -c '
    set -eu
    source "$1"
    RUNNERS="$2"
    runner_var "$3" name
  ' fixture "$BATS_TEST_TMPDIR/function.sh" "$BATS_TEST_TMPDIR/runner's settings.yaml" "site'one"
  [ "$status" -eq 0 ]
  [ "$output" = "runner one" ]
}

@test "smoke measurement records a payload failure when the clock works" {
  sed -n '/^measure()/,/^}/p' "$REPO/ops/smoke-test.sh" > "$BATS_TEST_TMPDIR/function.sh"
  run "${BASH_UNDER_TEST:-/bin/bash}" -c '
    source "$1"
    python3() { printf "123\n"; }
    set -e
    result=$(measure "false")
    printf "%s\n" "$result"
  ' fixture "$BATS_TEST_TMPDIR/function.sh"
  [ "$status" -eq 0 ]
  [ "$output" = "1:0" ]
}
