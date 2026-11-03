#!/usr/bin/env bash
# tests/test-fixture.sh — ansible-bastion role local smoke test.
#
# Runs ansible-playbook --syntax-check against a minimal play that
# imports the role with two operator-roster shapes (empty + populated).
# Catches Jinja2 + variable-loop errors before they reach a real host.
#
# Skips cleanly if ansible-playbook isn't on PATH.
#
# Usage:  ops/ansible/roles/ansible-bastion/tests/test-fixture.sh
# Exit:   0 = green / SKIP; 1 = red.

set -euo pipefail

cd "$(dirname "$0")/../../../../.."   # repo root

if ! command -v ansible-playbook >/dev/null 2>&1; then
  echo "ansible-bastion test-fixture: SKIP (ansible-playbook not on PATH)"
  exit 0
fi

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

cat > "$TMPDIR/empty-roster.yml" <<'YAML'
---
- name: ansible-bastion role — empty operator roster
  hosts: localhost
  gather_facts: false
  connection: local
  roles:
    - role: ansible-bastion
      vars:
        ansible_bastion_operators: []
        ansible_bastion_known_hosts: []
        ansible_bastion_trunk_repo_url: ""
YAML

cat > "$TMPDIR/populated-roster.yml" <<'YAML'
---
- name: ansible-bastion role — populated operator roster
  hosts: localhost
  gather_facts: false
  connection: local
  roles:
    - role: ansible-bastion
      vars:
        ansible_bastion_operators:
          - name: alice
            email: alice@example.org
            ssh_pubkey: "ssh-ed25519 AAAA alice"
            comment: "test fixture operator"
          - name: bob
            email: bob@example.org
            ssh_pubkey: "ssh-ed25519 AAAA bob"
        ansible_bastion_known_hosts:
          - name: orin-a
            host: 192.168.1.132
            pubkey: "ssh-ed25519 AAAA orin-a"
        ansible_bastion_trunk_repo_url: "https://example.org/repo.git"
YAML

export ANSIBLE_ROLES_PATH="$PWD/ops/ansible/roles"

for f in empty-roster populated-roster; do
  if ! ansible-playbook \
        -i localhost, \
        --syntax-check \
        "$TMPDIR/$f.yml" \
        > "$TMPDIR/$f.log" 2>&1; then
    echo "ansible-bastion test-fixture: FAIL ($f)" >&2
    cat "$TMPDIR/$f.log" >&2
    exit 1
  fi
  echo "ansible-bastion test-fixture: PASS ($f syntax-check)"
done

echo "ansible-bastion test-fixture: GREEN (2/2)"
