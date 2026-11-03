# Network-health monitoring — active reachability + degradation early-warning

**Status:** stubbed-complete skeleton (2026-07-28). Detection logic is real;
alert *dispatch* is a stub for the operator to wire PagerDuty/Matrix/bus into.
Born from F-263.

## The gap this closes

Every existing health surface — `goat health`, panoscope, the environment-health
domains, the relay-health host band — is **self-reported**: each node describes
its own view. A node that is **up but unreachable** (relay-only, relay
peer-forwarding stale) self-reports *healthy*. Nothing actively tries to reach it
from outside, so that failure class is invisible until a human notices pages not
loading. We also historically **conflated offline peers with dysfunction** (a
node showing many peers "Connecting" looked broken but was mostly just offline peers).

This surface adds the two things missing: **(1) active off-node reachability
probing** — the ground truth of "can this node actually be reached" — and
**(2) proven degradation leading-indicators** logged over time so we see a node
sliding *before* it falls over. Honesty discipline throughout: **offline peer ≠
unreachable node ≠ degraded node**, and a state is never a guessed colour.

## Architecture (reuses the relay-health ingest — ADR 1058, complexity budget)

```
  each mgmt/site node                        an OFF-node vantage (≥2 for quorum)
  ┌───────────────────────┐                  ┌──────────────────────────────┐
  │ netbird-degradation-  │                  │ reachability-canary.sh        │
  │  probe.sh (leading     │                  │  — for each declared node:    │
  │  indicators from the   │   POST           │    ICMP mesh-IP + HTTP svc    │
  │  netbird client.log)   │──── JSON ───┐    │  — emits a reach matrix       │
  └───────────────────────┘             │    └───────────────┬──────────────┘
                                         ▼                    │ POST JSON
                              token-gated ingest  ◄───────────┘
                              (existing relay-metrics-ingest.py,
                               new /nethealth-ingest route)
                                         │
                        ┌────────────────┼─────────────────┐
                        ▼                                   ▼
                 nethealth.html                     alert-eval.py (timer)
                 (new dashboard: reach              threshold checks →
                  matrix + per-node                 dispatch_alert()  ← STUB
                  degradation + trend)               (PagerDuty/Matrix/bus TODO)
```

Vantage independence is the whole point: the canary runs on hosts that are **not**
the target. A node can never mark *itself* reachable.

## Signals (all proven in F-263, not invented)

**Active reachability (ground truth)** — per (vantage → node):
- `icmp_reachable` (mesh IP ping = the **overlay** path), `rtt_ms`, `loss_pct`
- `lan_reachable` (ping a declared LAN IP = a **non-overlay** path) +
  `overlay_only_down` = LAN-up **and** overlay-down. This is the F-263 signature:
  the host is alive and the *netbird path specifically* is being dropped
  (firewall/routing), not a dead node. Set the LAN IP on a same-LAN vantage.
- `service_reachable` (HTTP to a declared per-node service), `http_status`, `connect_ms`
- Verdict per node: **reachable** (≥1 vantage reaches it) / **degraded**
  (some vantages fail) / **unreachable** (no vantage reaches it) / **unknown**
  (no fresh canary report — never a guessed green).

**Per-node netbird degradation (leading indicators)** — from the client log:
- `srflx_candidates` — 0 = **relay-only** (fragile: fully dependent on relay
  peer-forwarding, the exact F-263 failure surface).
- `peers_online_stuck` — peers seen in `ICE retries exhausted → hourly retry`
  that are **online** (have a recent handshake), i.e. genuinely can't connect —
  **explicitly excludes offline peers** (the F-263 miscount trap).
- `relay_forward_errors_60s` — `closed by server` / `failed to unsubscribe from
  peer state` / `failed to write to QUIC stream` — the relay peer-forwarding
  distress the incident turned on.
- `ice_gather_timeouts_60s` — context only; **normal for a CGNAT node**, graded
  informational, never alerted on alone.
- **`overlay_drops_per_min` + `ts_netfilter_mode`** — the **direct F-263 root-cause
  detector**. Tailscale and netbird both use `100.64.0.0/10`; tailscale's default
  `netfilter-mode=on` installs an anti-spoof rule that drops `100.64.x`-source
  packets not on `tailscale0` — i.e. inbound netbird. `overlay_drops_per_min` is
  that rule's drop-counter rate (nft/iptables); **any sustained non-zero value =
  the host firewall is silently eating overlay traffic.** `ts_netfilter_mode==2`
  is the at-risk config, flagged **before** drops accrue. Fix:
  `tailscale set --netfilter-mode=off` (codified in the tailscale bring-up).

## Alerting (thresholds real, dispatch stubbed)

`alert-eval.py` runs on a timer against the ingest and evaluates:
- **OVERLAY_PACKET_DROP** (critical) — `overlay_drops_per_min` ≥ 1 sustained: the
  host firewall is dropping netbird packets. *The direct F-263 root-cause alert.*
- **OVERLAY_UNREACHABLE_LAN_UP** (critical) — a vantage sees LAN-up + overlay-down.
  *The F-263 signature from the reachability side; would have fired in ~1 min.*
- **TAILSCALE_NETFILTER_AT_RISK** (warning, preventive) — `ts_netfilter_mode==2`
  alongside netbird: the misconfiguration that causes F-263, before it drops.
- **NODE_UNREACHABLE** — `unreachable` (no vantage) sustained ≥ `unreachable_secs`
  (default 180 s).
- **NODE_DEGRADED_REACH** — reachable from some but not all vantages ≥ 300 s.
- **RELAY_FORWARD_DISTRESS** — `relay_forward_errors_60s` over threshold on a
  relay-only node.
- **PEERS_ONLINE_STUCK** — `peers_online_stuck` over threshold (online peers that
  can't connect — never offline peers).
- **CANARY_STALE** — no fresh canary report (fail-closed: silence ≠ healthy).
- **ZENOH_ROUTER_DOWN** (critical) — the router container not running, or its
  self-report stale (reads `zenoh-health.json`, #1815).
- **ZENOH_ADMISSION_SEIZED** (critical) — new-session mTLS OPEN fails while REST
  still answers: the F-253 transport seizure the container HEALTHCHECK can't see.
- **ZENOH_ROUTER_DEGRADED** (warning) — slow OPEN, CLOSE-WAIT climbing, FD near
  cap, OOM, or restarts.

**Dispatch is live (2026-07-28).** `dispatch_alert()` posts to PagerDuty Events
API v2 — routing key + endpoint from the env (`PAGERDUTY_INTEGRATION_KEY_EU` /
`PAGERDUTY_EVENTS_URL`; **EU accounts must use `events.eu.pagerduty.com`**), no
secret in-repo. dedup_key = the rule's state key, and a condition that clears
sends a **resolve** so the incident auto-closes. Proven end-to-end: a crafted
netbird outage (`NODE_UNREACHABLE` + `OVERLAY_UNREACHABLE_LAN_UP`) and a zenoh
seizure (`ZENOH_ADMISSION_SEIZED`) fired three real PagerDuty incidents and then
auto-resolved.

`dispatch_alert(alert)` is a **stub** with clearly-marked integration points for
PagerDuty (env `PAGERDUTY_ROUTING_KEY`), Matrix/hedwig, and the `work bus`. It
currently logs + writes a fired-alerts file the dashboard reads. Wire the real
channel later; nothing downstream changes.

## Deploy (skeleton — `ops/monitoring/net-health/`)

- `reachability-canary.{sh,service,timer}` → on the off-node vantages. Declares targets in `nethealth-targets.yaml`.
- `netbird-degradation-probe.sh` → folded into the existing `host-net-publish`
  timer on each mgmt node (one extra parse of the netbird log; no new unit).
- `nethealth-alert-eval.{py,service,timer}` → on the ingest host.
- `nethealth.html` → served alongside relay-health (same Ory-admin gate).
- Ansible: `net-health.yml` playbook skeleton wires the above; per-goatnet target
  list in group_vars. All flags default **off** — opt-in per goatnet.

Rollback = flags off + drop the units (ADR 0859). No change to any enforcement
path; this is reveal-only monitoring.
