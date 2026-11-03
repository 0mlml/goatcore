# ADR 0510 — Zenoh DLQ pattern (dead-letter queue for non-conforming messages)

| Field | Value |
|---|---|
| Status | **Superseded by [ADR 1022](1022-zenoh-denial-observability.md)** (2026-06-09) |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [ADR 1022](1022-zenoh-denial-observability.md) (supersedes), [`0504-cds-conditioning.md`](0504-cds-conditioning.md), [`0506-fabric-schema-evolution.md`](0506-fabric-schema-evolution.md), [`../design/zenoh-dlq.md`](../design/zenoh-dlq.md), F-178 |

> **Superseded.** A dead-letter queue is the *wrong category* for our failure mode — Zenoh is a routing **fabric, not a broker**, with no native dead-letter primitive (F-178), and our actual problem (admission rejection at the ACL) is a synchronous produce-side condition, not a consumer-side delivery-attempt one. [ADR 1022](1022-zenoh-denial-observability.md) replaces this with the **expected-vs-actual write-denial monitor** + **producer-cooperation** (acked Query/Reply for must-land writes), and keeps byte-level DLQ capture only as a *deferred-forensic option* if that specific need ever arises. This ADR is retained for history; do not build new DLQ topics.

## TL;DR (for a layperson)

When a message doesn't conform to its schema or fails a
validation rule, we don't drop it silently — we move it to a
"dead-letter queue" where operators can inspect it later. This
gives us evidence that something is wrong and prevents bad
messages from disappearing without a trace.

## Context

Validation failures are diagnostic gold. A message that fails
schema validation tells you (a) something is wrong with a
producer, or (b) something has drifted in your schema rules.
Dropping the message destroys the evidence. Crashing the
consumer creates an outage.

The DLQ pattern preserves the evidence and isolates the
failure.

## Decision

Non-conforming messages go to a DLQ:

- **Trigger:** schema-validation failure, signature
  failure, content-rule failure, or boundary-policy
  violation.
- **Destination:** Zenoh key prefix `dlq/<reason>/<original-key>`
  with the original message body and metadata about the
  failure.
- **TTL:** 30 days (longer than ops/** because investigation
  may take time).
- **Operator review:** dashboard surfaces DLQ growth;
  per-reason alerting fires on rate change.
- **Reprocess flow:** an operator can re-publish a fixed
  message with the same `event_id` (idempotency from
  [`0507`](0507-event-telemetry-schema.md) supports this).

Full design at
[`../design/zenoh-dlq.md`](../design/zenoh-dlq.md).

## Alternatives considered

- **Drop silently.** Loses evidence. Rejected.
- **Crash consumer.** Creates outages. Rejected.
- **Reject at producer side.** Producer may not know the
  consumer's rules. Some validation happens at producer; DLQ
  catches what slips through.

## Consequences

**Positive.** Validation failures are visible and
investigable. Operators get signal on producer drift.
Reprocess flow exists.

**Negative.** DLQ storage and operator review are real
costs. Acceptable.

## Assessor view (SCA-R)

DLQ serves SI-7 (integrity — non-conforming messages are
quarantined), SI-10 (information input validation —
failures preserved), and AU-2 (audit — failures recorded).
The 30-day TTL is the compensating retention for forensic
investigation needs.

## Trigger conditions for revisit

- DLQ growth pattern points to systemic producer drift.
  Investigate the producer; do not raise the DLQ TTL to
  hide the problem.
- A class of failure overwhelms the DLQ. Add per-class
  DLQs (per-reason key prefixes already in place).

---

*See `GLOSSARY.md` for terms and acronyms.*
