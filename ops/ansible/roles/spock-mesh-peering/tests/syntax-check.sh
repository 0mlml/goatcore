#!/usr/bin/env bash
# Parse-only syntax + Jinja smoke for spock-mesh-peering. Cheap; safe to
# wire into CI. Catches YAML / Jinja / inventory errors without spinning
# up postgres-spock containers.
#
# Run from the role's tests/ directory:
#   ./syntax-check.sh

set -euo pipefail

cd "$(dirname "$0")"

echo "[syntax-check] ansible-playbook --syntax-check on test playbook"
ansible-playbook -i inventory.ini playbook.yml --syntax-check

if command -v ansible-lint >/dev/null 2>&1; then
  echo "[syntax-check] ansible-lint on the role"
  ansible-lint ../tasks/main.yml || true
else
  echo "[syntax-check] ansible-lint not installed; skipping"
fi

echo "[syntax-check] PASS"
