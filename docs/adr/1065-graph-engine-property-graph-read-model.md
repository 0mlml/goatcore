# ADR 1065 — Graph-engine choice: a first-class property-graph read-model for the actor relationship graph

**Status:** Proposed. **Spike complete (2026-07-01, 1M nodes / 8.7M edges, all four engines) — see "Spike results" below.** Property-graph target engine selected: **KuzuDB (embedded); Apache AGE ruled out; plain Postgres recursive CTEs remain the reachability default.** Adoption of the property-graph read-model is gated on the exploration/analytics consumer's queries actually appearing, not on this decision. Design: `actor-relationship-graph.md` §0.2, §4. Harness: `ops/graph-engine-spike/`.
**Related:** ADR 1043 §4 (the typed-edge vocabulary) · [ADR 1051](1051-identity-descriptor-standard.md) (effective-dated edges + consistency token) · [ADR 1064](1064-unit-reference-and-verification-service.md) (`unit_code`, the unit anchor edges key to) · [ADR 0112](0112-tech-strategy-workload-shape-language-boundary.md) (choose tech by workload shape).

## Plain-language summary

The actor relationship graph's earlier engine verdict was "reuse PostgreSQL; a graph database is overkill; Neo4j only if a graph-analytics workload appears, which we don't have." Two things changed that premise: the affiliation model gained a **rich typed-edge vocabulary** (comrel, c2-authority, data-authority, provisions, replaces, supports/coordinates-with — each property-bearing and effective-dated), and the scale envelope is a large federated backbone (millions of nodes, tens of millions of edges — still single-instance territory, not billions). Together those make the relationship-modeling/exploration workload genuinely **property-graph shaped**, independent of node count. This ADR promotes a **first-class property-graph read-model** from "someday, only for analytics" to "a warranted engine for the exploration/analytics consumer, chosen by a workload-representative spike." It changes **nothing** load-bearing: the declarative registry stays the system of record, the graph stays a derived/rebuildable read-model behind a stable query API, never at the tactical edge, never in an enforcement path.

## Context

`actor-relationship-graph.md` (§4, spike-confirmed 2026-06-20) chose PostgreSQL recursive CTEs for the start and framed escalation by **scale tier**, with a Zanzibar-style service (SpiceDB/OpenFGA) as the likely reachability engine at scale and Neo4j dismissed except for a graph-analytics workload "we do not yet have." Since then:

1. **The relationship model became graph-shaped** (ADR 1043 §4 / identity-grammar §3.2): `parent` is one of many typed edges. The load-bearing questions are now multi-edge-type, property-filtered, variable-depth traversals + emerging structural analytics (authority centrality, support-network shape) over a `unit_code`-anchored actor graph.
2. **The workload has two distinct consumers**: (a) authz/**reachability** ("everything under org X", releasability) — a Zanzibar-style workload; (b) relationship **exploration + analytics** — a property-graph workload. §4 optimized only (a).
3. **Scale is comfortable for a single-instance property graph** (§0.1): ~single-digit-million nodes / tens-of-millions of edges is well within KuzuDB/Memgraph/Neo4j single-node range — scale neither forbids nor forces; workload shape is the driver (ADR 0112).

## Decision

1. **Adopt a first-class property-graph engine as the target for the relationship exploration/analytics read-model** — a genuine candidate on workload-shape grounds, not contingent on hitting a node-count tier and not deferred to a hypothetical future analytics need.
2. **Keep the two consumers separable behind the stable query API.** Reachability/authz may be served by Postgres CTEs now → a Zanzibar-style service at scale; exploration/analytics by Postgres/AGE now → a native property graph. Both are **derived, rebuildable read-models off the one declarative SoT** — never a second source of truth.
3. **Preserve every invariant** (`actor-relationship-graph.md` §2): declarative registry = SoT; graph = disposable projection reconciled via the ADR 1051 consistency token; **never at the tactical/denied edge** (the edge carries the portable descriptor subgraph + token); **never in an enforcement path** (Zenoh ACL never depends on it). This ADR is purely a read-model engine decision.
4. **Engine chosen by the workload-representative spike (now complete) — KuzuDB, not AGE, not Neo4j.** Neo4j was excluded up front (Enterprise-only clustering / JVM / per-node licensing, disproportionate). The spike (below) then measured KuzuDB (embedded) and Memgraph (in-memory) against Postgres+Apache AGE and plain Postgres CTEs at 1M nodes / 8.7M edges. Outcome: **KuzuDB is the property-graph target** (fastest load by 12–16×, wins nearly every query). **Apache AGE is ruled out** — the "stay-on-Postgres" fallback does *not* survive the data: it is 10–71× slower on multi-hop + analytics, has a silent catastrophic-plan footgun, and — decisively for *this* domain — **cannot express relationship-type alternation (`-[:A|B]->`) at all**, which our multi-typed edge catalog (comrel/c2-authority/data-authority/…) is built on. Memgraph's query latencies are close to KuzuDB but its community-edition ingest is as slow as AGE, so KuzuDB is preferred for an embedded derived read-model.
5. **Effective-dated / bitemporal edges** (ADR 1051 `recorded_at` + valid-time) are a first-class requirement of the chosen engine or its modeling — the authority/comrel/replaces edges are inherently time-bounded.

## Alternatives considered

- **Keep "Postgres unless scale forces a swap" (status quo §4).** Rejected as the *framing* — it keys the decision on node count and treats a property graph as analytics-only, which the typed-edge model outdates. Postgres remains the correct *Tier-1 start*; this ADR corrects the *target*, not the start.
- **Neo4j as the default property graph.** Rejected as a default — operational weight (Enterprise clustering, JVM, licensing) is disproportionate at our scale, and the workload doesn't need its differentiators; it stays a spike candidate, not the assumed answer, and is most defensible only if a heavy GDS analytics workload materializes.
- **A property graph as the system of record.** Rejected — violates the load-bearing §2 decision (declarative registry is SoT; the graph is a rebuildable read-model). A graph-DB-as-SoT is the drift trap EclecticIQ/Trendyol evidence (spike §7) warns against.
- **Run the graph at the edge.** Rejected — denied-edge posture forbids heavy stateful systems locally; the edge already has the descriptor subgraph + consistency token.
- **One engine for both consumers.** Not forced — reachability (Zanzibar-style) and exploration (property graph) are different shapes; keeping them separable behind the query API lets each use the right tool without a second SoT.

## Spike results (2026-07-01)

Harness `ops/graph-engine-spike/` (synthetic generator matching our node/edge shapes + loaders/benchmarks for four engines + raw JSON). Full target reached: **1,000,000 nodes / 8,729,062 edges** (plus 10K + 100K tiers); all four engines loaded and queried every tier, none skipped. Hardware: Apple M3 laptop (8 cores / 16 GB). *Caveat (disclosed): Postgres+AGE and Memgraph ran in Docker's Linux VM (~7.65 GB cap); KuzuDB ran embedded with full host RAM — but the gaps are far too large to be memory-limit artifacts.* These are **measured numbers (evidence)**, not projection.

**Load, 1M nodes / 8.7M edges (seconds):** KuzuDB **9.2** · Postgres relational 20.5 · AGE graph-build 126.4 (146.9 total) · Memgraph 111.5.

**Query latency, large tier (cold / warm-median, seconds):**

| Query | KuzuDB | AGE | Memgraph | Plain Postgres CTE |
|---|---|---|---|---|
| command-chain traversal | 0.33 / 0.002 | 14.56 / 0.007 | 0.006 / 0.002 | 0.005 / 0.001 |
| authority resolution | 0.03 / 0.02 | 12.63 / **10.36** | 0.001 / 0.001 | 0.001 / 0.001 |
| org-subtree reachability | 0.002 / 0.002 | 0.055 / 0.009 | 0.001 / 0.001 | 0.001 / 0.001 |
| as-of-date bitemporal | 0.008 / 0.002 | 0.005 / 0.004 | 0.27 / 0.24 | 0.002 / 0.001 |
| degree centrality | 0.013 / 0.010 | **71.24 / 66.79** | 0.12 / 0.12 | 0.003 / 0.003 |

**Findings:**
- **Plain Postgres CTEs are fastest or tied-fastest on every query** — because the dominant real query (org-subtree reachability) walks the *org catalog* (thousands), never the million-system layer, and simple SQL aggregation beats Cypher-planned aggregation. This **confirms the 2026-06-20 spike's "reuse Postgres" verdict at 10× the node count.**
- **KuzuDB** wins load by 12–16× and every query except the one Memgraph as-of-date outlier; the right choice *when* a native property graph is actually needed.
- **Apache AGE loses decisively** and is ruled out: 10–71 s on multi-hop/analytics, a silent planner footgun (an unlabeled pattern endpoint → non-completing plan even at 10K nodes until labeled), and it **cannot express `-[:A|B]->` type-alternation at all** — forcing UNION rewrites for exactly the multi-typed edges this domain is built on. The "just add AGE to the Postgres we run" argument does not survive.
- Net: **the property-graph promotion is real, but its trigger is the *consumer*, not now** — reachability doesn't need it even at 1M; adopt KuzuDB as an embedded derived read-model when multi-typed traversal or graph analytics is genuinely required.

## Consequences

- The relationship exploration/analytics workload gets a fit-for-shape engine; authority/responsibility questions ("command chain + data-authority around this unit", "who provisions everything under X") become native traversals instead of hand-rolled recursive SQL.
- No change to the architecture, the edge story, or any enforcement path — this is an engine choice behind the existing query-API boundary, so it is a rebuild, not a migration, if revisited.
- Cost: standing up + operating a property-graph read-model at the sovereign plane (bounded — a single-instance, rebuildable projection), plus the spike to pick the engine.
- The Tier framing in `actor-relationship-graph.md` §4 is reframed (§0.2): Postgres to start, property graph as the workload-appropriate exploration target, Zanzibar-style for reachability at scale — three engines for two consumers off one SoT, all swappable behind the API.

## Rollback

Nothing is deployed. The decision is a design promotion + a completed spike; the spike narrowed it to "plain Postgres CTEs remain the reachability default; KuzuDB is the property-graph target *when* the exploration/analytics consumer materializes" — so there is no engine standing up today to revert. Because the graph is a rebuildable read-model behind the query API (ADR 1051 / `actor-relationship-graph.md` §2), any future engine choice is reversible by rebuilding from the SoT — no data migration to unwind. The spike harness (`ops/graph-engine-spike/`) is a self-contained, re-runnable artifact.
