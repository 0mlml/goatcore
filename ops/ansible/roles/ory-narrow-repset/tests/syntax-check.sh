#!/usr/bin/env bash
# Parse-only smoke for ory-narrow-repset, per Block 50H role-test parity.
# Verifies the role's YAML + Jinja parse cleanly. Does NOT execute SQL or
# require a running Postgres / Docker; the test fixture sets
# `ory_spock_enabled: false` so the role short-circuits at the top of
# tasks/main.yml.
#
# Full 2-node integration test (with real postgres-spock containers +
# kratos + hydra + cross-site subscription verification) lives in
# run-test.sh — queued as a follow-on; today it's a stub.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROLE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
ROLES_DIR="$(cd "${ROLE_DIR}/.." && pwd)"

echo "[syntax-check] ory-narrow-repset role-dir: ${ROLE_DIR}"
echo "[syntax-check] running ansible-playbook --syntax-check ..."

ANSIBLE_ROLES_PATH="${ROLES_DIR}" \
  ansible-playbook \
    --syntax-check \
    -i "localhost," \
    --connection local \
    "${SCRIPT_DIR}/playbook.yml"

echo "[syntax-check] PASS"
