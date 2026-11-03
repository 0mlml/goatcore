# NetBird boot-wedge self-heal

**Problem it solves:** a mesh host that reboots can start `netbird` before the
network to its management/relay endpoints is usable, wedge its engine at a
handful of peers, and never recover — the ACTIVE node goes unreachable to its
own peers for hours while self-reporting healthy. RCA: F-261.
This is the durable fix.

## Design in one paragraph

Two layers on every mesh host that runs netbird as a peer. **Layer 1 (root
cause):** a systemd drop-in orders `netbird.service` after
`network-online.target` so it stops starting into "no route to host".
**Layer 2 (backstop):** a timer-driven watchdog that restarts netbird *once*,
rate-limited, when the control plane is up but the connected-peer count is
stuck low — the exact wedge signature — and publishes a metric so the wedge is
visible off-host. Layer 1 makes the wedge rare; Layer 2 makes it self-clearing
when it happens anyway (the upstream engine non-recovery bug is unfixed).

## Why a watchdog and not just the ordering fix

`network-online.target` reduces the racy start but does not guarantee the
engine can't wedge — the observed failure was **non-recovery**: netbird's
process stayed alive (`Restart=always` never triggers) with its internal engine
latched in a bad state (ICE agents uninitialized, signal outbound channel
backpressured), not retrying. Only a full restart clears it. So the backstop is
load-bearing, not belt-and-suspenders.

## The wedge signature (what the watchdog keys on)

Restarting netbird is mildly disruptive (~30s tunnel rebuild), so the watchdog
acts only on the precise signature and never on a genuine outage:

- **Control plane UP** — `Management: Connected` AND `Signal: Connected`. If
  either is down it's a real upstream problem a restart can't fix; the watchdog
  does nothing (and resets its streak). This is what distinguishes a wedge from
  an outage.
- **Peers sustained-low** — connected peers `< netbird_selfheal_min_peers` for
  `netbird_selfheal_sustained_checks` consecutive samples (default 4 × 90s ≈
  6 min). `min_peers` is set per site to ≈ half the healthy steady-state
  connected count.

On a confirmed wedge it runs `systemctl restart netbird`, then:

- **Cooldown** `netbird_selfheal_cooldown_s` (default 30 min) before it may
  restart again — a restart needs time to rebuild tunnels before re-judging.
- **Restart cap** `netbird_selfheal_max_restarts_per_boot` (default 3). Past
  that it stops and escalates (leaves `netbird_selfheal_wedged 1` in the metric
  for a human) — if three restarts didn't fix it, it isn't the boot race.
- **Budget decay** `netbird_selfheal_restart_budget_window_s` (default 24h). A
  restart older than the window stops counting against the cap.

  The cap used to reset **only** on a new boot_id, which disarms a long-lived
  node indefinitely.

  Anti-thrash is intact: three restarts *inside* the window still cap and
  escalate, because if three in a day didn't fix it a fourth won't. What changes
  is that a node saved once a month is no longer disarmed by its own successes.
  Set 0 for the old per-boot-only behaviour.

The watchdog is dependency-free: it parses plain `netbird status` text (no
jq/python), so it works on a minimal host.

### Signature 3 — everything reads healthy and no data moves

Signatures 1 and 2 both key off the **control** plane: one needs it up with
peers low, the other needs it down with its endpoint reachable. A wedge that
leaves the control plane looking perfect is invisible to both.

So signature 3 probes the data plane itself:

- **Control plane UP and peers at/above the floor** — i.e. signatures 1 and 2
  both say healthy. Signature 3 only looks at the case they call fine.
- **No configured target answers a TCP connect**, sustained for
  `netbird_selfheal_sustained_checks` samples. **Success on ANY target means
  healthy** — the question is "does the tunnel carry traffic at all", not "is
  every peer up".

Three constraints are load-bearing:

- **TCP only, never ICMP.**
  F-067
  documents a transport state where TCP forwarding fails per-client while ping
  keeps working — a ping probe reports healthy straight through a real outage.
  And `BREAKGLASS` hosts are granted `tcp 22` outbound *only*
  (ADR 1114), so ICMP
  from one reads broken forever and would bounce netbird every cooldown.
- **Targets are explicit, never derived.** A guessed target the ACL does not
  permit is indistinguishable from a wedge and would restart a healthy daemon.
  Empty list ⇒ signature skipped entirely.
- **More than one target**, so a single peer rebooting is not read as a wedge.

Shares the cooldown and per-boot restart budget with the other two. **Default
off.** Contract pinned by `ops/ci/test-netbird-selfheal-signatures.sh` — 10
cases, and the "must not restart a healthy daemon" direction is tested as hard
as the firing direction, because a self-heal that bounces a working node is
worse than one that misses a wedge.

## Metrics (off-host visibility)

Written to `/var/lib/netbird-boot-selfheal/metrics.prom` (node_exporter
textfile format). The incident's core blind spot was that the node was reachable
to *itself* while no peer could reach it; these make that state legible:

| metric | meaning |
|---|---|
| `netbird_selfheal_mgmt_connected` / `_signal_connected` | control-plane up (1/0) |
| `netbird_selfheal_peers_connected` / `_peers_total` | connected vs known peers |
| `netbird_selfheal_wedged` | 1 = control-plane-up but peers sustained-low |
| `netbird_selfheal_low_streak` | consecutive low samples |
| `netbird_selfheal_restart_total` | restarts this boot |
| `netbird_selfheal_last_restart_timestamp` / `_last_run_timestamp` | freshness |

The relay-health dashboard consumes `_peers_connected` / `_wedged` for its
cross-host reachability and L0 FEDERATION surfaces (a wedged active node must be
visible without SSHing three boxes). See
relay-health-dashboard.md.

## Configuration & deploy

Role `ops/ansible/roles/netbird-boot-selfheal`. Opt-in per host group via
`netbird_selfheal_enabled: true` (+ `netbird_selfheal_min_peers`) in
`group_vars`. Wired default-off into
`mgmt-host-baseline.yml` so every future mgmt host inherits it once its group
opts in.

Deploy (mesh SSH, tiny + idempotent):

```
cd ops/ansible
OBJC_DISABLE_INITIALIZE_FORK_SAFETY=YES aws-vault exec usareur-af --no-session -- \
  ../scripts/ansible-preflight.sh -i inventory/mgmt-hosts.ini -i inventory/mgmt-hosts-mesh.ini \
    -e target=zagros_hosts:zagros_b_hosts netbird-boot-selfheal-deploy.yml
```

Rollback: the artifact is the committed `ops/netbird/netbird-boot-selfheal.sh`
(ADR 1060 ledger via the role's `rollback-ledger` meta-dep). Disabling is
`netbird_selfheal_enabled: false` + re-apply, or `systemctl disable --now
netbird-boot-selfheal.timer` on the host; the ordering drop-in is inert if left.

## Scope / boundaries

- Host-local netbird client resilience only. **Not** federation transport
  (`federation-proxy`, ADR 1085), **not** relay-side health
  (relay-health-dashboard.md), **not** fabric QoS.
- Does not fix the upstream netbird engine non-recovery bug — it detects and
  clears the symptom. If netbird ships a real fix, Layer 2 becomes redundant
  (Layer 1 stays correct regardless).
