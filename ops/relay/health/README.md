# goat relay-fleet health dashboard

A from-scratch, hackable health + metrics dashboard for the relay tier — the
public ingress that fronts `netbird.<goatnet-zone>:443` enrollment.
Relays are **not** mesh/fabric peers, so `goat health` / panoscope can't see
them; this probes their **public IPs directly**, so it works precisely when a
relay is broken and the mesh is unreachable.

Custom-first by design: get this beautiful + complete, then mirror it into
Grafana later — not the other way around.

## Parts

| File | Role |
|---|---|
| `relays.conf` | the relay fleet to probe (one line per relay — **edit this**) |
| `relay-health-probe.sh` | the prober → writes `relay-health.json` |
| `relay-health.html` | the dashboard — vanilla JS, no build, fetches the JSON every 15s |
| `placement-tokens.sh` | paste-ready placement token block for a new conf line (ADR 1080 seed data) |
| `serve.sh` | local loop: re-probe + serve the page (the laptop / outage-time path) |

The dashboard is a four-level, hash-routed drilldown (back-button works):
**fleet overview** (one compact row per relay; list ⇄ map toggle) →
**per-relay detail** (`#relay=<name>` — the full probe/throughput/peers
panel) → **peer card** (ADR 1055) → **identity block** (registry /
id-console deep-link). Relays without a declared `geo=` token render in the
map view's "unplaced" tray — never at a coordinate guessed from the region
column (ADR 1080).

## Run it (laptop, outage-independent)

```
ops/relay/health/serve.sh           # fast probes, opens on http://127.0.0.1:8791
ops/relay/health/serve.sh --deep    # adds SSM service-state + metrics (needs aws-vault usareur-af)
```

Or one-shot:

```
ops/relay/health/relay-health-probe.sh           # fast (no creds)
ops/relay/health/relay-health-probe.sh --deep     # + deep
open ./ops/relay/health/relay-health.html           # (serve via serve.sh — file:// can't fetch JSON)
```

## What it shows

**Fast (no creds, seconds):**
- `:443 enroll` — TLS to the enrollment tunnel, N tries, **ok-ratio** so the
  F-213 per-connection flap is visible (3/3 green · 1-2/3 amber · 0/3 red).
- `:2333 rathole` — control-channel reachability.
- `:3478 stun` — a real coturn STUN binding request.
- `latency` — min TCP-connect RTT.

**Deep (`--deep`, via SSM):**
- `rathole svc` / `watchdog` (+ heals/1h) / `goat-relay` (+ :33073) / `coturn`.
- `peers` — distinct PeerIds in the last 5m of goat-relay logs (honest
  "active ≤5m"; goat-relay is distroless with no stats endpoint).
- `throughput` / `packets/s` — host NIC rate, diffed across runs (shows `—`
  until a second sample exists).
- `relay host` — goat-relay CPU / mem.

## Honesty discipline

Never a false green: stale JSON greys the fleet + shows age; unmeasured reads
grey; the flap shows its real ratio, not a binary up/down; rates show `—`
until two samples exist.

## Hosted (mesh-internal, admin-gated)

Deployed to the mgmt host behind Traefik + the oauth2-proxy admin gate,
registered in `isitup_services` (→ CoreDNS + isitup) as
`relay-health.<site>.<zone>`. The hosted prober runs **fast probes only**
(credential-free); `--deep` stays the laptop-operator tool. See the
`relay-health` ansible role.

## Alerting (default-OFF)

With `--alert-sink <alertmanager-base-url>` the prober, after writing the JSON,
pushes [Alertmanager v2 push alerts](https://prometheus.io/docs/alerting/latest/clients/)
(`POST <sink>/api/v2/alerts`) for the F-229 conditions:

| Alert | Fires when | Severity |
|---|---|---|
| `RelayDiskUsageHigh` | a relay root disk ≥ `--disk-warn` (85) / `--disk-crit` (95) | warning / critical |
| `RelayContainerLogLarge` | largest container log ≥ `--maxlog-warn-mb` (2048) — the *leading* indicator | warning |
| `RelayProbeUnreachable` | a relay reports **no fresh deep metrics** when deep data was expected — under self-publish a genuine liveness loss (at 100% disk it can't POST), under `--deep` the SSM probe returned nothing; a stopped instance / lost SSM reads the same, so the wording stays honest | critical |

The disk%/log come from a relay's `deep` block, populated **either by `--deep`
(operator SSM) or by `--ingest-dir` (the credential-free self-publish path, ADR
1058)** — so this rides the **hosted credential-free timer with no AWS creds
anywhere**. Re-pushing each tick keeps an alert firing; when the condition clears
the prober stops pushing and Alertmanager auto-resolves after `resolve_timeout`.
The push is **best-effort** — a sink failure never fails the probe (the render
must not depend on the pager). Nothing fabricated: alerts derive only from real
`deep.disk_pct` / `max_container_log_mb` / an absent deep block.

### Per-peer auth rejections

The `deep` block also carries `auth_reject` — a 5-minute count of relay
handshake rejections **by reason**, plus the top offending source IPs. It is
log-derived (`docker logs goat-relay`, no relay rebuild): `expired_token`
(`validator rejected: … expired auth token` — a peer presenting a stale/corrupt
cached relay token, the F-269 profile-drift single-peer isolation),
`ws_upgrade_timeout` (`WS upgrade timed out`), and `ws_handoff_eof`
(`kTLS hand-off failed … eof` — **benign** QUIC-race-loser noise, F-041,
surfaced but never colored critical). `relay-health-to-prom.py` emits
`goat_relay_auth_reject_5m{relay,reason}` +
`goat_relay_auth_reject_top_offender_5m{relay,peer_ip}`; the dashboard turns
amber and **names the offending egress IP** (the explicit counter to the
"if you can't connect, it's you" verdict — a peer being auth-rejected IS a real
per-peer fault). The `RelayPeerAuthRejected` alert is a **Prometheus rule**
(`ops/compose/goat-obs/prometheus/rules/relay-auth-reject.yml`), not a
probe-pushed alert — it fires off the emitted metric once it flows to Prometheus.

```
# the hosted timer, credential-free: self-publish ingest feeds the deep blocks,
# the prober folds them in AND alerts on them — one pass, no AWS creds
relay-health-probe.sh --ingest-dir /opt/relay-health/www/ingest --alert-sink http://alertmanager:9093 --site zagros
# operator-deep path (SSM) emits too; --emit-only pushes from an existing snapshot
relay-health-probe.sh --emit-only --out relay-health.json --alert-sink http://alertmanager:9093
```

`--emit-only` skips probing and pushes from an existing `--out` JSON (so the
render timer and the alert pass can run independently, and the alert logic is
testable without AWS).

**Turning it on (two operational settings, this is the *relay half* of the W3
interlock in `observability-resilience.md`):**
the data source is solved — the self-publish ingest (ADR 1058) feeds relay disk
to the credential-free hosted timer — so all that remains is two group_vars edits:

1. **Wire the sink** — set `relay_health_alert_sink: "http://alertmanager:9093"`
   (the site Alertmanager base URL on the mgmt compose net) and re-deploy the
   `relay-health` role. The prober timer then passes `--alert-sink`/`--site`
   (default-off until set). Alerts now appear in the Alertmanager UI.
2. **Un-noop the receiver** — set `PAGERDUTY_INTEGRATION_KEY` and deploy the
   `observability` role. *Now* the alerts page (PagerDuty,
   ADR 1059).
   This also activates the mgmt host's own existing host/disk rules.

## Scheduled TCP availability check

`ops/scripts/relay-fleet-healthcheck.sh` reads the same three inventory files as
this dashboard. It checks TCP port 33073 on each declared relay and every IPv4
address returned by its enrollment DNS pool.

```sh
ops/scripts/relay-fleet-healthcheck.sh --json
ops/scripts/relay-fleet-healthcheck.sh --environment zagros-prod --regions rla,rlb
```

The default checks ZAGROS, ZAGROS dev, and ZAGROS prod. `--regions` (or `REGIONS`)
selects relay short names, not cloud region names. Each selected environment
must retain at least one relay. Select `--environment` when using a subset
that does not occur in every fleet. DNS pools are checked in full even with a
region subset. `RELAY_HC_TIMEOUT` sets the positive connection timeout in seconds
(default 6).

Exit codes: 0 means all checks passed; 1 means a listener or DNS pool is
unavailable; 2 means invalid input, inventory, or execution environment. JSON
retains `port`, `healthy`, and `checks`, with `tier`, `target`, and `result` in
each check. Checks also identify `environment`, and inventory checks identify
`relay`. `OPEN`, `REFUSED`, `FILTERED`, and `NODNS` describe the probe result.
Run this external probe from a host using public DNS, as the hosted workflow
does. A mesh-local resolver can return additional private addresses; the probe
checks those too and reports their failure rather than hiding them.
This check proves TCP listener availability; the dashboard's protocol probes
provide separate QUIC, enrollment, and STUN evidence.

The daily `relay-fleet-health` workflow checks each environment independently.
Manual runs can select an environment before supplying a region subset.
A failing live check requires investigation; changing an inventory is justified
only by a corresponding fleet change. Run offline regressions with
`python3 ops/scripts/test_relay_fleet_healthcheck.py`.
