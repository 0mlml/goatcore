# ADR 0216 — GOAT-Net scaling — federation plane + site plane separation

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-04-26 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [`0204-multi-site-active-active.md`](0204-multi-site-active-active.md), [`0205-intra-site-vrrp-failover.md`](0205-intra-site-vrrp-failover.md), [`0206-island-mode-by-construction.md`](0206-island-mode-by-construction.md), [`0210-zenoh-universal-substrate.md`](0210-zenoh-universal-substrate.md), [`0500-zenoh-data-fabric.md`](0500-zenoh-data-fabric.md), [`0508-audit-logger-interceptor.md`](0508-audit-logger-interceptor.md), [`0512-rapid-adaptation-with-ai.md`](0512-rapid-adaptation-with-ai.md), [`0801-postgresql-spock-multi-master.md`](0801-postgresql-spock-multi-master.md), [`0811-audit-log-integrity.md`](0811-audit-log-integrity.md), [`0821-mission-partner-sharing-pattern.md`](0821-mission-partner-sharing-pattern.md) |

## TL;DR (for a layperson)

GOAT-Net (Geo-Distributed Open Architecture Transformation Network) is the project's data backbone. Today it is a proof of concept; if it succeeds, it has to scale to support entire theaters. The naive answer ("replicate everything everywhere") doesn't work. The right answer — and the standard distributed-systems answer — is to separate the **small, fast-replicating control surface** from the **heavy, locally-scaling data surface**, and let data move between them through deliberate, audited operations. This ADR commits to that direction. It deliberately does *not* pre-commit to specific scaling technologies (Patroni, Scylla, Kuzu, MinIO are reference candidates, not commitments); those decisions are deferred to Phase 4+ when real load measurements exist. The Phase 3 commitment is the **boundary** — what replicates, what doesn't — so current work doesn't drift into shapes that won't scale.

## Maturity caveat

GOAT-Net is at proof-of-concept stage. We are in uncharted territory — sovereign tactical-edge data backbones at this shape and scope do not have established operational precedent at theater scale. The direction is sound and anchored in mainstream distributed-systems patterns; specific scaling, performance, and operational characteristics will be discovered, not asserted. The plan in this ADR becomes operative *if* GOAT-Net earns it through the early phases. The project commits to the direction, not to specific numbers.

## Context

goat realizes a data backbone the project externally calls **GOAT-Net**. Today GOAT-Net is at Phase 2D (single-site Zenoh fabric pilot) with a Phase 3 commitment to multi-site (six-site scope envelope per ADR `0215-scalability-continuity-scope.md`). At that envelope the existing patterns — Spock multi-master for the management plane, Zenoh router federation for the fabric — are sized correctly. The question this ADR answers is what happens *if* GOAT-Net succeeds and the operating envelope grows materially beyond the Phase 3 baseline: more sites, more vendors emitting heavy data, more agent activity, theater-scale volumes, GovCloud archival participation.

Two failure modes are common when projects answer that question poorly:

1. **Replicate-everything default.** "We already have Spock; add the data plane to it." Works at six sites with modest write rates. Does not work at theater scale: replication cost grows N×M, write hot-spots emerge, multi-master conflict rates exceed reconciliation capacity.
2. **Centralize-into-cloud default.** "Move the canonical store to GovCloud and have tactical sites be clients." Removes sovereignty; breaks island-mode (a partitioned site cannot reach GovCloud); contradicts the project's mission shape.

The right answer is well-established in distributed systems — separate the control plane (small, requires coordination, replicates) from the data plane (heavy, decoupled, local). Reference points: Kubernetes federation (KubeFed/Karmada), PostgreSQL Citus (coordinator + workers), Apache Pulsar (brokers + bookkeeper), Cassandra/Scylla (gossip + sharded data), CockroachDB, IPFS (DHT + content-addressable fetch), Solid (Pods + linked metadata), modern data-mesh patterns. The variations are *within* the two-plane shape, not against it.

This ADR commits to the direction. Specific technology choices for the eventual full-scale implementation are deferred to a Phase 4+ block triggered by real load measurements, not pre-committed now.

## Decision

GOAT-Net separates into two planes with clearly defined responsibilities. The boundary is **coordination requirement**, not location: the federation plane *requires* cross-site coordination to function; the site plane *does not require* peer-site coordination to function. **Both planes run at every participating site** — a site is not "federation-only" or "site-plane-only"; every site has a federation-plane endpoint that participates in the federation, and a site plane that scales locally.

### Federation plane

**Purpose.** The small, fast-replicating, theater-wide surface. Carries the state every participating site needs the same view of.

**Carries:**

| What | Where it lives today | Replication mechanism |
|---|---|---|
| Identity (Ory Kratos / Hydra) | Per-site mgmt-host PG | Spock multi-master per ADR [`0801`](0801-postgresql-spock-multi-master.md) (already operational pattern) |
| Policy (release allowlists, schema-enforcer config, egress allowlists) | Per-site mgmt-host PG | Spock multi-master |
| Schema declarations + queryable catalog (`attest/schema/**`, `attest/schema-declaration/**`, `attest/queryable/**`) | Zenoh `attest/` topics | Zenoh router federation (per ADR [`0500`](0500-zenoh-data-fabric.md) §"Phase 3+ scope expansion") |
| Audit-log **metadata + payload digests** | Per-site mgmt-host PG (separate table from bulk content) | Spock multi-master |
| Lineage edges (forward + backward adjacency) | Per-site lineage materializer storage | Spock-replicated index of edges; bulk content stays site-local |

**Coordination requirement.** Federation-plane state requires consistency across participating sites. Site A's identity store must agree with site B's identity store; a release decision approved at site C must be visible at site D. Spock multi-master is already deployed for this pattern and remains the mechanism.

**Failure semantics.** A site outage degrades that site's contribution to the federation but does not halt the federation. Spock's existing tolerance (per ADRs [`1009`](1009-ory-shared-network-nid-preseed.md), [`1010`](1010-ory-on-spock-mindful-operation.md)) governs; this ADR does not change federation-plane semantics.

### Site plane

**Purpose.** The locally-scalable, decoupled surface. Carries state that each site can scale on its own without sidechaining peer sites.

**Carries:**

| What | Where it lives today |
|---|---|
| Bulk sensor topic payloads (`data/...` actual bytes) | Per-site Zenoh storage (RocksDB) + object store for binary |
| Bulk audit-log **payload bodies** (the bytes; metadata + digests live in the federation plane) | Per-site object store with integrity tier per ADR [`0811`](0811-audit-log-integrity.md) |
| Local agent state — Tipper baselines, Scout request transcripts, lineage materializer working set | Per-site agent host storage |
| Vector embeddings (when Phase 3+ vector store lands) | Per-site vector store |

**Coordination requirement.** None across sites. The site plane's network and core services at site A do not need to coordinate with site B's network and core services to function. Each site's site plane scales on its own.

**Data mobility.** The site plane is *locally scalable*, not *location-bound*. Data in the site plane is mobile through deliberate, audited operations:

- **Topic-level federation** for selective cross-site flow — same shape as the partner release-namespace pattern (ADR [`0821`](0821-mission-partner-sharing-pattern.md)).
- **Content-addressable fetch by digest** — consumer at site B asks site A for the bytes a federation-plane lineage edge or audit-log digest points at.
- **Batch transfer** — bulk corpus moves between sites' object stores (MinIO mirroring or equivalent) for archival, analytics, or partner export.

All three are explicit operations under the existing release-namespace + policy + audit discipline. **No implicit background sync.** The site plane gives scaling autonomy at each site; data mobility is layered on top.

**Failure semantics.** A site plane outage at site A makes site A's bulk data temporarily unavailable to peers (until recovery); peers continue with their own site planes plus the shared federation plane. Within a site, HA is sibling-only — pair-of-routers / pair-of-DBs / pair-of-object-stores in the same site, mirroring the existing Router-A / Router-B intra-site failover pattern (ADR [`0205`](0205-intra-site-vrrp-failover.md)). Sibling-only HA does **not** replicate cross-site.

### The bridge between the planes

When a federation-plane reference (lineage edge, audit-log entry, schema-attested topic) points at bulk content, consumers fetch from the originating site's site plane via authenticated, audited content-addressable fetch:

1. Federation plane carries `(topic, timestamp, payload_digest)`.
2. Consumer at site B requests the bytes from site A by digest, scoped to its identity's authorization.
3. Site A's site plane returns the bytes if the digest matches; consumer verifies; both sides log the operation in the audit-log integrity tier.
4. If site A is unreachable, the federation-plane reference still tells site B *what* the data was (topic, time, digest); just not the bytes themselves.

The bridge is the same shape as the partner release-namespace pattern (ADR [`0821`](0821-mission-partner-sharing-pattern.md)) — explicit per-content fetch, identity-bound, policy-gated, audit-logged. **No new substrate.** Internal cross-site bridges and external partner bridges share the convention.

### GovCloud as a federation participant

GovCloud (IL4 / IL5 / IL6) participates as **another federation-plane node**, not as a centralizer:

- GovCloud gets a **full federation-plane mirror** — identity, policy, schema catalog, audit metadata + digests, lineage edges. Sponsor analytics, ATO archival, broader-community brokering all anchor here.
- GovCloud gets a **selective site plane** — only topics explicitly released up via the policy-engine workflow land in GovCloud's site plane. The same release-namespace pattern that governs partner release governs GovCloud release. Tactical data does **not** flow to GovCloud by default.

Tactical sites retain authority over their data. Selective release upward is deliberate, per-topic, audited. GovCloud is a participant, not an owner.

### What this ADR does NOT commit to

- **Specific technologies for site-plane HA siblings.** Patroni-shape (Raft-managed PG synchronous + async replicas), ScyllaDB / Cassandra-shape (gossip-based KV), MinIO-shape (object store) are **candidate references**, not commitments. Selection is deferred to Phase 4+ when real load measurements exist.
- **Specific sharding strategies for site-plane storage.** Sharded RocksDB by topic-prefix, sharded `zenohd` per topic-class, sharded lineage materializer by consistent hash — all deferred.
- **Specific bridge-fetch implementation.** Content-addressable fetch via Zenoh queryable, via S3-compatible HTTP API, via custom protocol — all deferred. The *shape* (authenticated, audited, by-digest) is committed; the wire detail is not.
- **Performance numbers.** No throughput, latency, or availability numbers are committed in this ADR. They will be discovered in Phase 3 operation and codified in the Phase 4+ block.
- **Cross-site agent state federation.** Lineage edges federate; agent working state (Tipper baselines, Scout transcripts) does not. If a future agent needs cross-site working state, that's a separate decision-review per ADR [`0512`](0512-rapid-adaptation-with-ai.md).

### What this ADR DOES commit to

- The boundary: which state replicates (federation plane), which does not (site plane).
- The bridge shape: authenticated + audited + by-digest, same convention as partner release-namespace.
- HA at the site plane: sibling-only within a site, not cross-site.
- GovCloud as a federation participant, not a centralizer.
- Tactical primacy: forward sites retain authority over their data.

These commitments are sufficient to keep Phase 3 work on a scalable shape; the rest is reserved for Phase 4+ triggered by load measurements.

## Alternatives considered

- **Replicate everything across all sites (single-tier).** Doesn't scale; multi-master conflict rate explodes; replication cost grows N×M. Rejected.
- **Centralize canonical store in GovCloud or a single hub.** Breaks sovereignty + island-mode; contradicts mission shape. Rejected.
- **Sharded peers in a single tier (Citus-style at the data plane only).** Possible, but loses the failure-isolation property — high-load at one site degrades coordination for others. The two-plane separation is strictly better. Rejected as primary shape; sharding remains valid *within* the site plane.
- **Edge-cloud "pull only" pattern (federation always pulls from local).** Cleaner but doesn't match how data needs to flow upward to GovCloud and outward to partners. The two-plane + bridge pattern handles both push and pull cleanly. Rejected as exclusive shape; pull is one of the bridge mechanisms supported.
- **Lake-house / Iceberg-shaped pattern.** Heavy; adds Spark/Trino-equivalent compute layer. Different shape problem; not the answer for tactical-edge fabric. Rejected.
- **Pre-commit to specific technologies now (Patroni + Scylla + MinIO + KuzuDB + …).** Premature; technology selection without measured load is over-claim. Rejected — candidates are named as reference points only.
- **Defer the entire scaling discussion until Phase 4+.** Risk: Phase 3 work drifts into shapes that don't scale. The cost of codifying the boundary now is small; the cost of correcting drift later is large. Rejected.

## Consequences

**Positive.**

- Replication cost stays bounded — federation plane is small by construction.
- Failure isolation — site plane outage at one site doesn't propagate.
- Tactical primacy — forward sites keep authority over their data.
- Composes with existing patterns — Spock for federation plane (already deployed); release-namespace pattern for both internal and external bridges (already designed for partners).
- Sovereign at every plane — no third-party platform owns either tier.
- GovCloud-compatible — participates as federation node without becoming a centralizer.
- Phase 3 protected from drift — engineers know which side of the boundary new state belongs on.
- Phase 4+ implementation deferred until measurable — no premature tech commitment.

**Negative.**

- The bridge introduces operational complexity. Mitigation: same shape as partner release; one pattern, two surfaces (internal + external).
- Site-plane data unavailability under site-plane outage at the originating site. Mitigation: federation plane still tells consumers *what* the data was (topic, time, digest); only the bytes are missing until recovery. Acceptable trade-off for failure isolation.
- The boundary will surface fuzzy cases (lineage edge cardinality, dense citations). Mitigation: ADR commits to the boundary direction; specific edge-cardinality limits are codified as load measurements arrive.
- Engineering cost of policing the boundary in Phase 3 PRs. Mitigation: Block 26 codifies the discipline as PR-template / reviewer-checklist additions; cost is small.
- This is a Phase 4+ commitment with no Phase 4+ implementation work today. Risk that the codification gets ignored. Mitigation: Block 26 makes the codification active in Phase 3 work.

**Out of scope for this ADR.**

- Specific technology selection for any site-plane component.
- Cross-classification (CDS) federation — separate ADR [`0504`](0504-cds-conditioning.md).
- Vector-store sharding strategy.
- Cross-site agent state federation.
- Disaster recovery semantics for site-plane bulk data loss.
- Post-quantum implications for the bridge (rides existing PQ posture per ADRs [`0600`](0600-two-pq-paths.md), [`0601`](0601-fips-pqc-boundary-node.md)).

## Assessor view (SCA-R)

This decision serves SC-7 (boundary protection), SC-32 (information system partitioning), and CP-2 (contingency planning) by establishing explicit boundaries between replicated state and locally-scaling state, with cross-boundary mobility through audited operations rather than implicit replication. AC-3 (access enforcement) and AC-4 (information flow enforcement) are supported because every cross-boundary fetch is identity-bound + policy-gated through the same release-namespace pattern that governs partner release.

The two-plane architecture preserves CP-7 (alternate processing site) properties at the federation plane (Spock-replicated; site failure does not halt federation) while adding failure-isolation properties at the site plane (one site's data-plane failure does not propagate to peers).

The deferral of specific technology selection to Phase 4+ is itself an SCA-R-relevant property: technology choices made under measured load have stronger accreditation rationale than choices made under speculation. The Phase 3 commitment is to the boundary, not to the technology stack.

The maturity caveat is explicit. The project does not claim GOAT-Net will scale and perform at theater scale exactly as described; the project commits to a viable scaling direction that becomes operative *if* GOAT-Net succeeds in earlier phases.

## Trigger conditions for revisit

- **Vendor data ingest crosses a measured threshold** that strains site-plane capacity. Triggers the Phase 4+ scaling implementation block (specific technology selection + sharding strategy).
- **Federation-plane replication latency or conflict rate** crosses operational tolerance. May trigger reconsideration of what belongs in the federation plane (i.e., move some state to site-plane-only).
- **GovCloud or other-classification federation becomes a real participant.** May surface boundary cases not covered today.
- **A foreseeable scaling pattern fails operationally** (e.g., sibling-only HA at the site plane proves insufficient under high write rates). Revisit HA shape.
- **A new substrate option** (post-quantum-by-default fabric, alternative to Zenoh, etc.) makes the boundary mechanically different. Revisit which mechanisms carry the federation plane.
- **The phoenix-native peer discipline** (ADR [`0208`](0208-phoenix-native-peer-discipline.md)) interacts with site-plane state in ways that surface unexpected discipline conflicts. Revisit.

---

*See `GLOSSARY.md` for terms and acronyms — including new terms `federation plane`, `site plane`, `GOAT-Net`.*
