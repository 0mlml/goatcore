# egress-shaping — ADR 1081 Layer 3 (Thor egress survivability)

Host-local WAN-egress shaping so the small control plane keeps moving when a
saturated bulk transfer bufferbloats the uplink. Implements Layer 3 of
[ADR 1081](../../../../docs/adr/1081-control-plane-storm-resilience.md) /
[control-plane-storm-resilience.md](../../../../docs/design/control-plane-storm-resilience.md)
§"Layer 3". Ansible-only; no fork changes.

## Boundary (read first)

**Host egress only — NOT fabric QoS.** The DSCP marks this role sets are a
host-local cake-tin selector; cake's `wash` strips them on egress, so nothing
reaches the wire or the Zenoh fabric. The ADR 1033
"never DSCP for fabric classes" rule is untouched. This is a different layer;
the boundary is stated in every artifact.

## What it does (behind one toggle, default OFF)

`egress_shaping_enabled: false` gates the whole role. When enabled on a host:

1. **cake** on the runtime-discovered default-route (WAN) interface:
   `tc qdisc replace dev <iface> root cake bandwidth <cap>mbit diffserv3 wash`.
2. **Control-plane priority** via a host-local nftables table that marks
   (a) rathole control channel (`tcp dport 2333`) and (b) mgmt/signal to the
   relay public IPs (`tcp dport 443`, relay IPs resolved at runtime from
   `/etc/rathole/client-*.toml`) into the `cs6` DSCP → cake diffserv3's top tin.
3. **BBR**: `net.core.default_qdisc=fq` + `net.ipv4.tcp_congestion_control=bbr`
   (cubic collapses under ~10% loss; BBR degrades ~linearly).

A systemd oneshot re-applies (1)+(2) at boot. Toggle off → the unit's teardown
reverts the qdisc and drops the nft table; the sysctl drop-in is removed
(BBR/fq revert on next boot).

## Required parameter — no guessed default

`egress_shaping_bandwidth_mbit` is **required when enabled** (the play fails
closed otherwise). Set it from the host's **measured** egress envelope
(host-net-health / node-net-health rolling measurement), floored to **~90% of
line rate**. Shaping just under the true rate keeps the queue on the Thor where
cake manages it, not in the modem buffer. On a moving link
re-derive from the rolling envelope — never a nameplate number.

| var | default | meaning |
|---|---|---|
| `egress_shaping_enabled` | `false` | master toggle |
| `egress_shaping_bandwidth_mbit` | `""` (required when enabled) | cake cap, ~90% of measured line rate |
| `egress_shaping_cake_diffserv` | `diffserv3` | cake tin mode |
| `egress_shaping_ctl_dscp` | `cs6` | host-local tin selector (washed on egress) |
| `egress_shaping_rathole_ctl_port` | `2333` | rathole control dport |
| `egress_shaping_mgmt_port` | `443` | mgmt/signal dport (marked only to relay IPs) |
| `egress_shaping_bbr_enable` | `true` | BBR + fq (same master toggle) |

## Usage

Applied in the mgmt-host baseline path (role is default-off, so a plain
baseline run is a no-op). To enable on a canary host after the gate below:

```
cd ops/ansible
OBJC_DISABLE_INITIALIZE_FORK_SAFETY=YES \
  ../scripts/ansible-preflight.sh -i inventory/mgmt-hosts-mesh.ini \
  -l mgmt-zagros-01-thor egress-shaping.yml \
  -e egress_shaping_enabled=true \
  -e egress_shaping_bandwidth_mbit=<measured-90pct>
```

Prefer setting `egress_shaping_enabled` + `egress_shaping_bandwidth_mbit` in the
host's group_vars once the gate passes, so a routine refresh keeps it applied.

## Verification — the tc-netem canary gate (ADR 1081 L3)

Deploy runs later through site windows. **Do not deploy from CI.** Before
enabling fleet-wide, gate on a single canary Thor:

1. **Baseline (no shaping):** on the canary, `iperf3` a saturating bulk flow
   over the uplink and record idle vs under-load RTT to a relay
   (`ping <relay-ip>`), and a signal round-trip time.
2. **Inject the storm shape** on the WAN iface:
   `tc qdisc add dev <iface> root netem loss 10% delay 20ms`
   (or layer netem under the cake test per your harness).
3. **Enable the role** on the canary (command above) with the measured cap.
4. **Gate criteria** (all must hold; log the result to `docs/measurements/`):
   - rathole tunnel uptime **≥99% / 1 h** under the repeated-drop storm shape
     (`systemctl status rathole-client@* ` + `journalctl` flap count);
   - RTT inflation under saturating bulk **≤2× idle** (observed ~6× pre-fix);
   - a signal round-trip **completes during** the bulk transfer.
5. **Confirm BBR actually took** (the role reports this): the effective-state
   report must show `tcp_congestion_control=bbr`. If it shows `cubic`, BBR is
   not available on this kernel — check `tcp_available_congestion_control`
   (the JetPack 7 Tegra kernel on Thor is the specific risk); do not claim the
   BBR leg until it reads `bbr`.
6. Remove the netem shape (`tc qdisc del dev <iface> root netem`) and re-verify
   the cake qdisc is what the role set.

**Rollback:** `egress_shaping_enabled=false` re-run, or `systemctl stop
goat-egress-shaping` (runs the teardown). qdisc + nft table revert idempotently;
BBR/fq revert on next boot once the drop-in is removed.
