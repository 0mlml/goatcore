#!/usr/bin/env bash
# Block 68D run-rapid-bringup-drill.sh — measured rapid-bringup drill.
#
# Operator-fired against a clean Orin (or fresh EC2 stand-in for an
# Orin via OS-equivalent emulation). Times each phase end-to-end,
# emits per-phase JSONL evidence, renders PASS/WARN/FAIL verdict per
# rapid-site-bringup.md §9.
#
# Spec: docs/design/rapid-site-bringup.md §9
#
# Usage:
#   ./run-rapid-bringup-drill.sh \
#     --scenario {S1|S2} \
#     --hardware-class {orin-mgmt|orin-obs|karbon-mgmt} \
#     --target <ssh-target>      # for remote; omit for self-drill
#     [--evidence <path>]
#     [--bandwidth-shape]        # apply tc qdisc tactical-edge shape
#                                # (Block 68E)
set -euo pipefail

SCENARIO=""
HW_CLASS=""
TARGET=""
EVIDENCE=""
BANDWIDTH_SHAPE=0
MODE="auto"          # auto = poll target's first-boot.jsonl; manual = operator-paced
POLL_INTERVAL=10     # seconds between target log polls in auto mode

usage() {
  cat <<EOF
run-rapid-bringup-drill.sh — Block 68D drill harness

Required:
  --scenario {S1|S2}
  --hardware-class {orin-mgmt|orin-obs|karbon-mgmt}

Optional:
  --target <ssh-target>      Remote drill target (required for --auto;
                             default: self-drill in --manual)
  --mode {auto|manual}       auto = poll target's first-boot.jsonl over ssh
                             every \$POLL_INTERVAL seconds, extract phase
                             timestamps, emit drill-side records. Default
                             when --target is set.
                             manual = operator hits Enter at each phase
                             boundary. Default when --target is empty.
  --poll-interval <seconds>  Auto-mode poll cadence (default: 10s)
  --evidence <path>          JSONL output path (default: docs/reports/...)
  --bandwidth-shape          Apply tc qdisc tactical-edge shape (68E)
EOF
  exit 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --scenario) SCENARIO=$2; shift 2;;
    --hardware-class) HW_CLASS=$2; shift 2;;
    --target) TARGET=$2; shift 2;;
    --mode) MODE=$2; shift 2;;
    --poll-interval) POLL_INTERVAL=$2; shift 2;;
    --evidence) EVIDENCE=$2; shift 2;;
    --bandwidth-shape) BANDWIDTH_SHAPE=1; shift;;
    -h|--help) usage;;
    *) echo "unknown arg: $1" >&2; usage;;
  esac
done

# Mode disambiguation: default to manual if no target provided.
if [[ -z "$TARGET" && "$MODE" == "auto" ]]; then
  MODE=manual
fi
case "$MODE" in
  auto|manual) ;;
  *) echo "invalid --mode: $MODE" >&2; usage;;
esac

[[ "$SCENARIO" =~ ^S[12]$ ]] || usage
case "$HW_CLASS" in
  orin-mgmt|orin-obs|karbon-mgmt) ;;
  *) usage;;
esac

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
DATE=$(date -u +%Y-%m-%d)
SCEN_LOWER=$(echo "$SCENARIO" | tr '[:upper:]' '[:lower:]')
EVIDENCE=${EVIDENCE:-"$REPO_ROOT/docs/reports/${DATE}-rapid-bringup-drill-${SCEN_LOWER}-${HW_CLASS}.jsonl"}
mkdir -p "$(dirname "$EVIDENCE")"

# Per design §6 phase budgets (seconds). Case dispatch, not an
# associative array: the drill runs on the operator's macOS
# /bin/bash 3.2 (#227).
budget_s_for() {
  case "$1" in
    boot)              echo 300  ;;  # 5 min
    self-test-luks)    echo 300  ;;  # 5 min
    first-boot-config) echo 300  ;;  # 5 min (operator-paced; not strict)
    bootstrap-1-4)     echo 1500 ;;  # 25 min
    bootstrap-5-8)     echo 600  ;;  # 10 min
    scenario-action)   echo 300  ;;  # 5 min
    smoke-verdict)     echo 300  ;;  # 5 min
    *)                 echo 0    ;;
  esac
}
TOTAL_BUDGET_S=3600  # 60 min single-Orin
[[ "$HW_CLASS" == "karbon-mgmt" ]] && TOTAL_BUDGET_S=3600

START_UNIX=$(date +%s)
log() { printf '[drill] %s\n' "$*"; }
emit() {
  local phase=$1 status=$2 elapsed=$3 budget=${4:-0} extra=${5:-"{}"}
  local now ts_unix
  now=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  ts_unix=$(date +%s)
  printf '{"ts":"%s","ts_unix":%s,"phase":"%s","status":"%s","elapsed_s":%s,"budget_s":%s,"extra":%s}\n' \
    "$now" "$ts_unix" "$phase" "$status" "$elapsed" "$budget" "$extra" >> "$EVIDENCE"
}

# Initial header record.
emit "drill.start" "ok" "0" "$TOTAL_BUDGET_S" \
  "{\"scenario\":\"$SCENARIO\",\"hardware_class\":\"$HW_CLASS\",\"target\":\"$TARGET\",\"bandwidth_shape\":$BANDWIDTH_SHAPE}"

# -- Optional: apply tc qdisc tactical-edge shape (Block 68E) ------------
if [[ $BANDWIDTH_SHAPE -eq 1 ]]; then
  log "applying tactical-edge bandwidth shape (100 Mbps + 100ms RTT)"
  if [[ -n "$TARGET" ]]; then
    ssh "$TARGET" "sudo tc qdisc replace dev eth0 root netem rate 100mbit delay 100ms" || \
      log "WARN: tc qdisc apply failed (may require existing qdisc removal)"
  fi
  emit "drill.bandwidth-shape" "ok" "0" "0" '{"rate":"100mbit","delay":"100ms"}'
fi

# -- Phase scoring ------------------------------------------------------
phase_marker() {
  local phase=$1 elapsed=$2 status=$3 extra=${4:-"{}"}
  local budget; budget=$(budget_s_for "$phase")
  emit "phase.$phase" "$status" "$elapsed" "$budget" "$extra"
  local v="PASS"
  if [[ $elapsed -gt $((budget * 11 / 10)) ]]; then v="WARN"; fi
  if [[ $elapsed -gt $((budget * 5 / 4)) ]]; then v="FAIL"; fi
  log "phase $phase: ${elapsed}s / ${budget}s — $v"
}

# Manual mode: operator hits Enter at each phase boundary.
operator_marker() {
  local phase=$1
  log ""
  log "========== phase: $phase =========="
  log "Budget: $(budget_s_for "$phase")s"
  log "Press Enter when this phase completes (Ctrl-C to abort drill)..."
  read -r
}

PHASE_START=$START_UNIX

run_phase_manual() {
  local phase=$1
  operator_marker "$phase"
  local now elapsed
  now=$(date +%s)
  elapsed=$((now - PHASE_START))
  phase_marker "$phase" "$elapsed" "ok"
  PHASE_START=$now
}

# Auto mode: poll target's /var/log/goat/first-boot.jsonl over
# ssh, extract per-phase ok-status records, map first-boot phases to
# drill-budget phases, emit drill-side records when boundaries are
# crossed. Drill timeout is 2x total budget.
TARGET_JSONL=/var/log/goat/first-boot.jsonl
drill_phase_for() {
  case "$1" in
    first-boot.userdata-read)     echo "first-boot-config" ;;
    first-boot.userdata-validate) echo "first-boot-config" ;;
    first-boot.serial-prompt)     echo "first-boot-config" ;;
    first-boot.envrc-assemble)    echo "first-boot-config" ;;
    first-boot.handoff)           echo "bootstrap-1-4" ;;
    first-boot.done)              echo "smoke-verdict" ;;
    *)                            echo "" ;;
  esac
}

run_phase_auto() {
  local poll_until=$1
  local seen=""
  log "auto mode: polling $TARGET:$TARGET_JSONL every ${POLL_INTERVAL}s"
  log "auto mode: drill timeout at $((poll_until - START_UNIX))s after start"
  while true; do
    local now
    now=$(date +%s)
    if [[ $now -ge $poll_until ]]; then
      log "auto mode: drill timeout reached"
      break
    fi
    # Pull current first-boot.jsonl content (small file; idempotent fetch)
    local fb_content
    fb_content=$(ssh -o ConnectTimeout=5 "$TARGET" "cat $TARGET_JSONL 2>/dev/null" || true)
    if [[ -n "$fb_content" ]]; then
      # Process every record we haven't seen yet
      while IFS= read -r record; do
        [[ -z "$record" ]] && continue
        local phase status ts_unix
        phase=$(echo "$record" | jq -r '.phase' 2>/dev/null || echo "")
        status=$(echo "$record" | jq -r '.status' 2>/dev/null || echo "")
        ts_unix=$(echo "$record" | jq -r '.ts_unix' 2>/dev/null || echo "")
        # Only emit on ok-status records that map to a drill phase
        local drill_phase; drill_phase=$(drill_phase_for "$phase")
        [[ -z "$drill_phase" ]] && continue
        [[ "$status" != "ok" ]] && continue
        # Emit only once per first-boot phase
        local key="${phase}@${ts_unix}"
        if [[ "$seen" != *"$key"* ]]; then
          seen="$seen|$key"
          local elapsed=$((ts_unix - PHASE_START))
          phase_marker "$drill_phase" "$elapsed" "ok" \
            "{\"firstboot_phase\":\"$phase\",\"firstboot_ts\":$ts_unix}"
          PHASE_START=$ts_unix
        fi
        # Drill is complete when we see first-boot.done
        if [[ "$phase" == "first-boot.done" ]]; then
          log "auto mode: target signaled first-boot.done; drill capture complete"
          return 0
        fi
      done <<< "$fb_content"
    fi
    sleep "$POLL_INTERVAL"
  done
}

# Phases per design §6:
if [[ "$MODE" == "auto" ]]; then
  poll_until=$((START_UNIX + 2 * TOTAL_BUDGET_S))
  run_phase_auto "$poll_until"
else
  run_phase_manual "boot"
  run_phase_manual "self-test-luks"
  run_phase_manual "first-boot-config"
  run_phase_manual "bootstrap-1-4"
  run_phase_manual "bootstrap-5-8"
  run_phase_manual "scenario-action"
  run_phase_manual "smoke-verdict"
fi

# -- Verdict ------------------------------------------------------------
END_UNIX=$(date +%s)
TOTAL_ELAPSED=$((END_UNIX - START_UNIX))
log ""
log "=========================================================="
log " Drill complete."
log "   Total elapsed: ${TOTAL_ELAPSED}s"
log "   Total budget:  ${TOTAL_BUDGET_S}s"

VERDICT="PASS"
if [[ $TOTAL_ELAPSED -gt $((TOTAL_BUDGET_S * 11 / 10)) ]]; then VERDICT="WARN"; fi
if [[ $TOTAL_ELAPSED -gt $((TOTAL_BUDGET_S * 5 / 4)) ]]; then VERDICT="FAIL"; fi
log "   Verdict:       $VERDICT"
log "=========================================================="

emit "drill.verdict" "ok" "$TOTAL_ELAPSED" "$TOTAL_BUDGET_S" \
  "{\"verdict\":\"$VERDICT\"}"

# Echo the canonical summary path for the validation-state register.
echo "Evidence: $EVIDENCE"
echo "Verdict:  $VERDICT"

if [[ "$VERDICT" == "FAIL" ]]; then
  log ""
  log "FAIL → draft an F-NNN finding per ADR 1011 against the slowest phase."
  exit 1
fi
