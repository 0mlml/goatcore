#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
ROLE_PATH=$(realpath "$(pwd)/../..")
OBJC_DISABLE_INITIALIZE_FORK_SAFETY=YES \
ANSIBLE_ROLES_PATH="$ROLE_PATH" \
ansible-playbook --syntax-check playbook.yml
echo "✓ oci-substrate-t2-sync role: syntax-check PASS"
