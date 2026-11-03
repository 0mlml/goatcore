# ADR 0511 — Publisher trust model

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0507-event-telemetry-schema.md`](0507-event-telemetry-schema.md), [`0508-audit-logger-interceptor.md`](0508-audit-logger-interceptor.md), [`../design/publisher-trust-model.md`](../design/publisher-trust-model.md) |

## TL;DR (for a layperson)

Anyone (any peer) can publish to the fabric, but consumers
don't have to trust everyone. Each event is signed by its
publisher; consumers (and the audit logger) verify the
signature, and policies decide what each consumer accepts. We
build a clear model of who can publish what, and consumers
choose their trust level explicitly.

## Context

Open publishing without a trust model means consumers must
treat every event as potentially adversarial — slowing
everything down and making consumers complicated. Centralized
publishing (only "trusted" services may publish) loses the
extensibility benefit of the fabric.

The middle path: signed events + per-consumer trust policies.

## Decision

Trust model:

- **Every event is signed** by its publisher (Ed25519, peer
  identity per [`0200`](0200-identity-based-mesh-overlay.md)).
- **Audit logger verifies signatures**
  ([`0508`](0508-audit-logger-interceptor.md)) — invalid
  signatures go to DLQ ([`0510`](0510-zenoh-dlq.md)).
- **Per-consumer trust policies** decide what each consumer
  accepts:
  - Allow-list of producer roles.
  - Required attestation level (e.g. some consumers accept
    only events from peers with role-declared attestation).
  - Schema-version constraints.
- **Policy distribution** via the policy engine
  ([`0805`](0805-policy-engine-active-passive.md)) — each
  consumer pulls its trust policy from the engine.
- **Default-deny:** a consumer with no explicit policy
  accepts only events from itself or from a documented
  default trust set (configurable per site).

Full design:
[`../design/publisher-trust-model.md`](../design/publisher-trust-model.md).

## Alternatives considered

- **No trust model; consume all events.** Adversarial-input
  surface across every consumer. Rejected.
- **Centralized publishing only.** Loses extensibility.
  Rejected.
- **TLS-mutual-auth at the transport layer only.** Doesn't
  give per-event trust; doesn't survive at-rest replay.
  Rejected as the only mechanism; complementary to event
  signatures.

## Consequences

**Positive.** Consumers control their own trust posture.
Audit and DLQ catch invalid events. Producer signing is
mechanical (every peer has a key).

**Negative.** Signing overhead per event. Acceptable;
Ed25519 is fast.

## Assessor view (SCA-R)

The publisher trust model serves AC-3 (access enforcement at
the consumer level), AC-4 (information flow enforcement —
per-consumer policy), AU-10 (non-repudiation — every event
signed), and SC-13 (cryptographic protection — signing).

## Trigger conditions for revisit

- Per-consumer policy maintenance becomes operationally
  expensive. Adopt role-based default policies; do not
  abandon per-consumer control where it matters.
- Signature overhead constrains throughput on a specific
  high-volume topic. Consider topic-level batched signing;
  do not abandon signing.

---

*See `GLOSSARY.md` for terms and acronyms.*
