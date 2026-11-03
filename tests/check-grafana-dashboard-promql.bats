#!/usr/bin/env bats

# Tests for ops/ci/check-grafana-dashboard-promql.sh.

REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
SUBJECT="$REPO_ROOT/ops/ci/check-grafana-dashboard-promql.sh"

setup() {
  if [ ! -f "$SUBJECT" ]; then
    echo "subject script not found at $SUBJECT" >&2
    return 1
  fi

  for tool in git python3; do
    if ! command -v "$tool" >/dev/null 2>&1; then
      if [ -n "${CI:-}" ]; then
        echo "missing required tool: $tool (required in CI)" >&2
        return 1
      else
        skip "$tool not available"
      fi
    fi
  done
}

init_sandbox_repo() {
  sandbox="$BATS_TEST_TMPDIR/check-grafana-dashboard-promql-$BATS_TEST_NUMBER"
  mkdir -p "$sandbox"

  (
    cd "$sandbox" &&
      git init --quiet &&
      git config user.email "test@example.com" &&
      git config user.name "Test User"
  )
}

# Helper: drop a stub promtool on PATH so the guard gets past its
# `command -v promtool` early-exit and reaches the discovery logic.
stub_promtool() {
  mkdir -p "$sandbox/bin"
  cat >"$sandbox/bin/promtool" <<'EOF'
#!/usr/bin/env bash
echo "stub promtool: $*" >&2
exit 0
EOF
  chmod +x "$sandbox/bin/promtool"
  export PATH="$sandbox/bin:$PATH"
}

@test "zero discovered dashboards is a FAIL, not a silent pass" {
  # Fail-closed: a renamed or moved dashboard tree must not let the guard
  # print OK having parsed nothing. `find` complains on stderr and the
  # process substitution swallows its exit status, so the empty array is
  # the only signal available.
  init_sandbox_repo

  mkdir -p "$sandbox/ops/compose/goat-obs/grafana/dashboards"
  cp "$SUBJECT" "$sandbox/check-grafana-dashboard-promql.sh"
  stub_promtool

  cd "$sandbox"
  run "${BASH_UNDER_TEST:-/bin/bash}" -u ./check-grafana-dashboard-promql.sh

  [ "$status" -eq 1 ]
  [[ "$output" == *"found zero dashboards"* ]]
  [[ "$output" != *"dashboard PromQL check OK"* ]]
}

@test "a missing dashboard tree is a FAIL, not a silent pass" {
  init_sandbox_repo

  cp "$SUBJECT" "$sandbox/check-grafana-dashboard-promql.sh"
  stub_promtool

  cd "$sandbox"
  run "${BASH_UNDER_TEST:-/bin/bash}" -u ./check-grafana-dashboard-promql.sh

  [ "$status" -eq 1 ]
  [[ "$output" == *"found zero dashboards"* ]]
}

@test "a discovered dashboard is parsed and passes under /bin/bash -u" {
  init_sandbox_repo

  mkdir -p "$sandbox/ops/compose/goat-obs/grafana/dashboards"
  cat >"$sandbox/ops/compose/goat-obs/grafana/dashboards/demo.json" <<'JSON'
{
  "panels": [
    {
      "title": "demo panel",
      "targets": [
        { "refId": "A", "expr": "rate(goat_bytes_total[$__rate_interval])" }
      ]
    }
  ]
}
JSON

  cp "$SUBJECT" "$sandbox/check-grafana-dashboard-promql.sh"
  stub_promtool

  cd "$sandbox"
  run "${BASH_UNDER_TEST:-/bin/bash}" -u ./check-grafana-dashboard-promql.sh

  [ "$status" -eq 0 ]
  [[ "$output" == *"1 expressions"* ]]
  [[ "$output" == *"dashboard PromQL check OK"* ]]
}

@test "a pre-Grafana-5 rows[] dashboard contributes its expressions" {
  # named-processes.json stores panels under rows[], not panels[]. The walker
  # read nothing from it, so promtool was handed an empty rule file and passed
  # — the dashboard was exempt from the guard for as long as both existed.
  init_sandbox_repo

  mkdir -p "$sandbox/ops/compose/goat-obs/grafana/dashboards"
  cat >"$sandbox/ops/compose/goat-obs/grafana/dashboards/legacy.json" <<'JSON'
{
  "rows": [
    {
      "panels": [
        {
          "title": "legacy panel",
          "targets": [
            { "refId": "A", "expr": "rate(goat_bytes_total[$interval])" }
          ]
        }
      ]
    }
  ]
}
JSON

  cp "$SUBJECT" "$sandbox/check-grafana-dashboard-promql.sh"
  stub_promtool

  cd "$sandbox"
  run "${BASH_UNDER_TEST:-/bin/bash}" -u ./check-grafana-dashboard-promql.sh

  [ "$status" -eq 0 ]
  [[ "$output" == *"1 expressions"* ]]
}

@test "a variable in range position substitutes to a duration, not a label" {
  # [$interval] must become [5m]. Substituting the generic 'x' yields [x],
  # which is not a duration; promtool rejects it and the dashboard — which is
  # correct, because Grafana interpolates $interval at render time — fails the
  # guard. The stub below stands in for that rejection.
  init_sandbox_repo

  mkdir -p "$sandbox/ops/compose/goat-obs/grafana/dashboards"
  cat >"$sandbox/ops/compose/goat-obs/grafana/dashboards/ranged.json" <<'JSON'
{
  "panels": [
    {
      "title": "ranged panel",
      "targets": [
        { "refId": "A", "expr": "rate(goat_bytes_total[$interval])" }
      ]
    }
  ]
}
JSON

  cp "$SUBJECT" "$sandbox/check-grafana-dashboard-promql.sh"

  # A promtool stub that rejects a non-duration range the way the real one does.
  mkdir -p "$sandbox/bin"
  cat >"$sandbox/bin/promtool" <<'EOF'
#!/usr/bin/env bash
for f in "$@"; do
  [ -f "$f" ] || continue
  # Only the expr: lines are PromQL; the panel comments carry [refId].
  if grep -E '^\s*expr:' "$f" | grep -qE '\[[^0-9][^]]*\]'; then
    echo "bad number or duration syntax in $f" >&2
    exit 1
  fi
done
exit 0
EOF
  chmod +x "$sandbox/bin/promtool"
  export PATH="$sandbox/bin:$PATH"

  cd "$sandbox"
  run "${BASH_UNDER_TEST:-/bin/bash}" -u ./check-grafana-dashboard-promql.sh

  [ "$status" -eq 0 ]
  [[ "$output" != *"bad number or duration syntax"* ]]
}

@test "a dashboard yielding zero expressions is a FAIL" {
  # Not the same hole as zero DISCOVERED dashboards: this is a dashboard the
  # walker found and could read nothing from.
  init_sandbox_repo

  mkdir -p "$sandbox/ops/compose/goat-obs/grafana/dashboards"
  echo '{"title":"empty","panels":[]}' \
    >"$sandbox/ops/compose/goat-obs/grafana/dashboards/empty.json"
  mkdir -p "$sandbox/ops/pins"
  printf 'always_available:\n  - up\nsources: []\n' \
    >"$sandbox/ops/pins/obs-metric-sources.yaml"

  cp "$SUBJECT" "$sandbox/check-grafana-dashboard-promql.sh"
  stub_promtool

  cd "$sandbox"
  run "${BASH_UNDER_TEST:-/bin/bash}" -u ./check-grafana-dashboard-promql.sh

  [ "$status" -ne 0 ]
  [[ "$output" == *"ZERO expressions"* ]]
}

@test "a metric no source produces FAILS the join" {
  init_sandbox_repo

  mkdir -p "$sandbox/ops/compose/goat-obs/grafana/dashboards"
  cat >"$sandbox/ops/compose/goat-obs/grafana/dashboards/unknown.json" <<'JSON'
{
  "panels": [
    {
      "title": "unknown metric",
      "targets": [
        { "refId": "A", "expr": "sum(widget_frobnicator_total)" }
      ]
    }
  ]
}
JSON
  mkdir -p "$sandbox/ops/pins"
  printf 'always_available:\n  - up\nsources: []\n' \
    >"$sandbox/ops/pins/obs-metric-sources.yaml"

  cp "$SUBJECT" "$sandbox/check-grafana-dashboard-promql.sh"
  stub_promtool

  cd "$sandbox"
  run "${BASH_UNDER_TEST:-/bin/bash}" -u ./check-grafana-dashboard-promql.sh

  [ "$status" -ne 0 ]
  [[ "$output" == *"widget_frobnicator_total"* ]]
}

@test "a missing source inventory narrows the guard rather than failing it" {
  init_sandbox_repo

  mkdir -p "$sandbox/ops/compose/goat-obs/grafana/dashboards"
  cat >"$sandbox/ops/compose/goat-obs/grafana/dashboards/demo.json" <<'JSON'
{
  "panels": [
    {
      "title": "demo",
      "targets": [
        { "refId": "A", "expr": "sum(widget_frobnicator_total)" }
      ]
    }
  ]
}
JSON

  cp "$SUBJECT" "$sandbox/check-grafana-dashboard-promql.sh"
  stub_promtool

  cd "$sandbox"
  run "${BASH_UNDER_TEST:-/bin/bash}" -u ./check-grafana-dashboard-promql.sh

  [ "$status" -eq 0 ]
  [[ "$output" == *"metric-name join skipped"* ]]
}
