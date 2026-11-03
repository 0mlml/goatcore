#!/usr/bin/env bash
# tests/test-block-12g.sh — verify the Block 12G NTS-server tier
# extension parses cleanly + renders different chrony.conf shapes
# under both flag values.
#
# Skips cleanly if ansible-playbook isn't on PATH.

set -euo pipefail

cd "$(dirname "$0")/../../../../.."

if ! command -v ansible-playbook >/dev/null 2>&1; then
  echo "block-12g test: SKIP (ansible-playbook not on PATH)"
  exit 0
fi

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

cat > "$TMPDIR/client-only.yml" <<'YAML'
---
- name: ntp-hygiene — client-only (default; pre-Block-12G shape)
  hosts: localhost
  gather_facts: false
  connection: local
  roles:
    - role: ntp-hygiene
      vars:
        chrony_serve_nts: false
YAML

cat > "$TMPDIR/server-tier.yml" <<'YAML'
---
- name: ntp-hygiene — server tier on (Block 12G)
  hosts: localhost
  gather_facts: false
  connection: local
  roles:
    - role: ntp-hygiene
      vars:
        chrony_serve_nts: true
        chrony_serve_nts_allow_cidrs:
          - "192.168.1.0/24"
          - "198.18.0.0/24"
          - "10.99.0.0/16"
YAML

cat > "$TMPDIR/plain-ntp.yml" <<'YAML'
---
- name: ntp-hygiene — plain-NTP server tier (Block 82 nova trial)
  hosts: localhost
  gather_facts: false
  connection: local
  roles:
    - role: ntp-hygiene
      vars:
        chrony_serve_plain_ntp: true
        chrony_serve_plain_ntp_allow_cidrs:
          - "192.168.1.0/24"
YAML

export ANSIBLE_ROLES_PATH="$PWD/ops/ansible/roles"

for f in client-only server-tier plain-ntp; do
  if ! ansible-playbook -i localhost, --syntax-check "$TMPDIR/$f.yml" \
        > "$TMPDIR/$f.log" 2>&1; then
    echo "block-12g test: FAIL ($f)" >&2
    cat "$TMPDIR/$f.log" >&2
    exit 1
  fi
  echo "block-12g test: PASS ($f syntax-check)"
done

# Also render the chrony.conf template directly with both flag shapes
# and confirm: client-only mode has `port 0` (server hardened off);
# server-tier mode has `port 123` + `ntsserverkey` + `allow 192.168...`.

# Render the chrony.conf template directly under both flag shapes via
# python+jinja2 to verify the rendered text differs in the expected
# ways. Skip cleanly when jinja2 isn't importable from system python.
# Pick a python that has jinja2 importable. ansible itself ships with
# jinja2 inside its python; reuse that env when system python doesn't.
PY=python3
if ! python3 -c 'import jinja2' >/dev/null 2>&1; then
  ANSIBLE_BIN=$(command -v ansible || true)
  if [[ -n "$ANSIBLE_BIN" ]]; then
    SHEBANG_PY=$(head -1 "$ANSIBLE_BIN" | sed -e 's,^#!,,' -e 's, .*,,')
    if [[ -x "$SHEBANG_PY" ]] && "$SHEBANG_PY" -c 'import jinja2' >/dev/null 2>&1; then
      PY="$SHEBANG_PY"
    fi
  fi
fi

if command -v "$PY" >/dev/null 2>&1 && "$PY" -c 'import jinja2' >/dev/null 2>&1; then
  TPL="ops/ansible/roles/ntp-hygiene/templates/chrony.conf.j2"
  if [[ ! -f "$TPL" ]]; then
    echo "block-12g test: SKIP (template not found)"
    exit 0
  fi

  "$PY" - <<PY > "$TMPDIR/render-client.txt"
import jinja2, sys
env = jinja2.Environment(undefined=jinja2.StrictUndefined)
# Ansible-equivalent bool filter so templates that gate on
# "something | default(false) | bool" render under raw jinja2.
env.filters["bool"] = lambda v: bool(v) if isinstance(v, bool) else (str(v).lower() in ("true","1","yes","on"))
with open("$TPL") as f: tpl = env.from_string(f.read())
print(tpl.render(
  chrony_nts_sources=["time.cloudflare.com"],
  chrony_driftfile="/var/lib/chrony/chrony.drift",
  chrony_logdir="/var/log/chrony",
  chrony_serve_nts=False,
))
PY

  "$PY" - <<PY > "$TMPDIR/render-server.txt"
import jinja2, sys
env = jinja2.Environment(undefined=jinja2.StrictUndefined)
# Ansible-equivalent bool filter so templates that gate on
# "something | default(false) | bool" render under raw jinja2.
env.filters["bool"] = lambda v: bool(v) if isinstance(v, bool) else (str(v).lower() in ("true","1","yes","on"))
with open("$TPL") as f: tpl = env.from_string(f.read())
print(tpl.render(
  chrony_nts_sources=["time.cloudflare.com"],
  chrony_driftfile="/var/lib/chrony/chrony.drift",
  chrony_logdir="/var/log/chrony",
  chrony_serve_nts=True,
  chrony_serve_nts_cert_path="/etc/chrony/server-cert.pem",
  chrony_serve_nts_key_path="/etc/chrony/server-key.pem",
  chrony_serve_nts_dump_dir="/var/lib/chrony",
  chrony_serve_nts_port=4460,
  chrony_serve_nts_allow_cidrs=["192.168.1.0/24", "198.18.0.0/24"],
))
PY

  if grep -q "^port 0" "$TMPDIR/render-client.txt"; then
    echo "block-12g test: PASS (client-only render has port 0)"
  else
    echo "block-12g test: FAIL — client-only render lacks port 0"
    sed -n '40,60p' "$TMPDIR/render-client.txt"
    exit 1
  fi

  if grep -q "^port 123" "$TMPDIR/render-server.txt" && \
     grep -q "ntsserverkey" "$TMPDIR/render-server.txt" && \
     grep -q "allow 192.168.1.0/24" "$TMPDIR/render-server.txt" && \
     grep -q "local stratum 10" "$TMPDIR/render-server.txt"; then
    echo "block-12g test: PASS (server-tier render has port 123 + ntsserverkey + allow + local stratum 10)"
  else
    echo "block-12g test: FAIL — server-tier render missing expected directives"
    cat "$TMPDIR/render-server.txt"
    exit 1
  fi

  # Block 82 — chrony_serve_plain_ntp branch. Lighter sibling of the
  # NTS server tier; no ntsserverkey / ntsservercert / ntsport directives,
  # but still port 123 + allow + local stratum 10.
  "$PY" - <<PY > "$TMPDIR/render-plain-ntp.txt"
import jinja2
env = jinja2.Environment(undefined=jinja2.StrictUndefined)
env.filters["bool"] = lambda v: bool(v) if isinstance(v, bool) else (str(v).lower() in ("true","1","yes","on"))
with open("$TPL") as f: tpl = env.from_string(f.read())
print(tpl.render(
  chrony_nts_sources=["time.cloudflare.com"],
  chrony_driftfile="/var/lib/chrony/chrony.drift",
  chrony_logdir="/var/log/chrony",
  chrony_serve_nts=False,
  chrony_serve_plain_ntp=True,
  chrony_serve_plain_ntp_allow_cidrs=["192.168.1.0/24"],
))
PY

  if grep -q "^port 123" "$TMPDIR/render-plain-ntp.txt" && \
     grep -q "allow 192.168.1.0/24" "$TMPDIR/render-plain-ntp.txt" && \
     grep -q "local stratum 10" "$TMPDIR/render-plain-ntp.txt" && \
     ! grep -q "ntsserverkey" "$TMPDIR/render-plain-ntp.txt"; then
    echo "block-12g test: PASS (plain-ntp render has port 123 + allow + local stratum 10, no NTS-KE)"
  else
    echo "block-12g test: FAIL — plain-ntp render missing/extra directives"
    cat "$TMPDIR/render-plain-ntp.txt"
    exit 1
  fi
fi

echo "block-12g test: GREEN"
