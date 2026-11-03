#!/usr/bin/env bash
# Parse-only smoke for the goat-zenoh role (Block 50H structural gate).

set -euo pipefail
cd "$(dirname "$0")"
if ! command -v ansible-playbook >/dev/null 2>&1; then
  echo "[goat-zenoh/syntax-check] SKIP (ansible-playbook not on PATH)"
  exit 0
fi
echo "[goat-zenoh/syntax-check] ansible-playbook --syntax-check"
ansible-playbook -i 'localhost,' -c local playbook.yml --syntax-check
echo "[goat-zenoh/syntax-check] PASS"
