#!/usr/bin/env bash
set -euo pipefail

# deploy-smoketest.sh — Wrapper around Ansible playbook.
#
# Usage:
#   bash ops/smoketest/deploy-smoketest.sh
#
# This script exists for convenience. The actual deployment logic is in:
#   ops/ansible/nethost-smoketest.yml

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Source credentials so NETBIRD_API_TOKEN is available to Ansible.
[[ -f "$REPO_ROOT/.envrc" ]] && source "$REPO_ROOT/.envrc"
[[ -f "$REPO_ROOT/.envrc.local" ]] && source "$REPO_ROOT/.envrc.local"

# Generate inventory if needed.
if [[ ! -f "$REPO_ROOT/ops/ansible/inventory/hosts.ini" ]]; then
    echo "Generating Ansible inventory..."
    bash "$REPO_ROOT/ops/ansible/inventory/generate.sh"
fi

cd "$REPO_ROOT/ops/ansible"
ansible-playbook nethost-smoketest.yml "$@"
