# ADR 0204 — Multi-site active-active management plane

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [`0205-intra-site-vrrp-failover.md`](0205-intra-site-vrrp-failover.md), [`0206-island-mode-by-construction.md`](0206-island-mode-by-construction.md), [`0801-postgresql-spock-multi-master.md`](0801-postgresql-spock-multi-master.md), [`0302-custom-federated-signal-server.md`](0302-custom-federated-signal-server.md). Forward-composes-with: [`0224-progressive-config-rollout-with-auto-revert.md`](0224-progressive-config-rollout-with-auto-revert.md) (config-class data on the Spock substrate gets consumer-side gated activation per 0224; replication itself is unchanged from this ADR's commitment). |

## TL;DR (for a layperson)

Every site runs a complete copy of the management plane — not a
backup, an active copy. Sites stay in sync by exchanging changes
continuously. If a site is cut off from the rest, it keeps
operating on its own; when it reconnects, changes flow back.
There is no single "primary" site that has to stay up for the
system to work.

## Diagram



Source: [`diagrams/0204-multi-site-active-active.mmd`](diagrams/0204-multi-site-active-active.mmd).

## Context

A typical multi-site deployment makes one site primary and the
others read-replicas. The primary failing means an outage. A
contested-environment partition cuts the read-only sites off
from policy changes and from each other.

goat operates in environments where partition is
expected — satellite links degrade, partner WAN links go
through commercial transport, and contested environments
deliberately interrupt connectivity. The management plane has
to keep working at every site, independently, while preserving
the property that changes converge once connectivity returns.

This is what "active-active" means here, and it is a
non-negotiable requirement, not a "nice to have."

## Decision

Every production site runs a complete management-plane stack.
The components and their multi-site behavior:

- **PostgreSQL with Spock extension** — multi-master logical
  replication. Every site can write; conflicts resolve via
  last-writer-wins (Spock's built-in mechanism) plus operational
  discipline (single-admin-at-a-time during connectivity, NTP
  hygiene, monitor `spock.conflict_log`) and pre-conflict schema
  design (xid-style server-side identifiers in primary keys to
  keep collisions rare). Spock does NOT support per-table
  configurable conflict classes — the original draft of this ADR
  oversimplified by implying it does; canonical conflict
  resolution per `pg-spock-security.md` is LWW + design discipline.
  See [`0801`](0801-postgresql-spock-multi-master.md).
- **Custom federated signal-server** — peers register with
  their local signal-server; signal-server-to-signal-server
  Forward RPC propagates registration and exchange across
  sites. See [`0302`](0302-custom-federated-signal-server.md).
- **NetBird management** — runs on every site, reads/writes the
  shared Spock database; setup keys, ACLs, peer registry are
  globally consistent under eventual convergence.
- **Per-site DNS, observability, backup** — fully independent
  per site. No cross-site PITR
  ([`0803`](0803-no-cross-site-pitr.md)).

Site loss durability is carried by Spock replication
(another site has the data). Cross-site PITR is
deliberately *not* offered — accepted trade-off, see
[`0803`](0803-no-cross-site-pitr.md).

The full design is in `../design/ha-multi-site.md`.

## Alternatives considered

- **Primary + read-replicas.** Loses partition survivability.
  Rejected.
- **Active-passive with VRRP across sites.** Sub-second
  failover at the cost of cross-site quorum; cannot survive
  partition. Rejected for cross-site (used intra-site, see
  [`0205`](0205-intra-site-vrrp-failover.md)).
- **Federated separate management servers (one per site).**
  Loses shared peer registry; coalition partner federation
  becomes per-server. Rejected.
- **Cloud-hosted single management plane.** Loses sovereignty;
  single point of failure. Rejected.

## Consequences

**Positive.** Every site is operationally independent. Partition
is a normal operating mode. New peer enrollment, ACL changes,
and key rotations work in isolation and converge on
reconnection. Site loss is a non-event for the rest of the
fleet.

**Negative.** Spock multi-master conflict resolution is more
complex than single-master replication. We invest in pre-
conflict design (per-table conflict classes, server-side
identifiers in primary keys) to keep conflicts rare. Cross-site
PITR is not available — Spock replication carries site-loss
durability; per-site pgBackRest paired stanzas
([`0802`](0802-pgbackrest-paired-stanzas.md)) carry per-site
PITR.

## Assessor view (SCA-R)

The active-active model serves CP-2 (contingency plan), CP-7
(alternate processing site — every site is one), CP-9 (system
backup — paired pgBackRest stanzas per site), and SC-5 (denial
of service — partition tolerance is built in, not retrofitted).

Spock multi-master is not a typical DoD pattern; reviewers
should expect to see [`0801`](0801-postgresql-spock-multi-master.md)
and `../design/pg-spock-security.md`
covering the conflict-resolution strategy. The signal-server
federation is a custom-built component; provenance is governed
by [`0702`](0702-build-from-source-mgmt-plane.md) and
[`0302`](0302-custom-federated-signal-server.md).

## Trigger conditions for revisit

- A specific authorization requires single-master operation
  (e.g. for legal-discovery reasons). Add a designated
  primary at submission time; do not architect to it.
- Spock conflict-resolution rules become unmanageable.
  Investigate per-table conflict classes; do not abandon
  multi-master.

---

*See `GLOSSARY.md` for terms and acronyms.*
