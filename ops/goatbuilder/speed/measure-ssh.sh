#!/usr/bin/env bash
# measure-ssh.sh — the per-connection and per-task cost of reaching one host,
# with SSH multiplexing off and on (docs/design/goatbuilder-speed.md, M-120).
# Read-only: `true` over ssh, and probe.yml (command: true, stat) through
# Ansible. Changes nothing on the host.
#
#   ops/goatbuilder/speed/measure-ssh.sh <user@host> <ansible-group>
set -uo pipefail
H="${1:?user@host}"; G="${2:?ansible group}"
HERE="$(cd "$(dirname "$0")" && pwd)"; REPO="$(cd "$HERE/../../.." && pwd)"
O=(-o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR)
CP="$HOME/.ssh/gbspd-%C"
now() { python3 -c 'import time;print(time.time())'; }
per() { python3 -c "print(f'{($2-$1)/$3*1000:.0f}')"; }
t0=$(now); for _ in 1 2 3 4 5 6 7 8 9 10; do ssh "${O[@]}" -o ControlMaster=no -o ControlPath=none "$H" true; done; t1=$(now)
ssh "${O[@]}" -o ControlMaster=auto -o ControlPath="$CP" -o ControlPersist=120 "$H" true; t2=$(now)
for _ in 1 2 3 4 5 6 7 8 9 10; do ssh "${O[@]}" -o ControlMaster=auto -o ControlPath="$CP" -o ControlPersist=120 "$H" true; done; t3=$(now)
ssh "${O[@]}" -o ControlPath="$CP" -O exit "$H" 2>/dev/null
echo "ssh per connection: $(per "$t0" "$t1" 10) ms without multiplexing; $(per "$t2" "$t3" 10) ms multiplexed"
cd "$REPO/ops/ansible" || exit 1
s=$(now); ANSIBLE_FACT_CACHING=memory ansible-playbook -i inventory/mgmt-hosts.ini -i inventory/mgmt-hosts-mesh.ini "$HERE/probe.yml" -e target="$G" >/dev/null 2>&1; rc=$?; e=$(now)
echo "probe.yml (11 read-only calls) through the inventory as configured: $(python3 -c "print(f'{$e-$s:.1f}')") s (exit $rc)"
