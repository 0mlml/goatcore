#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# run-loadtest.sh — orchestrate one load run (or rate sweep) on nova-a.
#
# Safety first (nova-a's single-rootfs Thor — OS + Docker + rocksdb + MinIO share
# one ext4 rootfs; see docs/design/zenoh-loadtest.md):
#   - pre-flight free-space gate + projected-size gate (refuse to start)
#   - live disk watchdog that SIGTERMs the loadgen if free space crosses the floor
#   - the MinIO bucket carries a hard quota; rocksdb is bounded by the watchdog
#
# What it does per rate point:
#   1. starts the goat-loadgen container (mTLS, on the router's docker network)
#   2. scrapes loadgen /metrics every second -> throughput + put-latency timeline
#   3. each tick, probes the freshest rocksdb key for publish->rocksdb lag
#   4. on stop, records the loadtest audit-subscriber drop counter, then runs
#      the rocksdb + MinIO completeness probes
#   5. appends everything to a single evidence JSONL
#
# Then: ops/loadtest/summarize.py <evidence.jsonl>  (report + charts)
#       ops/loadtest/wipe-loadtest.sh               (teardown)
#
# Usage:
#   run-loadtest.sh --rate 100 --payload-bytes 100 --duration 60 --sessions 4
#   run-loadtest.sh --rate-sweep 10,50,100,500,2000 --duration 60 --sessions 4
#   run-loadtest.sh --rate 0 --duration 30 --sessions 8     # unbounded (watchdog-gated)

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib-loadtest.sh
source "${HERE}/lib-loadtest.sh"

# Orchestrator-specific knobs.
: "${LOADTEST_SCRAPE_INTERVAL:=1}"          # seconds between /metrics scrapes
: "${LOADTEST_WATCHDOG_INTERVAL:=3}"        # seconds between disk checks
: "${LOADTEST_AUDIT_METRICS:=http://127.0.0.1:9305/metrics}"  # loadtest audit-subscriber
: "${LOADTEST_REPLICATION_FACTOR:=2}"       # rocksdb + minio copy, for projection
: "${LOADTEST_SETTLE_SECS:=8}"              # wait after loadgen for the batch timer (5s) to flush

RATE=""; SWEEP=""; PAYLOAD=100; DURATION=60; SESSIONS=1; LANES=1; RUN_ID=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --rate) RATE="$2"; shift 2 ;;
    --rate-sweep) SWEEP="$2"; shift 2 ;;
    --payload-bytes) PAYLOAD="$2"; shift 2 ;;
    --duration) DURATION="$2"; shift 2 ;;
    --sessions) SESSIONS="$2"; shift 2 ;;
    --lanes) LANES="$2"; shift 2 ;;          # informational; provisioned by ansible
    --run-id) RUN_ID="$2"; shift 2 ;;
    -h|--help) grep '^#' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) loadtest::die "unknown arg: $1" ;;
  esac
done
[[ -n "$RATE" || -n "$SWEEP" ]] || loadtest::die "specify --rate or --rate-sweep"

# Fail fast with a clear message if a required tool is missing, rather than
# mid-run (the projection gate needs bc; scraping needs curl+jq; the run needs
# docker; probes need the probe deps). mc is checked by the MinIO probe itself.
_missing=()
for t in bc jq curl docker; do command -v "$t" >/dev/null 2>&1 || _missing+=("$t"); done
[[ ${#_missing[@]} -eq 0 ]] || loadtest::die "missing required tools: ${_missing[*]}"

# Run id isolates this run's keyspace + objects so probes and wipe address only it.
if [[ -z "$RUN_ID" ]]; then
  RUN_ID="r$(date +%Y%m%d-%H%M%S)-$$"
fi
mkdir -p "$LOADTEST_OUT_DIR"
EVIDENCE="${LOADTEST_OUT_DIR}/$(date +%Y-%m-%d)-zenoh-fabric-load-${RUN_ID}.jsonl"
: > "$EVIDENCE"

scrape_counter() { # url metric -> value (0 if absent)
  curl -fsS "$1" 2>/dev/null | awk -v m="$2" '$1==m {print $2; found=1} END{if(!found)print 0}' | head -1
}
emit() { printf '%s\n' "$1" >> "$EVIDENCE"; }

# ---- Pre-flight gates -------------------------------------------------------

free_gb=$(loadtest::free_gb "$LOADTEST_DISK_GUARD_PATH")
loadtest::log "pre-flight: ${free_gb} GB free on $LOADTEST_DISK_GUARD_PATH (floor ${LOADTEST_DISK_FLOOR_GB} GB)"
(( free_gb >= LOADTEST_DISK_FLOOR_GB )) || loadtest::die "free space ${free_gb} GB already below floor; refusing to start"

project_and_gate() { # rate -> refuse if projected run would breach the floor
  local rate="$1"
  if (( $(echo "$rate <= 0" | bc -l) )); then
    loadtest::log "rate=0 (unbounded): projection skipped; relying on the live watchdog + bucket quota"
    return 0
  fi
  # worst-case bytes = rate * payload * duration * replication, in GB
  local proj_gb
  proj_gb=$(echo "scale=2; $rate * $PAYLOAD * $DURATION * $LOADTEST_REPLICATION_FACTOR / 1073741824" | bc -l)
  local after
  after=$(echo "scale=2; $free_gb - $proj_gb" | bc -l)
  loadtest::log "projection @rate=${rate}: ~${proj_gb} GB written, ~${after} GB free after"
  if (( $(echo "$after < $LOADTEST_DISK_FLOOR_GB" | bc -l) )); then
    loadtest::die "projected free-after (${after} GB) < floor (${LOADTEST_DISK_FLOOR_GB} GB) @rate=${rate}; lower rate/duration or add disk"
  fi
}

emit "{\"event\":\"run_start\",\"ts_ns\":$(date +%s%N),\"run_id\":\"${RUN_ID}\",\"payload_bytes\":${PAYLOAD},\"duration_secs\":${DURATION},\"sessions\":${SESSIONS},\"lanes\":${LANES},\"free_gb_start\":${free_gb}}"

# ---- One rate point ---------------------------------------------------------

run_point() {
  local rate="$1" point_run_id="${RUN_ID}-rate${1}"
  project_and_gate "$rate"
  loadtest::log "=== rate=${rate} msg/s · payload=${PAYLOAD}B · ${DURATION}s · sessions=${SESSIONS} · run=${point_run_id} ==="
  local cname="goat-loadgen-${point_run_id}"

  # Launch loadgen container. Metrics published to host loopback for scraping.
  docker rm -f "$cname" >/dev/null 2>&1 || true
  docker run -d --name "$cname" \
    --network "$LOADTEST_DOCKER_NETWORK" \
    -p 127.0.0.1:9304:9304 \
    -v "${LOADTEST_CERT}:/certs/cert.pem:ro" \
    -v "${LOADTEST_KEY}:/certs/key.pem:ro" \
    -v "${LOADTEST_ROOT_CA}:/certs/ca.pem:ro" \
    -e LOADGEN_ENDPOINT="$LOADTEST_ZENOH_ENDPOINT" \
    -e LOADGEN_KEY_PREFIX="$LOADTEST_KEY_PREFIX" \
    -e LOADGEN_RUN_ID="$point_run_id" \
    -e LOADGEN_RATE="$rate" \
    -e LOADGEN_PAYLOAD_BYTES="$PAYLOAD" \
    -e LOADGEN_DURATION_SECS="$DURATION" \
    -e LOADGEN_SESSIONS="$SESSIONS" \
    -e LOADGEN_CERT=/certs/cert.pem -e LOADGEN_KEY=/certs/key.pem -e LOADGEN_ROOT_CA=/certs/ca.pem \
    "$LOADTEST_LOADGEN_IMAGE" >/dev/null \
    || loadtest::die "failed to start loadgen container"

  # Watchdog: abort the run if the rootfs gets close to the floor.
  ( while docker ps --format '{{.Names}}' | grep -q "^${cname}$"; do
      local fg; fg=$(loadtest::free_gb "$LOADTEST_DISK_GUARD_PATH")
      if (( fg < LOADTEST_DISK_FLOOR_GB )); then
        loadtest::log "WATCHDOG: free ${fg} GB < floor ${LOADTEST_DISK_FLOOR_GB} GB — aborting loadgen"
        docker stop "$cname" >/dev/null 2>&1 || true
        break
      fi
      sleep "$LOADTEST_WATCHDOG_INTERVAL"
    done ) &
  local wd=$!

  # Scrape loop: throughput + put latency + freshest-key rocksdb lag, per tick.
  while docker ps --format '{{.Names}}' | grep -q "^${cname}$"; do
    local ts sent errs fg
    ts=$(date +%s%N)
    sent=$(scrape_counter "$LOADTEST_LOADGEN_METRICS" loadgen_sent_total)
    errs=$(scrape_counter "$LOADTEST_LOADGEN_METRICS" loadgen_send_errors_total)
    fg=$(loadtest::free_gb "$LOADTEST_DISK_GUARD_PATH")
    # put-latency histogram buckets (raw lines), captured for percentile calc by summarize.py
    local hist; hist=$(curl -fsS "$LOADTEST_LOADGEN_METRICS" 2>/dev/null | grep '^loadgen_put_latency_seconds_' | jq -R -s -c 'split("\n")|map(select(length>0))' || echo '[]')
    emit "{\"event\":\"sample\",\"ts_ns\":${ts},\"rate_target\":${rate},\"sent\":${sent:-0},\"errors\":${errs:-0},\"free_gb\":${fg},\"put_hist\":${hist}}"
    # rocksdb lag on the freshest stored key
    if [[ -n "${sent:-}" && "${sent:-0}" -gt 0 ]]; then
      "${HERE}/probe-rocksdb.sh" --lag "${LOADTEST_KEY_PREFIX}/${point_run_id}/$(( sent - 1 ))" >> "$EVIDENCE" 2>/dev/null || true
    fi
    sleep "$LOADTEST_SCRAPE_INTERVAL"
  done
  kill "$wd" >/dev/null 2>&1 || true

  # Let the audit-subscriber's 5s batch timer flush the tail before we probe MinIO.
  loadtest::log "loadgen stopped; settling ${LOADTEST_SETTLE_SECS}s for batch flush"
  sleep "$LOADTEST_SETTLE_SECS"

  local sent_final drops
  sent_final=$(docker logs "$cname" 2>/dev/null | grep -o '"event":"final".*' | tail -1 | jq -r '.sent' 2>/dev/null || echo 0)
  [[ "$sent_final" =~ ^[0-9]+$ ]] || sent_final=$(scrape_counter "$LOADTEST_LOADGEN_METRICS" loadgen_sent_total)
  drops=$(scrape_counter "$LOADTEST_AUDIT_METRICS" goat_audit_drop_total)
  emit "{\"event\":\"point_end\",\"ts_ns\":$(date +%s%N),\"rate_target\":${rate},\"sent_final\":${sent_final:-0},\"audit_drop_total\":${drops:-0}}"

  # Completeness probes (both sinks).
  "${HERE}/probe-rocksdb.sh" --completeness "$point_run_id" "${sent_final:-0}" >> "$EVIDENCE" 2>/dev/null || true
  "${HERE}/probe-minio.sh" "$point_run_id" "${sent_final:-0}" >> "$EVIDENCE" 2>/dev/null || true

  docker rm -f "$cname" >/dev/null 2>&1 || true
  loadtest::log "rate=${rate} done. sent=${sent_final} audit_drops=${drops}"
}

# ---- Drive the run ----------------------------------------------------------

if [[ -n "$SWEEP" ]]; then
  IFS=',' read -ra RATES <<< "$SWEEP"
  for r in "${RATES[@]}"; do run_point "$r"; done
else
  run_point "$RATE"
fi

emit "{\"event\":\"run_end\",\"ts_ns\":$(date +%s%N),\"run_id\":\"${RUN_ID}\",\"free_gb_end\":$(loadtest::free_gb "$LOADTEST_DISK_GUARD_PATH")}"
loadtest::log "evidence: $EVIDENCE"
loadtest::log "next: ${HERE}/summarize.py '$EVIDENCE'   then   ${HERE}/wipe-loadtest.sh"
printf '%s\n' "$EVIDENCE"
