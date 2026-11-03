# ADR 1095 — Per-entity Zenoh mode selection and peer-mode edge continuity

| Field | Value |
|---|---|
| Status | Proposed (client-default is the built state; peer-mode edge continuity is designed-not-built) |
| Date | 2026-07-10 |
| Repo scope | goat core — Zenoh deployment topology |
| Supersedes | — |
| Superseded by | — |
| Related | [ADR 0844](0844-zenoh-deployment-topology-and-acl-v1.md) (deployment topology + one-logical-mesh), [ADR 0858](0858-zenoh-image-upstream-pivot.md) (stock-Zenoh, no router fork), [ADR 1050](1050-reticulum-denied-edge-bridge.md) (sub-IP denied-edge bearer), design: `zenoh-data-fabric.md` §"Per-entity mode selection", resilience: `ha-multi-site.md` §8 (island mode) |

## TL;DR (for a layperson)

Our data fabric runs "routers" — one at each site — and every sensor, effector, and laptop talks to its local router, which forwards messages and also does the storage, auditing, and access checks. That works even when a site is cut off from the rest of the world, because the router is *at* the site, not in the cloud. The open question is: what if two things need to keep talking to each other but *neither* can reach any router? Zenoh (the fabric software) can let two endpoints talk directly with no router in the middle — a per-endpoint setting — and you can freely mix "router-only" endpoints with "can-go-direct" endpoints on one network. We are **not** turning that on yet, for three honest reasons, and this ADR records the decision and those reasons so nobody assumes we already have router-less peer-to-peer when we don't.

## Context

Per ADR 0844 the backbone is "one logical Zenoh mesh": `zenohd` routers live on the per-site mgmt-hosts (Orin-A/B, keepalived pair — ADR 0826), and the router plugins hold storage, audit, CDS conditioning, and the static ACL. Application publishers and subscribers connect to their **site** router in `mode: client`; routers federate to each other (`mode: router`). So a message travels publisher → site-router → (federation) → site-router → subscriber; there is no direct client-to-client Zenoh session.

This already delivers **island-mode continuity from the router's point of view**: because the router is co-located with the peers at the edge, a site cut off from every other site keeps its local pub/sub working (only cross-site federation pauses; Spock catches up on reconnect — `ha-multi-site.md` §8). "Disconnected from the core" does not mean "no router in reach."

The unanswered case is **router-less continuity**: both site routers down, or two peers on a LAN/WAN segment that cannot reach any router. Today those peers cannot exchange Zenoh pub/sub, even though their NetBird/WireGuard tunnel survives (network-layer P2P is self-sustaining post-enrollment) — because `mode: client` requires a reachable router. Zenoh natively supports `mode: peer` (endpoints discover each other and form direct sessions with no router) and the three modes interoperate on one network, so this is a per-entity **config** capability, not new code (`zenoh-data-fabric.md:531`). The question is whether to adopt it, and under what honest caveats.

## Decision

**Keep `mode: client` + router-mediated as the default for application endpoints. Treat per-entity mode selection as a supported, native capability, and peer-mode edge continuity as a designed-not-built option to be opted into per entity — never a blanket flip — gated on resolving three catches.**

1. **Per-entity mode is native and hybrid.** `mode` is per-session: each entity independently chooses `client`, `peer`, or `router`, and all three interoperate on one Zenoh network. Peer mode is a superset of client mode — a peer still uses the router when reachable (keeping storage/audit/ACL/routing) *and* can form direct sessions with other peers. So the adoption path is selective: leave most entities `client`, opt specific edge-critical entities into `peer`.
2. **Client-default stays.** Router-mediated is the default because the router is where the plugins live (storage, audit, static ACL, CDS conditioning — ADR 0844). Traffic that skips the router skips those.
3. **Peer-mode edge continuity is designed-not-built, gated on three catches** (recorded so it is not mistaken for a live capability):
   - **Discovery when the router is gone.** Peer discovery is multicast scouting, which does not traverse the routed NetBird L3 overlay; in normal operation peers learn each other through the router's gossip, which dies with the router. Router-less peers therefore need **pre-configured (or cached) direct `connect` endpoints** to find each other. Discovery is the real engineering problem, not the mode flag.
   - **Direct peer traffic bypasses the router plugins.** A router-less peer-mode exchange has **no ACL enforcement, no audit, no storage** unless the endpoints enforce/log themselves. The payload-level machine-read control (ADR 1094 grant-keyed encryption) still holds because it is content-bound; the namespace ACL does not.
   - **Unproven in our stack.** Our one empirical trial of peer mode failed — "default Zenoh peer mode silently drops publishes through the router; forced mode=client" (goat-cli Phase D E2E). Peer mode needs real bring-up work, not a flag flip.
4. **This is above IP, and distinct from the denied-edge bearer.** Peer mode presumes an IP path (the surviving WireGuard tunnel). When there is no IP path at all (RF / fully denied edge), the bearer is Reticulum (ADR 1050), not Zenoh peer mode.

## Alternatives considered

- **Blanket `mode: peer` fleet-wide.** Rejected — loses router-plugin enforcement/audit/storage on every exchange, and multicast discovery does not work over the routed overlay anyway. Peer mode is an opt-in per entity, not a default.
- **A local router per LAN segment (push the router even closer).** Partially already the answer (per-site routers give edge-local continuity). Pushing a router onto every LAN segment is heavier hardware/ops than opting the few edge-critical endpoints into peer mode; revisit if a segment routinely operates below the site-router tier.
- **Do nothing (client-only forever).** Rejected as the *documented* posture — leaves a real continuity gap unnamed. This ADR keeps client-default but records peer-mode edge continuity as a build option with explicit rationale.

## Consequences

- **Honesty:** the docs now state plainly that disconnected continuity today comes from edge-local routers, not router-less P2P; the latter is designed-not-built. No description of the fabric should imply endpoints keep talking with no router in reach.
- **Build trigger:** when an operating case needs two endpoints to survive total router loss (both site routers down, or a router-less forward segment), the work is (a) pre-configured `connect` endpoints for the peer pair, (b) an enforcement/audit story for the router-less window (encryption + local logging), (c) bring-up validation given the prior peer-mode failure.
- **Security interaction:** router-less peer exchanges are unaudited/unenforced at the namespace layer during the window; grant-keyed payload encryption (ADR 1094) is the control that still applies, which argues for encrypting any stream that might run router-less.
- **No fork, no new component:** adoption is Zenoh config + endpoint provisioning, consistent with the stock-Zenoh boundary (ADR 0858).
