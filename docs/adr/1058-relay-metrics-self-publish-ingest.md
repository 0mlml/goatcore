# ADR 1058 — relay metrics self-publish → management-host ingest (auto-populate the relay-health dashboard)

**Status:** Accepted — built + **deployed + live-fired on a production goatnet 2026-06-23**: every relay self-publishes, the token-gated ingest on the management host receives them, and the dashboard auto-populates real throughput for every relay with zero AWS creds / zero SSM / zero operator action; enrollment verified unbroken through the additive Traefik route. Two bugs caught + fixed live (the `relay-<name>` → `relays.conf` name strip; a Traefik keep-alive `501` from an undrained request body).
**Repo scope:** goat core — `ops/relay/metrics-publish.sh`, `ops/relay/health/relay-metrics-ingest.py`, `ops/relay/health/relay-health-probe.sh`, the `goat-relay` + `relay-health` ansible roles, the management host Traefik dynamic config.
**Related:** `docs/design/relay-health-dashboard.md` (this is its named "self-publish end state") · F-213 · ADR 0860 (admin-gate pattern the dashboard mirrors) · [ADR 1055](1055-relay-observability-logging-stats-anomaly-processor.md) (the *pull*-model relay stats endpoint — distinct from this; see Alternatives).

## Plain-language summary

The relay-health dashboard shows reachability + latency automatically, but **throughput/cpu/peers stayed blank** ("operator-deep only") because that data can only be read *on* the relay host, and the credential-free hosted prober has no way in — `--deep` needs AWS/SSM creds we deliberately keep off the management host. This makes the relays **publish their own host metrics**: each relay runs a small timer that reads its own `/proc/net/dev` + `docker stats` + relay logs locally and POSTs the result to a token-gated ingest endpoint on the management host, which the dashboard prober folds in. No AWS creds anywhere, no operator in the loop — the throughput panel auto-populates.

## Context

- Relays are **not** mesh/fabric peers, so on-bus health surfaces can't see them; the relay-health dashboard probes their public IPs blackbox (works during outages).
- The hosted prober timer runs **fast probes only** (credential-free). Throughput/cpu/peers come only from `--deep`, which SSMs into each relay — needing AWS creds. Putting long-lived creds on the on-prem management host is the posture we reject, so throughput never auto-populated.
- The relay **can** reach the management host: the rathole **server** runs on each relay (binds public `:443`), tunnelled to the rathole **client** on the management host which forwards to the management host's Traefik. So a process on a relay that connects to the relay's own `:443` hairpins through the tunnel to the management host — an existing, authenticated-by-rathole path, no new relay inbound port.

## Decision

**Relays self-publish; the management-host ingests; the prober merges.** Push model.

1. **Relay publisher** (`relay-metrics-publish.timer`, ~15s): `ops/relay/metrics-publish.sh` reads the *same* metrics `--deep` collects, but **locally** (no SSM) — `/proc/net/dev` rate-diffed (rx/tx bytes+pps), `docker stats` goat-relay (cpu/mem), `docker logs` (peers_5m), `df`/`du` (disk + biggest container log), `systemctl`/`ss` (service states) — plus its own rolling history ring, into the identical JSON `deep` shape. It POSTs that to the management host with a shared bearer token, over the rathole `:443` path.
2. **management-host ingest** (`relay-metrics-ingest`, a small container on the mgmt network): validates the bearer token + that the relay name is known, and writes `<name>.json` into `/opt/relay-health/www/ingest/`. A Traefik router on the **enrollment** entrypoint (`Host(netbird.<fqdn>) && PathPrefix(/relay-metrics-ingest)`, additive, higher-priority than the mgmt catch-all) routes to it; **not** behind oauth2-proxy (relays can't do OIDC) — the bearer token is the gate.
3. **Prober merge** (`--ingest-dir`): the hosted fast-timer prober, for each relay, reads its ingest file; if present and fresh, it becomes that relay's `deep` block. So the **credential-free** hosted timer now has a throughput source and the dashboard auto-populates — same freshness/honesty rendering (stale ingest → greys, never false-fresh).

Identity/auth: a per-goatnet shared bearer token (`<GOATNET>_RELAY_METRICS_BEARER_TOKEN`, env → group_vars → both the relays and the management-host ingest). Low-sensitivity operational telemetry; the token stops arbitrary injection. The endpoint only accepts the known relay names from `relays.conf`.

## Alternatives considered

- **Pull: relay exposes a stats endpoint, management host scrapes it (ADR 1055).** That's a *different*, complementary design the relay-area session owns — it needs a new relay **inbound** port (SG/terraform change) and is mesh-internal-scrape oriented. We deliberately chose push to (a) avoid a new relay inbound port + relay SG/terraform change, (b) reuse the existing rathole path, and (c) not collide with ADR 1055's in-flight work. The two can coexist (push now for the dashboard; ADR 1055's richer stats later).
- **Credentialed EC2 collector running `--deep` on a timer.** Works, but introduces a long-lived credentialed always-on collector (IAM role + a host to run it). More moving parts + a standing credential vs. the relays reading their own host with no creds at all.
- **AWS creds on the management host for a scheduled `--deep`.** Rejected — long-lived AWS creds on the on-prem management host is the exact posture we avoid.

## Consequences

- Throughput/cpu/peers **auto-populate** on the hosted dashboard with **zero AWS creds anywhere** and no operator action. The laptop `--deep` path still works and overrides (operator vantage).
- New surface: a token-gated ingest endpoint reachable via the enrollment path, and a metrics publisher on every relay. The ingest is additive Traefik (parse error keeps last-good config; enrollment unaffected) + a fail-closed token check; the publisher is a credential-free local read + outbound POST.
- Honesty preserved: an ingest file that goes stale greys the panel (the per-panel freshness stamp), never reads live when old; a relay that stops publishing degrades to "operator-deep only," not a false zero.
- Reversible: disable `relay_metrics_publish_enabled` (relays stop posting) + drop the ingest route/container; the dashboard returns to operator-deep-only with no other change.
