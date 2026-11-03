#!/bin/bash
# Entrypoint for wg-cp0 mesh-reach bench-env containers.
#
# Reads from environment:
#   ROLE        = "peer" | "relay"
#   NAME        = container identifier (e.g. "peer-0", "relay-1")
#
# Expects /work/<NAME>/wg-cp0.conf to be present (bind-mounted from host).
# Brings up wg-cp0 via wg-quick. Relay role additionally enables IPv4
# forwarding. Then blocks on tail -f /dev/null so the container stays alive
# for the cross-product ping phase.

set -euo pipefail

: "${ROLE:?ROLE env var required (peer|relay)}"
: "${NAME:?NAME env var required}"

CONF_SRC="/work/${NAME}/wg-cp0.conf"
CONF_DST="/etc/wireguard/wg-cp0.conf"

if [[ ! -f "${CONF_SRC}" ]]; then
    echo "entrypoint: missing config ${CONF_SRC}" >&2
    exit 2
fi

# wg-quick reads from /etc/wireguard/<interface>.conf
cp "${CONF_SRC}" "${CONF_DST}"
chmod 600 "${CONF_DST}"

if [[ "${ROLE}" == "relay" ]]; then
    # net.ipv4.ip_forward is per-netns but /proc/sys is read-only inside
    # the container; the harness sets it via `docker run --sysctl
    # net.ipv4.ip_forward=1`. We verify it took rather than trying to
    # write it again (which would fail with EROFS and crash the relay).
    if [[ "$(cat /proc/sys/net/ipv4/ip_forward 2>/dev/null)" != "1" ]]; then
        echo "entrypoint: net.ipv4.ip_forward != 1 — relay won't forward; harness must pass --sysctl" >&2
        exit 3
    fi
fi

# Bring up the wg-cp0 interface. wg-quick handles:
#   ip link add wg-cp0 type wireguard
#   wg setconf wg-cp0 ...
#   ip addr add ... dev wg-cp0
#   ip link set wg-cp0 up
#   ip route add ... dev wg-cp0
wg-quick up wg-cp0

echo "entrypoint: ${ROLE} ${NAME} ready"
wg show wg-cp0 | head -20

# Block forever; the harness does docker exec into us for ping probes.
exec tail -f /dev/null
