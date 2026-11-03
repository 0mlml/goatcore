# ADR 0801 — PostgreSQL Spock multi-master per site

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0204-multi-site-active-active.md`](0204-multi-site-active-active.md), [`0802-pgbackrest-paired-stanzas.md`](0802-pgbackrest-paired-stanzas.md), `../design/ha-multi-site.md`, `../design/pg-spock-security.md` |

## TL;DR (for a layperson)

The mesh management plane keeps its data in PostgreSQL with
the Spock extension, which lets every site write to its own
copy and have those writes replicate to every other site.
This is what makes our active-active multi-site design
possible. Conflicts (when two sites write the same row at the
same time) resolve via per-table rules so we don't end up
needing manual intervention.

## Diagram



Source: [`diagrams/0801-postgresql-spock-multi-master.mmd`](diagrams/0801-postgresql-spock-multi-master.mmd).

## Context

Active-active multi-site
([`0204`](0204-multi-site-active-active.md)) requires a
multi-master replication layer. Standard PostgreSQL streaming
replication is single-primary. Spock (from pgEdge) is the
mature extension that adds multi-master with logical
replication and conflict resolution.

## Decision

PostgreSQL 16 + Spock as the management-plane database:

- **Multi-master:** every site writes locally; changes
  propagate to other sites.
- **Conflict resolution:** per-table conflict classes (LWW,
  server-side identifiers in PKs) keep conflicts rare.
- **Replication peering:** Spock subscriptions across all
  sites form a full mesh.
- **Image:** custom postgres-spock image with pgBackRest
  baked in ([`0802`](0802-pgbackrest-paired-stanzas.md)).
- **Per-site:** Router-A and Router-B each run a local
  Postgres; both are Spock peers.
- **WAL:** monitored against ceiling alerts; replication-lag
  alerts; conflict-log alerts (see `ops/monitoring/`).

Designed in `../design/ha-multi-site.md`
and `../design/pg-spock-security.md`.

## Alternatives considered

- **Single-primary streaming replication.** Loses active-
  active. Rejected.
- **CockroachDB / YugabyteDB.** Cluster-quorum dependency
  breaks Island Mode. Rejected.
- **PostgreSQL BDR (older multi-master).** Less actively
  maintained. Rejected.
- **Application-level conflict resolution.** Pushes
  consistency burden to every consumer. Rejected.

## Consequences

**Positive.** Active-active multi-site by construction.
Each site survives partition independently.

**Negative.** Conflict resolution requires careful per-table
design. Spock is a pgEdge product (BSL-licensed); we monitor
license posture per [`0106`](0106-sovereign-tooling-preference.md).

## Assessor view (SCA-R)

Spock multi-master serves CP-2 (contingency — partition
tolerance), CP-7 (alternate processing site — every site
qualifies), CP-9 (system backup — paired with pgBackRest
[`0802`](0802-pgbackrest-paired-stanzas.md)), and SC-5
(DoS — single-site loss absorbed). The conflict-resolution
design is reviewable in
`../design/pg-spock-security.md`.

## Trigger conditions for revisit

- pgEdge / Spock governance changes adversely.
- A specific authorization mandates single-primary. Negotiate.
- Conflict-resolution rules become operationally
  unmanageable. Re-design conflict classes; do not abandon
  multi-master.

---

*See `GLOSSARY.md` for terms and acronyms.*
