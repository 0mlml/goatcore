#!/usr/bin/env bash
# check-no-decaying-fact-port-bind.sh — a container port publish must not be
# bound to an address resolved from a host fact.
#
# THE DEFECT CLASS. `docker-compose.yml` is RENDERED ONCE and then lives on the
# host. Any Jinja value in it is frozen at the moment ansible ran. That is fine
# for a pin or a port; it is a bug for `ansible_default_ipv4.address`, because
# a host's default-route address is not a property of the host — it is a
# property of the network the host happened to be on that day.
#
# The shape: a zenoh router's compose file publishes
# "<old-lan-ip>:8000:8000", rendered at standup. The site LAN then moves
# and the host holds no such address at all. The
# container keeps running only because docker-proxy still holds the old bind;
# the next `docker compose up` fails to bind and leaves the fabric
# router down. A deploy health probe on the same dead address cannot tell
# anyone either.
#
# THE RULE. In a compose template, the host side of a `ports:` publish must be
# a literal, a stable name, or a var that resolves to one — never
# ansible_default_ipv4 / ansible_facts['default_ipv4'] / ansible_<iface>.
#
# NOT IN SCOPE, deliberately: a mesh address (wt0) on the host side. It is also
# a decaying fact in principle, but it is re-derived every deploy from the
# interface that defines the goatnet, and ADR 0844 requires the data port to be
# mesh-bound. That is a considered trade, not an oversight — the difference is
# that wt0 IS the property being expressed, while default_ipv4 is a coincidence.
#
# --self-test: proves RED against the decaying-fact shape before its verdict on the
# real tree is trusted.
set -euo pipefail

FACT_RE='ansible_default_ipv4|ansible_facts\[.default_ipv4|ansible_facts\["default_ipv4'

scan() {
  local root="$1" hits=0
  # Only compose templates, and only inside a ports: block's host side.
  while IFS= read -r f; do
    python3 - "$f" "$FACT_RE" <<'PY' || hits=1
import re, sys
path, fact_re = sys.argv[1], sys.argv[2]
lines = open(path, encoding="utf-8").read().splitlines()
in_ports = False
bad = []
for n, raw in enumerate(lines, 1):
    stripped = raw.strip()
    if re.match(r'^ports\s*:', stripped):
        in_ports = True
        continue
    if in_ports:
        # a list item stays in the block; anything else at or left of the
        # ports: indent ends it
        if stripped.startswith("-") or stripped.startswith("#") or not stripped:
            if stripped.startswith("-") and re.search(fact_re, raw):
                bad.append((n, stripped))
            continue
        in_ports = False
if bad:
    print(f"FAIL: {path} publishes a container port on a host-fact address:")
    for n, s in bad:
        print(f"        {n}: {s}")
    print("      That literal is frozen into the rendered compose file and rots when the")
    print("      host's network changes; the container then cannot be recreated.")
    print("      Bind 127.0.0.1 for host-local admin surfaces, or a stable name.")
    sys.exit(1)
PY
  done < <(find "$root" -path '*/templates/*' -name '*compose*.j2' 2>/dev/null | sort)
  return $hits
}

if [ "${1:-}" = "--self-test" ]; then
  tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
  mkdir -p "$tmp/red/roles/x/templates" "$tmp/green/roles/x/templates"
  cat > "$tmp/red/roles/x/templates/docker-compose.yml.j2" <<'EOF'
services:
  r:
    ports:
      - "{{ ansible_default_ipv4.address }}:8000:8000"
EOF
  cat > "$tmp/green/roles/x/templates/docker-compose.yml.j2" <<'EOF'
services:
  r:
    ports:
      - "{{ goat_zenoh_rest_host }}:8000:8000"
      - "127.0.0.1:9000:9000"
    environment:
      SOMEWHERE_ELSE: "{{ ansible_default_ipv4.address }}"
EOF
  if scan "$tmp/red" >/dev/null 2>&1; then
    echo "SELF-TEST FAIL: guard passed a host-fact port bind — it never goes red"
    exit 1
  fi
  echo "self-test: RED proven — host-fact port bind fails"
  scan "$tmp/green" >/dev/null
  echo "self-test: GREEN proven — var/literal binds pass, and the fact is fine OUTSIDE ports:"
  echo "OK: decaying-fact-port-bind self-test — red-first proof holds"
  exit 0
fi

[ $# -eq 0 ] && cd "$(git rev-parse --show-toplevel)"
ROOT="${1:-ops/ansible}"
if scan "$ROOT"; then
  echo "OK: no container port is published on a deploy-time host-fact address"
else
  exit 1
fi
