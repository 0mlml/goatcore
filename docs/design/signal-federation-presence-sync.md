# Signal federation: push-based presence sync (proposal, not built)

**Status:** designed-not-built. Proposed scalable evolution of the ADR 1081
storm-resilience work (tactical version: F-259 / negcache). Needs an ADR before
build — the partition semantics (§4) are the load-bearing part.

## The one-paragraph pitch

Today cross-node signal federation is **pull**: each signal server knows only
its own live registrations, and on a local miss it Forwards to the peer node to
ask "do you have Y?". Cost scales with **offer volume × miss rate**, and while a
peer node is empty every such Forward is a guaranteed-miss round-trip (plus a
`negcache` re-search per offline peer on every cache expiry). Replace it with **push**: the signal servers
replicate their **positive presence set** ("keys currently streaming on me") to
each other as a delta on a cadence. Then any lookup resolves **O(1) locally**,
negative is *derived* (a key in nobody's set is offline), federation cost
becomes a flat sync heartbeat independent of offer volume, and forwards to an
empty node drop to **zero**.

## 2. Why this is the right direction for scale

- **Decouples federation cost from offer volume.** Pull: cost ∝ offers × miss.
  Push: cost ∝ presence-change rate + a fixed heartbeat. At the ADR-0215
  envelope the pull model's cross-node search
  rate grows with the fleet; the push model's does not.
- **O(1) local resolution.** An offer to a peer on the other node resolves from
  the local synced view — no per-offer discovery round-trip.
- **Kills the empty-node waste.** node A would *know* node B's positive set is
  empty and send it zero Forwards (vs today's guaranteed misses).
- **Subsumes the negative cache.** With an authoritative synced positive set,
  "offline" is derived, not cached — no TTL, no re-search. The negcache
  (ADR 1081 L2) becomes unnecessary in steady state (keep it as the
  partition-fallback shed, §4).
- **Bounded payload.** Only the *positive* set is replicated — bounded by
  *online* peers (thousands fleet-wide), not total enrolled. Delta-sync (adds
  /removes since last cursor), not full snapshots.

## 3. Shape of the mechanism

- Each signal server maintains, in memory, `presence[peerKey] -> {node, seen_at}`
  merged from (a) its own `ConnectStream` register/deregister events and (b)
  peer nodes' presence deltas.
- A **presence delta** streams over the **existing dual-path federation
  transport** (`federation-proxy`, mesh-primary + relay-backup — ADR 1085 Amd 1)
  — no new port/ingress. Register/deregister events push immediately (small) with
  a periodic full-set reconcile (anti-entropy) to heal missed deltas.
- Peer-to-peer gossip between the site's signal servers — **not** a central
  broker (consistent with the brokerless ethos; it is presence between two
  known nodes, N-node = full-mesh gossip, bounded N).
- Lookup path: local registry → local synced presence → (only if uncertain,
  see §4) on-demand Forward.

## 4. The hard part — partition semantics (get this wrong and it delivers into a black hole)

The whole risk is **stale presence during a node⇄node partition** (the F-256
case). If the sync stops and node A keeps believing "Y is on node B" while the
link is down and Y actually moved, offers route into a hole. Rules:

1. **Every synced entry carries a freshness bound.** If the last successful sync
   from node B is older than `presence_stale_after`, node B's entries are
   **UNKNOWN**, not authoritative.
2. **UNKNOWN falls back to the pull path** — the on-demand Forward is retained as
   the *fallback*, gated by the same `transportHealthy()` check the negcache
   uses (ADR 1085 Amd 1 point 3). So push is the hot path; pull survives as the
   partition/uncertainty fallback. We do **not** delete cross-node search — we
   demote it.
3. **During a partition, "absent across federation" is unknowable** (same
   invariant as the negcache): do not confidently drop; let the client retry —
   it self-heals when the link heals and anti-entropy reconciles.

## 5. Incremental path

1. **Cheap first step (no new protocol):** background *renewal* of the negcache
   instead of lazy-expire-then-re-search — a refresher re-verifies cached-offline
   keys on a fixed cadence, bounding the search *rate* to a constant. ~an
   afternoon; buys most of the "constant overhead" win without presence state.
2. **The real thing:** positive-presence delta-sync + derived-negative +
   partition fallback (§3–4). ADR-worthy; the partition semantics are the design.

## 6. Boundaries / non-goals

- Rides the **existing** dual-path federation transport — no new ingress, ports,
  or SGs.
- Presence is ephemeral connection state — stays in signal-server memory, **not**
  Postgres/Spock (that plane replicates *enrolled* peers, not *live* streams;
  putting high-churn presence in Postgres = write amplification).
- New stateful component → weigh against the complexity budget (ADR 1016) in the
  ADR. A two-node goatnet is the proving ground; N-node full-mesh gossip is the
  general case.
- **Not built.** This doc captures the direction; the ADR captures the decision.

## Related

ADR 1081 (control-plane storm resilience) · F-259 (signal federation
miss-storm) · ADR 1085 Amendment 1 (dual-path federation transport,
`transportHealthy()` gate) · ADR 1084 (two-node federation) ·
`signal-server/internal/server/negcache.go` (the L2 cache this would subsume).
