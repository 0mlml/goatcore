#!/usr/bin/env bash
# wg-cp0-syntax-check.sh — Block 22K.5 cheap CI gate.
#
# Catches YAML/Jinja2 syntax errors in wg-cp0-bootstrap.yml + the wg-cp0
# roles before they hit an Orin. Skips cleanly when ansible-playbook
# isn't on PATH so contributors without ansible installed can still
# `git commit` without a hook explosion.
#
# Usage:
#   ops/ansible/tests/wg-cp0-syntax-check.sh
#
# Exit codes:
#   0  syntax-check green (or ansible-playbook absent → SKIP)
#   1  syntax-check red

set -euo pipefail

cd "$(dirname "$0")/../../.."  # repo root: ops/ansible/tests → ops/ansible → ops → repo

if ! command -v ansible-playbook >/dev/null 2>&1; then
  echo "wg-cp0-syntax-check: SKIP (ansible-playbook not on PATH)"
  exit 0
fi

PLAYBOOKS=(
  ops/ansible/wg-cp0-bootstrap.yml
  ops/ansible/wg-cp0-rotate-bundles.yml
)
HOSTS_PROD=ops/ansible/inventory/hosts-prod.ini
MGMT=ops/ansible/inventory/mgmt-hosts.ini
WG_CP0=ops/ansible/inventory/wg-cp0-prod.ini

for f in "${PLAYBOOKS[@]}" "$HOSTS_PROD" "$MGMT" "$WG_CP0"; do
  if [[ ! -f "$f" ]]; then
    echo "wg-cp0-syntax-check: FAIL — missing file: $f" >&2
    exit 1
  fi
done

# --syntax-check parses YAML + Jinja2 without connecting to any host.
# The vars passed via -e are needed for the bundle-flow conditionals to
# evaluate cleanly during the dry parse — empty defaults are fine, no
# network or filesystem side-effects occur in syntax-check mode.
for PB in "${PLAYBOOKS[@]}"; do
  echo "wg-cp0-syntax-check: parsing $PB ..."
  ansible-playbook \
    -i "$HOSTS_PROD" \
    -i "$MGMT" \
    -i "$WG_CP0" \
    --syntax-check \
    -e wg_cp0_bundle_path= \
    -e wg_cp0_ca_pubkey_local_path= \
    -e wg_cp0_bundle_allowlist_file= \
    -e ca_id=test-ca \
    -e ca_key_path=/dev/null \
    -e age_identity_path=/dev/null \
    -e wg_cp0_allowlist_file=/dev/null \
    "$PB"
done

echo "wg-cp0-syntax-check: PASS"
