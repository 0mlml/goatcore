#!/usr/bin/env bash
# parity-endstate.sh — both avenues grade a goatnet's end state the same (ADR 1201, ADR 1206).
#
# For every end-state fixture, run `goatbuilder endstate` and
# ops/scripts/goatnet-endstate.sh on the fixture's declarations, facts and assay
# rows, and compare every printed line and the exit code. The binary prints the
# script's lines and then its own one-line verdict, so the script's output must
# be the binary's, line for line, up to that verdict. Neither reads a host.
#
#   ops/goatbuilder/parity-endstate.sh [path/to/goatbuilder]
set -euo pipefail
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
GB="${1:-${GOATBUILDER:-$REPO/crates/target/debug/goatbuilder}}"
[ -x "$GB" ] || { echo "parity-endstate: no goatbuilder binary at $GB (cargo build -p goatbuilder)" >&2; exit 2; }
fail=0; n=0
for case in "$REPO"/ops/goatbuilder/fixtures/endstate/*/; do
  site="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["site"])' "$case/expected.json")"
  args=(--facts "$case/facts.json" --assay-rows "$case/assay.jsonl" --root "$case")
  set +e
  cli="$("$GB" endstate "$site" "${args[@]}" 2>/dev/null)"; cli_rc=$?
  scr="$("$REPO/ops/scripts/goatnet-endstate.sh" "$site" "${args[@]}" 2>/dev/null)"; scr_rc=$?
  set -e
  n=$((n + 1))
  lines="$(printf '%s\n' "$scr" | wc -l | tr -d ' ')"
  head_cli="$(printf '%s\n' "$cli" | head -n "$lines")"
  if [ "$head_cli" != "$scr" ] || [ "$cli_rc" != "$scr_rc" ]; then
    echo "FAIL $(basename "$case"): goatbuilder exit $cli_rc, script exit $scr_rc"
    diff <(printf '%s\n' "$head_cli") <(printf '%s\n' "$scr") | head -20 || true
    fail=1
  else
    echo "ok   $(basename "$case"): $lines lines identical, exit $cli_rc"
  fi
done
[ "$n" -gt 0 ] || { echo "parity-endstate: no fixtures" >&2; exit 2; }
exit "$fail"
