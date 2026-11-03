#!/usr/bin/env bash
# Unit-style tests for the wg-cp0 conf-change classifier. Runs the
# files/wg-cp0-classify-conf-change.sh script against fixture confs and
# asserts the expected classification.
#
# This covers the substrate-side change in Block 22R.3:
# the classifier picks between `reload wg-cp0 peers` (wg syncconf) and
# `restart wg-cp0` based on whether the [Interface] stanza changed.
#
# A complementary bench-env test that exercises wg syncconf preserves
# handshakes on a live interface is scaffolded at the wg-cp0-mesh-reach
# location and is operator-fired (needs the WireGuard kernel module).

set -euo pipefail
cd "$(dirname "$0")"

CLASSIFIER="../files/wg-cp0-classify-conf-change.sh"
FIX="fixtures"

if [[ ! -x "$CLASSIFIER" ]]; then
  echo "[wg-cp0-server/test-classify-conf-change] FAIL — classifier not executable: $CLASSIFIER" >&2
  exit 1
fi

pass=0
fail=0
fail_log=""

assert_class() {
  local label="$1" live="$2" staged="$3" expected="$4"
  local got
  if [[ "$live" == "<absent>" ]]; then
    got="$("$CLASSIFIER" /nonexistent/wg-cp0.conf "$staged")"
  else
    got="$("$CLASSIFIER" "$live" "$staged")"
  fi
  if [[ "$got" == "$expected" ]]; then
    echo "  PASS  $label  → $got"
    pass=$((pass + 1))
  else
    echo "  FAIL  $label  expected=$expected got=$got" >&2
    fail=$((fail + 1))
    fail_log+="    $label: expected=$expected got=$got"$'\n'
  fi
}

echo "[wg-cp0-server/test-classify-conf-change] running fixture suite"

# Identity — live and staged are the same file.
assert_class "live vs itself"                     "$FIX/live.conf"          "$FIX/live.conf"             "no_change"
# Cosmetic reformat — whitespace + comments only.
assert_class "live vs cosmetic reformat"          "$FIX/live.conf"          "$FIX/same-reformatted.conf" "no_change"
# Peer-set deltas.
assert_class "add a peer"                         "$FIX/live.conf"          "$FIX/add-peer.conf"         "peer_change"
assert_class "remove a peer"                      "$FIX/live.conf"          "$FIX/remove-peer.conf"      "peer_change"
assert_class "rotate a peer's pubkey"             "$FIX/live.conf"          "$FIX/rotate-peer-key.conf"  "peer_change"
# Listener-shape deltas.
assert_class "change ListenPort"                  "$FIX/live.conf"          "$FIX/change-port.conf"      "shape_change"
assert_class "change Address"                     "$FIX/live.conf"          "$FIX/change-address.conf"   "shape_change"
assert_class "rotate relay's own PrivateKey"      "$FIX/live.conf"          "$FIX/rotate-privkey.conf"   "shape_change"
# First-install (live conf does not exist yet).
assert_class "first install (no live conf)"       "<absent>"                "$FIX/live.conf"             "first_install"
# Reverse direction — going from add-peer back to live is also a peer_change.
assert_class "reverse: remove a peer back to live" "$FIX/add-peer.conf"     "$FIX/live.conf"             "peer_change"
# Going from shape-change config back to baseline is shape_change too.
assert_class "reverse: revert ListenPort to baseline" "$FIX/change-port.conf" "$FIX/live.conf"           "shape_change"

echo ""
echo "[wg-cp0-server/test-classify-conf-change] summary: $pass pass / $fail fail"
if [[ $fail -gt 0 ]]; then
  echo "FAILURES:"
  echo "$fail_log"
  exit 1
fi
echo "[wg-cp0-server/test-classify-conf-change] PASS"
