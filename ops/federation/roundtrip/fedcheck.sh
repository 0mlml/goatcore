#!/usr/bin/env bash
# Federation round-trip check — the final validation stage.
#
# Publishes a nonce under OUR wire name and waits for the counterparty's
# acknowledgement to come back under THEIRS. A pass means the sample crossed the
# peering, was delivered by the PARTNER'S OWN ROUTER to a process on their side,
# and the answer returned across the same agreement.
#
# This is the only check the SENDING side can make alone. Every other federation
# assertion requires shell on both sides, which no real publisher has.
set -uo pipefail
BASE=${BASE:-http://127.0.0.1:8000}
OUR_WIRE=${1:?our wire prefix, e.g. from/zagros}
THEIR_WIRE=${2:?their wire prefix, e.g. from/xaa}
TIMEOUT=${3:-20}

NONCE="rt$(date +%s)$$"
PAYLOAD="fedcheck-$NONCE"
WANT=$(printf '%s' "$PAYLOAD" | shasum -a 256 2>/dev/null | cut -d' ' -f1 || printf '%s' "$PAYLOAD" | sha256sum | cut -d' ' -f1)

t0=$(python3 -c 'import time;print(time.time())')
curl -s -m 8 -X PUT -d "$PAYLOAD" "$BASE/$OUR_WIRE/fedcheck/$NONCE" -o /dev/null

got=""; 
for _ in $(seq 1 $((TIMEOUT*2))); do
  r=$(curl -s -m 5 "$BASE/$THEIR_WIRE/fedcheck-ack/$NONCE" 2>/dev/null)
  v=$(printf '%s' "$r" | grep -oE '"value":"[^"]*"' | head -1 | sed 's/.*"value":"//;s/"$//')
  if [ -n "$v" ]; then got=$(printf '%s' "$v" | base64 -d 2>/dev/null); break; fi
  sleep 0.5
done
t1=$(python3 -c 'import time;print(time.time())')
rtt=$(python3 -c "print(f'{($t1-$t0)*1000:.0f}')")

if [ -z "$got" ]; then
  echo "  FAIL  no acknowledgement in ${TIMEOUT}s — the sample did not complete the round trip"
  echo "        (a one-way check would have passed here if their router never delivered it)"
  exit 1
fi
saw=$(printf '%s' "$got" | python3 -c 'import json,sys; print(json.load(sys.stdin)["sha256"])' 2>/dev/null)
key=$(printf '%s' "$got" | python3 -c 'import json,sys; print(json.load(sys.stdin)["observed_key"])' 2>/dev/null)
if [ "$saw" != "$WANT" ]; then
  echo "  FAIL  acknowledged, but the payload digest differs — sent $WANT, they saw $saw"
  exit 1
fi
echo "  PASS  round trip complete in ${rtt}ms"
echo "        they observed $key with the digest we sent"
