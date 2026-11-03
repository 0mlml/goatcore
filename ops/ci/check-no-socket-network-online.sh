#!/usr/bin/env bash
# Refuse a systemd SOCKET unit (or drop-in) that orders itself on
# network-online.target.
#
# A socket unit belongs to sockets.target, which is ordered before basic.target,
# which everything network-related is ordered after. `After=`/`Wants=`/
# `Requires=network-online.target` on a socket closes that loop, and systemd
# breaks the loop at boot by DELETING a job it chooses. If it chooses
# tangd.socket, a data volume that unlocks from that Tang server stays locked and
# the mgmt stack stays down. It is a coin toss per boot, so a host can pass many
# reboots first.
#
# A socket that must bind an address which appears late uses FreeBind=true
# (plus net.ipv4.ip_nonlocal_bind=1), never network ordering.
#
# Scans socket paths, inline socket destinations and [Socket] sections. Comment lines
# (# or ;) are ignored, so explaining the rule in a template is fine.
#
#   ops/ci/check-no-socket-network-online.sh [root]      # default: repo root
#   ops/ci/check-no-socket-network-online.sh --self-test
set -euo pipefail

scan() {
  local root="$1" bad=0 f
  while IFS= read -r -d '' f; do
    # Drop-ins inherit [Socket] from their base unit; [Unit] alone is valid.
    case "$f" in
      *.socket|*.socket.j2|*.socket.d/*) ;;
      *)
        grep -qE '^[[:space:]]*\[Socket\][[:space:]]*$' "$f" \
          || grep -qE '^[[:space:]]*dest:.*\.socket(\.d/|[^[:alnum:]_.-]|$)' "$f" \
          || continue
        ;;
    esac
    if grep -nE '^[[:space:]]*(After|Wants|Requires|BindsTo)[[:space:]]*=.*network-online\.target' "$f" \
         | grep -vE '^[0-9]+:[[:space:]]*[#;]' >/dev/null; then
      echo "FAIL: $f renders a socket unit/drop-in ordered on network-online.target:"
      grep -nE '^[[:space:]]*(After|Wants|Requires|BindsTo)[[:space:]]*=.*network-online\.target' "$f" | sed 's/^/    /'
      bad=1
    fi
  done < <(find "$root/ops" -type f \( -name '*.j2' -o -name '*.yml' -o -name '*.yaml' \
             -o -name '*.socket' -o -name '*.conf' \) -not -path '*/node_modules/*' -print0 2>/dev/null)
  return $bad
}

if [ "${1:-}" = "--self-test" ]; then
  t=$(mktemp -d); trap 'rm -rf "$t"' EXIT
  mkdir -p "$t/ops/a" "$t/ops/b" "$t/ops/c"
  printf '[Unit]\nAfter=network-online.target\n\n[Socket]\nListenStream=10.0.0.1:7500\n' > "$t/ops/a/override.conf"
  printf '# no After=network-online.target here\n[Socket]\nFreeBind=true\nListenStream=10.0.0.1:7500\n' > "$t/ops/b/ok.conf"
  printf '[Unit]\nAfter=network-online.target\n[Service]\nExecStart=/bin/true\n' > "$t/ops/c/svc.service.j2"
  if scan "$t" >/dev/null; then echo "self-test FAIL: the 2026-09-26 override was not refused"; exit 1; fi
  rm "$t/ops/a/override.conf"
  if ! scan "$t" >/dev/null; then echo "self-test FAIL: a FreeBind socket or a plain service was refused"; exit 1; fi
  mkdir -p "$t/ops/a/tangd.socket.d"
  printf '[Unit]\nAfter = network-online.target\n' > "$t/ops/a/tangd.socket.d/ordering.conf.j2"
  if scan "$t" >/dev/null; then echo "self-test FAIL: Unit-only socket drop-in was not refused"; exit 1; fi
  rm "$t/ops/a/tangd.socket.d/ordering.conf.j2"
  printf 'dest: /etc/systemd/system/tangd.socket.d/override.conf\ncontent: |\n  [Unit]\n  Wants=network-online.target\n' > "$t/ops/a/task.yml"
  if scan "$t" >/dev/null; then echo "self-test FAIL: inline socket drop-in was not refused"; exit 1; fi
  rm "$t/ops/a/task.yml"
  if ! scan "$t" >/dev/null; then echo "self-test FAIL: clean controls were refused"; exit 1; fi
  echo "self-test OK — socket sections, Unit-only drop-ins and inline destinations refused; FreeBind socket and ordinary service pass."
  exit 0
fi

root="${1:-.}"
if scan "$root"; then
  echo "ok: no socket unit orders itself on network-online.target."
else
  echo
  echo "Fix: drop the network-online ordering from the socket and add FreeBind=true."
  exit 1
fi
