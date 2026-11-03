#!/usr/bin/env bats

@test "Zenoh Prometheus rendering never executes glob text from Python comments" {
  repo="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  fixture="$BATS_TEST_TMPDIR/prom"
  mkdir -p "$fixture/child"
  export UNEXPECTED_COMMAND_LOG="$fixture/commands"
  : > "$UNEXPECTED_COMMAND_LOG"
  # In an unquoted shell heredoc, Markdown backticks in a Python comment are
  # command substitutions. Make both wildcard spellings resolve to harmless
  # executable fixtures so this catches execution rather than hiding stderr.
  for command in probe-unexpected-command child/subcommand expected; do
    cat > "$fixture/$command" <<'SH'
#!/usr/bin/env bash
printf 'unexpected command\n' >> "$UNEXPECTED_COMMAND_LOG"
SH
    chmod +x "$fixture/$command"
  done
  python3 - "$repo/ops/relay/health/zenoh-health-probe.sh" "$fixture/render.sh" <<'PY'
import pathlib, sys
source = pathlib.Path(sys.argv[1]).read_text()
start = source.index('  python3 - "$ZENOH_PROM_OUT" "$NAME" "$now" "$delivery" <<PY')
end = source.index('\nPY\n', start) + len('\nPY\n')
pathlib.Path(sys.argv[2]).write_text(source[start:end])
PY
  export PATH="$fixture:$PATH"
  export ZENOH_PROM_OUT="$fixture/metrics.prom" NAME=test-router now=12345
  export delivery='{"fabric_wide_subscribers":["one"],"fabric_wide_shapes":["*/**"]}'
  export ZENOH_FABRIC_WIDE_EXPECTED=1
  cd "$fixture"
  run "${BASH_UNDER_TEST:-/bin/bash}" "$fixture/render.sh"
  [ "$status" -eq 0 ]
  [ ! -s "$UNEXPECTED_COMMAND_LOG" ]
  grep -q '^goat_zenoh_fabric_wide_subscribers{router="test-router"} 1$' "$ZENOH_PROM_OUT"
  grep -q '^goat_zenoh_fabric_wide_expected{router="test-router"} 1$' "$ZENOH_PROM_OUT"
  grep -q '^goat_zenoh_prom_last_run_timestamp_seconds{router="test-router"} 12345$' "$ZENOH_PROM_OUT"
}
