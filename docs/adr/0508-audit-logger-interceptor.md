# ADR 0508 — Audit-logger interceptor and Prometheus metrics

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0501-rust-interceptors.md`](0501-rust-interceptors.md), [`0507-event-telemetry-schema.md`](0507-event-telemetry-schema.md), [`0811-audit-log-integrity.md`](0811-audit-log-integrity.md) |

## TL;DR (for a layperson)

Every message that flows through the data fabric is observed
by a small Rust plugin called the audit logger. It writes a
record of each message — who sent it, who received it, when —
to the audit storage tier, and exports operational metrics so
we can alert on anomalies. This is the cheapest place to put
universal audit, and putting it inside the fabric router means
it can't be bypassed.

## Context

Audit logging written into every consumer is unreliable
(consumers can skip it; consumers can be silently dropped;
consumers vary in implementation). Audit at the fabric layer
is universal: every message that flows through the router
gets recorded.

The interceptor pattern in Zenoh
([`0501`](0501-rust-interceptors.md)) makes this practical.

## Decision

The audit-logger interceptor is the universal fabric-side
audit-and-metrics component:

- **Implementation:** `zenoh-plugins/audit-logger`, Rust.
  Subscribes to `ops/**`. Plugin name + config key:
  `audit_logger`.
- **Output:** writes structured audit records (per
  [`0507`](0507-event-telemetry-schema.md)) to the audit
  storage tier ([`0811`](0811-audit-log-integrity.md)).
- **Metrics:** Prometheus `/metrics` endpoint exposes per-
  topic message counts, byte counts, and `processing_duration`
  histogram. Alert rules on these are baked in.
- **CI:** unit tests cover the interceptor logic; the
  Cargo workspace has cargo-audit + clippy + test in CI.
- **Deployment:** built from source, signed image, pinned
  digest, deployed via the Zenoh Ansible role.

Plugin tracks the Zenoh runtime pin (1.0.x family per
[`0500`](0500-zenoh-data-fabric.md)) and is rebuilt against
each pinned Zenoh release. The original draft pinned the
plugin specifically at "1.0.4" — that level of per-plugin
specificity is not in the design doc; the canonical pin
target is the runtime, and the plugin tracks. Per-plugin
pinning would be a further hardening; flagged as a forward
consideration if reproducibility evidence requires it.

## Alternatives considered

- **Per-consumer audit logging.** Skippable; inconsistent.
  Rejected.
- **Out-of-process audit sidecar.** Adds latency; can be
  bypassed if interceptor is not also installed. Rejected.
- **No fabric audit.** Loses universal AU-2 coverage.
  Rejected.

## Consequences

**Positive.** Every fabric message produces an audit record.
Bypassing requires removing the interceptor (a configuration
change visible in CM-3 review). Prometheus metrics give
operational signal without separate instrumentation.

**Negative.** Throughput overhead per message. We size and
monitor; if throughput becomes a problem we revisit
sampling vs full audit.

## Assessor view (SCA-R)

The audit-logger serves AU-2 (event logging), AU-3 (content
— enforced by the schema in [`0507`](0507-event-telemetry-schema.md)),
AU-9 (protection of audit information — paired with the
audit-log integrity tier in [`0811`](0811-audit-log-integrity.md)),
AU-12 (audit generation — by the interceptor itself), and
SI-4 (system monitoring — Prometheus metrics drive
alerting).

## Trigger conditions for revisit

- Throughput overhead becomes operationally limiting.
  Implement sampling (with explicit ADR documenting which
  message classes can be sampled and why).
- An audit gap is identified. Add coverage in the
  interceptor; do not bypass.

---

*See `GLOSSARY.md` for terms and acronyms.*
