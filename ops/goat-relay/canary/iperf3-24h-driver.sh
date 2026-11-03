#!/bin/bash
# iperf3-24h-driver.sh — Step 3 wrapper for goat-relay-prod-canary.md.
#
# 30-min iperf3 single-flow loop × 48 cycles → JSONL output. Run on the
# CLIENT peer; iperf3 server runs on the OTHER peer at $TARGET_HOST:5201.
#
# Usage:
#   TARGET_HOST=<peer-a-mesh-ip> bash iperf3-24h-driver.sh
#
# Optional:
#   OUT_DIR=/tmp/canary       # output directory (default /tmp/canary)
#   CYCLE_LEN_S=1740          # 29 min iperf3 per cycle (default 1740)
#   CYCLE_GAP_S=60            # 1 min sleep between cycles (default 60)
#   TARGET_END_S=86400        # 24h total (default 86400)
#
# Acceptance check via aggregate.py after the run.

set -euo pipefail

: "${TARGET_HOST:?set TARGET_HOST=<peer-a-mesh-ip>}"
OUT_DIR="${OUT_DIR:-/tmp/canary}"
CYCLE_LEN_S="${CYCLE_LEN_S:-1740}"
CYCLE_GAP_S="${CYCLE_GAP_S:-60}"
TARGET_END_S="${TARGET_END_S:-86400}"

mkdir -p "$OUT_DIR"
JSONL="$OUT_DIR/canary.jsonl"

START=$(date -u +%s)
TARGET_END=$((START + TARGET_END_S))
i=0

echo "iperf3-24h-driver: target=$TARGET_HOST cycles_until=$(date -u -r "$TARGET_END" +%FT%TZ) jsonl=$JSONL"

while [ "$(date -u +%s)" -lt "$TARGET_END" ]; do
  i=$((i+1))
  ts=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  if result=$(iperf3 -c "$TARGET_HOST" -p 5201 -t "$CYCLE_LEN_S" -J 2>&1); then
    echo "{\"ts\":\"$ts\",\"cycle\":$i,\"result\":$result}" >> "$JSONL"
  else
    echo "{\"ts\":\"$ts\",\"cycle\":$i,\"result\":{\"error\":true,\"stderr\":$(printf '%s' "$result" | jq -Rs .)}}" >> "$JSONL"
  fi
  sleep "$CYCLE_GAP_S"
done

echo "iperf3-24h-driver: done. cycles=$i jsonl=$JSONL"
echo "Run aggregator: python3 $(dirname "$0")/aggregate.py $JSONL"
