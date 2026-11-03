#!/usr/bin/env bash
# Parse-only smoke test for the oci-substrate-client role.
# Lint-level coverage; behavioral coverage is the live `mgmt-host.yml`
# deploy on vega-A (Block 75 verdict gate).

set -euo pipefail

cd "$(dirname "$0")"
ROLE_PATH=$(realpath "$(pwd)/../..")

OBJC_DISABLE_INITIALIZE_FORK_SAFETY=YES \
ANSIBLE_ROLES_PATH="$ROLE_PATH" \
ansible-playbook --syntax-check playbook.yml

echo "✓ oci-substrate-client role: syntax-check PASS"
