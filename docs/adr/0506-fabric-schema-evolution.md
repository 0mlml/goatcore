# ADR 0506 — Schema evolution and compatibility rules for the fabric

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0500-zenoh-data-fabric.md`](0500-zenoh-data-fabric.md), [`0507-event-telemetry-schema.md`](0507-event-telemetry-schema.md), `../design/fabric-schema-evolution.md` |

## TL;DR (for a layperson)

Producers and consumers on the data fabric have to agree on the
shape of the data. Producers add fields, change types, deprecate
fields — and consumers can't all be upgraded at once. We follow
strict rules about how schemas evolve so that an old consumer
keeps working when a producer ships a new version, and so that a
new consumer can read old data.

## Context

Fabric data shapes evolve over time. Without rules, a producer
schema change breaks consumers; consumers refuse to start;
operators flip image tags by hand to recover. The fix: a small
set of rules that producers and consumers agree to.

The rules are conventional in distributed-system schema design
(forward + backward compatibility), but we codify them
explicitly so reviewers can enforce them.

## Decision

Schema-evolution rules for fabric data:

- **Backward compatible.** New schema can read old data.
- **Forward compatible.** Old schema can read (and ignore)
  new fields.
- **Required vs optional:** new fields are *optional* with a
  documented default. Required fields are *never* added in a
  minor revision.
- **Field removal:** deprecate-then-remove across two minor
  revisions. Remove only after all known consumers have
  migrated.
- **Type changes:** never in-place; introduce a new field with
  the new type, deprecate the old, remove later.
- **Schema registry:** schemas live in-repo at
  `docs/design/event-telemetry-schema.md` (registry doc) +
  per-domain schema files; PR review enforces the rules.
- **CDS conditioning** ([`0504`](0504-cds-conditioning.md))
  validates messages against schemas at the boundary.

Full design at `../design/fabric-schema-evolution.md`.

## Alternatives considered

- **No rules; ad-hoc evolution.** Producer-consumer breakage
  is routine. Rejected.
- **Strict versioning, never break.** Doable but heavy;
  semantic versioning + the rules above is sufficient.
- **External schema registry (Confluent / Apicurio).** Adds
  operational dependency; not yet justified. In-repo
  schemas are reviewable in PRs.

## Consequences

**Positive.** Producer and consumer can be upgraded
independently. Migration plans for breaking changes are
documented. Reviewers can enforce the rules.

**Negative.** Discipline cost in PR review; we accept.

## Assessor view (SCA-R)

Schema-evolution rules serve CM-3 (change control — schema
changes are reviewed), SI-10 (information input validation
— consumers validate inputs against schemas), and CA-2
(security assessment — schema regression is testable).

## Trigger conditions for revisit

- A breaking change is proposed. Apply the deprecate-then-
  remove flow; do not loosen the rules.
- Schema count exceeds the in-repo manageability limit
  (~50 schemas). Consider an external registry; do not
  abandon the rules.

---

*See `GLOSSARY.md` for terms and acronyms.*
