# ADR 0504 — CDS conditioning at the fabric (Phase 3+)

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0500-zenoh-data-fabric.md`](0500-zenoh-data-fabric.md), [`0501-rust-interceptors.md`](0501-rust-interceptors.md), `../design/zenoh-cds-data-conditioning.md` |

## TL;DR (for a layperson)

When data has to cross between security domains (for example,
between a partner network and ours, or between unclassified
and classified networks), it has to pass through a Cross-
Domain Solution (CDS) that filters, transforms, and validates
it. We do that work at the data fabric — using Rust
interceptors that condition each message before it crosses the
boundary. This is a Phase 3+ deliverable.

## Context

Cross-Domain Solutions handle data flowing between security
domains. Conventional CDS deployments are appliance-based
products that sit between systems. Our approach: condition the
data *at the fabric*, by inserting Rust interceptors that:

- Validate schema and content rules.
- Strip or redact specific fields.
- Apply per-direction policy (e.g. some flows allowed only
  outbound).
- Audit every transit.
- Quarantine non-conforming messages.

This puts CDS logic where the data is — in the fabric — rather
than in a separate appliance with its own attack surface and
its own integration cost.

## Decision

Phase 3+ deliverable: CDS conditioning interceptors:

- **Per-direction interceptors** apply per-direction policy.
- **Schema validation** against the fabric schema-evolution
  registry ([`0506`](0506-fabric-schema-evolution.md)).
- **Content rule library** in Rust; reviewable; testable.
- **Quarantine** for non-conforming messages → DLQ
  ([`0510`](0510-zenoh-dlq.md)).
- **Audit-log integration** ([`0508`](0508-audit-logger-interceptor.md)).
- **Boundary node** ([`0601`](0601-fips-pqc-boundary-node.md))
  is the typical CDS-conditioning host, but the interceptor
  pattern can run anywhere.

Not in Phase 2D pilot. Phase 3+ scope, conditional on
Phase 2 gate ([`0509`](0509-phase-2-gate-criteria.md)).

The full design is in
`../design/zenoh-cds-data-conditioning.md`.

## Alternatives considered

- **Appliance-based CDS.** Adds an out-of-band component;
  does not benefit from interceptor / schema-registry
  ecosystem. Rejected as primary.
- **Application-layer CDS.** Distributes conditioning logic
  across every consumer; worse audit posture. Rejected.
- **Both (appliance + fabric)** for redundancy.
  Complementary; an appliance may still be required for
  specific cross-domain accreditations. Decided per
  authorization boundary.

## Consequences

**Positive.** CDS logic lives where the data lives. Audit is
unified with fabric audit. Schema validation is centralized.
Reviewer can read the conditioning interceptor source.

**Negative.** A specific cross-domain authorization may
mandate an appliance (CSfC, certain partner accreditations).
We accept the dual-pattern when required.

## Assessor view (SCA-R)

CDS conditioning serves AC-4 (information flow enforcement),
SC-7 (boundary protection), SI-7 (integrity), and CA-3
(system interconnections). The interceptor pattern is
auditable at the source level; the schema-evolution registry
is reviewable. CSfC Capability Package compliance for cross-
domain flows is handled at the boundary node where required.

## Trigger conditions for revisit

- A specific authorization mandates appliance-based CDS
  exclusively. Add the appliance; preserve interceptor-based
  conditioning for other flows.
- Phase 2 gate produces a no-go on Zenoh; revisit CDS-
  conditioning on the fallback substrate (NATS) — the
  pattern survives the substrate change.

---

*See `GLOSSARY.md` for terms and acronyms.*
