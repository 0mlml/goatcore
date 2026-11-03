#!/usr/bin/env bash
# ops/scripts/vxlan-rca.sh
#
# Block 14 host-side RCA snapshot. Run on a goat host that's a VXLAN-segment
# endpoint. Captures everything we need to debug "the device says L2 isn't
# working" / "MTU smells wrong" / "broadcasts aren't arriving" without
# requiring a live operator at the keyboard.
#
# Output: a single tarball at /tmp/vxlan-rca-<host>-<utc-ts>.tar.gz containing
# - state.txt   — per-interface MTU, link state, FDB, ARP, drivers, counters
# - iptables.txt, tc.txt, networkd.txt, dmesg.txt, journal.txt, sockets.txt
# - <bridge>.pcap, vxlan<vni>.pcap, wt0.pcap — 30s captures (if tcpdump is present)
#
# Operator usage:
#   sudo ops/scripts/vxlan-rca.sh
#   # then share the tarball path printed at the end.
#
# Knobs (env vars):
#   VXLAN_RCA_CAPTURE_SECONDS=30   how long to tcpdump
#   VXLAN_RCA_NO_PCAP=1            skip pcaps (tarball stays small)
#   VXLAN_RCA_NETBIRD_IFACE=wt0    override netbird interface name

set -euo pipefail

CAPTURE_SECONDS="${VXLAN_RCA_CAPTURE_SECONDS:-30}"
NETBIRD_IFACE="${VXLAN_RCA_NETBIRD_IFACE:-wt0}"
TS="$(date -u +%Y%m%d-%H%M%SZ)"
HOST="$(hostname -s)"
WORK="$(mktemp -d -t vxlan-rca-XXXXXX)"
OUT="/tmp/vxlan-rca-${HOST}-${TS}.tar.gz"

# Enumerate the bridges and VXLAN ifaces this host owns. The role names them
# br-l2*  and vxlan<vni>; we accept either pattern present.
mapfile -t BRIDGES < <(ip -o link show type bridge 2>/dev/null \
    | awk -F': ' '/^[0-9]+: br-l2/{print $2}' | awk '{print $1}')
mapfile -t VXLANS  < <(ip -o link show type vxlan  2>/dev/null \
    | awk -F': ' '{print $2}' | awk '{print $1}')

# Build the list of interfaces we care about for state + pcap.
IFACES=("${BRIDGES[@]}" "${VXLANS[@]}")
if ip -o link show dev "$NETBIRD_IFACE" >/dev/null 2>&1; then
  IFACES+=("$NETBIRD_IFACE")
fi

# Bridged member ifaces (so we capture local NIC perspective too).
mapfile -t MEMBER_IFACES < <(
  for br in "${BRIDGES[@]}"; do
    bridge link show 2>/dev/null | awk -v BR="$br" '$0 ~ "master "BR{print $2}'
  done
)
IFACES+=("${MEMBER_IFACES[@]}")

# De-dup
mapfile -t IFACES < <(printf '%s\n' "${IFACES[@]}" | awk 'NF && !seen[$0]++')

echo "[*] vxlan-rca starting on $HOST at $TS"
echo "[*] bridges: ${BRIDGES[*]:-<none>}"
echo "[*] vxlan:   ${VXLANS[*]:-<none>}"
echo "[*] member NICs: ${MEMBER_IFACES[*]:-<none>}"
echo "[*] netbird: $NETBIRD_IFACE $(ip -o link show dev "$NETBIRD_IFACE" >/dev/null 2>&1 && echo '(present)' || echo '(MISSING)')"
echo "[*] capture: ${CAPTURE_SECONDS}s per iface (set VXLAN_RCA_NO_PCAP=1 to skip)"
echo

# ---------------------------------------------------------------------------
# state.txt — per-iface MTU + link + driver + counters
# ---------------------------------------------------------------------------
{
  echo "# vxlan-rca state — host=$HOST ts=$TS"
  echo
  echo "## kernel + os"
  uname -a
  command -v lsb_release >/dev/null && lsb_release -a 2>/dev/null
  echo
  echo "## per-interface (ip -d link)"
  for i in "${IFACES[@]}"; do
    echo "----- $i -----"
    ip -d link show dev "$i" 2>&1 || true
    echo
  done
  echo "## per-interface counters (ip -s link)"
  for i in "${IFACES[@]}"; do
    echo "----- $i -----"
    ip -s link show dev "$i" 2>&1 || true
    echo
  done
  echo "## per-interface addresses (ip -4 -o addr)"
  for i in "${IFACES[@]}"; do
    ip -4 -o addr show dev "$i" 2>&1 || true
  done
  echo
  echo "## bridge link membership"
  bridge link show 2>&1 || true
  echo
  echo "## bridge fdb (per VXLAN)"
  for v in "${VXLANS[@]}"; do
    echo "----- $v -----"
    bridge fdb show dev "$v" 2>&1 || true
    echo
  done
  echo "## ARP / neigh (per bridge)"
  for br in "${BRIDGES[@]}"; do
    echo "----- $br -----"
    ip neigh show dev "$br" 2>&1 || true
    echo
  done
  echo "## routes"
  ip -4 route show 2>&1 || true
  echo
  echo "## netbird state (best-effort)"
  command -v netbird >/dev/null && netbird status 2>&1 || echo "(netbird CLI not present)"
} > "$WORK/state.txt"

# ---------------------------------------------------------------------------
# iptables.txt — mangle FORWARD (MSS clamp) + filter FORWARD
# ---------------------------------------------------------------------------
{
  echo "# iptables snapshot"
  echo
  for tbl in mangle filter nat; do
    echo "## table=$tbl"
    iptables -t "$tbl" -S 2>&1 || true
    echo
    echo "## table=$tbl counters"
    iptables -t "$tbl" -L -nv --line-numbers 2>&1 || true
    echo
  done
} > "$WORK/iptables.txt"

# ---------------------------------------------------------------------------
# tc.txt — qdisc + class state
# ---------------------------------------------------------------------------
{
  echo "# tc snapshot"
  for i in "${IFACES[@]}"; do
    echo "----- $i (qdisc) -----"
    tc -s qdisc show dev "$i" 2>&1 || true
    echo "----- $i (class)  -----"
    tc -s class show dev "$i" 2>&1 || true
    echo "----- $i (filter) -----"
    tc -s filter show dev "$i" 2>&1 || true
    echo
  done
} > "$WORK/tc.txt"

# ---------------------------------------------------------------------------
# networkd.txt — config + status
# ---------------------------------------------------------------------------
{
  echo "# systemd-networkd snapshot"
  echo "## status"
  systemctl status systemd-networkd --no-pager 2>&1 || true
  echo
  echo "## networkctl status"
  networkctl status --no-pager 2>&1 || true
  echo
  echo "## /etc/systemd/network/ inventory"
  ls -la /etc/systemd/network/ 2>&1 || true
  echo
  echo "## role-managed config files (25-vxlan-* + 25-br-l2*)"
  for f in /etc/systemd/network/25-vxlan-* /etc/systemd/network/25-br-l2*; do
    [[ -e "$f" ]] || continue
    echo "----- $f -----"
    cat "$f" 2>&1 || true
    echo
  done
  echo "## /etc/vxlan/segments.conf (runtime-state input)"
  cat /etc/vxlan/segments.conf 2>&1 || true
  echo
  echo "## vxlan-runtime-state.service status + recent journal"
  systemctl status vxlan-runtime-state.service --no-pager 2>&1 || true
  echo
  journalctl -u vxlan-runtime-state.service --no-pager -n 100 2>&1 || true
} > "$WORK/networkd.txt"

# ---------------------------------------------------------------------------
# dmesg.txt — MTU / fragment / driver hints
# ---------------------------------------------------------------------------
{
  echo "# dmesg (filtered for MTU / vxlan / bridge / fragment)"
  dmesg --time-format=iso 2>&1 \
    | grep -iE "(fragment|needed|mtu|vxlan|bridge|netlink|conntrack|tun|wt0)" \
    | tail -300 || true
  echo
  echo "# (full last-200 lines for general context)"
  dmesg --time-format=iso 2>&1 | tail -200 || true
} > "$WORK/dmesg.txt"

# ---------------------------------------------------------------------------
# journal.txt — networkd recent + system errors
# ---------------------------------------------------------------------------
{
  echo "# journalctl -u systemd-networkd (last 200)"
  journalctl -u systemd-networkd --no-pager -n 200 2>&1 || true
  echo
  echo "# journal -p err since 1 hour ago"
  journalctl -p err --since "1 hour ago" --no-pager 2>&1 || true
} > "$WORK/journal.txt"

# ---------------------------------------------------------------------------
# sockets.txt — anything listening or connected on relevant ports
# ---------------------------------------------------------------------------
{
  echo "# ss -tunap"
  ss -tunap 2>&1 || true
  echo
  echo "# UDP 4789 (VXLAN) listeners"
  ss -tunap 'sport = :4789 or dport = :4789' 2>&1 || true
} > "$WORK/sockets.txt"

# ---------------------------------------------------------------------------
# pcaps — 30s on bridge + vxlan + wt0 + member NICs (background, then wait)
# ---------------------------------------------------------------------------
if [[ "${VXLAN_RCA_NO_PCAP:-0}" == "0" ]] && command -v tcpdump >/dev/null; then
  echo "[*] starting ${CAPTURE_SECONDS}s parallel tcpdump on ${#IFACES[@]} iface(s)"
  PIDS=()
  for i in "${IFACES[@]}"; do
    out="$WORK/${i}.pcap"
    tcpdump -nn -i "$i" -s 0 -w "$out" 2>"$WORK/${i}.tcpdump.stderr" &
    PIDS+=($!)
  done
  sleep "$CAPTURE_SECONDS"
  for p in "${PIDS[@]}"; do
    kill -INT "$p" 2>/dev/null || true
  done
  wait 2>/dev/null || true
  echo "[*] capture complete"
else
  echo "[*] pcap skipped (VXLAN_RCA_NO_PCAP set or tcpdump missing)"
fi

# ---------------------------------------------------------------------------
# tarball
# ---------------------------------------------------------------------------
tar czf "$OUT" -C "$WORK" .
rm -rf "$WORK"

echo
echo "[OK] snapshot written to: $OUT"
echo "     bytes: $(stat -c %s "$OUT" 2>/dev/null || stat -f %z "$OUT")"
echo
echo "Next steps:"
echo "  - share the tarball ($OUT) with whoever's debugging"
echo "  - or open the pcaps in Wireshark: tar xzf $OUT -C /tmp/rca && wireshark /tmp/rca/*.pcap"
echo "  - or grep state files: tar xzf $OUT -C /tmp/rca && grep -i mtu /tmp/rca/state.txt"
