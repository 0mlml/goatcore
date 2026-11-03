#!/usr/bin/env bash
# pulse.sh — TCP/UDP connectivity pulse for mesh troubleshooting
#
# Sends a timestamped, sequenced message to a target host at a fixed
# interval. Logs success/failure with enough detail to diagnose whether
# the problem is routing, firewall, or listener availability.
#
# Usage:
#   bash ops/pulse.sh [--udp] <target_ip> [port] [interval_sec]
#
# Examples:
#   bash ops/pulse.sh 100.127.1.22                  # TCP, port 4444, every 5s
#   bash ops/pulse.sh 100.127.1.22 9000             # TCP, port 9000, every 5s
#   bash ops/pulse.sh 100.127.1.22 4444 2           # TCP, port 4444, every 2s
#   bash ops/pulse.sh --udp 100.127.1.22 5453       # UDP, port 5453, every 5s
#   bash ops/pulse.sh --udp 100.127.1.22 5453 2     # UDP, port 5453, every 2s
#
# Receiver (run on target machine):
#   TCP:  ncat -lk -p 4444                 # Windows (nmap), Linux, macOS
#         nc -lk -p 4444                   # Linux (GNU netcat)
#         while true; do nc -l 4444; done  # macOS / BSD netcat
#   UDP:  nc -luk -p 5453                  # Linux
#         ncat -luk -p 5453                # Windows (nmap)
#
# What to look for:
#   - All FAIL, "Connection refused"  -> listener not running on target
#   - All FAIL, "timed out"           -> routing or firewall blocking traffic
#   - Intermittent FAIL               -> unstable link, check wireguard handshake
#   - Sequence gaps on receiver       -> sends succeed but data lost in transit
#   - UDP always shows OK             -> UDP is fire-and-forget; check receiver for gaps
#
# Exit codes:
#   0 = user interrupted (CTRL+C)
#   1 = missing dependencies or bad arguments

set -euo pipefail

# --- Arguments -----------------------------------------------------------

PROTO="tcp"
if [[ "${1:-}" == "--udp" ]]; then
    PROTO="udp"
    shift
fi

TARGET_IP="${1:-}"
TARGET_PORT="${2:-4444}"
INTERVAL="${3:-5}"

if [[ -z "$TARGET_IP" ]]; then
    echo "Usage: bash ops/pulse.sh [--udp] <target_ip> [port] [interval_sec]"
    exit 1
fi

# --- State ----------------------------------------------------------------

SEQ=0
PASS=0
FAIL=0

cleanup() {
    echo ""
    echo "--- Summary ---"
    echo "Sent: $SEQ  Success: $PASS  Failed: $FAIL"
    if [[ $SEQ -gt 0 ]]; then
        echo "Loss: $(( (FAIL * 100) / SEQ ))%"
    fi
    exit 0
}
trap cleanup INT TERM

# --- UDP mode (bash built-in, no nc needed) -------------------------------

if [[ "$PROTO" == "udp" ]]; then
    echo "Pulsing $TARGET_IP:$TARGET_PORT every ${INTERVAL}s (UDP via /dev/udp)"
    echo "Note: UDP is fire-and-forget. OK means sent, not received. Check receiver for gaps."
    echo "Press CTRL+C to stop."
    echo "---"

    while true; do
        SEQ=$((SEQ + 1))
        TS=$(date +'%H:%M:%S')
        PAYLOAD="hello from $(hostname) in seq $SEQ over the CUN-T"

        if echo "$PAYLOAD" > /dev/udp/"$TARGET_IP"/"$TARGET_PORT" 2>/dev/null; then
            PASS=$((PASS + 1))
            echo "[$TS] #$SEQ OK"
        else
            FAIL=$((FAIL + 1))
            echo "[$TS] #$SEQ FAIL"
        fi

        sleep "$INTERVAL"
    done
fi

# --- TCP mode -------------------------------------------------------------

if command -v nc &>/dev/null; then
    NC_CMD="nc"
elif command -v ncat &>/dev/null; then
    NC_CMD="ncat"
else
    echo "Error: nc (netcat) or ncat not found"
    exit 1
fi

# Detect timeout flag: -w is POSIX/Linux, -G is macOS
NC_TIMEOUT="-w 2"
if [[ "$(uname)" == "Darwin" ]]; then
    NC_TIMEOUT="-G 2"
fi

echo "Pulsing $TARGET_IP:$TARGET_PORT every ${INTERVAL}s (TCP via $NC_CMD)"
echo "Press CTRL+C to stop."
echo "---"

while true; do
    SEQ=$((SEQ + 1))
    TS=$(date +'%H:%M:%S')
    PAYLOAD="hello from $(hostname) in seq $SEQ over the CUN-T"

    # Send and capture stderr for diagnostics on failure
    if echo "$PAYLOAD" | $NC_CMD $NC_TIMEOUT "$TARGET_IP" "$TARGET_PORT" 2>/tmp/pulse_err.$$; then
        PASS=$((PASS + 1))
        echo "[$TS] #$SEQ OK"
    else
        FAIL=$((FAIL + 1))
        ERR=$(cat /tmp/pulse_err.$$ 2>/dev/null || true)
        # Trim to the useful part
        ERR=${ERR##*: }
        echo "[$TS] #$SEQ FAIL${ERR:+ ($ERR)}"
    fi

    sleep "$INTERVAL"
done
