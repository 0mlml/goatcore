#!/usr/bin/env bash
# Parse-only YAML + Jinja2 check for the operator-idp-postgres role.
# Per Block 50H Tier-B per-role syntax-check pattern.
#
# Coverage: structural (catches YAML/Jinja2 errors + register-name
# collision via the workspace lint). Behavioral coverage (real
# pg_isready, dump round-trip) is the operator-fired 21H.1 bringup
# smoke against a real Orin-C.

set -euo pipefail

cd "$(dirname "$0")"

if ! command -v ansible-playbook >/dev/null 2>&1; then
  echo "[operator-idp-postgres/syntax-check] SKIP (ansible-playbook not on PATH)"
  exit 0
fi

echo "[operator-idp-postgres/syntax-check] ansible-playbook --syntax-check on parse-only fixture"
ansible-playbook -i 'localhost,' -c local playbook.yml --syntax-check

echo "[operator-idp-postgres/syntax-check] register-uniqueness lint (workspace)"
bash ../../../tests/check-register-uniqueness.sh

echo "[operator-idp-postgres/syntax-check] PASS"
