#!/usr/bin/env bash
# utility-bootstrap-syntax-check.sh
#
# Composite test for the orin-c-utility-host buildout. Runs:
#   1. ansible-playbook --syntax-check on the orchestrator
#   2. each role's per-role test-fixture.sh
#   3. the Block 12G ntp-hygiene render-verification harness
#
# Skips cleanly when ansible-playbook isn't on PATH (CI environments
# without ansible installed). Exit 0 on success, 1 on first failure.

set -euo pipefail

cd "$(dirname "$0")/../../.."   # repo root

if ! command -v ansible-playbook >/dev/null 2>&1; then
  echo "utility-bootstrap-syntax-check: SKIP (ansible-playbook not on PATH)"
  exit 0
fi

PLAYBOOK=ops/ansible/vega-utility-bootstrap.yml
HOSTS=ops/ansible/inventory/hosts.ini
MGMT=ops/ansible/inventory/mgmt-hosts.ini
MGMT_MESH=ops/ansible/inventory/mgmt-hosts-mesh.ini

for f in "$PLAYBOOK" "$HOSTS" "$MGMT" "$MGMT_MESH"; do
  if [[ ! -f "$f" ]]; then
    echo "utility-bootstrap-syntax-check: FAIL — missing file: $f" >&2
    exit 1
  fi
done

# 1. Orchestrator-level --syntax-check against the real inventory.
# Pre-populate FORGEJO_* env vars + chrony_serve_nts override so the
# pre-flight assertions don't trip during the parse.
export FORGEJO_SECRET_KEY="parse-time-placeholder-not-a-real-secret"
export FORGEJO_INTERNAL_TOKEN="parse-time-placeholder-not-a-real-secret"
export FORGEJO_DB_PASSWORD="parse-time-placeholder"

echo "==> orchestrator --syntax-check"
if ! ansible-playbook \
      -i "$HOSTS" \
      -i "$MGMT" \
      -i "$MGMT_MESH" \
      --syntax-check \
      "$PLAYBOOK" \
      -e chrony_serve_nts=false \
      > /tmp/utility-bootstrap-orchestrator.log 2>&1; then
  echo "FAIL — orchestrator --syntax-check"
  cat /tmp/utility-bootstrap-orchestrator.log
  exit 1
fi
echo "    PASS"

# 2. Each role's per-role test-fixture.
echo "==> per-role test fixtures"
for role in ansible-bastion apt-cacher-ng docker-registry-mirror forgejo ollama; do
  fixture="ops/ansible/roles/$role/tests/test-fixture.sh"
  if [[ ! -x "$fixture" ]]; then
    echo "    skip $role (no test-fixture.sh)"
    continue
  fi
  if ! "$fixture" > /tmp/role-fixture-$role.log 2>&1; then
    echo "    FAIL $role"
    cat /tmp/role-fixture-$role.log
    exit 1
  fi
  echo "    PASS $role"
done

# 3. Block 12G render verification.
echo "==> ntp-hygiene Block 12G render verification"
if ! ops/ansible/roles/ntp-hygiene/tests/test-block-12g.sh \
      > /tmp/role-fixture-ntp-hygiene.log 2>&1; then
  echo "    FAIL ntp-hygiene"
  cat /tmp/role-fixture-ntp-hygiene.log
  exit 1
fi
echo "    PASS"

echo
echo "utility-bootstrap-syntax-check: GREEN"
