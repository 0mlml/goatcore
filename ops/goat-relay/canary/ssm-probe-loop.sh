#!/bin/bash
# ssm-probe-loop.sh — Step 4 wrapper for goat-relay-prod-canary.md.
#
# Periodic SSM probes against the three prod relays. Captures
# restartcount / oomkilled / panic-class log lines / 4h log volume.
# Run on the operator workstation with aws-vault usareur-af creds.
#
# Usage:
#   bash ssm-probe-loop.sh
#
# Optional:
#   PROBE_INTERVAL_S=14400   # 4h (default)
#   TARGET_END_S=86400       # 24h total (default)
#   OUT_DIR=/tmp/canary      # output dir (default /tmp/canary)
#
# Watch-for: restartcount increment / oomkilled=true / panic|fatal|OOM in
# logs / monotonic memory growth across snapshots.

set -euo pipefail

PROBE_INTERVAL_S="${PROBE_INTERVAL_S:-14400}"
TARGET_END_S="${TARGET_END_S:-86400}"
OUT_DIR="${OUT_DIR:-/tmp/canary}"
mkdir -p "$OUT_DIR"
LOG="$OUT_DIR/relay-probes.jsonl"

# Resolve instance IDs at run time so a relay recreate is picked up.
# Region per relay short-code — a case dispatch, not an associative array:
# this runs on the operator's macOS /bin/bash 3.2 (#227).
region_for() {
  case "$1" in
    fra) echo "eu-central-1" ;;
    isr) echo "il-central-1" ;;
    mum) echo "ap-south-1" ;;
    *)   echo "" ;;
  esac
}

probe_relay() {
  local name=$1
  local region
  region=$(region_for "$name")
  local iid
  iid=$(aws-vault exec usareur-af --no-session -- aws ec2 describe-instances \
    --region "$region" \
    --filters "Name=tag:Name,Values=usareur-af-prod-goat-relay-rewrite-$name" \
              "Name=instance-state-name,Values=running" \
    --query 'Reservations[].Instances[].InstanceId' --output text 2>/dev/null | head -1)
  if [ -z "$iid" ]; then
    echo "{\"ts\":\"$(date -u +%FT%TZ)\",\"relay\":\"$name\",\"err\":\"no_running_instance\"}" >> "$LOG"
    return
  fi
  # Docker shape per ADR 0709 D3 (current Phase 1 deploy). Switch to
  # systemctl show goat-relay when the systemd-native amendment ships.
  local cmd_id
  cmd_id=$(aws-vault exec usareur-af --no-session -- aws ssm send-command \
    --region "$region" --instance-ids "$iid" \
    --document-name AWS-RunShellScript \
    --parameters 'commands=["sudo docker inspect goat-relay --format \"started={{.State.StartedAt}} restartcount={{.RestartCount}} oomkilled={{.State.OOMKilled}} pid={{.State.Pid}}\"","sudo docker stats goat-relay --no-stream","sudo docker logs goat-relay --since 4h 2>&1 | grep -iE \"panic|fatal|oom|listener exit|killed|aborted\" | head -20","sudo docker logs goat-relay --since 4h 2>&1 | wc -l"]' \
    --output json 2>&1 | grep '"CommandId"' | sed -E 's/.*"CommandId": "([^"]+)".*/\1/')
  echo "{\"ts\":\"$(date -u +%FT%TZ)\",\"relay\":\"$name\",\"region\":\"$region\",\"instance_id\":\"$iid\",\"command_id\":\"$cmd_id\"}" >> "$LOG"
}

START=$(date -u +%s)
TARGET_END=$((START + TARGET_END_S))
i=0

echo "ssm-probe-loop: interval=${PROBE_INTERVAL_S}s until=$(date -u -r "$TARGET_END" +%FT%TZ) log=$LOG"

while [ "$(date -u +%s)" -lt "$TARGET_END" ]; do
  i=$((i+1))
  echo "ssm-probe-loop: cycle $i at $(date -u +%FT%TZ)"
  for name in fra isr mum; do
    probe_relay "$name" || true
  done
  sleep "$PROBE_INTERVAL_S"
done

echo "ssm-probe-loop: done. cycles=$i log=$LOG"
echo "Fetch command outputs with: aws ssm get-command-invocation --region <region> --command-id <id> --instance-id <iid>"
