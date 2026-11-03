# ADR 0507 — Event telemetry schema

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0506-fabric-schema-evolution.md`](0506-fabric-schema-evolution.md), [`0508-audit-logger-interceptor.md`](0508-audit-logger-interceptor.md), `../design/event-telemetry-schema.md` |

## TL;DR (for a layperson)

Every event published to the fabric — whether it's a status
update, a security alert, or a user action — uses the same
basic shape: who, what, when, where, why, how. This common
shape lets us build observability and audit without per-event
custom parsing.

## Context

Without a common event shape, every consumer (audit logger,
observability stack, status dashboard, partner federation)
parses events differently. The result: rules can't be shared,
analytics can't aggregate, and migration becomes painful.

Adopting a common schema for all fabric events is a one-time
investment that pays off across every consumer.

## Decision

All fabric events conform to the canonical envelope defined in
`../design/event-telemetry-schema.md`.
Required envelope fields:

| Field | Type | Meaning |
|---|---|---|
| `schema_version` | string | Topic-scoped schema version. Bumped on any non-additive change. Subscribers gate on this. |
| `ts_ntp_ns` | int64 | NTP-attested wall-clock nanoseconds since Unix epoch. Sourced from `ntp-health-agent` via `timehygiene.AttestedNow()` — publishers do not call `time.Now()` directly. |
| `ts_mono_ns` | int64 | Monotonic nanoseconds since process start. Used for rate/interval computations when wall clock is suspect. |
| `site_id` | string | Authoritative site identifier; matches the `site` keyexpr segment. |
| `source_id` | string | Stable identifier for the source within its source-group; matches the `source_id` keyexpr segment. |
| `seq` | int64 | Monotonically increasing per `source_id`. Gaps detectable by subscribers → lost-sample alerts. |
| `payload` | object | Topic-specific body. |

The keyexpr hierarchy is `{domain}/{site}/{source_group}/{source_id}/{topic_path}` —
the canonical doc gives concrete examples and rules (no leading/
trailing slash, lowercase, `kebab-case`, `site` always second
segment for wildcard rollup).

**NTP attestation discipline.** When `ntp_health` is `unsynced` or
`degraded`, the publisher either refuses to publish (strict) or
stamps an `x-ntp-suspect: true` header (lenient). The admit-audit
sidecar validates timestamps on ingest. See the design doc's "NTP
attestation on every event" section.

**Versioning.** Per-topic `schema_version` evolves under
[`0506`](0506-fabric-schema-evolution.md). The envelope itself is
stable; envelope changes require a new ADR.

> **Correction note (2026-04-25).** The original draft of this ADR
> invented an 8-field envelope (`event_id`, `event_type`,
> `timestamp`, `producer`, `subject`, `attributes`, `signature`,
> `correlation_id`) that does not exist in the canonical design.
> The 7-field envelope above is the actual schema; the original
> draft was authored without opening the design doc. See commit
> history.

**Enhancement (landed 2026-04-25):** the canonical design doc now
permits two **optional** envelope fields — `signature` (Ed25519
of the canonical envelope, per ADR 0511 publisher trust model)
and `correlation_id` (UUIDv7, paired with the signal-server
correlation-ID convention from ADR 0302). They are forward-
compatible additions that don't bump `schema_version`. Producers
may include either or both for high-trust topics; consumers must
tolerate their absence. See
`../design/event-telemetry-schema.md`
"Optional envelope fields."

## Alternatives considered

- **No common schema.** Per-consumer parsing pain.
  Rejected.
- **Adopt CloudEvents.** Useful pattern; we adopt the spirit
  but the wire format is custom because Zenoh key-expression
  semantics carry routing that CloudEvents does not.
- **Adopt OpenTelemetry semantic conventions.** Subset
  applies; we map our schema onto OTel where it overlaps.

## Consequences

**Positive.** Audit logger, observability, and dashboards
share a parser. Correlation across services works by
construction.

**Negative.** Schema discipline at every producer; we
accept.

## Assessor view (SCA-R)

The event schema serves AU-2 (event logging — common shape),
AU-3 (content of audit records — required fields), AU-12
(audit generation — produced by every component), and SI-10
(information input validation — schema-validated).

## Trigger conditions for revisit

- A consumer category doesn't fit the common shape. Add
  type-specific sub-schemas in `attributes`; do not break
  the top-level fields.
- Field volume in `attributes` becomes unwieldy. Re-balance
  between top-level and attribute fields.

---

*See `GLOSSARY.md` for terms and acronyms.*
