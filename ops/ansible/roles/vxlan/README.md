# Role: `vxlan`

VXLAN-over-WireGuard L2 segment role. **Block 14** of the implementation plan.

## What this role does

Creates one or more virtual L2 segments by combining a Linux bridge with a VXLAN interface that rides existing Netbird/WireGuard tunnels. Each segment is an isolated broadcast domain; peers in the segment see each other as if on the same Ethernet LAN. Useful for industrial protocols, ONVIF discovery, legacy control software — anything that assumes same-subnet ARP/broadcast.

## Architecture: bash + iproute2 + systemd oneshot

The role does **not** use systemd-networkd `.netdev` / `.network` files. F-051 captured why: systemd-networkd 249 (Ubuntu 22.04) silently parses VXLAN `.netdev` files but never instantiates the kernel device when `Local=` depends on a DHCP-configured underlay. Manual `ip link add type vxlan ...` with the same parameters works instantly.

The role manages kernel state via `bash + iproute2` directly:

- `/etc/vxlan/segments.conf` — config rendered from `vxlan_segments`.
- `/usr/local/sbin/vxlan-runtime-state.sh` — reads the config and applies bridges + VXLAN netdevs + FDB head-end-replication + IP + MTU + MSS clamp + `tc tbf` qdisc using iproute2. Idempotent at end-state via `iptables -C`/`-A`, `tc qdisc replace`, `ip addr replace`, and presence-checked `ip link add`.
- `/etc/systemd/system/vxlan-runtime-state.service` — oneshot, `RemainAfterExit=yes`, `After=network-online.target`. Runs at boot, and on every Ansible apply (the role notifies a restart on config / script / unit changes plus an unconditional restart task to handle the no-diff case).

## Default posture: off

The role no-ops cleanly when `vxlan_segments` is unset or empty. **No production host is touched unless an operator explicitly declares a segment in inventory.** This is deliberate — L2 flattening is opt-in per use case, not universal.

## Schema

```yaml
vxlan_segments:
  - vni: 100                       # 24-bit VNI, unique per segment
    bridge: br-l2seg0              # Linux bridge name (must start with vxlan_managed_bridge_prefix)
    address: "10.248.0.1/24"       # this host's IP on the bridge
    mtu: 1370                      # optional, default 1370 (WG 1420 - VXLAN 50)
    peers:                         # ALL peer mesh IPs in the segment, including self
      - "100.104.x.y"              #   role filters self out before rendering FDB
      - "100.104.x.z"
    bridge_interfaces: []          # optional local physical interfaces to bridge
    dstport: 4789                  # optional, default 4789
    tcp_mss_clamp: true            # optional, default true (PMTU-friendly TCP)
```

`vxlan_remove_orphans: true` (default `false`) makes the role clean up `.netdev`/`.network` files for segments that were removed from inventory. Off by default to avoid surprises on shared hosts.

## What the role manages

| File / artifact | Per-segment | Purpose |
|---|---|---|
| Kernel `br-l2*` bridge | yes | Linux bridge (MTU, IP, member NICs, STP off) |
| Kernel `vxlan{vni}` netdev | yes | VXLAN over the underlay UDP port 4789 |
| Bridge FDB `00:00:00:00:00:00 dst <peer>` | per remote peer | Head-end-replication entry; one per remote in `peers` |
| iptables mangle FORWARD rule | per bridge | TCP MSS clamp to PMTU (PMTUD-friendly) |
| `tc qdisc … root tbf` | per bridge with `bum_rate_limit` set | Bridge-root traffic-cap (v1 caveat: caps ALL traffic, not just BUM) |
| `/etc/vxlan/segments.conf` | host | Config rendered from `vxlan_segments`; consumed by the script |
| `/usr/local/sbin/vxlan-runtime-state.sh` | host | Applies + re-applies kernel state via iproute2; idempotent |
| `/etc/systemd/system/vxlan-runtime-state.service` | host | Oneshot, runs the script at boot after `network-online.target` |

## Constraints + gotchas

- **Netbird must be up first.** The role reads this host's mesh IP from `wt0` (override via `vxlan_netbird_interface`). It fails fast with a clear error if the interface isn't there.
- **Self-filtering peer list.** `peers` should include every peer in the segment, including this host. The role compares each peer to the local mesh IP and drops the match before rendering FDB. Saves operators from per-host inventory churn.
- **Bridge name prefix is load-bearing for orphan cleanup.** Bridges named outside `vxlan_managed_bridge_prefix` (default `br-l2`) won't be removed by `vxlan_remove_orphans` even if the segment is dropped — operator must clean those up by hand.
- **Runtime state survives reboot.** MSS clamp + (when set) `tc tbf` are re-applied at every boot by `vxlan-runtime-state.service` (oneshot, `After=systemd-networkd.service`). The service reads `/etc/vxlan/segments.conf` and runs `/usr/local/sbin/vxlan-runtime-state.sh`. Same code path is used at Ansible-apply time (`notify: restart vxlan-runtime-state` after config / script / unit changes), so what runs at boot is exactly what the role just verified.
- **BUM rate-limit is a blunt instrument in v1.** When `bum_rate_limit` is set, the role installs a `tc tbf` qdisc at the bridge root, which caps all traffic out the bridge — not only broadcast/multicast/unknown-unicast. Size `rate:` so normal unicast still fits. A proper BUM-only rate-limit (per-source-MAC ebtables) is v2.
- **Encapsulation overhead is structural.** Inner Ethernet MTU is 1370B (50B VXLAN + 80B WG conservative). Applications that hardcode 1500-byte frames will break regardless; ones that respect PMTUD/MSS work fine. Consider jumbo frames on local segments where the device plugs in physically — only the cross-mesh hop pays the encap.

## Why VXLAN, not GRE-tap

GRE-tap saves ~12B/packet (38B vs 50B overhead) but is point-to-point — needs N interfaces per host vs VXLAN's one multipoint interface — and the savings don't unlock applications that are currently broken. Reserved as a future swap (`vxlan_transport: gretap`) if a measured workload demands it. v1 ships VXLAN only.

## Validation

End-to-end validation lives at `ops/ansible/integdev-vxlan-validate.yml` and the operator runbook at [`docs/operations/vxlan-runbook.md`](../../../../docs/operations/vxlan-runbook.md). After a successful apply, on each peer:

```bash
# VXLAN interface up
ip link show vxlan100

# FDB has remote peer entries
bridge fdb show dev vxlan100

# Bridge has the configured address
ip addr show br-l2seg0

# L3 ping across the segment
ping -c 3 10.248.0.<remote>

# L2 reachability (ARP)
arping -I br-l2seg0 10.248.0.<remote>

# MTU is set
ip link show vxlan100 | grep mtu

# MSS clamp is in place
sudo iptables -t mangle -L FORWARD -nv | grep TCPMSS
```

## References

- Design: `docs/design/l2-vxlan-networking.md`
- Plan: `docs/design/l2-vxlan-multi-transport-plan.md`
- Tracking: `docs/project/implementation-plan.md` Block 14
- RFC 7348 — VXLAN
