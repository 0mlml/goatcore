#!/usr/bin/env bash
# Parse-only check for the secret-bearing canary (its plays are checked by
# roles/e2e-canary/tests/syntax-check.sh).
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "$(cd "$HERE/../../.." && pwd)"
ansible-playbook --syntax-check "$HERE/playbook.yml" >/dev/null
echo "e2e-canary-secret: syntax ok"
