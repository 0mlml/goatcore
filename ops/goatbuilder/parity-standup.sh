#!/usr/bin/env bash
# parity-standup.sh — the parity gate for `goatbuilder standup` against
# ops/scripts/goat-standup.sh (goatbuilder-conformance.md §The parity gate).
#
# Both sides plan the same site — the script with --plan, the binary with
# --dry-run — and every phase header, every command, every obs-leg line and
# every rescue-class skip is compared in order after normalisation: the
# repository path and the home directory are masked, runs of whitespace are
# collapsed (the script's multi-line command strings fold their continuation
# indents into the command), and the resume instruction each side prints in
# its manual steps is reduced to its phase number (the script names itself,
# the binary names itself). Banners and the plan footer are not compared.
#
# usage: parity-standup.sh <goatbuilder binary> <env>:<site> [<env>:<site> ...]
#        e.g. parity-standup.sh crates/target/debug/goatbuilder <env>:<site>
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"; REPO="$(cd "$HERE/../.." && pwd)"
BIN="${1:?}"; shift
[ $# -gt 0 ] || { echo "usage: parity-standup.sh <binary> <env>:<site> ..."; exit 2; }

norm() {
  sed -E 's#\x1b\[[0-9;]*m##g' \
  | sed -E 's#'"$REPO"'#REPO#g; s#'"$HOME"'#HOME#g' \
  | sed -E 's#ops/scripts/goat-standup\.sh [a-z0-9-]+ --from#RESUME --from#g; s#goatbuilder standup [a-z0-9_]+ --site [a-z0-9-]+ --from#RESUME --from#g' \
  | sed -E 's#ops/scripts/goat-standup\.sh [a-z0-9-]+ --obs-only#RESUME --obs-only#g' \
  | grep -E '^(=== |    )' \
  | sed -E 's/^[[:space:]]+//; s/[[:space:]]+$//; s/[[:space:]]+/ /g'
}
script_side() { "$REPO/ops/scripts/goat-standup.sh" "$1" --plan 2>&1 | norm; }
binary_side() {
  # The rows from -o json: the human table prints phase rows as headings.
  "$BIN" -C "$REPO" -o json standup "$1" --site "$2" --dry-run 2>/dev/null \
  | python3 -c 'import json,sys
for c in json.load(sys.stdin)["checks"]:
    if c["name"] in ("phase", "leg"): print(c["detail"])
    elif c["name"] in ("cmd", "note", "log"): print("    " + c["detail"])' \
  | norm
}

fail=0
for pair in "$@"; do
  env="${pair%%:*}"; site="${pair#*:}"
  a="$(script_side "$site" || true)"; b="$(binary_side "$env" "$site" || true)"
  if [ -z "$a" ]; then
    fail=1; echo "PARITY FAIL: standup $site — the script produced no phases (run it by hand with --plan to see why)"; continue
  fi
  if [ "$a" != "$b" ]; then
    fail=1; echo "PARITY FAIL: standup $env:$site"
    { diff <(printf '%s\n' "$a") <(printf '%s\n' "$b") || true; } | sed 's/^/    /' | cut -c1-220 | head -40
  else
    echo "parity ok: standup $env:$site --dry-run ($(printf '%s\n' "$a" | grep -c '^=== ') phases, $(printf '%s\n' "$a" | grep -c .) lines identical)"
  fi
done
exit $fail
