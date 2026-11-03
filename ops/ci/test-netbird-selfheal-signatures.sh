#!/usr/bin/env bash
# test-netbird-selfheal-signatures.sh — pin the decision contract of
# ops/netbird/netbird-boot-selfheal.sh, with the emphasis on signature 3.
#
# Signature 3 covers a node reporting `Management: Connected`, `Signal: Connected`,
# `Peers count 5/10` and every peer `Connected / P2P`, while its data plane is dead
# in both directions. Signatures 1 and 2 cannot see it, because both key off the
# control plane. One `systemctl restart netbird` clears it.
#
# The cases below are negative-tested in both directions on purpose: a
# self-heal that restarts a HEALTHY daemon is worse than one that misses a
# wedge, so "does not fire" is tested as hard as "fires".
#
# No network, no root, no netbird. Stubs the daemon and the restart command.
set -uo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/ops/netbird/netbird-boot-selfheal.sh"
[ -r "$SCRIPT" ] || { echo "cannot read $SCRIPT" >&2; exit 2; }

PASS=0; FAIL=0
TMPROOT="$(mktemp -d)"
trap 'rm -rf "$TMPROOT"; [ -n "${LISTENER_PID:-}" ] && kill "$LISTENER_PID" 2>/dev/null' EXIT

# A port that accepts TCP (stand-in for a reachable mesh peer), and one that
# refuses (stand-in for a peer the wedged tunnel cannot carry us to).
python3 -c '
import socket,sys,time
s=socket.socket(); s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1)
s.bind(("127.0.0.1",0)); s.listen(64)
print(s.getsockname()[1], flush=True)
while True:
    try: s.accept()
    except Exception: break
' > "$TMPROOT/port" &
LISTENER_PID=$!
for _ in $(seq 1 50); do [ -s "$TMPROOT/port" ] && break; sleep 0.1; done
LIVE_PORT="$(cat "$TMPROOT/port")"
[ -n "$LIVE_PORT" ] || { echo "could not start test listener" >&2; exit 2; }
ALIVE="127.0.0.1:${LIVE_PORT}"
DEAD="127.0.0.1:1"          # nothing listens on port 1

# run <name> <expect-restart 0|1> <status-text> [env assignments...]
run() {
  local name="$1" expect="$2" status="$3"; shift 3
  local dir="$TMPROOT/$RANDOM$RANDOM"; mkdir -p "$dir/state"
  printf '#!/usr/bin/env bash\ncase "$1" in status) cat <<%s\n%s\n%s\n;; esac\n' \
    "EOS" "$status" "EOS" > "$dir/netbird"
  chmod +x "$dir/netbird"
  printf '#!/usr/bin/env bash\ntouch "%s/RESTARTED"\n' "$dir" > "$dir/restart"
  chmod +x "$dir/restart"

  env NB_BIN="$dir/netbird" \
      RESTART_CMD="$dir/restart" \
      STATE_DIR="$dir/state" \
      METRICS_FILE="$dir/state/metrics.prom" \
      PROBE_TIMEOUT_S=2 \
      "$@" \
      bash "$SCRIPT" >"$dir/out" 2>"$dir/err"

  local got=0; [ -f "$dir/RESTARTED" ] && got=1
  if [ "$got" = "$expect" ]; then
    PASS=$((PASS+1)); printf '  ok   %s\n' "$name"
  else
    FAIL=$((FAIL+1))
    printf '  FAIL %s (expected restart=%s, got=%s)\n' "$name" "$expect" "$got"
    sed 's/^/       /' "$dir/err" | tail -3
  fi
}

# Repeat a scenario N times against ONE state dir so streaks accumulate the way
# the timer would. Returns whether a restart happened by the Nth tick.
run_streak() {
  local name="$1" expect="$2" ticks="$3" status="$4"; shift 4
  local dir="$TMPROOT/s$RANDOM$RANDOM"; mkdir -p "$dir/state"
  printf '#!/usr/bin/env bash\ncase "$1" in status) cat <<%s\n%s\n%s\n;; esac\n' \
    "EOS" "$status" "EOS" > "$dir/netbird"
  chmod +x "$dir/netbird"
  printf '#!/usr/bin/env bash\ntouch "%s/RESTARTED"\n' "$dir" > "$dir/restart"
  chmod +x "$dir/restart"
  local _i
  for _i in $(seq 1 "$ticks"); do
    env NB_BIN="$dir/netbird" RESTART_CMD="$dir/restart" STATE_DIR="$dir/state" \
        METRICS_FILE="$dir/state/metrics.prom" PROBE_TIMEOUT_S=2 "$@" \
        bash "$SCRIPT" >/dev/null 2>&1
  done
  local got=0; [ -f "$dir/RESTARTED" ] && got=1
  if [ "$got" = "$expect" ]; then
    PASS=$((PASS+1)); printf '  ok   %s\n' "$name"
  else
    FAIL=$((FAIL+1)); printf '  FAIL %s (expected restart=%s after %s ticks, got=%s)\n' "$name" "$expect" "$ticks" "$got"
  fi
}

HEALTHY_STATUS='Management: Connected
Signal: Connected
Peers count: 5/10 Connected'

LOWPEER_STATUS='Management: Connected
Signal: Connected
Peers count: 1/10 Connected'

CP_DOWN_STATUS='Management: Disconnected
Signal: Disconnected
Peers count: 0/10 Connected'

echo "signature 3 — the shape that took the breakglass node out:"
run_streak "fires when everything reads healthy and NO target answers" 1 4 "$HEALTHY_STATUS" \
  MIN_PEERS=2 SUSTAINED_CHECKS=4 COOLDOWN_S=60 DATA_WEDGE_ENABLED=1 DATA_PROBE_TARGETS="$DEAD $DEAD"
run_streak "does NOT fire before the streak is met" 0 3 "$HEALTHY_STATUS" \
  MIN_PEERS=2 SUSTAINED_CHECKS=4 COOLDOWN_S=60 DATA_WEDGE_ENABLED=1 DATA_PROBE_TARGETS="$DEAD"

echo "signature 3 — must never restart a healthy daemon:"
run_streak "ANY reachable target means healthy, even with dead ones alongside" 0 6 "$HEALTHY_STATUS" \
  MIN_PEERS=2 SUSTAINED_CHECKS=4 COOLDOWN_S=60 DATA_WEDGE_ENABLED=1 DATA_PROBE_TARGETS="$DEAD $ALIVE"
run_streak "disabled by default — dead targets change nothing" 0 6 "$HEALTHY_STATUS" \
  MIN_PEERS=2 SUSTAINED_CHECKS=4 COOLDOWN_S=60 DATA_PROBE_TARGETS="$DEAD"
run_streak "enabled but NO targets configured never infers a wedge" 0 6 "$HEALTHY_STATUS" \
  MIN_PEERS=2 SUSTAINED_CHECKS=4 COOLDOWN_S=60 DATA_WEDGE_ENABLED=1 DATA_PROBE_TARGETS=""
run_streak "malformed targets are skipped, not treated as failures" 0 6 "$HEALTHY_STATUS" \
  MIN_PEERS=2 SUSTAINED_CHECKS=4 COOLDOWN_S=60 DATA_WEDGE_ENABLED=1 DATA_PROBE_TARGETS="not-a-target $ALIVE"
run_streak "cooldown is respected once a restart has happened" 1 12 "$HEALTHY_STATUS" \
  MIN_PEERS=2 SUSTAINED_CHECKS=4 COOLDOWN_S=99999 MAX_RESTARTS_PER_BOOT=1 \
  DATA_WEDGE_ENABLED=1 DATA_PROBE_TARGETS="$DEAD"

echo "no regression in the two existing signatures:"
run_streak "peers-low still owns the below-floor path" 1 4 "$LOWPEER_STATUS" \
  MIN_PEERS=5 SUSTAINED_CHECKS=4 COOLDOWN_S=60 DATA_WEDGE_ENABLED=1 DATA_PROBE_TARGETS="$DEAD"
run_streak "control-plane-down does NOT trip signature 3" 0 6 "$CP_DOWN_STATUS" \
  MIN_PEERS=2 SUSTAINED_CHECKS=4 COOLDOWN_S=60 DATA_WEDGE_ENABLED=1 DATA_PROBE_TARGETS="$DEAD"
run "daemon not answering at all is reported dark, never restarted" 0 "" \
  MIN_PEERS=2 DATA_WEDGE_ENABLED=1 DATA_PROBE_TARGETS="$DEAD"

# --- restart budget decay ----------------------------------------------------
# Seeds a spent budget with a chosen last-restart age, then checks whether the
# watchdog is armed again. A boot_id that stays at restart_count == MAX for days
# is a watchdog silently disarmed.
budget_case() {
  local name="$1" expect="$2" age_s="$3" window="$4"
  local dir="$TMPROOT/b$RANDOM$RANDOM"; mkdir -p "$dir/state"
  printf '#!/usr/bin/env bash\ncase "$1" in status) cat <<%s\n%s\n%s\n;; esac\n' \
    "EOS" "$HEALTHY_STATUS" "EOS" > "$dir/netbird"; chmod +x "$dir/netbird"
  printf '#!/usr/bin/env bash\ntouch "%s/RESTARTED"\n' "$dir" > "$dir/restart"; chmod +x "$dir/restart"
  # Spent budget, last restart age_s ago, and the CURRENT boot_id so the
  # boot-reset path cannot be what clears it.
  echo 3 > "$dir/state/restart_count"
  echo $(( $(date +%s) - age_s )) > "$dir/state/last_restart"
  cat /proc/sys/kernel/random/boot_id 2>/dev/null > "$dir/state/boot_id" || echo unknown > "$dir/state/boot_id"
  local _i
  for _i in 1 2 3 4 5; do
    env NB_BIN="$dir/netbird" RESTART_CMD="$dir/restart" STATE_DIR="$dir/state" \
        METRICS_FILE="$dir/state/metrics.prom" PROBE_TIMEOUT_S=2 \
        MIN_PEERS=2 SUSTAINED_CHECKS=4 COOLDOWN_S=1 MAX_RESTARTS_PER_BOOT=3 \
        RESTART_BUDGET_WINDOW_S="$window" \
        DATA_WEDGE_ENABLED=1 DATA_PROBE_TARGETS="$DEAD" \
        bash "$SCRIPT" >/dev/null 2>&1
  done
  local got=0; [ -f "$dir/RESTARTED" ] && got=1
  if [ "$got" = "$expect" ]; then
    PASS=$((PASS+1)); printf '  ok   %s\n' "$name"
  else
    FAIL=$((FAIL+1)); printf '  FAIL %s (expected restart=%s, got=%s)\n' "$name" "$expect" "$got"
  fi
}

echo "restart budget must decay, or a long-uptime node disarms itself:"
budget_case "spent budget + last restart 9 DAYS ago ⇒ re-armed" 1 777600 86400
budget_case "spent budget + last restart 2h ago ⇒ still capped (anti-thrash)" 0 7200 86400
budget_case "decay disabled (window=0) ⇒ per-boot only, stays capped" 0 777600 0

echo
echo "passed=$PASS failed=$FAIL"
[ "$FAIL" -eq 0 ] || exit 1
