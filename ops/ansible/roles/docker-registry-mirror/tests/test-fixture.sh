#!/usr/bin/env bash
# tests/test-fixture.sh — docker-registry-mirror role local smoke test.
# Skips cleanly if ansible-playbook isn't on PATH.

set -euo pipefail

cd "$(dirname "$0")/../../../../.."

if ! command -v ansible-playbook >/dev/null 2>&1; then
  echo "docker-registry-mirror test-fixture: SKIP (ansible-playbook not on PATH)"
  exit 0
fi

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

cat > "$TMPDIR/default.yml" <<'YAML'
---
- name: docker-registry-mirror — defaults
  hosts: localhost
  gather_facts: false
  connection: local
  roles:
    - role: docker-registry-mirror
YAML

cat > "$TMPDIR/single-upstream.yml" <<'YAML'
---
- name: docker-registry-mirror — single upstream + read-only
  hosts: localhost
  gather_facts: false
  connection: local
  roles:
    - role: docker-registry-mirror
      vars:
        docker_registry_mirror_upstreams:
          - name: dockerhub
            upstream: "https://registry-1.docker.io"
            host_port: 5000
            bind: "192.168.1.253"
        docker_registry_mirror_storage_read_only: true
        docker_registry_mirror_gc_schedule: ""
YAML

export ANSIBLE_ROLES_PATH="$PWD/ops/ansible/roles"

for f in default single-upstream; do
  if ! ansible-playbook -i localhost, --syntax-check "$TMPDIR/$f.yml" \
        > "$TMPDIR/$f.log" 2>&1; then
    echo "docker-registry-mirror test-fixture: FAIL ($f)" >&2
    cat "$TMPDIR/$f.log" >&2
    exit 1
  fi
  echo "docker-registry-mirror test-fixture: PASS ($f syntax-check)"
done

echo "docker-registry-mirror test-fixture: GREEN (2/2)"
