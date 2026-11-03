# Zenoh `volatile/` class — routed live, never retained

> **60-second summary:** A reserved top-level key-expression prefix, `volatile/`, for two
> workloads that the fabric should **route but never persist**: high-throughput telemetry
> streams and bounded large-payload (file) transfer. Persistence on this fabric is
> *namespace-scoped* — a rocksdb storage entry on a key-expr persists everything under it,
> and the audit subscriber sweeps `**` into the locked audit-integrity bucket. The `volatile/`
> class turns both off: no `storages` entry ever matches it (so nothing reaches rocksdb), and
> the audit subscriber excludes it (so nothing reaches MinIO). The result is a live-only
> transport plane that cannot fill the router's disk. Telemetry rides `volatile/<peer>/stream/**`
> (loss-tolerant, fire-and-forget); large files ride `volatile/<peer>/blob/**` (chunked,
> reliable, manifest-verified, capped at 100 MiB reassembled). Identity and read access are
> unchanged from the rest of the fabric; only the *retention* behaviour differs.

| | |
|---|---|
| Status | Code + image pin landed on main; pending operator deploy + nova-a live-fire. The ACL write-grant (router templates + `add-vendor.sh`), the audit-subscriber `--exclude-key` filter (default `volatile/**`, with `goat_audit_excluded_total`), and the `audit_subscriber_image` pin to the exclude-capable build are all merged. Remaining (operator-gated): deploy the exclude-capable subscriber + router role on nova-a, then live-fire (route confirmed, rocksdb empty, audit empty → `M-NNN`). |
| Anchor ADRs | [ADR 1032](../adr/1032-zenoh-volatile-non-persisted-namespace-class.md) (this decision), [ADR 0844](../adr/0844-zenoh-deployment-topology-and-acl-v1.md) (namespace + ACL), [ADR 0811](../adr/0811-audit-log-integrity.md) (audit Object-Lock tiers), [ADR 0858](../adr/0858-zenoh-image-upstream-pivot.md) (external audit-subscriber), [ADR 1012](../adr/1012-load-testing-discipline.md) (decouple-and-drop live plane) |
| Reference designs | `zenoh-data-fabric.md` (namespace), [`pubsub-best-practices.md`](pubsub-best-practices.md) (§5 large payloads — Pattern E chunking), [`zenoh-loadtest.md`](zenoh-loadtest.md) (the `loadtest/**` ephemeral-sink precedent), [`audit-cold-storage-offload.md`](audit-cold-storage-offload.md) (the retention tiers this opts out of), [`zenoh-audit-external-subscriber.md`](zenoh-audit-external-subscriber.md) (the subscriber that needs the exclude) |

---

## 1. Why a dedicated class

Two requested workloads do not fit the default retain-everything posture:

1. **High-throughput telemetry** — sensor/metric firehoses where the value is the *live*
   sample and history is uninteresting. Persisting every sample to rocksdb (7d TTL) and to the
   audit bucket (locked, 30d) would fill the router rootfs and evict real vendor data, for data
   nobody queries back.
2. **Large-file transfer (10–100 MiB)** — moving a bounded artifact peer-to-peer over the
   fabric. A single large artifact persisted to rocksdb is both wasteful and unreliable (stock
   Zenoh 1.9.0 silently drops samples above the storage cap — see
   [`pubsub-best-practices.md`](pubsub-best-practices.md) §5).

The operator requirement for both is the same: **be able to turn off any storage to rocksdb or
MinIO**, because it would fill the disk and evict everything else.

On this fabric, persistence is **namespace-scoped**, not per-message:

- **rocksdb** persists a key only if a `storages` entry's `key_expr` matches it. Today every
  registered peer gets a storage entry rooted at `<peer>/**`, so *all* of a peer's traffic is
  retained. You cannot cheaply carve a no-persist subtree out of a `<peer>/**` storage — Zenoh
  storage matching is inclusive, with no exclusion operator.
- **MinIO** retains a key if the external `goat-audit-subscriber` — which subscribes to `**` —
  routes it into the Object-Locked `audit-integrity` bucket.

The clean way to get "never persisted" is therefore a **separate top-level prefix that no
storage entry matches and the audit subscriber excludes** — placed at the top level (not
`<peer>/volatile/`) precisely so the existing `<peer>/**` storage never swallows it.

This is the same shape the load-test apparatus already uses: `loadtest/**` is "just another
vendor" routed by the real router, with its sinks isolated and the real audit subscriber
narrowed off it ([`zenoh-loadtest.md`](zenoh-loadtest.md)). `volatile/` generalises that from a
test-only namespace into a first-class, supported transport class.

## 1a. Why this matters — brokerless scale, and the fabric as conduit not bottleneck

"Don't fill the disk" is the immediate requirement. The larger reason this class matters is that
it is the path by which the data backbone scales to high message volumes —
without the durable tier becoming the limiting factor.

**The non-persisted path removes the throughput-binding resource.** On a persisted prefix, every
sample is routed through the durability sinks — the per-peer rocksdb storage and the external
audit subscriber that ships to MinIO. Those sinks, not the wire, are the binding constraint on
sustained throughput; the load-test apparatus is built to measure exactly that, and the fabric
is deliberately **decouple-and-drop** so a slow sink sheds load rather than back-pressuring the
live plane ([`zenoh-loadtest.md`](zenoh-loadtest.md), [ADR 1012](../adr/1012-load-testing-discipline.md)).
The `volatile/` class takes both sinks out of the path entirely. What remains is the route/wire
path — and on this fabric the data plane is **WireGuard peer-to-peer with the management plane
out of the path**, so a `volatile/<peer>/stream/**` flow from one peer to another can travel
**directly, peer to peer, never funnelling through a central broker or a storage-bearing
router**.

**Brokerless ⇒ no central chokepoint ⇒ the ceiling is hardware × independent flows, not a shared
broker.** This is the same horizontal-scale logic the project already commits to at the relay
byte-throughput layer (`goat-net-scaling.md` Strategy A), now expressed at the pub/sub
message-rate layer: fleet aggregate grows with node count and with the number of independent
flows, because there is no single component every message must pass through. That is the
structural property a brokered bus (Kafka, Solace, a central MQTT broker) cannot offer, and it
is why this class — not the persisted classes — is the one that carries high-rate telemetry.

**Specific numbers are deliberately not asserted here.** Per the project's scaling discipline
(`goat-net-scaling.md` maturity caveat, [ADR 1012](../adr/1012-load-testing-discipline.md)),
the architecture commits to the *direction* — no central funnel, horizontal headroom — not to a
throughput figure. The measured ceilings and the ceiling-sweep that pushes past today's
completeness-validated rates live in the load-test doc; treat those as the source of truth and do
not restate aspirational per-node or fleet figures as if measured.

**For large files, the same principle makes the fabric a conduit, not a store.** A service that
needs to move a bounded large artifact uses `volatile/<peer>/blob/**` to stream it live, and
**remains responsible for its own durable storage and mediation** — the fabric carries the bytes
but never absorbs the burden of retaining them (anything beyond the cap publishes an
external-store pointer instead, §4 / [`pubsub-best-practices.md`](pubsub-best-practices.md)
Pattern G). So bulk file movement can use the shared backbone without turning it into the
bottleneck for everyone else. This is the live-transfer expression of the standing
"bulk data is mobile, not centralized" principle (`goat-net-scaling.md` §"The bridge"): the
backbone moves data; it is not the place data comes to rest.

## 2. The namespace

```
volatile/<peer>/stream/<...>     high-throughput telemetry (loss-tolerant)
volatile/<peer>/blob/<...>       bounded large-payload transfer (chunked)
```

- `<peer>` is the publishing peer's identity prefix — the same anchor its writes are ACL-scoped
  to elsewhere on the fabric (the cert CN / URI-SAN identity axis of [ADR 0844](../adr/0844-zenoh-deployment-topology-and-acl-v1.md)
  / [ADR 1025](../adr/1025-zenoh-dynamic-tenant-authz-slot-pool.md)).
- `stream` and `blob` are the two **kinds**; subtree shape below them is the publisher's choice.
- **Read access and identity are unchanged.** A `volatile/` key is community-readable to any
  onboarded peer exactly like any other topic; writes are scoped to the peer's own
  `volatile/<peer>/**`. The *only* thing that differs from a normal prefix is retention.

`volatile/` is distinct from the existing `telemetry/` prefix:
`telemetry/` is **format-shaped and persisted** (wire-faithful non-canonical formats, retained
for replay); `volatile/` is **retention-shaped and not persisted**. A wire-faithful telemetry
feed that you *do* want to replay stays on `telemetry/`; a firehose you only tap live goes on
`volatile/<peer>/stream/`.

## 2a. Access control is unchanged — persistence and access are orthogonal

Putting the class at the top level is a **storage/audit** decision (it controls whether a
`storages` entry matches and whether the audit subscriber captures it). It does **not** change
the access model. The fabric's ACL discipline ([ADR 0844](../adr/0844-zenoh-deployment-topology-and-acl-v1.md),
[ADR 1020](../adr/1020-zenoh-federated-peering-trust-model.md) ingress/egress split,
[ADR 1025](../adr/1025-zenoh-dynamic-tenant-authz-slot-pool.md) slot pool) applies to `volatile/`
exactly as it does to every other prefix:

- **Default-deny floor, unchanged.** A peer cannot write to `volatile/` unless explicitly
  granted.
- **Writes stay identity-anchored per peer.** The grant is `volatile/<peer>/**` — the `<peer>`
  segment is the same load-bearing identity boundary as in `<peer>/**`. A peer cannot write to
  `volatile/<other>/**`. `volatile/` is **not** a shared open prefix; it is sub-partitioned by
  identity exactly like the main namespace, so the per-tenant write isolation that the ADR 1020
  ingress/egress split protects is preserved (no return of the any-peer-publishes-anywhere
  flood vector).
- **Reads need no new rule.** The universal community-read (`trial-peer-r-all`) and delivery
  (`trial-peer-deliver-all`) rules already match `**`, so subscribing to and receiving
  `volatile/**` works with zero new read rules — `volatile/` is community-readable like any
  topic.

The single ACL-visible consequence is that a peer's writable region is now described by **two
key-expressions** — its persisted `<peer>/**` and its non-persisted `volatile/<peer>/**`. Keep
this as **one grant per peer** by folding both into the peer's existing ingress write rule:

```json5
// trial-peer-w-<peer> — the peer's single write grant, now listing both prefixes:
{ id: "trial-peer-w-<peer>", messages: ["put", "delete"], flows: ["ingress"],
  permission: "allow", key_exprs: ["<peer>/**", "volatile/<peer>/**"] }
```

This keeps the policy/subject count flat (one subject, one rule, one policy per peer), keeps the
ADR 1025 slot pool at **one slot per identity** (a slot's rule simply lists `["<uuid>/**",
"volatile/<uuid>/**"]`), and keeps exactly one place to grant or revoke a peer. Mint a *separate*
rule (bound to the same subject) only if you ever need to grant volatile-write **independently**
of persisted-write — a deliberate, rare choice, not the default.

This is also why the class is top-level rather than `<peer>/volatile/**`: nesting it under the
peer would keep the ACL to a single literal prefix, but the per-peer `<peer>/**` storage would
then persist it, and Zenoh storages have no exclusion operator — you would have to re-key every
peer's persisted topics into an enumerable sub-prefix. Top-level keeps storage-off trivial at
the cost of one extra key-expr on the same identity-anchored grant — a good trade.

## 3. Turning storage off — the two halves

### 3a. rocksdb — off by construction

No `storages` entry in the router config has a `key_expr` matching `volatile/**`, and none is
ever added (the seed list, the per-peer loop, and `add-vendor.sh` all root storages at
`<peer>/**`, which does not match `volatile/<peer>/...`). The router **routes** `volatile/`
samples to live subscribers but writes **nothing** to disk. A reservation comment in
`config.json5.j2` documents that `volatile/**` must never gain a storage entry.

Consequence: `session.get()` against a `volatile/` key returns nothing — there is no store
behind it. This is intended (see §6).

### 3b. MinIO / audit — off by exclusion

The `goat-audit-subscriber` subscribes to `**` and writes every sample's audit record into the
Object-Locked `audit-integrity` bucket. Left alone, it would sweep the entire `volatile/`
firehose into the *locked* compliance bucket — exactly the disk-fill the operator wants to
avoid, and harder to reclaim because Object-Lock blocks deletion.

The fix is a **client-side exclude filter** on the subscriber:

- New repeatable flag `--exclude-key <key-expr>` (env `AUDIT_SUBSCRIBER_EXCLUDE_KEYS`,
  comma-separated), defaulting to `volatile/**`. The subscriber keeps its `**` subscription
  (simple, future-proof against new classes) and drops samples matching any exclude pattern
  before the persistence path. Excluded samples are counted in a
  `goat_audit_excluded_total{key_prefix}` metric so the exemption is observable, not silent.

**Ships-today alternative (no code):** narrow `--subscribe-key` from `**` to the explicit list
of persisted top-level prefixes (`data/**`, `telemetry/**`, `health/**`, …), omitting
`volatile/`. This is the loadtest precedent and works immediately, but is brittle — every new
top-level class must be remembered and added. The exclude flag is the durable form and should
land with the class.

**Optional breadcrumb mode (default off).** If an operator wants a forensic trail of *who moved
what* over `volatile/` without the disk cost, run a second, narrowed audit subscriber that
captures **metadata only** (key, payload hash, size, timestamp, src — never payload bytes) into
a dedicated, **quota-capped, GOVERNANCE-Object-Lock, FIFO-evicting** bucket (the loadtest
`loadtest-ephemeral` shape). This bounds disk by quota and can never block the live plane. Off
by default because the stated requirement is storage off.

## 4. The two kinds — QoS and shape

| | `volatile/<peer>/stream/**` | `volatile/<peer>/blob/**` |
|---|---|---|
| Use | High-throughput telemetry firehose | Bounded large-file transfer (10–100 MiB) |
| Congestion control | `Drop` (Zenoh default) — never back-pressure the publisher | `Block` — every chunk must land |
| Reliability | Best-effort | Reliable (per chunk) |
| Loss posture | Loss-tolerant; a dropped sample is gone (decouple-and-drop live plane) | Lossless within the transfer; missing chunk ⇒ re-request or fail |
| Storage | None (rocksdb + audit both off) | None |
| Retrieval | Live subscribers only | Live receiver only (see §5) |

### High-throughput telemetry (`stream`)

Publish fire-and-forget at full rate. Do **not** confirm every message (per
[`pubsub-best-practices.md`](pubsub-best-practices.md) §9 tier 0). The platform's
expected-vs-actual producer monitor ([ADR 1022](../adr/1022-zenoh-denial-observability.md))
covers "did this producer go silent" without per-message cost. The class rides the
**decouple-and-drop** live plane ([`zenoh-loadtest.md`](zenoh-loadtest.md)): the live route runs
at its own rate and is never throttled by a durability sink — because there is no durability sink.

### Large-file transfer (`blob`)

Stock Zenoh 1.9.0 cannot reliably move a single >16 MiB sample
([`pubsub-best-practices.md`](pubsub-best-practices.md) §5), so a blob is **chunked** (Pattern
E): split into ~1.5 MiB chunks under `volatile/<peer>/blob/<artifact-id>/chunk/<NNNNNNNN>`,
publish reliably, and publish a small `volatile/<peer>/blob/<artifact-id>/manifest` carrying
`{chunks, size, sha256}`. The receiver subscribes, reassembles in order, and verifies the
SHA-256 against the manifest. Reassembled-artifact cap: **100 MiB** (`volatile_blob_max_bytes`,
configurable); above that, use an external-storage pointer (Pattern G) instead — a 100 MiB+
artifact does not belong on the bus.

## 5. Live-only is a design consequence, not a gap

Because nothing is persisted, `session.get()` returns nothing for `volatile/` keys and there is
**no late-joiner catch-up**. For telemetry this is the point (a tap shows you "now"). For blob
transfer it means the **receiver must be online for the whole transfer** — this is a *streamed*
transfer, not a store-and-fetch. Coordinate it with a control handshake on a **persisted**
prefix (e.g. announce `data/.../xfer-offer/<id>` or a `cmd/` queryable so the receiver can
declare readiness and the sender can confirm completion), then stream the chunks over
`volatile/`. If you need store-and-forward semantics (sender and receiver never online
together), `volatile/` is the wrong tool — use the persisted path (and accept its disk cost) or
an external object store (Pattern G).

## 6. Footguns / consequences

- **No `session.get()` on volatile keys.** By design. Subscribers must be live before the put.
- **Audit gap is deliberate.** `volatile/` traffic is intentionally *not* in the compliance
  trail. If a workload needs both high throughput *and* an audit record, it does not belong in
  `volatile/` — put it on a persisted prefix and size the durable tier for its rate, or use the
  metadata-only breadcrumb bucket (§3b).
- **Loss under overload.** The live plane drops rather than block; with no durable copy, a
  dropped `stream` sample is unrecoverable. Acceptable for loss-tolerant telemetry. For
  must-land control data use acked submission (best-practices §9 tier 3), not `volatile/`.
- **Never single-put a large blob.** The 16 MiB wire ceiling is real; always chunk.
- **ACL still applies.** Default-deny means a peer cannot publish to `volatile/<peer>/**` until
  granted an ingress write rule for it (§7). Reads are community-default as everywhere.

## 7. Build status

1. **ACL write-grant (one grant per peer) — DONE.** Each peer's ingress write rule
   (`trial-peer-w-<peer>`) lists both prefixes — `key_exprs: ["<peer>/**", "volatile/<peer>/**"]`
   — rather than a second rule, so the per-peer policy/subject count stays flat (§2a). Wired in
   the `goat-zenoh` and `nova-lan-zenoh` `config.json5.j2` per-peer loops and in both
   `add-vendor.sh` marker-block emitters. Reads need nothing new — the universal community-read +
   delivery rules already match `**`.
2. **Audit exclude — DONE (code + image pin).** `goat-audit-subscriber` has
   `--exclude-key` / `AUDIT_SUBSCRIBER_EXCLUDE_KEYS` (default `volatile/**`); matching samples are
   dropped before hashing/upload and counted in `goat_audit_excluded_total{exclude_pattern}`. The
   subscription stays `**`. Surfaced as `audit_subscriber_exclude_keys` in the role defaults +
   compose env template, and the `audit_subscriber_image` pin is bumped to the exclude-capable
   build (`sha-24cdcf6@sha256:c0b08ea1…`) with a `deploy-history.yml` row per
   [ADR 0859](../adr/0859-easy-rollback-engineering-principle.md). **Deployment note:** until a
   host actually runs that image the env var is ignored (old binary) — so when deploying, bring up
   the exclude-capable subscriber **before** applying the router ACL grant to a live fabric, or
   the firehose lands in the locked audit bucket.
3. **Caps + QoS defaults — DONE (reference below).** `volatile_blob_max_bytes` = 100 MiB; the
   per-kind QoS + chunking guidance is the publisher reference in §8. (Folding it into the
   generated `first-publisher.py` blob variant is a portal-onboarding follow-on.)
4. **Verify — blob round-trip DONE on ZAGROS 2026-06-12 (M-059);
   non-persistence re-probe TODO.** A 5 MiB and a 50 MiB `goat blob` transfer round-tripped with a
   sha256 match against ZAGROS's router (the `volatile/<peer>/blob/**` write-grant is live; the
   live-fire also surfaced + fixed F-195,
   the ZID-pin collision). Still operator-gated (needs host access to the router host): confirm the
   `volatile/` rocksdb dir stays empty and `audit-integrity` receives nothing for `volatile/`
   (`goat_audit_excluded_total` climbing while no `volatile/` objects land), and a sustained
   `stream` firehose.

Ordering invariant: **the ACL write-grant and the audit exclude go live together** — granting the
write while the running subscriber still captures `**` routes the firehose straight into the
locked compliance bucket. Concretely: deploy/redeploy the audit-subscriber (exclude-capable
image) first, then apply the router role / run `add-vendor.sh`.

## 8. Publisher reference (QoS + chunking)

```python
import zenoh, hashlib, json, math

session = zenoh.open(config)          # same mTLS config as any peer
PEER = "acme"                         # your identity prefix

# --- stream: high-throughput telemetry, loss-tolerant ---
pub = session.declare_publisher(
    f"volatile/{PEER}/stream/sensor/thermal",
    congestion_control=zenoh.CongestionControl.DROP,   # never back-pressure us
    priority=zenoh.Priority.DATA,
)
pub.put(sample_bytes)                 # fire-and-forget; nothing persisted

# --- blob: bounded large-file transfer (<= 100 MiB) ---
def send_blob(artifact_id: str, payload: bytes, chunk=1_500_000):
    assert len(payload) <= 100 * 1024 * 1024, "use an external pointer above 100 MiB"
    base = f"volatile/{PEER}/blob/{artifact_id}"
    for i in range(0, len(payload), chunk):
        session.put(f"{base}/chunk/{i // chunk:08d}", payload[i:i+chunk],
                    congestion_control=zenoh.CongestionControl.BLOCK)  # every chunk must land
    session.put(f"{base}/manifest", json.dumps({
        "chunks": math.ceil(len(payload) / chunk),
        "size": len(payload),
        "sha256": hashlib.sha256(payload).hexdigest(),
    }).encode(), congestion_control=zenoh.CongestionControl.BLOCK)
```

The receiver must be subscribed to `volatile/<peer>/blob/<artifact-id>/**` **before** the send
(no catch-up — §5); reassemble chunks in order, verify the SHA-256 against the manifest. For a
firehose `stream`, the consumer is a live subscriber on `volatile/<peer>/stream/**`.
