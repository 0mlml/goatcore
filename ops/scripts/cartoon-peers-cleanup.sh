#!/usr/bin/env bash
# cartoon-peers-cleanup.sh — Block 50I Tier-C cleanup helper.
#
# Cartoon peers auto-shutdown at +7d via systemd timer. This script handles
# the manual remainder:
#   1. terraform destroy (clears Terraform state for the 3 modules)
#   2. Remove cartoon entries from ops/enrollment/wg-cp0-bundle-allowlist.json
#   3. Re-push allowlist to relays so cartoon pubkeys are no longer accepted
#   4. Remove local bundle artifacts under ~/.goat/state/cartoon-peers/
#
# The setup-keys expire automatically at 7d so don't need explicit revoke.
#
# Usage:
#   ops/scripts/cartoon-peers-cleanup.sh
#
# Requires: aws-vault usareur-af profile, age identity (none needed for
# cleanup — only for issuing new bundles).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
ALLOWLIST="$REPO_ROOT/ops/enrollment/wg-cp0-bundle-allowlist.json"
STATE_DIR="$HOME/.goat/state/cartoon-peers"

echo "==> Step 1/4: terraform destroy (3 cartoon-peer modules)"
cd "$REPO_ROOT/terraform-prod"
aws-vault exec usareur-af --no-session -- terraform destroy \
  -target=module.cartoon_peer_huey \
  -target=module.cartoon_peer_dewey \
  -target=module.cartoon_peer_louie

echo "==> Step 2/4: remove cartoon entries from allowlist"
cd "$REPO_ROOT"
# Idempotent — uses jq to filter by name prefix.
tmp="$(mktemp)"
jq '[.[] | select(.name | startswith("cartoon-") | not)]' "$ALLOWLIST" > "$tmp"
mv "$tmp" "$ALLOWLIST"
git diff --stat "$ALLOWLIST"

echo "==> Step 3/4: push allowlist to 6 prod relays"
cd "$REPO_ROOT/ops/ansible"
OBJC_DISABLE_INITIALIZE_FORK_SAFETY=YES aws-vault exec usareur-af --no-session -- ansible-playbook \
  -i inventory/hosts-prod.ini \
  -i inventory/wg-cp0-prod.ini \
  --limit 'relay_servers:goat_relay_hosts:!relay-rld:!goat-relay-rld' \
  wg-cp0-bootstrap.yml \
  -e wg_cp0_bundle_allowlist_file=../enrollment/wg-cp0-bundle-allowlist.json

echo "==> Step 4/4: remove local bundle artifacts"
rm -rf "$STATE_DIR"

echo "==> done."
echo "    - 3 EC2 instances + EBS volumes destroyed"
echo "    - allowlist entries removed + relays updated"
echo "    - local bundle artifacts shredded"
echo "    - setup-keys expire automatically at TTL (7d from issuance)"
echo
echo "Don't forget: commit the allowlist change:"
echo "    git add ops/enrollment/wg-cp0-bundle-allowlist.json"
echo "    git commit -s -m 'chore(cartoon-peers): remove after 7-day TTL'"
