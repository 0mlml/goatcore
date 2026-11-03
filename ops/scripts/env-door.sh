#!/usr/bin/env bash
# env-door.sh — open a mesh-independent door into one goatnet's mgmt host.
#
# Design: docs/design/env-doors.md (ADR 1117). Tier 1: SSM port-forward via
# the env's cloud mesh peer; SSH authenticates end-to-end with YOUR key, the
# cloud peer only moves ciphertext. The laptop's netbird profile is not
# touched — run one door per env, in parallel, from any session.
#
# usage:
#   ops/scripts/env-door.sh <env>            # open the tunnel (foreground; Ctrl-C closes)
#   ops/scripts/env-door.sh <env> --print    # print ssh/ansible usage, don't connect
#
# envs: zagros | zagros-prod
set -euo pipefail

ENV="${1:-}"
MODE="${2:-run}"

case "$ENV" in
  zagros)
    INSTANCE=i-0000083a640499a93   # mesh-dns-zagros-a (ZAGROS mesh peer, SSM)
    REGION=eu-central-1
    TARGET_IP=100.127.0.179       # mgmt-zagros-01-thor
    TARGET_NAME=mgmt-zagros-01-thor
    PORT=12201
    DOOR_USER=goat
    ;;
  zagros-prod)
    INSTANCE=i-000002bff6114fc6b   # relay-health-zagros-prod-a (zagros-prod mesh peer, SSM)
    REGION=eu-central-1
    TARGET_IP=100.127.0.221         # mgmt-zagros-prod-01-orin
    TARGET_NAME=mgmt-zagros-prod-01-orin
    PORT=12202
    DOOR_USER=goat
    ;;
  uk)
    INSTANCE=i-0000083a640499a93
    REGION=eu-central-1
    TARGET_IP=100.127.0.179   # mgmt-zagros-01-thor
    TARGET_NAME=mgmt-zagros-01-thor
    PORT=12201
    DOOR_USER=goat
    ;;
  zagros-dev)
    INSTANCE=i-00000f9f89849d917
    REGION=eu-central-1
    TARGET_IP=100.127.0.152   # mgmt-zagros-dev-01-thor
    TARGET_NAME=mgmt-zagros-dev-01-thor
    PORT=12203
    DOOR_USER=goat
    ;;
  *)
    echo "usage: $0 <zagros|zagros-prod> [--print|--ssh-target]" >&2
    exit 1
    ;;
esac

# --ssh-target: the one line a caller needs to run something on the host through
# an OPEN door (door-open.sh <env> first): user@127.0.0.1:port. The release
# driver's rows use it (goatnet-runners.yaml `goat_via: door:<env>`).
if [ "$MODE" = "--ssh-target" ]; then printf '%s@127.0.0.1:%s\n' "$DOOR_USER" "$PORT"; exit 0; fi

cat <<EOF
# door: $ENV -> $TARGET_NAME ($TARGET_IP) via $INSTANCE, local port $PORT
#
# interactive:
#   ssh -p $PORT $DOOR_USER@127.0.0.1
# fleet (any $ENV mesh peer, jumped through the mgmt host):
#   ssh -o ProxyJump=$DOOR_USER@127.0.0.1:$PORT $DOOR_USER@<peer-mesh-ip>
# ansible, host-level play (no site group_vars — wireguard-kmod.yml et al):
#   ansible-playbook -i inventory/doors.ini <play>.yml
# ansible, SERVICE deploy that templates from group_vars:
#   ansible-playbook -i inventory/mgmt-hosts.ini -i inventory/doors-groupvars.ini \\
#     <play>.yml -e target=${ENV//-/_}_hosts
#   (base inventory first for group membership + router_role; the door file
#    overrides transport only. Do NOT also pass mgmt-hosts-mesh.ini.)
# host-key churn note: pair with
#   -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null
EOF

# One-fact-one-home assert: this script decides the door's port and user, and
# two inventories restate them. A door whose inventories disagree does not fail
# loudly — it produces "unreachable", or worse, connects somewhere else. Refuse
# to open rather than let the operator debug a transport that was never going to
# work. Both files are checked because a fix applied to one is the likely drift.
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
door_drift=0
for inv in "${REPO_ROOT}/ansible/inventory/doors.ini" \
           "${REPO_ROOT}/ansible/inventory/doors-groupvars.ini"; do
  [ -f "$inv" ] || { echo "env-door: missing inventory $inv" >&2; door_drift=1; continue; }
  if ! grep -q "^${TARGET_NAME} .*ansible_port=${PORT} .*ansible_user=${DOOR_USER}" "$inv"; then
    echo "env-door: $(basename "$inv") does not carry ${TARGET_NAME} on port ${PORT} as ${DOOR_USER}." >&2
    door_drift=1
  fi
done
if [ "$door_drift" != 0 ]; then
  echo "env-door: REFUSING to open — the door facts here and in the inventories disagree." >&2
  echo "          This script is where the port/user are decided; fix the inventory to match." >&2
  exit 1
fi

[ "$MODE" = "--print" ] && exit 0

command -v session-manager-plugin >/dev/null || {
  echo "session-manager-plugin not found (brew install --cask session-manager-plugin)" >&2
  exit 1
}

exec "$(dirname "$0")/with-aws" usareur-af -- \
  aws ssm start-session \
    --target "$INSTANCE" \
    --region "$REGION" \
    --document-name AWS-StartPortForwardingSessionToRemoteHost \
    --parameters "{\"host\":[\"$TARGET_IP\"],\"portNumber\":[\"22\"],\"localPortNumber\":[\"$PORT\"]}"
