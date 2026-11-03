#!/usr/bin/env bash
# Parse-only YAML + Jinja2 check for the operator-idp-hydra role.
# Per Block 50H Tier-B per-role syntax-check pattern.

set -euo pipefail

cd "$(dirname "$0")"

if ! command -v ansible-playbook >/dev/null 2>&1; then
  echo "[operator-idp-hydra/syntax-check] SKIP (ansible-playbook not on PATH)"
  exit 0
fi

echo "[operator-idp-hydra/syntax-check] ansible-playbook --syntax-check on parse-only fixture"
ansible-playbook -i 'localhost,' -c local playbook.yml --syntax-check

echo "[operator-idp-hydra/syntax-check] register-uniqueness lint (workspace)"
bash ../../../tests/check-register-uniqueness.sh

echo "[operator-idp-hydra/syntax-check] PASS"
