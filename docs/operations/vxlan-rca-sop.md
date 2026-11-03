# VXLAN-over-WG RCA — operator SOP

When a device on a goat L2 segment isn't behaving — packets dropping, ARP failing, MTU-smelling stalls — start here. Two complementary capture surfaces:

1. **Host-side** — the goat box that runs the bridge / VXLAN endpoint. We own this. Gather state with [`ops/scripts/vxlan-rca.sh`](../../ops/scripts/vxlan-rca.sh).
2. **Device-side** — the radar / camera / industrial controller. We typically *don't* own this. Capture from outside via SPAN port or LAN tap.

Run both when symptoms are unclear. One alone is often enough when the symptom is obviously on one side or the other.

---

## Step 0 — symptoms classifier

Before capturing, write down what you actually see. Common shapes:

| Symptom | Most likely class | Where to look first |
|---|---|---|
| Device can ping its own gateway but cannot reach the controller across the segment | L2 reachability across mesh | host-side: bridge fdb + ARP + WG tunnel state |
| Small UDP works, large UDP / TCP transfers stall or hang | MTU / PMTUD black-hole | host-side iptables MSS clamp; device-side ICMP PTB receipt |
| Broadcasts/ARP discovery times out (radar can't find peer) | BUM replication | host-side FDB head-end-replication entries; device-side broadcasts |
| Connection works briefly then resets repeatedly | Rate-limit cap, NAT keepalive, or WG handshake flap | host-side tc counters; netbird status churn |
| Device shows "MTU mismatch" / fragmentation errors | PMTU below configured 1370 | host-side dmesg; device-side ICMP fragmentation-needed |
| Worked yesterday, broken today after host reboot | Runtime state didn't re-apply | check `vxlan-runtime-state.service` — should be `active (exited)` |

---

## Step 1 — host-side snapshot

On the bridge host (the one running the `vxlan` Ansible role):

```bash
sudo /Users/path/to/repo/ops/scripts/vxlan-rca.sh
```

Or after Ansible deploy, the script is at `ops/scripts/` in the repo — easiest is to `scp` it and run.

What you get: a single `/tmp/vxlan-rca-<host>-<ts>.tar.gz` tarball with:

- `state.txt` — per-interface MTU + counters + driver, FDB, ARP, routes, netbird status
- `iptables.txt` — mangle FORWARD (MSS clamp), counters, every chain
- `tc.txt` — qdisc + class + filter state per interface (look for tbf drop counters)
- `networkd.txt` — systemd-networkd config + status, role-managed `.netdev`/`.network` files, `vxlan-runtime-state.service` state + journal
- `dmesg.txt` — filtered for MTU / vxlan / bridge / fragment hints
- `journal.txt` — networkd journal + system errors in the last hour
- `sockets.txt` — TCP/UDP listeners, including UDP 4789 (VXLAN)
- `<iface>.pcap` — 30s captures on bridge / vxlan / wt0 / bridged member NICs (parallel)

Skip pcaps if you want a smaller tarball: `VXLAN_RCA_NO_PCAP=1 sudo ops/scripts/vxlan-rca.sh`.

### Quick checks while you have the data

Common things to grep for first:

```bash
tar xzf /tmp/vxlan-rca-*.tar.gz -C /tmp/rca/

# MTU is what we expect (1370 inner, 1420 wt0, 1500 underlay)?
grep -i "mtu " /tmp/rca/state.txt | grep -E "(br-l2|vxlan|wt0)"

# MSS clamp rule actually present on the right bridge?
grep -A1 -B1 TCPMSS /tmp/rca/iptables.txt

# tc tbf qdisc dropping packets? (look for "dropped N" counters)
grep -A3 "qdisc tbf" /tmp/rca/tc.txt

# FDB has all expected remote peers?
grep -A20 "bridge fdb" /tmp/rca/state.txt

# Recent fragment-needed events?
grep -i "fragment\|needed\|mtu" /tmp/rca/dmesg.txt | tail -30

# Did the runtime-state service apply at boot?
grep -i "vxlan-runtime-state" /tmp/rca/networkd.txt
```

### Open the pcaps in Wireshark

```bash
wireshark /tmp/rca/br-l2seg0.pcap   # what the device sees on the segment
wireshark /tmp/rca/vxlan100.pcap    # what's leaving the bridge into VXLAN
wireshark /tmp/rca/wt0.pcap         # what's leaving WG (encapsulated VXLAN UDP/4789)
```

Useful Wireshark filters:

| Goal | Filter |
|---|---|
| Just the device's traffic | `eth.src == <device MAC>` (the real MAC from state.txt) |
| ICMP "fragmentation needed" reaching the device | `icmp.type == 3 && icmp.code == 4` |
| TCP retransmissions clustering at a packet size | `tcp.analysis.retransmission` |
| ARP requests / replies | `arp` |
| VXLAN-encapsulated frames (on wt0 or bridge if reading the encapsulated side) | `vxlan` (Wireshark dissects auto-magically on UDP 4789) |
| Inner protocol after VXLAN decap | open the VXLAN frame, the inner Ethernet+IP is dissected one layer down |

---

## Step 2 — device-side capture (when you don't own the device's host)

Many radars / cameras / industrial controllers are black-box appliances we can't run tooling on. Two practical patterns:

### Option A — managed switch with SPAN port (cleanest)

If the device is plugged into a managed switch that supports port mirroring:

1. Configure the switch to mirror the device's port to a free port (most managed switches: SPAN / port mirroring / monitor session in CLI).
2. Plug a laptop running Wireshark into the mirror port.
3. Filter for the device's MAC. Capture for 5-10 min during a failing operation.

Pros: no in-line hardware, no risk of disrupting the device. Cons: requires a managed switch.

### Option B — Throwing Star LAN tap or equivalent (when no managed switch)

If the device is on a dumb (unmanaged) switch or a directly-connected goat-host NIC:

1. Get a passive ethernet tap — Hak5 Throwing Star is ~$15, or a Dualcomm DCSW-1005PT for gigabit. Both are read-only and add no latency.
2. Insert the tap inline between the device and its switch / NIC.
3. Connect a laptop running Wireshark to the tap's monitor port(s). For gigabit, you need both A→B and B→A taps and either two NICs on the laptop or a switch in promisc mode.
4. Capture for 5-10 min during the failing operation.

Pros: works on any L2 setup, even completely dumb infrastructure. Cons: needs two NICs (or a small dedicated capture appliance) for full-duplex gigabit.

### Option C — temporary inline goat host (last resort)

If neither of the above is feasible, a Pi or mini-PC running Linux can be inserted in line as a temporary L2 bridge, with `tcpdump` capturing on the bridge:

```bash
# On the inline host
sudo apt install bridge-utils tcpdump
sudo ip link add br-tap type bridge
sudo ip link set eth0 master br-tap   # device side
sudo ip link set eth1 master br-tap   # network side
sudo ip link set br-tap up
sudo ip link set eth0 up
sudo ip link set eth1 up
sudo tcpdump -i br-tap -nn -s 0 -w /tmp/device-side.pcap
```

Pros: free if you have the hardware. Cons: introduces a real L2 device in the path; misconfiguration can break the device's connectivity. Use only when A and B aren't options.

---

## Step 3 — what to look for in device-side pcaps

| Observation | Likely meaning | Next step |
|---|---|---|
| Device sends 1500-byte frames, never sees ICMP PTB / fragmentation-needed back | PMTUD black-hole — middlebox blocked ICMP | Confirm MSS clamp on the bridge is active (host-side); if it is, check whether the *device* honors MSS in its TCP SYN |
| Device sends frames > 1500 bytes (jumbo) | Local switch supports jumbo but mesh path doesn't | Either configure jumbo on local switch + lower segment MTU or disable jumbo on device |
| Device ARPs the controller; never gets a reply | L2 path broken between segment endpoints | Host-side: verify FDB entries on `vxlan{vni}` for the controller's mesh peer; verify WG tunnel up |
| Device sends multicast (e.g., ONVIF 239.255.255.250); replies don't return | BUM replication not reaching the right peers | Host-side: check FDB has the all-zero-MAC head-end-replication entries for every peer in the segment |
| Device sees inbound from controller but its own outbound never leaves | Local switch / NIC issue, NOT a goat issue | Re-check device's local link state, switch counters, cable |
| TCP works briefly then sees `RST` | Could be many things — capture host-side simultaneously, look for matching tc drops or WG handshake events |

---

## Step 4 — pair host-side + device-side timestamps

The most useful debug move is correlating host-side state at the same instant as a device-side failure:

1. On the device, trigger a known-bad operation at a known time (e.g., `nc -w 5 <controller> <port>` from a laptop on the same segment as the device, or push a button on the device).
2. Run `sudo ops/scripts/vxlan-rca.sh` on the host within the next minute (the `dmesg` + `journal` captures are time-bounded).
3. Run `tcpdump` (or have Wireshark capturing) on the device side at the same moment.
4. Open both side-by-side. Look for: device sends frame at T=0, what does the bridge see at T=0+epsilon? What does `wt0` see at T=0+~1ms?

---

## Step 5 — escalation matrix

| Confirmed root | Owner | Fix |
|---|---|---|
| MSS clamp not present | Host-side, ours | Re-run the role; confirm `vxlan-runtime-state.service` is enabled + active |
| FDB missing peer | Host-side, ours | Inventory bug — confirm `peers:` includes the missing mesh IP (ALL peers including self; role auto-filters self) |
| MTU set lower than expected | Host-side, ours | Adjust segment `mtu:` based on actual underlay PMTU |
| WG tunnel down | Host-side, ours (netbird) | `netbird status`, `netbird up`, check relay availability |
| Device hardcodes 1500 + no PMTUD | Device, vendor | Either jumbo-on-local-segment workaround, or vendor escalation, or rule out L2 flatten and use L3 |
| Device's own switch / cable | Device-side, customer | Replace cable, switch port, NIC |

---

## See also

- [`ops/scripts/vxlan-rca.sh`](../../ops/scripts/vxlan-rca.sh) — the host-side snapshot tool
- [`docs/operations/vxlan-runbook.md`](vxlan-runbook.md) — apply / validate / teardown a segment
- `docs/design/l2-vxlan-networking.md` — design + per-transport behavior
- [`ops/ansible/roles/vxlan/`](../../ops/ansible/roles/vxlan/) — role source
