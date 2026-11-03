#!/usr/bin/env bash
# tests/test-fixture.sh — apt-cacher-ng role local smoke test.
# Skips cleanly if ansible-playbook isn't on PATH.
#
# Verifies the templates render without Jinja2 errors against two
# variable shapes (default + custom-bind).

set -euo pipefail

cd "$(dirname "$0")/../../../../.."

if ! command -v ansible-playbook >/dev/null 2>&1; then
  echo "apt-cacher-ng test-fixture: SKIP (ansible-playbook not on PATH)"
  exit 0
fi

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

cat > "$TMPDIR/default.yml" <<'YAML'
---
- name: apt-cacher-ng — defaults
  hosts: localhost
  gather_facts: false
  connection: local
  roles:
    - role: apt-cacher-ng
YAML

cat > "$TMPDIR/custom.yml" <<'YAML'
---
- name: apt-cacher-ng — custom bind + extra remap
  hosts: localhost
  gather_facts: false
  connection: local
  roles:
    - role: apt-cacher-ng
      vars:
        apt_cacher_ng_bind_addresses: ["192.168.1.253", "198.18.0.3"]
        apt_cacher_ng_remap:
          - name: ubuntu
            backends_url: "http://archive.ubuntu.com/ubuntu"
            aliases: ["archive.ubuntu.com/ubuntu", "security.ubuntu.com/ubuntu"]
          - name: kubernetes
            backends_url: "https://pkgs.k8s.io/core:/stable:/v1.30/deb"
            aliases: ["pkgs.k8s.io"]
YAML

export ANSIBLE_ROLES_PATH="$PWD/ops/ansible/roles"

for f in default custom; do
  if ! ansible-playbook -i localhost, --syntax-check "$TMPDIR/$f.yml" \
        > "$TMPDIR/$f.log" 2>&1; then
    echo "apt-cacher-ng test-fixture: FAIL ($f)" >&2
    cat "$TMPDIR/$f.log" >&2
    exit 1
  fi
  echo "apt-cacher-ng test-fixture: PASS ($f syntax-check)"
done

echo "apt-cacher-ng test-fixture: GREEN (2/2)"
