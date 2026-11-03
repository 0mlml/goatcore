# Subscriber / consumption visibility

**Status:** **Tier A (subscription-interest census, both directions) LIVE-FIRED on
ZAGROS 2026-07-11 (M-080).**
A shared `panoscope-core::census` primitive consumed by **panoscope** (21 subscriber
nodes on the live Data-COP, was 0), **`goat top`** (SUBS consumer-fan-out column
populated live), and **`goat who`** (live-subscriptions path proven). Tier B
(per-subscriber throughput) designed-not-built.

**Query the exact granted selectors, not a broad admin query.** The ACL grant is
least-privilege (`@/*/router/{subscriber,publisher}/**`), and zenoh authorization
admits a query only when an allow rule *includes* it (`authorization.rs`
`nodes_including`). So a reader MUST issue those exact selectors
(`census::CENSUS_SELECTORS` / `query_census`) — a broad `@/<zid>/**` admin query is
correctly denied (no rule includes it; `**` can't cross the verbatim `@`). This bit
panoscope's admin-tail on first deploy (it queried broad → zero census) and was
fixed to query the granted selectors (#1547). goat top / goat who use
`query_census`, which issues them by construction.

**Anchor:** F-152
(root-caused + fixed here), F-147
acceptance #1. Composes with panoscope.md, [data-access-cop.md](data-access-cop.md),
[goat-top.md](goat-top.md), [goat-who-identity-cop.md](goat-who-identity-cop.md),
[zenoh-brokerless-consequences.md](zenoh-brokerless-consequences.md).

## The gap this closes

Every fabric-visibility surface we ship is **producer-centric**. `goat top` ranks
producers (`ProducerFootprintV1`); `goat who`'s DATA layer shows read *grants*, not
live subscriptions; the Data-Access COP answers "who *can* read," not "who *is*
reading"; relay throughput is whole-NIC, not split consume/produce. panoscope draws
subscribers only from each peer's voluntary `subscribes[]` schema-decl, so a peer
that only reads and never declares is invisible.

In a brokerless P2P fabric the subscribe side is not a footnote to the publish side —
it *is* the data-access active surface area: who is pulling which data defines the
consumption load and the real read-exposure of every topic. This doc makes the
subscribe side observable.

Two tiers, deliberately separated so the cheap 80% is not held hostage to the
expensive 20%:

- **Tier A — subscription-interest census** (who subscribes to what). Buildable now
  from the router's own admin space. Built here.
- **Tier B — per-subscriber throughput** (bytes actually delivered per consumer).
  Needs a router-exported per-subscription byte metric the stock zenohd image does
  not provide. Not built; options below.

## Why it was invisible — and why it is not impossible

Subscriber accounting is **not** a fundamental cost of going brokerless. The router
*must* track every subscription (interest declaration) to route P2P at all, and it
exposes them in its admin space:

```
@/<router_zid>/router/subscriber/<subscribed_keyexpr>  ->  {routers:[…],peers:[…],clients:[<zid>…]}
@/<router_zid>/router/publisher/<published_keyexpr>    ->  {…}
```

One reply per resource; the subscribed key-expression is the admin-key **suffix**,
and the payload (`Sources`, zenoh `net/routing/hat/mod.rs`) names the peer ZIDs
subscribing to it, bucketed by mode. Verified against zenoh-1.9.0
`net/runtime/adminspace.rs` `subscribers_data` and the crate's own
`tests/adminspace.rs` (which pins the key shape `@/<zid>/router/subscriber/some/key`).
This is the authoritative consumption surface — **no self-declaration**.

### Root cause of F-152's zero replies (`@`-verbatim keyexpr semantics)

F-152 enabled the router adminspace (#337) and added a panoscope
`session.get("@/<router_zid>/**")` query (#331); both merged and deployed, both got
zero replies. The reason, now confirmed from the vendored zenoh source rather than
hypothesised:

`@`-prefixed key chunks are **verbatim** in zenoh keyexpr matching — `**` never
matches them. `zenoh-keyexpr-1.9.0` `key_expr/intersect/classical.rs`:
`(b"**", _) => return !it2.has_verbatim()`, and the crate's `tests.rs` asserts that
matching an `@`-prefixed key requires the explicit `@a/**` form
(`intersect("@a","@a/**")`), while a plain `**` does not. So the router's ACL read
rule `key_exprs: ["**"]` structurally cannot match `@/<router_zid>/…`: the query
reached the router, but egress of every admin reply was ACL-denied. (The pre-existing
`**/@adv/**` pattern handles *data-key-parented* advertisement keys `<key>/@adv/…`;
it does not reach the *top-level* `@/` admin space.)

## Tier A — the fix (built)

**1. ACL grant** — `ops/ansible/roles/goat-zenoh/templates/config.json5.j2`,
rule `trial-peer-r-admin-census`, bound to the universal `trial-peer-read` subject:

```json5
key_exprs: ["@/*/router/subscriber/**", "@/*/router/publisher/**", "@/*/router/queryable/**"]
```

Least-privilege: only the pub/sub/queryable resource lists — a read surface
consistent with this fabric's wide-open reads and strictly less sensitive than the
data itself. Deliberately **not** `@/*/router/**`, which would expose
`@/*/router/config/**` (the live ACL ruleset + storage config) and `metrics` /
`linkstate`. The `*` matches the ZID chunk (router-ZID-agnostic, multi-router-safe);
the verbatim `@` chunk still pins it to the admin space. Reload is a **router
restart**, not SIGHUP (F-156).

**2. Reader** — `ops/panoscope/src/zenoh_client.rs`. `process_admin_reply`
routes census reply keys through `parse_router_resource_key` (extracts role +
owner_zid + keyexpr suffix) and `admin_source_zids` (extracts subscriber ZIDs from
the `Sources` payload), then per subscriber ZID: `upsert_admin_peer(NodeKind::Subscriber)`
+ `record_subscribe_declaration(zid, keyexpr)`. This feeds the **existing** viz —
`snapshot_view` already renders `PeerInfo.subscribes`, the `universal_observer` flag
for `**` subscribers, and publisher→subscriber (sink) edges. The census keys already
arrive via the existing `@/<router_zid>/**` poll once the ACL allows egress; the new
parse branch is what turns them into subscribe records (the generic parser mis-read
`.../router/subscriber/<ke>` as merely "the router peer").

**Two attribution filters — both required, both learned from the live router
(2026-07-11 REST-admin dump of the ZAGROS sandbox router):**

- **`routers` is propagation metadata, not a subscriber.** Every census entry —
  external or not — carries the local router's ZID in `routers` (it is the router
  that holds/propagates the subscription). `admin_source_zids` attributes
  **`clients` + `peers` only**; including `routers` would record the router as a
  subscriber to every topic. (A multi-router/fleet census unions each router's own
  `clients`+`peers`.)
- **The router's own ZID (owner) is filtered from source attribution.** The router's
  in-process storage-manager subscribes every `<uuid>/**` storage prefix, surfacing
  with the router's own ZID in `clients` — 1000 of 1011 entries on the live ZAGROS
  router were exactly this. Without the owner-zid filter the router renders as a
  ~1000-key mega-subscriber, burying the real consumers. Unit tests offline had used
  a clean audit-subscriber-on-`**` fixture and missed both; the live dump caught them.

The real consumption census on ZAGROS after filtering: `**` → 19 subscribers
(audit-subscriber + the synthetic publishers that watch everything); site-scoped
wildcard filters → site-scoped consumers;
`ops/time/attest/**` → the time-health consumer. That is the data-access active
surface — who reads what — with per-topic read-exposure.

**What Tier A delivers:** pure subscribers (audit-subscriber on `**`, non-publishing
sinks, sidecar observers) finally appear on the Data-COP with their subscribed
key-expressions; publisher→subscriber edges become real rather than
self-declaration-only; each topic's live read-exposure (the set of ZIDs subscribing
to it) is enumerable. Closes F-147 acceptance #1.

### Brokerless-safety

Reading the router's own admin space is **passive / reveal-only** — the router
already holds this state to route at all; panoscope reads it, it does not add a
central control point. This does not re-introduce a broker (cf.
[zenoh-brokerless-consequences.md](zenoh-brokerless-consequences.md)): there is no
new QoS chokepoint, no dynamic-ACL authority, no message-rate bottleneck. It is the
same shape as the environment-health surface — each fact self-published/held at the
edge, a passive reader rolls it up.

## Tier B — per-subscriber throughput (designed-not-built)

Tier A answers *who subscribes to what*, not *how much each consumer pulls*. Bytes
delivered per subscriber needs a metric the stock zenohd image does not export —
the same shape of gap the relay dashboard hit
(relay-health-dashboard.md: `goat-relay` has no per-peer
byte counters). Options, none built, in rough order of honesty vs cost:

- **Router metrics adminspace** (`@/<zid>/router/metrics`) — whole-router counters,
  not per-subscription. Cheap but coarse; would give total egress, not
  attribution.
- **Egress accounting in the router** — a per-(subscriber, keyexpr) byte counter
  exported on the admin space. Authoritative and per-consumer, but requires a router
  patch — reopens the "no zenohd fork" line (ADR 0858); do not pursue without that
  decision.
- **Inference from the publish side** — `delivered ≈ published_rate × fanout`, where
  fanout is the Tier-A subscriber count per keyexpr. Buildable on top of Tier A +
  the existing `ProducerFootprintV1`, but it is an *estimate* and must be graded as
  one (never a fabricated %, per the relay-dashboard discipline).

Recommendation: if Tier B is pursued, start with inference (Tier A × footprint) and
label it an estimate; treat a router egress metric as a separate, gated decision.

## One primitive, three surfaces (the dynamics, not a panoscope feature)

The purpose is the pub/sub **dynamics** — production AND consumption, and the
producer→consumer relationships between them — across every operator surface,
not a single view. So the census is **one shared primitive** in `panoscope-core`
(`census` module: `Census { subscriptions, publications }` + `query_census`),
which every mesh-native surface already builds on:

- **`panoscope`** — records the census into its topology so pure subscribers
  (audit-subscriber, sinks) render as nodes with their subscribed
  key-expressions and real publisher→subscriber edges (was self-declaration
  only). Consumes `parse_resource_key` + `census_sources`.
- **`goat top`** — a per-producer **consumer fan-out** column: how many distinct
  subscribers each producer's output actually reaches
  (`Census::fanout_for_anchor`). This is the core pub→sub dynamic — production
  rate is only half the story; fan-out is who's on the other end.
- **`goat who`** — an identity's **live subscriptions** (is-reading) shown beside
  its read **grants** (may-read) in the DATA layer
  (`Census::subscriptions_for_anchor`). Grant = capability; live sub = actual use.

**Both directions are first-class.** The census reads subscriber *and* publisher
lists (`@/<zid>/<whatami>/{subscriber,publisher}/**`); the ACL grants both; the
P2P widening (below) applies to both. Consumption was the missing half, but the
primitive is symmetric.

**Identity correlation.** The census keys on opaque Zenoh ZIDs; the surfaces key
on the slot-anchor. They are unrelated (verified live: 0/20 matched). The bridge
is the publish side — a peer publishes under `<anchor>/…`, so its ZID maps to
that anchor (`zid_anchors`); producer fan-out is anchor-native (keys off the
published-topic prefix, no map needed). A **pure consumer that never publishes
cannot be anchor-correlated from the census alone** — surfaces render that
honestly (no live subscription attributed), never faked. Closing that gap needs
a second correlation source (presence tokens carry both ZID and identity) — a
named follow-on.

**Non-mesh consumers** (a browser/flashlight tier without a Zenoh session) would
need the portal to expose the census over HTTP; the mesh-native trio needs no
shim.

## P2P evolution — when the router stops being the census vantage

**This surface reads the router's adminspace because today's fabric is
single-router, all-peers-in-client-mode — so the router sees 100% of
subscriptions.** That is a current-topology choice, not a permanent one. The
router is a throughput choke point (everything routes through it); the intended
direction is peer/mesh mode where peers route directly
(goat-net-scaling.md,
[zenoh-brokerless-consequences.md](zenoh-brokerless-consequences.md)). When that
lands, subscriptions between directly-connected peers (and same-LAN peers with the
router out of the path) **never touch the router's adminspace**, and a
router-only census goes blind to them.

The primitive generalizes cleanly — every Zenoh node, router or peer, exposes its
*own* adminspace subscriber list — so the census does not need re-architecting,
it needs to move from "query the one router" to "query each node, union the
reachable set." Concrete deltas, in order:

1. **ACL — widen the whatami chunk.** Today the grant is `@/*/router/subscriber/**`;
   a peer-mode node's adminspace is `@/<zid>/peer/subscriber/**`. Generalize the
   `router` segment (`@/*/*/subscriber/**`, or add an explicit `@/*/peer/...`
   leaf), keeping the same least-privilege scope (subscriber/publisher/queryable
   only, never `@/*/*/config/**`).
2. **Adminspace enabled on peers**, not only the router.
3. **Reader — enumerate then union.** Replace the single `@/<router_zid>/**` query
   with: discover live peers (presence/liveliness tokens or scouting), query each
   one's `@/<zid>/<whatami>/router/subscriber/**`, and union the `clients`+`peers`
   per key-expression across nodes (dedup keyexpr→subscriber-set). The owner-zid +
   `routers`-propagation filters already in the reader stay correct per node.
4. **Reveal-only under partition.** In true P2P there is no vantage that sees all —
   a reader sees only the peers it can reach, so a partition yields a *partial*
   census that must be graded honestly (never presented as complete). This is the
   same reveal-only shape as the environment-health surface, and it keeps the
   brokerless invariant: the census vantage moves from one router to N peers; it
   does **not** re-introduce a central observer or broker.

None of this changes the data contract or the reader's per-node parse — only where
the reader points and how many nodes it unions.

## Pointers

- ACL: `ops/ansible/roles/goat-zenoh/templates/config.json5.j2` (rule
  `trial-peer-r-admin-census`).
- Reader: `ops/panoscope/src/zenoh_client.rs`
  (`parse_router_resource_key`, `admin_source_zids`, census branch in
  `process_admin_reply`).
- Viz (unchanged, already consumes it): `ops/panoscope/src/snapshot_view.rs`,
  `crates/panoscope-core/src/{topology,peer}.rs`.
- Findings: F-152,
  F-147.
