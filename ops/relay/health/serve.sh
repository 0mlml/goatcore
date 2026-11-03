#!/usr/bin/env bash
# serve.sh — re-probe the relay fleet on a loop and serve the dashboard.
# The laptop / outage-time path: works while the mesh is down (probes relay
# public IPs directly). Ctrl-C stops both the loop and the server.
#
# Usage: ./serve.sh [--deep] [--port 8791] [--interval 15]
#
# --deep needs AWS creds (SSM into the relays). To avoid the ~70-keychain-
# prompts-per-pass foot-gun, the ENTIRE loop runs under ONE aws-vault session:
# if you pass --deep without ambient AWS creds, serve.sh re-execs itself once
# under `aws-vault exec` (you're prompted ONCE, not per probe). Override the
# profile with GOAT_RELAY_HEALTH_AWS_PROFILE (see default below).
set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PORT=8791; INTERVAL=15; DEEP=""
while [ $# -gt 0 ]; do
  case "$1" in
    --deep) DEEP="--deep" ;;
    --port) PORT="$2"; shift ;;
    --interval) INTERVAL="$2"; shift ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
  shift
done

# One-time aws-vault wrap for the whole --deep session (single keychain unlock).
# Skipped when creds are already ambient (env vars or an EC2 instance profile —
# the automation path), or when already re-exec'd.
if [ -n "$DEEP" ] && [ -z "${AWS_ACCESS_KEY_ID:-}" ] && [ -z "${_RH_AWSVAULT_WRAPPED:-}" ] \
   && [ -z "${GOAT_RELAY_HEALTH_AWS_VAULT_PROFILE:-}" ]; then
  PROFILE="${GOAT_RELAY_HEALTH_AWS_PROFILE:-usareur-af}"
  if command -v aws-vault >/dev/null 2>&1 \
     && ! aws sts get-caller-identity >/dev/null 2>&1; then
    echo "relay-health: --deep needs AWS creds — wrapping the whole session in ONE aws-vault exec ($PROFILE); you'll be prompted once." >&2
    export _RH_AWSVAULT_WRAPPED=1
    exec aws-vault exec "$PROFILE" --no-session -- "$0" --deep --port "$PORT" --interval "$INTERVAL"
  fi
fi

cd "$HERE" || exit 1
( while true; do
    "$HERE/relay-health-probe.sh" $DEEP >/dev/null 2>&1 || true
    # persist the overall status for the top-of-page history timeline
    "$HERE/relay-status-record.py" --relay-health "$HERE/relay-health.json" \
      --peers "$HERE/ingest/peers.json" --db "$HERE/status.db" \
      --out "$HERE/status-history.json" >/dev/null 2>&1 || true
    sleep "$INTERVAL"
  done ) &
LOOP_PID=$!
trap 'kill $LOOP_PID 2>/dev/null; exit 0' INT TERM

echo "relay-health: probing every ${INTERVAL}s ${DEEP:+(deep) }→ http://127.0.0.1:${PORT}/relay-health.html"
python3 -m http.server "$PORT" --bind 127.0.0.1
