# ADR 1108 — Signal federation presence-sync (push model): direct forward, broadcast only as fallback

| Field | Value |
|---|---|
| Status | Proposed — **built + rig-validated 2026-07-29** (config-gated, default off) |
| Date | 2026-07-29 |
| Repo scope | signal-server (federation path: `internal/server/federation.go`, `exchange.go`, the `SignalFederation` gRPC surface) |
| Related | [ADR 1085](1085-relay-ingress-dispatch.md) (relay ingress dispatch + federation transport) · ADR 1100 (reproduce-and-fix rig) · F-265 (silent cross-shard miss + the `forward_failed` fix) · F-256 · `zenoh-brokerless-consequences.md` (the no-broker discipline this must honor) |

## TL;DR (for a layperson)

When the mesh's "matchmaker" (signal) runs on more than one server, and someone
on server A wants to reach someone on server B, A currently **shouts the request
at every other server** and hopes one of them has the person. That's wasteful
(every message hits every server) and it drowns the logs in "not here" answers —
which is part of why the recent outage was so hard to read. This change has each
server **quietly tell its peers who it's currently holding**, so A can send the
request **straight to B** instead of shouting. If A hasn't heard about someone
yet, it falls back to shouting — so nothing breaks while the lists catch up. No
central directory (that would re-introduce the broker we deliberately don't
have); each server just pushes its own list to its peers.

## Context

Today `relay()` handles a cross-shard message by calling
`federation.Broadcast()`, which **fans the `Forward` out to every configured
federation peer concurrently** (`federation.go`). Exactly one peer (the owner)
delivers; the rest return not-found. Consequences:

- **O(N) fan-out per cross-shard message.** At the ADR-0215 envelope (up to 6000
  peers, multiple mgmt hosts) every miss touches every peer server. It works, but
  it scales the wrong way and it is the bulk of the "60k/h notfound" the F-265
  triage waded through — most of those were the broadcast's own non-owning legs,
  not real failures.
- **It muddies the very distinction F-265
  just fixed.** `forward_failed` now separates "federation broken" from "peer
  offline". But because a Send *broadcasts*, the server can't cheaply assert
  "this peer IS online, just on an unreachable shard" — it only knows "nobody
  answered". Presence would let it say exactly that.

The brokerless root (`zenoh-brokerless-consequences.md`, and the whole reason
signal is federated rather than centralized) forbids a central presence registry.
So presence must be **peer-to-peer pushed**, eventually-consistent, with the
existing broadcast as the correctness backstop.

## Decision

1. **Each signal server PUSHES its local presence set to its federation peers.**
   Presence = the set of registered peer keys on this node (already tracked in
   `registry`). On `register`/`unregister` the server sends an incremental
   **delta** to each federation peer; a periodic **full snapshot** reconciles
   drift (missed deltas, restarts). A new `PresenceUpdate` RPC on the existing
   `SignalFederation` surface carries it, authenticated by the same
   `SIGNAL_FEDERATION_TOKEN` and TLS as `Forward` (ADR 1085 Amendment 1 — no new
   trust surface, no new unauthenticated ingress).

2. **Each server keeps a `remotePresence` map** `peerKey → owningPeer` built from
   inbound `PresenceUpdate`s. Bounded by total fleet size (O(6000) keys — cheap);
   deltas keep steady-state cost near zero.

3. **The Send path forwards DIRECTLY to the known owner; broadcast is the
   fallback.** In `relay()` on a local miss:
   - `remotePresence[key]` known → `Forward` to **that one peer** (O(1)).
   - unknown → `Broadcast` as today (covers the eventual-consistency gap: a peer
     that registered on B microseconds ago, before B's delta reached A).
   This preserves correctness unconditionally — presence is an *optimization +
   an information source*, never the sole authority (mirrors the slot-alias /
   actor-graph "projection, never an enforcement authority" discipline).

4. **Presence sharpens `forward_failed` (composes with F-265).** If
   `remotePresence[key]` names an owner and the direct `Forward` fails (RPC
   error / breaker), the server KNOWS the recipient is online on an unreachable
   shard — an unambiguous `forward_failed` (not a maybe). If the key is in no
   presence set at all, a miss is a real `notfound`. This makes the honesty
   metric from F-265 *precise* rather than *inferred from broadcast completeness*.

## Consequences

- Cross-shard delivery becomes **O(1) forward** in the common (presence-known)
  case; the notfound broadcast-noise that dominated the incident logs largely
  disappears.
- One new federation RPC + one bounded in-memory map + a delta/snapshot pusher.
  Within the complexity budget (ADR 1016): reuses the federation surface, auth,
  and transport; adds no plane and no central component (brokerless preserved).
- **Eventually-consistent by design.** The broadcast fallback means a stale/empty
  presence map degrades to *today's* behavior, never to incorrectness. Worst case
  (all presence lost) = current system.
- Privacy: presence shares peer keys (WireGuard pubkeys) across federation peers —
  which are same-goatnet mgmt hosts inside one trust boundary (the same hosts that
  already relay each other's offers). No new external exposure.
- Reversible (ADR 0859): config-gated (`presence_sync_enabled`); off = pure
  broadcast, exactly today.

## Alternatives (brief)

- **Keep broadcast-to-all (status quo).** Simple, but O(N) fan-out, notfound
  noise, and the can't-assert-online ambiguity F-265 exposed. Rejected as the
  target; kept as the fallback.
- **Pull model (query peers on miss).** Adds a federation round-trip *before*
  every cross-shard delivery — latency on the hot path. Push pre-shares presence
  so the hot path stays one hop. Rejected.
- **Central presence registry / broker.** Rejected outright — re-introduces the
  broker the brokerless architecture exists to avoid; a single point that fails
  exactly when the fabric is stressed.

## Implementation plan (follow-on — the "build" half of task #9)

1. Proto: add `PresenceUpdate` (delta + full-snapshot variants) to the
   `SignalFederation` service; wire the shared-token auth interceptor.
2. Server: on `registry.register/unregister`, enqueue a delta; a pusher goroutine
   coalesces + sends deltas to each peer, plus a periodic full snapshot. Inbound
   handler maintains `remotePresence` (with a generation/epoch to drop stale full
   snapshots).
3. `relay()`: consult `remotePresence` → direct `Forward` to owner, else
   `Broadcast`. Feed the known-owner case into the `forward_failed`/`notfound`
   decision (ADR §4).
4. Metrics: `federation_presence_keys`, `presence_delta_sent/recv_total`,
   `relay_total{path=...,outcome="forwarded",route="direct|broadcast"}` so the
   direct-vs-broadcast ratio is observable.
5. Validate on the ADR-1100 rig: at scale, the notfound broadcast-noise collapses,
   direct-forward ratio → ~1.0, and a killed owner yields a *precise*
   `forward_failed` (presence says online, forward fails) — then the load/soak +
   dual-path fault scenarios still pass.

## Built + validated (2026-07-29)

Implemented behind `--presence-sync` (default off): a `PresenceUpdate` RPC on the
`SignalFederation` surface (`proto/federation.proto`), a `presenceState` pusher +
`ownerByKey`/`nodeByAddr` projection (`internal/server/presence.go`), the inbound
handler + `relay()` direct-forward-with-broadcast-fallback, and metrics
`presence_keys` / `presence_push_total` / `presence_route_total{route}`. Full
`internal/server` suite green **including `-race`**; unit test
`TestPresenceOwnerResolution`. Rig (3 servers, presence on): `presence_keys=80`
(all remote peers known), **632/632 cross-shard forwards routed `direct`, 0
broadcast**, and `relay_total{notfound}=0` — the broadcast fan-out noise (the bulk
of the incident's "60k/h notfound") collapses. Full snapshots only in this first
cut; incremental deltas + the killed-owner precise-`forward_failed` composition
are noted follow-ons. Off = pre-1108 broadcast, unchanged.
