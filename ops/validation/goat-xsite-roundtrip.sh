#!/usr/bin/env bash
# goat-xsite-roundtrip.sh — prove a sample published through one site's router
# reaches a subscriber on another site's router, with the goat CLI only.
#
#   goat-xsite-roundtrip.sh <publisher-context> <subscriber-context>
#
# The subscriber listens on `hello/**` through its own context; the publisher
# runs `goat hello`, which puts `lo` on `hello/<its context>` through its own
# router. Nothing is stored. Prints `xsite: PASS <key>` when the subscriber saw
# the publisher's key, `xsite: FAIL …` otherwise; exit 0 only on PASS.
# Used by the acceptance assay's tutorial-2 rows (ops/validation/acceptance-tests.yaml).
set -euo pipefail

[ $# -eq 2 ] || { echo "usage: goat-xsite-roundtrip.sh <publisher-context> <subscriber-context>" >&2; exit 2; }
pub_ctx="$1"
sub_ctx="$2"
export NO_COLOR=1 GOAT_NO_FUN=1 RUST_LOG=error

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

goat --context "$sub_ctx" sub 'hello/**' --keys-only --take 1 --for 40s >"$work/sub.out" 2>"$work/sub.err" &
sub_pid=$!
# The subscriber must be declared before the one-shot put, or it misses it.
sleep 4
goat --context "$pub_ctx" hello --json >"$work/pub.out" 2>"$work/pub.err" || {
  echo "xsite: FAIL publisher ($pub_ctx) could not run goat hello: $(tail -n 1 "$work/pub.err")"
  kill "$sub_pid" 2>/dev/null || true
  exit 1
}
wait "$sub_pid" || true

topic="$(sed -n 's/.*"topic":"\([^"]*\)".*/\1/p' "$work/pub.out")"
if [ -n "$topic" ] && grep -qF "$topic" "$work/sub.out"; then
  echo "xsite: PASS $topic published via $pub_ctx, received via $sub_ctx"
  exit 0
fi
echo "xsite: FAIL $sub_ctx did not receive ${topic:-the publisher key} within 40s ($(tail -n 1 "$work/sub.err" 2>/dev/null))"
exit 1
