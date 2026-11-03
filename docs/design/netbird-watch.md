# netbird-watch — peer-side liveness watchdog for the netbird daemon

**Status.** Design — not yet implemented. Block 39 in `../project/implementation-plan.md`.

**Sibling pattern.** [`ops/ansible/roles/mgmt-host-compose-watch/`](../../ops/ansible/roles/mgmt-host-compose-watch/). `netbird-watch` follows the same shape — small bash + systemd watcher, polled probe loop, journald logging, exponential backoff on remediation, metrics exposure. It runs on every netbird-enrolled peer, not just mgmt-hosts.

## Problem

The netbird daemon loses connectivity in observable but uncatalogued failure modes — typically after WAN link flaps, host suspend / resume, container daemon restarts, or station power cycles. Existing recovery paths cover *some* cases:

- The daemon's own internal reconnect logic handles transient mgmt-plane and signal-plane drops.
- `systemd Restart=on-failure` covers daemon crashes.
- WireGuard's persistent-keepalive handles short NAT-mapping expirations.

What is not covered: states where the daemon process is alive, the WG interface is up, but the peer has functionally fallen off the mesh — `netbird status` reports Disconnected from mgmt, the WG interface has no recent handshakes, peers are unreachable. Today recovery in those states requires an operator running `netbird down && netbird up` by hand.

## Non-goal: blind reconnect

The naive shape ("if anything looks wrong, restart netbird") is wrong because it conflates two failure classes:

1. **Local-peer broken** — daemon hung, mgmt connection lost, WG interface stuck. Action: restart locally.
2. **Remote peers offline** — known mesh peers unreachable because *they* are down. Action: nothing local; a local restart would tear down working tunnels for no gain.

Without disambiguation, a peer thrashes its own daemon every time some other peer goes offline. The watchdog must distinguish.

## Design

### Probe ladder

Run on each peer, every `POLL_INTERVAL` seconds (default 60). Top-down; first failing rung determines the verdict.

| Rung | Probe | What it tells us |
|---|---|---|
| 1 | `systemctl is-active netbird` (or `pgrep netbird` on container deployments) | Daemon process alive |
| 2 | `netbird status --json` parses + reports `management.connected = true` | Mgmt-plane connection healthy |
| 3 | `netbird status --json` reports `signal.connected = true` | Signal-plane connection healthy |
| 4 | `ip link show wt0` (or configured interface) reports state UP | WG interface present |
| 5 | `wg show wt0 latest-handshakes` — at least one peer has a handshake within `HANDSHAKE_FRESHNESS` (default 180s) | WG data plane has at least one live counterparty |
| 6 | ICMP / TCP probe to N known mesh peers from a configured list | Reachability check |

Verdict matrix:

- Rungs 1–5 fail → **local issue**; trigger remediation (escalation ladder below) and emit `netbird_local_unhealthy{rung=...}`.
- Rungs 1–5 pass, rung 6 fails for *all* probed peers → **likely-local** (data plane stuck despite control plane reporting healthy); treat as local issue.
- Rungs 1–5 pass, rung 6 fails for *some* peers → **remote issue**; *no* local action; emit `netbird_peer_unreachable{peer=...}` per failing peer.
- All rungs pass → emit `netbird_watch_probe_total{outcome="healthy"}` and continue.

### Escalation ladder (local-issue path)

When the verdict is "local issue," do the least-destructive thing first; escalate on repeated failure.

| Step | Action | Cooldown to next step |
|---|---|---|
| 1 | `systemctl restart netbird` (or container `docker restart netbird`) | 60s |
| 2 | `systemctl restart netbird` again — second attempt only if probe still fails after step 1's cooldown | 120s |
| 3 | `netbird down && netbird up` — last resort, tears down + re-establishes from scratch | 240s |
| 4 | Hold; emit `netbird_watch_giving_up`, page operator via Block 17A.5 policy engine | until next manual intervention |

Exponential backoff between failed escalation cycles (60s → 120s → 240s → 480s, capped at 30 min). On a successful probe after remediation, reset the backoff and emit `netbird_watch_recovery_total`.

`netbird down && netbird up` is *not* the first action — it's the sledgehammer. `systemctl restart` preserves the daemon's own teardown logic and is sufficient for the majority of observed failure modes.

### Metrics

Exposed on `localhost:9754/metrics` (Prometheus text format). Scraped by the per-site Prometheus.

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `netbird_watch_probe_total` | counter | `outcome` (healthy / local_unhealthy / peer_unreachable) | Probe loop outcomes |
| `netbird_local_unhealthy` | gauge | `rung` (1-5) | 1 if local probe currently failing at this rung; 0 otherwise |
| `netbird_peer_unreachable` | gauge | `peer` | 1 if specified peer unreachable; 0 otherwise |
| `netbird_watch_remediation_total` | counter | `step` (1-3), `outcome` (success / fail) | Each remediation attempt |
| `netbird_watch_giving_up_total` | counter | — | Page-the-operator events |
| `netbird_watch_recovery_total` | counter | — | Successful recoveries after remediation |

### Alert rules

Live alongside existing rules in [`ops/monitoring/prometheus/rules/`](../../ops/monitoring/prometheus/rules/) as `netbird-watch.yml`.

- `NetbirdWatchFrequentRemediation` — `rate(netbird_watch_remediation_total[1h]) > 1/3600` (more than one remediation per hour sustained over 1h). **Severity: warning.** A working watchdog firing rarely is fine; firing often is a real bug being papered over.
- `NetbirdWatchGivingUp` — `increase(netbird_watch_giving_up_total[10m]) > 0`. **Severity: critical.** Watchdog has exhausted its escalation ladder.
- `NetbirdPeerUnreachableMany` — `count by (site) (netbird_peer_unreachable == 1) > 3`. **Severity: warning.** Multiple peers unreachable from one vantage suggests a fleet-level problem, not a single peer being down.
- Notably absent: an alert on `netbird_local_unhealthy == 1`. The watchdog *handles* that locally; the alert is on remediation-rate, not on the underlying signal.

### Wide events (Block 27)

Each remediation attempt emits an OTLP wide event with attributes:

- `dbb.netbird_watch.event` — `probe_failed | remediation_attempted | recovered | gave_up`
- `dbb.netbird_watch.failed_rung` — 1-5 for local-issue events
- `dbb.netbird_watch.remediation_step` — 1-3 for remediation events
- `dbb.netbird_watch.host` — peer hostname / mesh ID
- standard correlation_id for joining across mgmt + signal + audit trails

Querying "show me every netbird-watch remediation across the fleet in the last 7 days" becomes one OpenObserve query joined with mesh + signal + audit by correlation_id.

### Configuration

Per-host config at `/etc/netbird-watch/config.env`, written by the Ansible role:

```
POLL_INTERVAL=60
HANDSHAKE_FRESHNESS=180
NETBIRD_INTERFACE=wt0
KNOWN_PEERS_FILE=/etc/netbird-watch/known-peers.txt
METRICS_PORT=9754
LOG_LEVEL=info
```

`known-peers.txt` is one peer mesh-IP per line, written by Ansible from the per-site inventory. Production sites pin to ~3 known-good peers across the site (mgmt-A, mgmt-B, observability host); validation envs pin to 1-2.

## Anti-patterns the watchdog must avoid

- **Restart on remote-peer failure.** Already covered in the verdict matrix — explicit because this is the failure mode a naive watchdog hits first.
- **Suppressing the underlying bug.** The remediation-rate alert exists to surface "the watchdog is firing too often" as a real signal that something is broken below it. A silent watchdog is worse than no watchdog.
- **Restart loops without cooldown.** Exponential backoff is non-negotiable.
- **Tearing down healthy WG tunnels.** `netbird down && netbird up` is step 3 of the escalation ladder, not step 1. `systemctl restart netbird` is the lighter touch and handles the majority of cases.

## Retirement / supersession

If a future netbird release ships native handling for the failure modes that drive watchdog activations (visible via low `netbird_watch_remediation_total` rates), the role retires. Until then, the metric *is* the bug report — sustained activation rate per failure-mode rung tells us which upstream issue to file.

The watcher is also a candidate Block 38 chaos probe target — `netbird-watch-recovers-after-deliberate-daemon-kill` would be a low-effort probe that exercises the escalation ladder against a known-good system, and would catch regressions in the watchdog itself.

## Cross-refs

- Sibling watcher pattern: [`ops/ansible/roles/mgmt-host-compose-watch/README.md`](../../ops/ansible/roles/mgmt-host-compose-watch/README.md)
- Wide-event observability: [`observability-wide-events.md`](observability-wide-events.md)
- Continuous architectural chaos (probe candidate): [`continuous-architectural-chaos.md`](continuous-architectural-chaos.md)
- Notification adapter for `NetbirdWatchGivingUp` paging: per-site policy engine ([`egress-proxy.md`](egress-proxy.md) §6)
- Implementation tracking: `../project/implementation-plan.md` Block 39
