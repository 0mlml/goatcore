#!/usr/bin/env bash
# goat-zenoh-portal-store: the cutover and rollback playbooks parse, and the
# portal env and compose render the right store input (render-test.yml,
# localhost only).
set -euo pipefail
cd "$(dirname "$0")"
if ! command -v ansible-playbook >/dev/null 2>&1; then
  echo "[goat-zenoh-portal-store/syntax-check] SKIP (ansible-playbook not on PATH)"
  exit 0
fi
A="$(cd ../../.. && pwd)"
echo "[goat-zenoh-portal-store/syntax-check] ansible-playbook --syntax-check (cutover, rollback)"
( cd "$A" && ansible-playbook -i 'localhost,' -e target=localhost --syntax-check \
    portal-postgres-cutover.yml portal-postgres-rollback.yml )
echo "[goat-zenoh-portal-store/syntax-check] render (sqlite unchanged, postgres DSN, password encoding)"
ANSIBLE_ROLES_PATH="$A/roles" ansible-playbook -i 'localhost,' -c local render-test.yml
echo "[goat-zenoh-portal-store/syntax-check] PASS"
