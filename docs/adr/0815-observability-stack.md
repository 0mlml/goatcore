# ADR 0815 — Observability stack — Prometheus + Alertmanager + isitup + gatewatch

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0508-audit-logger-interceptor.md`](0508-audit-logger-interceptor.md), [`0813-service-registry-pattern.md`](0813-service-registry-pattern.md) |

## TL;DR (for a layperson)

Operational visibility into the system uses four components:
Prometheus scrapes metrics from every service, Alertmanager
fires alerts when something breaks, isitup is a public-facing
status page that runs continuous health checks, and gatewatch
correlates bridge/login events across the system. Together
they tell us what's running, what's broken, and who's doing
what.

## Context

Operators need three different views of the system:

1. **Metrics** (real-time): Prometheus.
2. **Alerts** (threshold-triggered): Alertmanager.
3. **Status** (operator + sponsor visibility):
   isitup at `isitup.<mesh zone>`.
4. **Audit correlation** (security): gatewatch.

Each runs on the mesh; each is sovereign; each is open-
source; each scrapes/aggregates from the same well-defined
producer endpoints.

## Decision

The observability stack:

| Component | Role |
|---|---|
| **Prometheus** | Metrics scrape from every service `/metrics` endpoint. Per-site instance. |
| **Alertmanager** | Alert routing; per-site instance. |
| **node_exporter / postgres_exporter / chrony_exporter** | Per-host metrics. |
| **isitup** | Health checks every 5 min, 30-day history. Auto-discovers services from `services.env` (generated from `services.yaml`). |
| **gatewatch** | Bridge access monitor; correlates bridge connection logs with Authelia auth events. Serves gate status at `/api/gate-status` (CORS-enabled) consumed by all dashboards. |

Sources: `ops/monitoring/` (Prometheus + Alertmanager
skeleton), `ops/status/` (isitup), `ops/gatewatch/`
(gatewatch).

Deployment topology:

- **Dev network host:** runs `isitup` and `gatewatch`.
  It does NOT run Prometheus or Alertmanager — those are
  the sovereign mgmt-host's job, not the dev network host's.
- **Production mgmt-hosts:**
  run Prometheus + Alertmanager + `node_exporter` /
  `postgres_exporter` / `chrony_exporter` via the per-env
  observability bootstrap phase.
- **Status + gatewatch on each mgmt-host pair:** as the
  fleet expands, isitup and gatewatch deploy alongside
  Prometheus / Alertmanager on each site's Router-A /
  Router-B pair. The dev network host's instances are dev/demo;
  production per-site observability is the shipped pattern.

## Alternatives considered

- **Cloud SaaS (Datadog / NewRelic).** Loses sovereignty /
  Island Mode. Rejected.
- **No status page.** Loses operator + sponsor visibility.
  Rejected.
- **OpenTelemetry collector central pipeline.** Useful
  long-term but adds operational dependency now. Held;
  Phase 3+ enhancement candidate.

## Consequences

**Positive.** Sovereign observability. Per-site Island Mode
correct. Service registry feeds isitup automatically.

**Negative.** Components to operate. All open-source and
mature.

## Assessor view (SCA-R)

The observability stack serves SI-4 (system monitoring),
AU-5 (response to audit logging failures), and IR-4
(incident handling — operator visibility supports
detection-to-response workflow). Prometheus alert rules
are reviewable in `ops/monitoring/`.

## Trigger conditions for revisit

- Metric volume exceeds Prometheus single-server scale per
  site. Adopt federation or remote-write to a long-term
  store. Do not lose Island Mode.
- A specific authorization mandates a different stack.
  Federate via remote-write; do not abandon the per-site
  baseline.

---

*See `GLOSSARY.md` for terms and acronyms.*
