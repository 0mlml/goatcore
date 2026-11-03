# ADR 0206 — Island Mode by construction (no quorum dependency)

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [`0204-multi-site-active-active.md`](0204-multi-site-active-active.md), [`0207-docker-compose-orchestration.md`](0207-docker-compose-orchestration.md), [`0805-policy-engine-active-passive.md`](0805-policy-engine-active-passive.md) |

## TL;DR (for a layperson)

If a site is cut off from the rest of the world — by a partition,
a contested environment, a blocked WAN link — it keeps operating
fully. New devices can still join, policies can still change,
services keep running. We achieve this by never building cross-
site coordination into our decision-making path. There is no
"central authority" to consult.

## Diagram



Source: [`diagrams/0206-island-mode-by-construction.mmd`](diagrams/0206-island-mode-by-construction.mmd).

## Context

"Island mode" is sometimes treated as a contingency mode that
some systems can enter under stress. We treat it as a *normal*
operating mode that every site is always already in.

The architectural requirement is: no decision the system makes
during steady-state operation requires reaching another site.
Cross-site exchange happens — Spock replication, signal-server
federation — but it is *eventually consistent* and never on the
critical path of a per-site decision.

This rules out a class of orchestrators and coordinators that
look attractive on paper:

- Kubernetes / Swarm / Nomad cluster across sites — all require
  quorum, all break under partition. See
  [`0207`](0207-docker-compose-orchestration.md).
- etcd / Consul / Raft-backed coordinators — same problem.
- Centralized service discovery — central means single point;
  partition kills it.

## Decision

Every system component is designed so that a site running in
isolation makes correct decisions without consulting any other
site.

Specific consequences across the stack:

- **Management plane.** Every site has its own complete copy
  ([`0204`](0204-multi-site-active-active.md)). New peer
  enrollment, ACL changes, key rotations work in isolation.
- **Database.** PostgreSQL Spock multi-master
  ([`0801`](0801-postgresql-spock-multi-master.md)) — each
  site writes locally; conflicts resolve on reconnection.
- **DNS.** Per-site mesh CoreDNS resolves the mesh-hosts zone
  from a local file ([`0312`](0312-dns-two-tier.md)); no
  central DNS dependency.
- **Identity / OIDC.** Per-site Dex
  ([`0401`](0401-per-site-dex-oidc.md)); no external IdP
  dependency at decision time.
- **Policy engine.** Per-site, Spock-replicated
  ([`0805`](0805-policy-engine-active-passive.md)). The
  CODEOWNERS-on-GitHub approval pattern was explicitly
  rejected for violating Island Mode.
- **Backup.** Per-site paired pgBackRest stanzas
  ([`0802`](0802-pgbackrest-paired-stanzas.md)) — site-local
  repo on NVMe; no cross-site dependency at backup time.
- **Container orchestration.** Docker Compose
  ([`0207`](0207-docker-compose-orchestration.md)) — no
  cluster-wide consensus.
- **Time discipline.** Three-tier NTP with local GNSS source
  ([`0804`](0804-three-tier-ntp.md)) — site time stays
  honest without WAN.

## Alternatives considered

- **"Eventually" island-mode (works after manual reconfig).**
  Operationally fragile; fails the most basic property of the
  pattern. Rejected.
- **Best-effort island mode (services degrade gracefully but
  some operations require online).** Acceptable for
  cosmetic services; not acceptable for the management plane.
  Rejected as the system-wide posture; tolerable case-by-case.
- **No island-mode commitment; assume connectivity is always
  available.** Defeats the use case. Rejected.

## Consequences

**Positive.** A site under partition or contested-environment
disruption keeps working. New peers can enroll. Policy can
change. Services keep running. Re-convergence on reconnection
is the only thing that requires cross-site exchange, and it is
deliberately eventually consistent.

**Negative.** Several attractive technologies are off-limits
because they would violate the property. Kubernetes is the
largest example (see [`0207`](0207-docker-compose-orchestration.md));
external SaaS dependencies are another. We accept the
constraint; it is the price of the property.

## Assessor view (SCA-R)

Island Mode serves CP-2 (contingency plan — *every* site is
operating in contingency mode at all times), CP-7 (alternate
processing site — every site qualifies), CP-9 (backup —
site-local), and SC-5 (DoS protection — partition is
absorbed). The "no quorum dependency" property is an
unusually strong one for an SCA-R to encounter; the rejected
alternatives section is the clearest way to communicate
*why* certain familiar technology choices were declined.

The compliance-posture artifact
(`../compliance/comparative-posture.md`)
notes Island Mode survivability as a dimension where
goat is meaningfully ahead of median DoD network
posture.

## Trigger conditions for revisit

- A specific decision is consistently delayed by waiting for
  a non-island-mode resource. Investigate whether the
  resource can be made site-local; do not loosen the
  property.
- A new feature requires cross-site coordination on the
  critical path. Either re-design or carve a deliberate
  exception with explicit documentation.

---

*See `GLOSSARY.md` for terms and acronyms.*
