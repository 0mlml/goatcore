# ADR 1041 — Pin router ZID per-host + attach native source provenance on owned publishers

**Status:** Proposed (2026-06-11)
**Repo scope:** goat core (the three long-lived router roles `zenoh` / `nova-zenoh` / `nova-lan-zenoh`; owned plain publishers, synth-vendor first)
**Related:** [`zenoh-capability-adoption.md`](../design/zenoh-capability-adoption.md) (the review-closure design + rollout this extends) · [ADR 1040](1040-zenoh-advanced-pubsub-liveliness-adoption.md) (sibling client-capability adoption; its AdvancedPublisher auto-attaches the same `SourceInfo`) · [ADR 0844](0844-zenoh-deployment-topology-and-acl-v1.md) (cert = trust anchor; ZID is not authz) · [ADR 0858](0858-zenoh-image-upstream-pivot.md) (stock router image — **unchanged**) · [`zenoh-version-pinning.md`](../design/zenoh-version-pinning.md) (`=1.9.0` ACL/admin-shape pin) · [`deliberate-deltas.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/deliberate-deltas.md) (the federation-ZID delta recorded here) · [`zenoh-audit-external-subscriber.md`](../design/zenoh-audit-external-subscriber.md) (`src_zid` audit dedup) · `panoscope.md` (topology keyed on `src_zid`).

## Plain-language summary

A review of how we handle Zenoh node identity (the ZID) against best practice found two gaps in an otherwise-strong posture (cert is already the trust anchor, mTLS is enforced, transport is TLS with explicit connect-lists, the version is pinned):

1. **Routers minted a fresh random ZID on every restart.** A ZID is the identity of a Zenoh runtime. Leaving it random means the same physical router reads as a brand-new node each time it bounces — churning the routing graph, the audit-subscriber's `(src_zid, key_expr)` dedup key, and panoscope's topology. **Fix: pin it, deterministically, per host.**

2. **Publishers never attached `SourceInfo`, so the provenance our consumers already read came back empty.** flashlight and panoscope already call `sample.source_info().source_id().zid()` — but a plain `publisher.put()` leaves `source_info` `None`, so that read resolves to nothing and they fall back to guessing origin from the topic prefix. **Fix: attach the native `(ZID, EntityId)` source info on our owned publishers**, so a sample can be traced to the node that produced it.

Neither touches the trust model: the ZID stays a routing/observability **label**, never a credential. The cert (CN + URI-SAN) remains the only authz anchor (ADR 0844). Neither reopens the router build (ADR 0858).

## Context

The ZID identifies a Zenoh **runtime** (the thing behind a `Session`), up to 16 bytes, random at session open unless pinned via the config `id` field. The globally-unique handle for any entity on that runtime is the pair `(ZID, EntityId)`, surfaced in a sample's `source_info` when the publisher attaches it.

Verified against `zenoh-1.9.0` / `zenoh-ext-1.9.0` source:

- The config `id` field is `Option<String>`, parsed via `uhlc::ID`: **lowercase hex, ≤ 16 bytes (≤ 32 hex chars), and NO leading `0`** — the id is a u128 rendered as minimal hex, so `uhlc` rejects `03454ed1…` outright ("Leading 0s are not valid", `uhlc-0.8.2/src/id.rs:258`). Unset ⇒ random per session. Since every ZID we set is derived from a hash — the per-host router seed here, the slot anchor in `goat-cli` — about one draw in sixteen is illegal, so **every derivation strips leading zeros** (`regex_replace('^0+','')` in the roles, `zid_pin_value()` in the CLI). This costs nothing: `0345…` and `345…` are the same u128. It was learned twice, as F-281 (router crash loop) and F-350 (publish path dead for the affected identity), and is now CI-enforced by `ops/ci/check-zid-derivation-leading-zero.sh`.
- A plain `Publisher::put()` defaults `source_info: None` — no auto-attach. The `.source_info(...)` builder method (on `SampleBuilderTrait`, `#[unstable]`) attaches `SourceInfo::new(EntityGlobalId, SourceSn)` where `SourceSn = u32` and `publisher.id()` returns the publisher's `EntityGlobalId`.
- The **AdvancedPublisher** (`zenoh-ext`, the ADR 1040 gap-5 reliability path) auto-attaches `SourceInfo::new(publisher.id(), seq)` when sequencing is enabled. So native provenance and must-deliver reliability are the **same wire mechanism** — a publisher that adopts AdvancedPublisher gets provenance for free.

What we already do right (no change): ACL keyed on `cert_common_names`, `enable_mtls: true`, TLS transport with explicit `connect` endpoint lists (no multicast scout on exposed segments), and `=1.9.0` pinned fleet-wide so the ACL-subject + admin-space shapes don't drift.

## Decision

### 1. Pin the router ZID, deterministically, per host

Set `id` in the three long-lived router roles (`zenoh`, `nova-zenoh`, `nova-lan-zenoh`) to a value derived in the role defaults:

```
zid_seed = "<inventory_hostname>|<realm>|zenoh-router"
zid      = lowercase_hex( sha256(zid_seed) )[:32]      # 16 bytes
```

- **Derived, not stored, not baked.** A fixed `id` inside a container/golden image is an instant collision the moment you scale past one instance — so the ZID is computed per-host at render time from the operator-assigned inventory identity, never written into the image.
- **Not from `machine-id`.** `machine-id` is a notorious clone-collision source on VMs (our validation envs clone instances). The seed is the inventory hostname (operator-assigned, unique fleet-wide, stable across reinstalls) plus a realm salt.
- **Opaque, no structured prefix.** We deliberately do **not** encode site/region into the ZID (see Deliberate delta below).
- **Override per-host** by setting `<role>_router_zid` directly.
- The **sandbox** router (`zenoh-sandbox`) stays random — it is disposable, with no audit, peering, or provenance; pinning ephemeral nodes buys nothing.

### 2. Attach native `SourceInfo` on owned plain publishers

On publishers we own that stay **plain** publishers, attach `SourceInfo::new(publisher.id(), sn)` per put (monotonic `sn`). First and reference implementation: **synth-vendor** (the canonical data producer; the samples the audit-subscriber records). This populates the `source_info` read flashlight and panoscope already perform, giving them a real origin instead of topic-prefix fallback — and, paired with the pinned router ZID, a **stable** origin across restarts.

**Division of labor with ADR 1040:** reliability-tier / long-lived publishers (the env-health publishers) get the identical `(ZID, sn)` for free when they move to **AdvancedPublisher** under ADR 1040 gap-5 — wiring `SourceInfo` on them manually now would be throwaway. So: plain producers attach manually (this ADR); reliability producers attach via AdvancedPublisher (ADR 1040). `goat pub` is built without the `unstable` feature, so its one-shot publishes cannot attach `source_info`; that is an accepted gap (operator-interactive, low provenance value) — the bridged real-vendor path (`rest-pub-bridge`, which has `unstable`) is the place to add it for non-Rust producers if/when provenance there is required.

## Invariants

- **ZID is a label, not a credential.** Trust stays anchored on the cert (CN + URI-SAN); ACL subjects are unchanged. A pinned or attached ZID is operational filtering / observability among already-authenticated nodes, never an authz input (ADR 0844). It is not authenticated by itself.
- **No router fork, no rebuild** — `id` is stock config; `source_info` is a stock client feature. ADR 0858 intact.
- **Brokerless-safe** — per-publisher sequence numbers live in the producer's own session; no central component, no message-rate funnel.
- **Goatnet-local** — ZIDs travel on the wire but are not cross-goatnet identifiers; the realm salt only de-duplicates a shared inventory hostname across goatnets.

## Deliberate delta — site/region is NOT encoded in the ZID

Best practice for federated ZID-space allocation suggests a structured prefix encoding site/region. We **do not** do this. With random/​hashed 16-byte ZIDs, cross-site collision is already negligible, and we already carry site/region where it is authz-load-bearing: the **namespace** (`…/<site>/…`) and the **cert URI-SAN** (`urn:goat:peer:site=<site>,…`). Encoding it a third time in the ZID would be a parallel, unauthenticated copy of a fact the cert already states under signature. Recorded in [`deliberate-deltas.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/deliberate-deltas.md).

## Consequences

- Router restarts no longer fork identity: routing graph, audit dedup, and topology continuity hold across a bounce.
- Audit/​STANAG-style provenance becomes real and stable: a recorded sample resolves to a specific origin node, not "some publisher on this prefix."
- One more per-host derived value to keep stable (the inventory hostname) — already a hard requirement elsewhere (cert URI-SANs, `site_id` threading).
- ~17 bytes/sample of source-info overhead on attached publishers — trivial against payload, per ADR 1012 direction-not-numbers; the `volatile/stream` high-rate path can opt out if measurement ever shows it matters.

## Alternatives considered

- **Leave routers random (status quo).** Rejected: it is the gap — identity churns on every restart, weakening every consumer that keys on `src_zid`.
- **Derive the ZID from `machine-id`.** Rejected: clone-collision trap on cloned VMs.
- **Pin a fixed ZID in the image / a structured site-prefixed ZID.** Rejected: image-baked ⇒ collision at scale; site-prefix ⇒ duplicate of the cert's signed site claim (the deliberate delta).
- **Wire `SourceInfo` manually on the health publishers too.** Rejected as throwaway: ADR 1040's AdvancedPublisher conversion attaches it for them automatically.
