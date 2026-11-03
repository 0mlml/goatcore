# ADR 1122 — Scale envelope revision: systems vs peers, the pod aggregation tier, and the 1M-systems trajectory

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-08-11 |
| Repo scope | Multi |
| Supersedes | `0215-scalability-continuity-scope.md` **in part** — the scale-envelope section only; the six-site demonstration deliverable, the honesty framing, and the non-claims stand |
| Superseded by | — |
| Related | `../project/scale-1m-implementation-plan.md` (phased plan + parallel-lane pickup), [`0200-identity-based-mesh-overlay.md`](0200-identity-based-mesh-overlay.md), [`1020-zenoh-federated-peering-trust-model.md`](1020-zenoh-federated-peering-trust-model.md), [`1025-zenoh-dynamic-tenant-authz-slot-pool.md`](1025-zenoh-dynamic-tenant-authz-slot-pool.md), [`1081-control-plane-storm-resilience.md`](1081-control-plane-storm-resilience.md), [`1092-moon-pod-peer-classes.md`](1092-moon-pod-peer-classes.md), [`1096-delegated-machine-enrollment.md`](1096-delegated-machine-enrollment.md), [`1101-zenoh-lazy-per-slot-storage.md`](1101-zenoh-lazy-per-slot-storage.md), `1116-retire-opsystem-gear-classes.md`, `../design/goat-net-scaling.md`, `../design/actor-relationship-graph.md` §0.1, the integration-pod design, [`../design/scale-validation-plan.md`](../design/scale-validation-plan.md), [`../design/control-plane-storm-resilience.md`](../design/control-plane-storm-resilience.md), `../design/zenoh-cardinality-budget-and-lazy-crawler.md` |

## TL;DR (for a layperson)

ADR 0215 sized the fleet in *peers* — devices enrolled in the mesh — and capped the aim at
6,000. That conflated two different counts. What the mission counts is **systems**: sensors,
effectors, processors, services — everything with an identity that produces or consumes on the
fabric. What the mesh control plane pays for is **peers**: WireGuard enrollments holding a
control channel. Pods and octo-pods sit *between* those counts: one enrolled box fronting many
attached systems. Split the two units and the envelope changes shape: a goatnet should support
**up to 1,000,000 systems carried on roughly 100,000 mesh peers**, reached by aggregation and
partitioning, not by making one flat mesh a hundred times bigger. Peer-count ceilings become an
engineering budget; system count becomes the mission number.

## Context

Three pressures force the revision:

1. **The envelope is already contradicted in-repo.**
   `actor-relationship-graph.md` §0.1 sets a growth
   trajectory toward **>1M systems** — far beyond ADR 0215's ~6,000-node cap. ADR 0215
   and that document have been in open conflict. This ADR resolves the conflict in
   the direction the operator set.
2. **ADR 0200's revisit trigger has effectively fired.** It committed to revisit identity-mesh
   economics "at 5000+ peers"; the stated trajectory blows through that band.
3. **A new tier appeared below host nodes.** ADR 1092 (POD/MOON) and the octo-pod
   integration gateway (the integration-pod design)
   give the fleet an aggregation appliance: one mesh peer running a local Zenoh router and
   per-protocol ingest arms, fronting site systems that never enroll in the mesh themselves.
   ADR 0215 had no vocabulary for what these carry.

## Decision 1 — count three units, never one

| Unit | What it is | What it costs | Where it lives |
|---|---|---|---|
| **System** | Anything with a registry identity that produces or consumes on the fabric | A registry row + a fabric identity (slot cert) or, at the floor, ip-only provenance behind an octo-pod arm | Portal registry; Zenoh ACL |
| **Mesh peer** | A WireGuard/NetBird enrollment holding a management control channel | Management-plane fan-out, signal, netmap compute — the O(·) unit of the control plane | NetBird `peers` |
| **Fabric identity** | An mTLS cert bound to a write slot (ADR 1025) | One slot from the per-goatnet pool; one lazy store when live (ADR 1101) | Zenoh static ACL |

The rule the whole revision hangs on: **aggregation reduces peers, never identities.** The
integration-pod doctrine stands unweakened — per-system fabric identity is mandatory, because a failover
path that depends on the thing it fails over from is not a failover path, and
wire-distinguishability is the point of the identity plane
(the integration-pod design §"three data paths";
ADR 1096 "own-slot identity"). Octo-pod's ip-only provenance grammar
(`<prefix>/<label>/<src-ip-dashed>`) is the declared floor for legacy gear that cannot hold a
cert, not a substitute for identity.

## Decision 2 — the aggregation tier

The scaling hierarchy gains a rung below host nodes:

```
site
 └─ host node               (mesh peer: mgmt host, human endpoint, standalone gear)
     └─ pod / octo-pod      (ONE mesh peer; local Zenoh router; ingest arms)
         └─ attached systems (fabric identities or ip-only provenance — NOT mesh peers)
```

- **Pod** (ADR 1092): a node inside our mesh running our stack, with its own local router for
  the systems behind it. Fabric peering into a pod's router is the `router-peer` graduated
  capability — per-pod, audited, never base-group reach.
- **Octo-pod**: the fan-in variant we operate — per-wire-protocol ingest arms mediating gear
  that speaks no x509/mesh/OIDC, publishing north under mTLS with per-source provenance.
- Aggregation ratios are heterogeneous by design: a human laptop is 1:1; a pod carries tens;
  an octo-pod can carry hundreds. Envelope arithmetic below assumes a **fleet-average ~10
  systems per peer at target and stretch** — an assumption to be measured, not asserted
  (maturity caveat, `goat-net-scaling.md`).

**Honesty note on current practice:** today, pod-attached systems still *also* enroll as mesh
peers (each system behind a current integration pod holds a NetBird identity), because router-to-router peering
(ADR 1020) is designed-not-built and direct-to-mgmt failover requires a mesh path. The
aggregation tier is the forward model; ADR 1020 landing is its gate. Until then, systems ≈
peers and the old arithmetic still governs deployments.

## Decision 3 — the revised envelope (per goatnet)

| Tier | Systems | Mesh peers | Posture |
|---|---|---|---|
| Floor | 30,000 | 10,000 | Must work. The first band where the pod tier and policy partitioning are doing real work — if the architecture cannot hit this, the revision failed. |
| Target | 300,000 | 30,000 | Design center. 100× ADR 0215's target fleet. Partitioning, enrollment, registry, and observability budgets are sized for this. |
| Stretch | 1,000,000 | 100,000 | The trajectory ceiling. Nothing in the architecture may *preclude* it; nothing is claimed validated at it. |

- The envelope is **per goatnet**. Site count is no longer part of the envelope: sites scale
  horizontally per `goat-net-scaling.md` (its Phase 4–6
  ladder and theater-scale scenario remain the site-axis story).
- The **six-site, 600-peer demonstration of ADR 0215 stands** as the Phase-3 deliverable and
  its validation gates ([`scale-validation-plan.md`](../design/scale-validation-plan.md)) are
  unchanged — bands validate in order, and nothing here licenses skipping them.
- ADR 0215's trigger (e), a programme-scale rewrite, is deliberately **not**
  invoked: this is the same system with a corrected unit of count and a raised ceiling, not a
  hundreds-of-sites program rewrite.

## Decision 4 — the preconditions ledger

Each row names the plane, the **verified current state** (cited to code or config, not to
vendor docs), and what must change. This ledger is the engineering plan's skeleton; no row is
optional at stretch.

| # | Plane | Verified today | Required for the envelope |
|---|---|---|---|
| P1 | Mesh netmap fan-out | A peer's network map is already **policy-bounded**, not account-sized (`getPeersGroupsPoliciesRoutes`, fork `management/server/types/account_components.go`) — but every account change recomputes **all connected peers'** maps (semaphore 10) with account-wide inputs (`sendUpdateAccountPeers`, `management/internals/controllers/network_map/controller/controller.go`) | Default-deny group partitioning fired on every goatnet (the F-132 L4 cutover), so per-peer reachable sets — and thus per-peer map cost — stay bounded as N grows. Partition size, not fleet size, becomes the fan-out unit |
| P2 | Update buffering | Buffering exists but `NB_PEER_UPDATE_INTERVAL_MS` **unset defaults to ~1 ms — effectively unbuffered** (`controller.go` `StartWarmup`); a 1 s per-stream debouncer exists | Set and tune the interval on every mgmt host as routine config (group_vars, not a fork); this is free headroom we are not taking today |
| P3 | Posture checks | Re-evaluated **per peer per round, no caching** (`getPeerPostureChecks` inside the per-peer goroutine) | Keep posture-check count near zero until upstream caches them; treat any posture-check adoption as a scale decision, not a feature toggle |
| P4 | Lazy connections | Client-side only (`client/internal/lazyconn/`); management stores a boolean and does **zero less work**; our fleets run `lazy: false` everywhere | Bench-gate and enable (ADR 1081 item 5, C7) for pair-count/signal-rate relief at the data plane; do **not** book it as management-plane relief — it isn't one |
| P5 | Mgmt process ceiling | One process, one store-level global mutex (`sql_store.go` `globalAccountLock`), goroutine-per-stream Sync, `NB_MAX_CONCURRENT_SYNCS` default **1000**; our fork carries **no management-plane deltas** (verified: all goat/L1 patches are client/relay/infra) | Beyond ~30–50k peers per goatnet: shard management (per-goatnet is the natural shard we already have; within a goatnet, site-sharded mgmt + a stream-termination tier separate from map-compute). This is upstream-shaped work — adopt upstream's account-refactor trajectory, contribute rather than fork (fork-free discipline, `environment-parity.md`) |
| P6 | Presence vs netmap | A peer status flip triggers a buffered whole-account round (`SyncPeer` → `OnPeersUpdated`) | Keep liveness expectations off the netmap path (`peer-liveness-expectations.md`); flap-damping at the pod tier (a pod absorbs its systems' churn) |
| P7 | Fabric slot pool | Pool = **1,000 slots per goatnet** (group_vars `zenoh_slot_pool_size`), lazy per-slot stores decouple store count from pool size (ADR 1101: 10k-slot pool ≈ free headroom) | Grow the pool with the salt-stable re-render; per-system identities behind pods live on **pod-local routers**, so the fleet router's ACL and keyspace never see per-system cardinality. Keyspace partitioning across routers is the named backstop (`zenoh-cardinality-budget-and-lazy-crawler.md`) |
| P8 | Fabric cardinality | Budget tiers 100/1,000/exception keys per publisher, live since M-097; one router, one global lock is the stated ceiling | 1M systems × 100-key budget = 10⁸ keys **must not** reach one router: pod-local routers + selective northbound sync are the structural answer; the budget grammar already prices it |
| P9 | Identity & registry | Custodian quota enrollment (ADR 1096) and collision-safe minting are the right shapes; registry read-model tiers staged at 10K/100K (`actor-relationship-graph.md`) | Volume-rate the mint path (the ~30 s single-mint queue becomes a batch surface); hold the 10K/100K re-benchmark gates |
| P10 | Signal | Stateless relay (~400 lines, external dispatcher for cross-instance routing) — shards trivially; F-256 invariant: multi-backend signal only over partition-independent transport | Nothing structural; scale with instance count when establishment rate demands it |

## What this deliberately does not claim

- **No validated numbers above the ADR 0215 bands exist.**
  The revised envelope is a *commitment of direction and a ceiling-not-precluded*, exactly the
  posture `goat-net-scaling.md`'s maturity caveat prescribes.
- **A flat any-to-any million-peer account is not the goal and is explicitly rejected** — the
  envelope is reached by aggregation (pods), partitioning (default-deny groups), and sharding
  (goatnets, sites), never by growing one flat mesh.
- **The pod tier is not an identity shortcut.** Every system keeps its own identity; the tier
  changes only which layer carries the control-channel cost.

## Alternatives considered (brief)

- **Flat 1M-peer single account.** Rejected: the management plane's account-wide recompute and
  single-process ceiling make it an upstream-rewrite hostage; nothing in the mission needs
  any-to-any at that size.
- **Scale only by adding goatnets.** Already the two-plane model; rejected as the *whole*
  answer because a single backbone goatnet can grow past 100K by itself.
- **Fork the management server now.** Rejected: fork-free discipline; our fork carries zero
  management-plane deltas today and upstream is actively refactoring exactly this path.

## Trigger conditions for revisit

- A validation band misses (each 10× step re-runs the [`scale-validation-plan.md`](../design/scale-validation-plan.md) harness shape at the new band).
- The 10K and 100K re-benchmarks (shared with `actor-relationship-graph.md`) contradict a ledger row.
- ADR 1020 (router-to-router peering) lands materially different from designed — the
  aggregation tier's arithmetic is re-derived.
- Fleet-average systems-per-peer measures far from ~10 — the peer budget is re-derived.
