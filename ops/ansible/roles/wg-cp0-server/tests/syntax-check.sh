#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

if command -v ansible-playbook >/dev/null 2>&1; then
  echo "[wg-cp0-server/syntax-check] ansible-playbook --syntax-check"
  ansible-playbook -i 'localhost,' -c local playbook.yml --syntax-check
else
  echo "[wg-cp0-server/syntax-check] SKIP ansible-playbook (not on PATH)"
fi

# Block 22R.3 — classifier unit suite. Pure bash, runs anywhere.
./test-classify-conf-change.sh

echo "[wg-cp0-server/syntax-check] PASS"
