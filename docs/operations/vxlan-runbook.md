# VXLAN-over-WireGuard L2 segment — operator runbook

Operator-facing companion to the role at [`ops/ansible/roles/vxlan/`](../../ops/ansible/roles/vxlan/) and the design at `docs/design/l2-vxlan-networking.md`. Block 14 of the implementation plan.

**Scope:** stand up, validate, and tear down a virtual L2 segment between mesh peers. Each segment is opt-in — declared explicitly per host or group; default posture is off.

---

## When to use this

A peer-pair (or small peer-set) needs to act as if on the same Ethernet LAN — broadcast/ARP discovery, same-subnet addressing, multicast. The canonical cases:

- Sensor control software that expects to find devices via ARP/broadcast.
- Cameras or NVRs that use ONVIF discovery (UDP multicast 239.255.255.250).
- Industrial protocols (Modbus over Ethernet, EtherNet/IP).
- Legacy control software with hardcoded same-subnet assumptions.

If the workload is pure unicast to a static IP, **don't use this** — L3 routing across the mesh is cheaper (no encapsulation overhead, no broadcast-replication cost).

## What you get

- A virtual broadcast domain bridging two-or-more mesh peers.
- Inner Ethernet MTU 1370B (50B VXLAN + 80B WG conservative).
- Static FDB head-end replication: BUM traffic is unicast to listed peers, not flooded to the whole mesh.
- TCP MSS clamping on the bridge so PMTUD-respecting TCP works without 1500-byte black-holes.
- Bridge subnet (default `10.248.x.x/24`) is non-routed — traffic on the segment cannot leak into mesh routing or the underlay internet.

## Prerequisites

- Each participating host is an enrolled mesh peer (`netbird status` shows `Connected`).
- Each host has Linux with systemd-networkd active (Ubuntu 22.04 / 24.04 default).
- Each host has the `vxlan` role available in `ops/ansible/roles/`.
- For local hardware bridging (a device plugged into a NIC that joins the bridge), you know the interface name (e.g. `eth1`) and have confirmed nothing else is using it.

## Defining a segment

Per host (or group, in `group_vars/<group>/vars.yml`):

```yaml
vxlan_segments:
  - vni: 100
    bridge: br-l2seg0
    address: "10.248.0.1/24"      # this host's IP on the segment
    mtu: 1370
    peers:                         # ALL peers in segment, including self
      - "100.127.x.10"             # role auto-filters this host's mesh IP
      - "100.127.x.20"
    bridge_interfaces: []          # optional: ["eth1"] to bridge a local NIC
    tcp_mss_clamp: true            # default; clamps TCP MSS to PMTU
```

Constraints:

- `vni` is a 24-bit number unique across all segments on this host (1..16777215). Convention: 100 for first segment, 101 for next, etc.
- `bridge` must start with `br-l2` (the role's managed-bridge prefix) for orphan cleanup to consider it ours.
- `address` is per host — peer A might be `10.248.0.1/24`, peer B `10.248.0.2/24`, peer C `10.248.0.3/24`.
- `peers` is the full list including this host's mesh IP. The role drops the local entry before rendering FDB. This lets you declare the segment once at group level and have every host render correctly.

## Lab validation on integdev (Block 14A)

The integdev cloud env is the validation harness. The standalone playbook builds, asserts, and tears down a single segment.

### Apply

```bash
cd ops/ansible
OBJC_DISABLE_INITIALIZE_FORK_SAFETY=YES aws-vault exec legacy --no-session -- \
  ansible-playbook -i inventory/hosts.ini \
                   -i inventory/mgmt-hosts.ini \
                   -i inventory/mgmt-hosts-mesh.ini \
    integdev-vxlan-validate.yml --limit integdev_hosts
```

What this proves on each host:

- `vxlan100` interface is up at MTU 1370.
- `br-l2seg0` bridge is up at MTU 1370 with the right `10.248.0.X/24` address.
- FDB on `vxlan100` has one entry per remote peer.
- TCP MSS clamp rule is present in `mangle/FORWARD`.
- L3 `ping -I br-l2seg0` to the remote peer's `10.248.0.Y` succeeds.
- L2 `arping -I br-l2seg0` to the remote peer reports (informational; some EC2 SG paths drop arping but L3 works).

### Tear down

```bash
ansible-playbook ... integdev-vxlan-validate.yml --limit integdev_hosts \
  -e vxlan_validate_teardown=true
```

Removes all role-managed `.netdev`/`.network` files. Asserts no residual interfaces or files. The validator runs with `vxlan_remove_orphans: true` so the cleanup is automatic.

## Manual on-host inspection

After an apply, on any participating host:

```bash
# Link state
ip link show vxlan100
ip link show br-l2seg0

# Bridge membership
bridge link show

# FDB head-end-replication entries (one all-zeros MAC per peer)
bridge fdb show dev vxlan100 | grep -E '^(00:){5}00'

# Bridge IP
ip -4 addr show br-l2seg0

# MTU
ip -o link show vxlan100 | grep -oE 'mtu [0-9]+'
ip -o link show br-l2seg0 | grep -oE 'mtu [0-9]+'

# MSS clamp rule
sudo iptables -t mangle -L FORWARD -nv | grep TCPMSS

# Probe a remote peer
ping -I br-l2seg0 -c 3 10.248.0.<remote>

# Watch encapsulated traffic on the WG interface (sanity)
sudo tcpdump -i wt0 -nn 'udp port 4789' -c 10
```

## Production rollout (per device segment)

The integration with a real device follows the same role, with these per-deployment decisions:

1. **Where does the device plug in?** Physical NIC on which mgmt-host? Add that interface to `bridge_interfaces`.
2. **What IPs?** Pick a `vni` not in use, an address range outside your site LAN and the existing `10.248.X.x/24` segments. Convention: VNI 100 is the integdev test segment; the first production segment starts at VNI 101 with `10.248.1.0/24`, etc.
3. **Who's in the segment?** List the device's host + the controller's host + any analytics consumer. Keep the segment small — broadcast traffic is replicated to every peer in `peers`.
4. **MTU sanity for the device.** Confirm the device handles 1370B (most do via PMTUD/MSS). If it hardcodes 1500, consider:
   - Plug the device into a local switch capable of jumbo frames so the device→bridge hop is full-MTU; only cross-mesh traffic pays the encap cost.
   - Or rule out L2 flatten and use L3 + NAT remapping if the device's protocol is unicast-only.

## Failure modes

| Symptom | Likely cause | Action |
|---|---|---|
| Role fails: "interface wt0 not found" | netbird isn't up on the host | `sudo netbird status`; re-enroll if needed before re-running |
| `ping -I br-l2seg0` works but `arping` doesn't | Underlay path drops arping (some cloud SGs) | Informational only — L3 working is sufficient evidence the segment is healthy |
| Bridge interface up but no FDB entries | `peers` list missing or only contains self | Confirm inventory has every peer including this host's mesh IP; the role filters self out |
| Inner traffic capped at ~1300B even though MTU=1370 | Underlay path MTU lower than expected (Starlink/CGNAT can clamp below 1500) | Check WG MTU on `wt0`; if underlay PMTU is e.g. 1380, set segment `mtu` to 1310 or lower |
| TCP connections hang under load | PMTUD black-holed somewhere; MSS clamp not active | `sudo iptables -t mangle -S FORWARD \| grep TCPMSS` — if missing, re-run the role |
| MSS clamp lost after host reboot | `vxlan-runtime-state.service` failed to start (e.g. networkd didn't bring bridges up in time) | `systemctl status vxlan-runtime-state` + `journalctl -u vxlan-runtime-state -n 50`; restart the service or re-run the Ansible role |

## Removing a segment

The role manages live kernel state via `bash + iproute2`, not systemd-networkd files. Removal is:

1. Drop the segment from inventory (or set `vxlan_segments: []` for full teardown).
2. Re-run the role with `vxlan_remove_orphans: true`. It deletes any live `br-l2*` bridges and `vxlan*` netdevs not in the new desired set, then re-renders `/etc/vxlan/segments.conf` to match.
3. The `vxlan-runtime-state.service` is automatically restarted on config change. With an empty config, the script does nothing on boot.
4. The iptables MSS clamp rule and `tc tbf` qdisc are bound to the bridge — when the bridge is deleted, the rules become inert. The script does NOT explicitly flush them on segment removal in v1; if you need clean iptables state, restart the service or flush manually:

```bash
sudo iptables -t mangle -D FORWARD -o <bridge> -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu
```

## RCA when something is wrong

When the device says L2 isn't working, follow [`docs/operations/vxlan-rca-sop.md`](vxlan-rca-sop.md). Two-prong:

1. Host-side snapshot: `sudo ops/scripts/vxlan-rca.sh` — drops a tarball with state + iptables + tc + networkd config + dmesg + 30s pcaps on bridge / vxlan / wt0 / member NICs.
2. Device-side capture: SPAN port on a managed switch, or a Throwing Star LAN tap on a dumb switch, or last-resort inline goat host. SOP doc has filters and decision trees.

## Limits + future work

- **GRE-tap transport** is reserved as `vxlan_transport: gretap` but not wired in v1. Saves ~12B/packet vs VXLAN; gated on a measured trigger.
- **Multi-site / cross-region** scenarios are exercised in lab on integdev (single AWS region); cross-mesh-region testing happens at 14B vendor-trigger time.
- **BUM-only rate-limit** is v2. Today's `bum_rate_limit` installs a blunt `tc tbf` at the bridge root which caps ALL traffic; per-source-MAC ebtables would be the proper BUM-only treatment.

## See also

- Design: `docs/design/l2-vxlan-networking.md`
- Plan: `docs/design/l2-vxlan-multi-transport-plan.md`
- Block tracking: `docs/project/implementation-plan.md` Block 14
- Role: [`ops/ansible/roles/vxlan/README.md`](../../ops/ansible/roles/vxlan/README.md)
