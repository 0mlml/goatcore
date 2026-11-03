#!/usr/bin/env bash
set -euo pipefail

# goat — Peer cleanup and audit
#
# Detects stale, duplicate, and unnamed peers on the Netbird mesh.
# Can delete stale duplicates via the API.
#
# Prerequisites:
#   - NETBIRD_API_TOKEN set (PAT from dashboard)
#   - NETBIRD_DOMAIN set (default: netbird.legacy.example)
#   - curl, python3 available
#
# Usage:
#   bash ops/peer-cleanup.sh --audit        Show all peers with status and age
#   bash ops/peer-cleanup.sh --duplicates   Find duplicate hostnames (different peer IDs)
#   bash ops/peer-cleanup.sh --stale [days] Show peers disconnected longer than N days (default: 7)
#   bash ops/peer-cleanup.sh --auto         Delete stale duplicates (safe: never deletes the last peer with a name)
#   bash ops/peer-cleanup.sh --delete <id>  Delete a specific peer by ID

DOMAIN="${NETBIRD_DOMAIN:-netbird.legacy.example}"
API_URL="https://${DOMAIN}/api"
STALE_DAYS=7

: "${NETBIRD_API_TOKEN:?Set NETBIRD_API_TOKEN (create a PAT in the Netbird dashboard: Users → Personal Access Tokens)}"

fetch_peers() {
  curl -sk -H "Authorization: Token $NETBIRD_API_TOKEN" \
    -H "Accept: application/json" \
    "$API_URL/peers" 2>/dev/null
}

delete_peer() {
  local peer_id="$1"
  curl -sk -X DELETE \
    -H "Authorization: Token $NETBIRD_API_TOKEN" \
    "$API_URL/peers/$peer_id" 2>/dev/null
}

cmd_audit() {
  local peers
  peers=$(fetch_peers)

  echo "$peers" | python3 -c "
import sys, json
from datetime import datetime, timezone

data = json.load(sys.stdin)
now = datetime.now(timezone.utc)

print(f'{'NAME':<30} {'IP':<18} {'STATUS':<12} {'LAST SEEN':<22} {'ID'}')
print('-' * 110)

for peer in sorted(data, key=lambda p: p.get('name', '')):
    name = peer.get('name', '(unnamed)')
    ip = peer.get('ip', '').split('/')[0]
    connected = peer.get('connected', False)
    status = 'connected' if connected else 'disconnected'
    peer_id = peer.get('id', '')

    last_seen = peer.get('last_seen', '')
    if last_seen:
        try:
            ls = datetime.fromisoformat(last_seen.replace('Z', '+00:00'))
            age = now - ls
            days = age.days
            if days > 0:
                last_str = f'{days}d ago ({ls.strftime(\"%Y-%m-%d\")})'
            else:
                hours = age.seconds // 3600
                last_str = f'{hours}h ago' if hours > 0 else 'just now'
        except (ValueError, TypeError):
            last_str = last_seen[:19]
    else:
        last_str = 'never'

    print(f'{name:<30} {ip:<18} {status:<12} {last_str:<22} {peer_id}')

print(f'\nTotal: {len(data)} peers')
"
}

cmd_duplicates() {
  local peers
  peers=$(fetch_peers)

  echo "$peers" | python3 -c "
import sys, json
from collections import defaultdict
from datetime import datetime, timezone

data = json.load(sys.stdin)
now = datetime.now(timezone.utc)

by_name = defaultdict(list)
for peer in data:
    name = peer.get('name', '').lower().replace(' ', '-')
    if name:
        by_name[name].append(peer)

found = False
for name, peers in sorted(by_name.items()):
    if len(peers) < 2:
        continue
    found = True
    print(f'DUPLICATE: {name} ({len(peers)} entries)')
    for p in peers:
        ip = p.get('ip', '').split('/')[0]
        connected = p.get('connected', False)
        status = 'connected' if connected else 'disconnected'
        peer_id = p.get('id', '')
        last_seen = p.get('last_seen', '')
        if last_seen:
            try:
                ls = datetime.fromisoformat(last_seen.replace('Z', '+00:00'))
                age_days = (now - ls).days
                last_str = f'{age_days}d ago' if age_days > 0 else 'recent'
            except (ValueError, TypeError):
                last_str = '?'
        else:
            last_str = 'never'
        print(f'  {ip:<18} {status:<12} last_seen={last_str:<10} id={peer_id}')
    print()

if not found:
    print('No duplicates found.')
"
}

cmd_stale() {
  local days="${1:-$STALE_DAYS}"
  local peers
  peers=$(fetch_peers)

  echo "$peers" | python3 -c "
import sys, json
from datetime import datetime, timezone, timedelta

data = json.load(sys.stdin)
now = datetime.now(timezone.utc)
threshold = now - timedelta(days=$days)

print(f'Peers disconnected for more than $days days:')
print(f'{'NAME':<30} {'IP':<18} {'LAST SEEN':<22} {'ID'}')
print('-' * 100)

count = 0
for peer in sorted(data, key=lambda p: p.get('last_seen', '')):
    if peer.get('connected', False):
        continue
    last_seen = peer.get('last_seen', '')
    if not last_seen:
        continue
    try:
        ls = datetime.fromisoformat(last_seen.replace('Z', '+00:00'))
    except (ValueError, TypeError):
        continue
    if ls < threshold:
        name = peer.get('name', '(unnamed)')
        ip = peer.get('ip', '').split('/')[0]
        peer_id = peer.get('id', '')
        age_days = (now - ls).days
        print(f'{name:<30} {ip:<18} {age_days}d ago{\"\":<14} {peer_id}')
        count += 1

if count == 0:
    print('  (none)')
else:
    print(f'\n{count} stale peer(s). Delete with: bash ops/peer-cleanup.sh --delete <id>')
"
}

cmd_auto() {
  local peers
  peers=$(fetch_peers)

  echo "$peers" | python3 -c "
import sys, json
from collections import defaultdict
from datetime import datetime, timezone, timedelta

data = json.load(sys.stdin)
now = datetime.now(timezone.utc)
threshold = now - timedelta(days=$STALE_DAYS)

by_name = defaultdict(list)
for peer in data:
    name = peer.get('name', '').lower().replace(' ', '-')
    if name:
        by_name[name].append(peer)

to_delete = []
for name, peers in by_name.items():
    if len(peers) < 2:
        continue
    # Sort: connected first, then by last_seen descending
    peers.sort(key=lambda p: (
        not p.get('connected', False),
        p.get('last_seen', '') or ''
    ), reverse=True)

    # Keep the first (most recent / connected), consider deleting the rest
    for stale in peers[1:]:
        if stale.get('connected', False):
            continue  # never delete a connected peer
        last_seen = stale.get('last_seen', '')
        if not last_seen:
            continue
        try:
            ls = datetime.fromisoformat(last_seen.replace('Z', '+00:00'))
        except (ValueError, TypeError):
            continue
        if ls < threshold:
            to_delete.append({
                'id': stale.get('id', ''),
                'name': stale.get('name', ''),
                'ip': stale.get('ip', '').split('/')[0],
                'age_days': (now - ls).days
            })

for d in to_delete:
    print(f'DELETE:{d[\"id\"]}:{d[\"name\"]}:{d[\"ip\"]}:{d[\"age_days\"]}d')

if not to_delete:
    print('NONE')
" | while IFS= read -r line; do
    if [ "$line" = "NONE" ]; then
      echo "$(date): No stale duplicates to clean up."
      return
    fi
    local peer_id name ip age
    peer_id=$(echo "$line" | cut -d: -f2)
    name=$(echo "$line" | cut -d: -f3)
    ip=$(echo "$line" | cut -d: -f4)
    age=$(echo "$line" | cut -d: -f5)

    echo "$(date): Deleting stale duplicate: $name ($ip, $age stale) id=$peer_id"
    delete_peer "$peer_id"
  done
}

cmd_delete() {
  local peer_id="${1:?Usage: peer-cleanup.sh --delete <peer-id>}"
  echo "Deleting peer $peer_id..."
  delete_peer "$peer_id"
  echo "Done."
}

case "${1:---audit}" in
  --audit)      cmd_audit ;;
  --duplicates) cmd_duplicates ;;
  --stale)      cmd_stale "${2:-}" ;;
  --auto)       cmd_auto ;;
  --delete)     cmd_delete "${2:-}" ;;
  -h|--help)
    echo "Usage: peer-cleanup.sh [--audit|--duplicates|--stale [days]|--auto|--delete <id>]"
    echo ""
    echo "  --audit        Show all peers with status and age"
    echo "  --duplicates   Find duplicate hostnames (different peer IDs)"
    echo "  --stale [days] Show peers disconnected longer than N days (default: 7)"
    echo "  --auto         Delete stale duplicates (never deletes last peer with a name)"
    echo "  --delete <id>  Delete a specific peer by ID"
    ;;
  *)
    echo "Unknown option: $1 (try --help)"
    exit 1
    ;;
esac
