# ADR 1040 — Adopt liveliness + zenoh-ext advanced pub/sub client-side; the "own the build" gate never applied to them

**Status:** Accepted (2026-06-11; gaps 1–5 built, ZAGROS-targeted gaps deployed + live-fired 2026-06-13 — M-066 presence + must-deliver)
**Repo scope:** goat core (`panoscope-core` reconnect-catch-up wrapper, liveliness presence in env-health, AdvancedPublisher must-deliver path, sample `Encoding` on owned publishers, asserted router `timestamping`)
**Related:** [`zenoh-capability-adoption.md`](../design/zenoh-capability-adoption.md) (the design + rollout) · F-193 (the build-boundary correction this rests on) · [ADR 0858](0858-zenoh-image-upstream-pivot.md) (the router-image boundary — **unchanged**, sharpened) · [ADR 1025](1025-zenoh-dynamic-tenant-authz-slot-pool.md) §7 (which framed AdvancedPub/Sub as a build-ownership decision — corrected here) · F-178 (no native DLQ — AdvancedPublisher is its native answer) · [ADR 1036](1036-environment-health-surface.md) (presence is a new health axis) · [ADR 1037](1037-fabric-internal-time-synchronization-first.md) (HLC as the ordering clock) · F-124 (wired the liveliness ACL *vocabulary* — but F-205 found no rule ever *granted* it; the gap-3 build added the grants) · [`zenoh-brokerless-consequences.md`](../design/zenoh-brokerless-consequences.md).

## Plain-language summary

We had parked Zenoh's two most valuable resilience features — **liveliness** (native presence: be told the
instant a peer drops, instead of guessing from traffic) and **advanced pub/sub** (a subscriber that catches up on
what it missed after a reconnect; a publisher that detects and recovers dropped samples) — believing adopting them
meant *reopening the router build* we deliberately froze ([ADR 0858](0858-zenoh-image-upstream-pivot.md)). That
belief was wrong. Those features live in the **client libraries we already compile ourselves** (`zenoh` /
`zenoh-ext`, behind the `unstable` cargo feature we already turn on almost everywhere); they run over the **stock
router** unchanged. This decision adopts them client-side, in order, with no router fork — and corrects the
record so we stop deferring them against a cost that does not exist.

## Context

A capability review (2026-06-11, captured in [`zenoh-capability-adoption.md`](../design/zenoh-capability-adoption.md))
found four native Zenoh capabilities unused or hand-rolled despite serving our hardest requirements:

1. **Liveliness** — presence/health. Today inferred passively from data traffic, which can't tell *quiet* from
   *dead* (a C2 false-negative). Native, **stable** in 1.9.0; ACL variants already wired (F-124).
2. **QueryingSubscriber / reconnect catch-up** — the bridge between durable storage and live subscription. Core
   to "survives partitions"; currently hand-rolled or absent.
3. **HLC timestamping** — router-default-on; storages already use it for LWW. Assert + consume per [ADR 1037](1037-fabric-internal-time-synchronization-first.md).
4. **AdvancedPub/Sub** — automatic sample-miss recovery + history: the **native answer to F-178** (no DLQ → producer-cooperation).

Items 2 and 4 are one `zenoh-ext` family. [ADR 0858](0858-zenoh-image-upstream-pivot.md) addendum and
[ADR 1025](1025-zenoh-dynamic-tenant-authz-slot-pool.md) §7 had flagged AdvancedPub/Sub as "the strongest reason
to ever own the build." F-193
shows that framing conflated the **router image** (the thing ADR 0858 froze — SHM and `stats` are genuine zenohd
compile features) with **client cargo features** (`zenoh-ext`'s `unstable`, which we compile ourselves and already
enable). AdvancedPub/Sub and liveliness are client-side constructs built on a queryable-backed PublicationCache +
querying subscriber + liveliness — all routed by the stock router. Adopting them is a dependency addition.

## Decision

**Adopt liveliness and zenoh-ext advanced pub/sub client-side, against the stock `eclipse/zenoh:1.9.0` router, in
the rollout order of [`zenoh-capability-adoption.md`](../design/zenoh-capability-adoption.md):** verify the
`zenoh-ext` build resolves → wire sample `Encoding` on owned publishers → assert router `timestamping` → liveliness
presence + subscriber-delivery line → `panoscope-core` reconnect-catch-up wrapper → AdvancedPublisher must-deliver
path (the F-178 native answer, keeping `goat pub --confirm` as the one-shot path).

**Correct the record:** AdvancedPub/Sub and liveliness do **not** require owning the build. ADR 0858's router-image
boundary stays exactly as-is — and is *sharpened*: the real "own the build" items are SHM transport, router
`stats`, and any custom interceptor/zenohd fork. ADR 1025 §7's "watch-item / build decision" framing for
AdvancedPub/Sub is superseded by this ADR.

## Invariants

- **No zenohd fork, no router rebuild** — ADR 0858 intact.
- **Brokerless** — the PublicationCache is per-producer (in the producer's own session), serving retransmission
  of *its own* keys; it is not a central funnel. No message-rate bottleneck, no central QoS/delivery authority.
  This is the cooperative edge-local reliability the brokerless model intended, added without a central component.
- **Identity-bound ACL** — liveliness is already ACL-gated by identity; no new authz surface.
- **Additive wire metadata** — sample `Encoding` is display/decode hinting; no machine-path change (mirrors the
  slot-alias and display-lexicon display-only invariants).

## Consequences

- F-178's must-land reliability gains a **native** path (AdvancedPublisher) alongside producer-cooperation; the
  `zenoh-dlq.md` narrative reconciles to "AdvancedPub/Sub is the native cooperative recovery; acked Query/Reply is
  the explicit one-shot."
- A new **presence** axis exists for the environment-health surface (ADR 1036) without polling.
- Inspection surfaces (panoscope/flashlight/stethoscope) inherit correct **reconnect catch-up** from one
  `panoscope-core` wrapper instead of each re-inventing it. (Mind the three lockfiles per the panoscope-core build note.)
- `goat-cli` is the one client without `unstable`; the subscriber-delivery line / any ext use there needs the
  feature added to `goat-cli/Cargo.toml` (currently `default-features = false`).

## Alternatives considered

- **Keep deferring behind "own the build"** — rejected: the gate was a category error; deferral cost us
  the resilience features for no real reason.
- **Hand-roll catch-up + must-deliver per consumer** (status quo) — rejected: re-implements `zenoh-ext` in
  application code, inconsistently, and is what we're paying for already.
- **Fork zenohd to add reliability** — rejected: unnecessary (client-side suffices) and violates ADR 0858.
