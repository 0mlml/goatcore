#!/usr/bin/env bash
# id-admin-console-store: the cutover and rollback playbooks parse, and the
# console env renders the right store keys (render-test.yml, localhost only).
set -euo pipefail
cd "$(dirname "$0")"
if ! command -v ansible-playbook >/dev/null 2>&1; then
  echo "[id-admin-console-store/syntax-check] SKIP (ansible-playbook not on PATH)"
  exit 0
fi
A="$(cd ../../.. && pwd)"
echo "[id-admin-console-store/syntax-check] ansible-playbook --syntax-check (cutover, rollback)"
( cd "$A" && ansible-playbook -i 'localhost,' -e target=localhost --syntax-check \
    id-console-postgres-cutover.yml id-console-postgres-rollback.yml )
echo "[id-admin-console-store/syntax-check] env render (sqlite unchanged, postgres DSNs, password encoding)"
ANSIBLE_ROLES_PATH="$A/roles" ansible-playbook -i 'localhost,' -c local render-test.yml
echo "[id-admin-console-store/syntax-check] PASS"
