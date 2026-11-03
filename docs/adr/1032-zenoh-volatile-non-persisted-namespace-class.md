# ADR 1032 — Zenoh `volatile/` class: a non-persisted namespace for high-throughput telemetry and bounded large-file transfer

**Status:** Proposed (2026-06-07)
**Repo scope:** goat core (Zenoh namespace + router config, audit-subscriber, onboarding ACL flow)
**Related:** [`zenoh-volatile-class.md`](../design/zenoh-volatile-class.md) (the design) · [ADR 0844](0844-zenoh-deployment-topology-and-acl-v1.md) (namespace + ACL this extends) · [ADR 0811](0811-audit-log-integrity.md) (audit Object-Lock tiers this opts out of) · [ADR 0858](0858-zenoh-image-upstream-pivot.md) (external audit-subscriber that gains the exclude) · [ADR 1012](1012-load-testing-discipline.md) (decouple-and-drop live plane; `loadtest/**` ephemeral-sink precedent) · [`pubsub-best-practices.md`](../design/pubsub-best-practices.md) §5 (large-payload chunking)

## Plain-language summary

Some data should travel across the fabric but never be kept: a telemetry firehose where only
the live sample matters, or a large file being moved peer-to-peer. Keeping that data — in the
router's rocksdb cache and in the locked audit bucket — fills the disk and evicts real data, for
records nobody reads back. This decision reserves a top-level prefix, `volatile/`, for exactly
that traffic: the fabric **routes it live but persists none of it**, to either rocksdb or MinIO.
Identity and read access are unchanged from the rest of the fabric; only retention differs.

## Context

Persistence on this fabric is **namespace-scoped**, not per-message:

- A rocksdb `storages` entry persists a key only if its `key_expr` matches. Every registered
  peer gets a storage rooted at `<peer>/**`, so all of a peer's traffic is retained, and Zenoh
  storage matching has no exclusion operator to carve out a no-persist subtree.
- The external `goat-audit-subscriber` subscribes to `**` and writes every sample into the
  Object-Locked `audit-integrity` bucket.

The operator wants two workloads enabled with persistence turned **off** so they cannot fill the
disk: high-throughput telemetry, and bounded large-file transfer (10–100 MiB). Because
persistence is namespace-scoped, the correct mechanism is a dedicated **top-level** prefix that
no storage entry matches and the audit subscriber excludes — top-level (not `<peer>/volatile/`)
so the existing `<peer>/**` storage cannot swallow it. The load-test apparatus already proves
the shape: `loadtest/**` is routed by the real router with isolated sinks and the real audit
subscriber narrowed off it. This ADR generalises that into a first-class transport class.

Beyond "don't fill the disk," this class is the fabric's **message-rate scaling path**. On a
persisted prefix the durability sinks (RocksDB + the audit subscriber → MinIO) — not the wire —
bind sustained throughput, and the fabric is deliberately decouple-and-drop so a slow sink sheds
load rather than back-pressuring the live plane ([ADR 1012](1012-load-testing-discipline.md)).
Removing both sinks leaves the route/wire path, which on this fabric is WireGuard peer-to-peer
with the management plane out of the path — so a `volatile/stream` flow can travel directly,
peer to peer, with no central broker or storage-bearing router in the path. Brokerless ⇒ no
central chokepoint ⇒ fleet aggregate scales with node count and independent-flow count, the same
horizontal-scale logic the project commits to at the byte-throughput layer, now at the
message-rate layer (`goat-net-scaling.md` §"Message-rate
scale"). Per the project's scaling discipline the commitment is to the direction
(no central funnel), **not** to a throughput figure — measured ceilings live in the load-test
doc. For large files the same property makes the fabric a conduit, not a store: the publishing
service keeps responsibility for durable storage and mediation, so bulk movement never makes the
shared backbone the bottleneck.

## Decision

1. **Reserve a top-level prefix `volatile/`** for traffic the fabric routes live but never
   persists. Shape: `volatile/<peer>/stream/**` (high-throughput telemetry, loss-tolerant) and
   `volatile/<peer>/blob/**` (bounded large-file transfer, chunked).

2. **rocksdb persistence is off by construction.** No `storages` entry matches `volatile/**`,
   and none is ever added; a reservation comment in the router config template records the
   invariant. The router routes `volatile/` samples but writes nothing to disk. `session.get()`
   on a `volatile/` key therefore returns nothing — intended.

3. **Audit/MinIO persistence is off by exclusion.** `goat-audit-subscriber` gains a client-side
   `--exclude-key` filter (default `volatile/**`, env `AUDIT_SUBSCRIBER_EXCLUDE_KEYS`) that
   drops matching samples before the persistence path, counted in `goat_audit_excluded_total`.
   The subscription stays `**` (future-proof against new classes). A ships-today stopgap is to
   narrow `--subscribe-key` to the explicit persisted-prefix list, but the exclude flag is the
   durable form.

4. **Identity and read access are unchanged — persistence and access are orthogonal axes.** The
   top-level placement is a storage/audit decision, not an access one. Default-deny still holds;
   writes stay identity-anchored per peer (`volatile/<peer>/**`, same `<peer>` boundary as
   `<peer>/**` — `volatile/` is sub-partitioned by identity, not a shared open prefix, so the
   ADR 1020 per-tenant write isolation is preserved); reads are community-default and need no
   new rule (the universal `**` read/deliver rules already match). The grant is kept to **one
   per peer** by extending the peer's existing ingress write rule to list both prefixes
   (`key_exprs: ["<peer>/**", "volatile/<peer>/**"]`) rather than minting a second rule — so the
   policy count stays flat and the ADR 1025 slot pool stays at one slot per identity. A separate
   rule is used only if volatile-write must be granted independently of persisted-write (rare).

5. **QoS differs by kind.** `stream` uses Drop congestion control / best-effort (never
   back-pressure the publisher). `blob` uses Block / reliable per chunk, with chunks ≤ ~1.5 MiB
   under a `<artifact-id>/chunk/<NNNNNNNN>` subtree plus a `manifest` (`{chunks, size,
   sha256}`); the reassembled artifact is capped at **100 MiB** (`volatile_blob_max_bytes`).
   Larger artifacts use an external-storage pointer (best-practices Pattern G), not the bus.

6. **The audit exemption is deliberate and the ACL grant must not precede it.** `volatile/`
   traffic is intentionally absent from the compliance trail. Granting a peer the
   `volatile/<peer>/**` write rule **without** the audit exclude (or the subscribe-key stopgap)
   in place would route the firehose into the *locked* audit bucket — the disk-fill this ADR
   exists to prevent. The two land together.

7. **Sandbox stage: reserve now, wire on ratification.** The prefix reservation (namespace
   table + config reservation comment) is additive and harmless and ships with this ADR. The
   ACL write-grant, the audit exclude flag, and a live-fire verification on a sandbox goatnet are the build step.

## Alternatives considered

- **Sub-prefix under each peer (`<peer>/volatile/**`).** Rejected — the per-peer `<peer>/**`
  storage would persist it, and Zenoh storage matching has no exclusion operator to carve it
  out. Excluding it would mean splitting every peer's storage into explicit persisted
  sub-prefixes — far more invasive than a top-level sibling.
- **A per-message "do not store" flag/attachment.** Rejected — stock Zenoh has no per-sample
  persistence opt-out the storage manager honours; persistence is decided by storage `key_expr`
  match. Encoding intent in the address (the namespace) is the mechanism the substrate actually
  provides.
- **Reuse `telemetry/` or `data/`.** Rejected — those are persisted classes; the whole point is
  non-persistence. `telemetry/` stays for *replayable* wire-faithful feeds; `volatile/` is the
  *non-retained* sibling.
- **Bump rocksdb TTL very low instead of disabling.** Rejected — still writes (and audits)
  every sample, so the disk still churns and the audit bucket still fills; a near-zero TTL just
  adds GC load. Off means off.
- **Per-class dedicated ephemeral buckets for everything (always-on breadcrumb).** Rejected as
  default — the stated requirement is storage off. Offered as an opt-in metadata-only,
  quota-capped, FIFO-evicting mode for operators who want a forensic trail without disk risk.

## Consequences

**Positive.** A supported way to run telemetry firehoses and move bounded large files over the
existing fabric without filling the router disk or evicting real vendor data. Reuses the proven
`loadtest/**` ephemeral pattern, the existing identity/read model, and the decouple-and-drop
live plane. rocksdb-off is structural (cannot regress silently — there is simply no storage to
write to). The audit exclude is observable via a metric, so the exemption is never silent.
Strategically, this is the fabric's brokerless **message-rate scaling path**: by removing the
durability sinks that bind a persisted prefix's throughput, `volatile/stream` scales with node
count and independent-flow count rather than a central broker — the message-rate analog of the
project's horizontal byte-throughput commitment. And `volatile/blob` lets services move large files over the shared
backbone while owning their own storage, so bulk movement never becomes the fabric-wide
bottleneck.

**Negative / cost.** `volatile/` is deliberately outside the compliance trail — a workload that
needs both throughput and audit must not use it. No late-joiner catch-up: subscribers must be
live, and `blob` transfer is streamed (receiver online for the whole transfer), not
store-and-fetch. Requires an audit-subscriber code change + image rebuild to land the durable
exclude. The ACL-grant / audit-exclude ordering invariant is a real footgun if ignored.

**Neutral.** Composes with the load-test apparatus (which is the `volatile/` pattern applied to
a test namespace) and with the producer expected-vs-actual monitor (which covers "did this
producer go silent" without per-message confirmation).

## Open items

- Final flag/env names and whether the exclude default ships as `volatile/**` only or a
  configurable list.
- Whether `stream` and `blob` warrant distinct ACL rules or one `volatile/<peer>/**` grant
  (leaning: one grant; the kind split is convention + QoS, not access).
- The blob transfer control-handshake shape (announce + readiness on a persisted prefix) —
  whether to standardise it or leave to publishers.
- Sandbox live-fire numbers (route confirmed, rocksdb empty, audit empty) → `M-NNN`.

## Status

**Proposed** pending ratification.
