#!/usr/bin/env bash
set -euo pipefail

# goat — Publish a service to mesh DNS
#
# Adds a DNS entry for a service, pushes to the NETHOST, and reloads CoreDNS.
# Idempotent — safe to run multiple times with the same arguments.
#
# Run from LOCAL:
#   bash ops/dns/publish-service.sh demogame
#   bash ops/dns/publish-service.sh --remove demogame
#
# IP defaults to NETHOST_HOST (all services route through the NETHOST reverse proxy).
# Override with a second arg only if the service runs on a different node.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(dirname "$(dirname "$SCRIPT_DIR")")"
HOSTS_FILE="$SCRIPT_DIR/mesh-hosts"
DOMAIN="netbird.legacy.example"
REMOTE_HOSTS_PATH="/opt/goat/dns/mesh-hosts"
REMOTE_DNS_DIR="/opt/goat/dns"

# Source credentials
if [ -f "$REPO_DIR/.envrc" ]; then
  set -a
  source "$REPO_DIR/.envrc" 2>/dev/null || true
  [ -f "$REPO_DIR/.envrc.local" ] && source "$REPO_DIR/.envrc.local" 2>/dev/null || true
  set +a
fi

: "${NETHOST_HOST:?Set NETHOST_HOST}"
: "${NETHOST_USER:?Set NETHOST_USER}"

SSH_TARGET="$NETHOST_USER@$NETHOST_HOST"

run_remote() {
  if [ -n "${NETHOST_PASS:-}" ]; then
    sshpass -p "$NETHOST_PASS" ssh -o StrictHostKeyChecking=no "$SSH_TARGET" "$1"
  else
    ssh -o StrictHostKeyChecking=no "$SSH_TARGET" "$1"
  fi
}

scp_to_remote() {
  if [ -n "${NETHOST_PASS:-}" ]; then
    sshpass -p "$NETHOST_PASS" scp -o StrictHostKeyChecking=no "$1" "$SSH_TARGET:$2"
  else
    scp -o StrictHostKeyChecking=no "$1" "$SSH_TARGET:$2"
  fi
}

sudo_remote() {
  if [ -n "${NETHOST_PASS:-}" ]; then
    run_remote "echo '$NETHOST_PASS' | sudo -S $1"
  else
    run_remote "sudo $1"
  fi
}

# Parse arguments
REMOVE=false
if [ "${1:-}" = "--remove" ]; then
  REMOVE=true
  shift
fi

NAME="${1:-}"
IP="${2:-$NETHOST_HOST}"

if [ -z "$NAME" ]; then
  echo "Usage:"
  echo "  publish-service.sh <name> [ip]             Add/update a service (IP defaults to NETHOST_HOST)"
  echo "  publish-service.sh --remove <name>          Remove a service"
  echo ""
  echo "Examples:"
  echo "  publish-service.sh demogame"
  echo "  publish-service.sh --remove demogame"
  exit 1
fi

FQDN="$NAME.$DOMAIN"

if $REMOVE; then
  if grep -q "$FQDN" "$HOSTS_FILE" 2>/dev/null; then
    sed -i '' "/$FQDN/d" "$HOSTS_FILE"
    echo "Removed $FQDN from $HOSTS_FILE"
  else
    echo "$FQDN not found in $HOSTS_FILE — nothing to remove"
    exit 0
  fi
else
  ENTRY="$IP  $FQDN"

  # Check if already exists with same IP
  if grep -q "^$IP.*$FQDN" "$HOSTS_FILE" 2>/dev/null; then
    echo "$FQDN already points to $IP — no change needed"
  else
    # Remove old entry for this name if IP changed
    sed -i '' "/$FQDN/d" "$HOSTS_FILE" 2>/dev/null || true
    echo "$ENTRY" >> "$HOSTS_FILE"
    echo "Added: $ENTRY"
  fi
fi

# Push to NETHOST and reload
echo "Pushing to $SSH_TARGET..."
scp_to_remote "$HOSTS_FILE" "/tmp/mesh-hosts"
sudo_remote "cp /tmp/mesh-hosts $REMOTE_HOSTS_PATH"
run_remote "cd $REMOTE_DNS_DIR && echo '${NETHOST_PASS:-}' | sudo -S docker compose restart coredns 2>/dev/null"
echo "NETHOST coredns reloaded."

# Push to VPS relays (redundant DNS)
# Relay metadata sourced from ops/relay-env.sh (single source of truth).
# SSH keys are fetched by inventory/generate.sh and stored in $RELAY_KEY_DIR.
source "$REPO_DIR/ops/relay-env.sh"

for i in "${!RELAY_IPS[@]}"; do
  rip="${RELAY_IPS[$i]}"
  rname="${RELAY_NAMES[$i]}"
  keyfile="$RELAY_KEY_DIR/relay-${rname}.pem"

  ssh_opts="-o StrictHostKeyChecking=no -o ConnectTimeout=5"
  if [ -f "$keyfile" ]; then
    ssh_opts="$ssh_opts -i $keyfile"
  fi

  echo "Pushing to relay-$rname ($rip)..."
  if scp $ssh_opts "$HOSTS_FILE" "ubuntu@$rip:/tmp/mesh-hosts" 2>/dev/null; then
    ssh $ssh_opts "ubuntu@$rip" \
      "sudo cp /tmp/mesh-hosts $RELAY_HOSTS_PATH && sudo systemctl restart dnsmasq" 2>/dev/null \
      && echo "  relay-$rname dnsmasq reloaded." \
      || echo "  relay-$rname: mesh-hosts copied but dnsmasq restart failed (may not be installed yet)"
  else
    echo "  relay-$rname: unreachable (run inventory/generate.sh to fetch SSH keys)"
  fi
done

echo ""
if $REMOVE; then
  echo "Removed: $NAME"
else
  echo "Published: https://$FQDN (resolves to $IP on the mesh)"
fi
