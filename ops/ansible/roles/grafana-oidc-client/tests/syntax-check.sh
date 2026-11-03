#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

if ! command -v ansible-playbook >/dev/null 2>&1; then
  echo "[grafana-oidc-client/syntax-check] SKIP (ansible-playbook not on PATH)"
  exit 0
fi

echo "[grafana-oidc-client/syntax-check] ansible-playbook --syntax-check"
ansible-playbook -i 'localhost,' -c local playbook.yml --syntax-check

echo "[grafana-oidc-client/syntax-check] register-uniqueness lint"
bash ../../../tests/check-register-uniqueness.sh

echo "[grafana-oidc-client/syntax-check] PASS"
