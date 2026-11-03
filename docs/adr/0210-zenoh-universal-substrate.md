# ADR 0210 — Zenoh as universal peer-interaction substrate (Phase 3+)

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [`0208-phoenix-native-peer-discipline.md`](0208-phoenix-native-peer-discipline.md), [`0500-zenoh-data-fabric.md`](0500-zenoh-data-fabric.md), [`0509-phase-2-gate-criteria.md`](0509-phase-2-gate-criteria.md), `../design/zenoh-data-fabric.md` |

## TL;DR (for a layperson)

Components on our network need to do seven different kinds of
talking to each other: pushing data updates, finding services,
sending commands, distributing configuration, attesting identity,
reporting health, and federating with partners. Most systems use
seven different mechanisms for these. We use one — Zenoh — and
treat it as a universal substrate that every component talks
through. This commitment is contingent on a Phase 2 evaluation
gate; if that gate fails, we fall back to NATS JetStream.

## Context

Distributed systems typically accumulate one mechanism per
interaction pattern: a pub/sub bus for telemetry, a service
registry for discovery, an RPC layer for commands, a config
distribution tool for configuration, an attestation service for
identity, a metrics pipeline for health, and a federation
gateway for partners. Each is its own operational dependency.

Zenoh's design (key-expression-addressed pub/sub + queryable
storage + structured discovery) maps onto all seven patterns
naturally. The full design is at
`../design/zenoh-data-fabric.md`.
The "Phase 3+ scope expansion" section enumerates the seven
patterns and the architecture diagram.

The commitment to Zenoh as the *universal* substrate (vs. just
the data-fabric pilot) is conditioned on the Phase 2 gate
([`0509`](0509-phase-2-gate-criteria.md)).

## Decision

**Phase 2 (now):** Zenoh is piloted as the data fabric on
integdev with two routers, RocksDB storage, Rust audit-logger
interceptor, Prometheus metrics. See
[`0500`](0500-zenoh-data-fabric.md) and
`../project/zenoh-implementation.md`.

**Phase 3+ (contingent on Phase 2 gate):** Zenoh is committed as
the universal peer-interaction substrate. Every component on the
network uses Zenoh for:

| Pattern | Zenoh shape |
|---|---|
| Data pub/sub | Key-expression publishers / subscribers |
| Service discovery | Capability key-prefix queries |
| Control / command | Key-addressed RPC over Zenoh queryables |
| Configuration distribution | Subscribed-on-startup config keys |
| Identity attestation | Signed key-publication payloads |
| Health / observability | Per-peer health key publication |
| Partner federation | Federated routers across boundary nodes |

The phoenix-native peer discipline
([`0208`](0208-phoenix-native-peer-discipline.md)) requires
this substrate; the shim pattern
([`0209`](0209-shim-pattern.md)) maps onto it.

**Fallback:** if Phase 2 gate produces a no-go, fall back to
NATS JetStream + Go consumers (pre-decided per
[`0500`](0500-zenoh-data-fabric.md)). The fallback covers the
data-fabric role but does not cover all seven interaction
patterns; if invoked, sub-patterns (especially capability
discovery, federation) need separate decisions.

**Publication discipline:** the universal-substrate framing is
deferred for external publication until six readiness criteria
are met (three phoenix-native peers in production, two shims,
one tier-3 rebuild, one caught-bug narrative, six months
operational data, partner federation preservation
demonstrated). See
[`../design/phoenix-architecture.md`](../design/phoenix-architecture.md) §9.1.

## Alternatives considered

- **NATS JetStream + service registry + RPC layer + config
  service + ...** — multiple components for the seven
  patterns. Wider operational surface. Held in reserve as
  fallback.
- **DDS as universal substrate.** DDS is the right choice
  inside specific real-time domains and at specific
  partner federations; DDS bridge is in scope for Phase 3+
  ([`0503`](0503-dds-bridge.md)). DDS as universal
  substrate is heavier and less interception-friendly than
  Zenoh. Rejected as universal; adopted as bridge.
- **gRPC + Kubernetes service mesh + ...** — depends on
  cluster-quorum infrastructure that violates Island Mode
  ([`0206`](0206-island-mode-by-construction.md)). Rejected.
- **libp2p (gossipsub + Kademlia DHT + multiplexed streams).**
  gossipsub is a real pub/sub but lacks Zenoh's key-expression
  routing, queryables, and storage tier — three of the seven
  patterns above (capability discovery, command/RPC,
  configuration distribution) would each need their own bolt-on
  rather than falling out of the substrate. libp2p's
  center-of-mass is decentralized content addressing
  (IPFS / Filecoin / Ethereum p2p), which is a different
  problem than capability-keyed tactical pub/sub. The
  substrate-encapsulation story
  ([`1016`](1016-complexity-budget-substrate-encapsulation.md))
  is also thinner. Rejected for the universal-substrate role;
  the DHT-discovery sub-pattern is parked separately at
  [`../design/phoenix-architecture.md`](../design/phoenix-architecture.md) §5.2 Pattern B.
- **Roll our own.** Engineering cost out of scope.

## Consequences

**Positive.** One substrate, one operational dependency, one
audit surface. Capability-based discovery falls out naturally.
Phoenix-native peers and shims map cleanly. Federation crosses
boundaries via federated routers, not via separate gateways.

**Negative.** Zenoh is a younger ecosystem than NATS or DDS.
Rust-language interceptors are a new ecosystem to operate.
Phase 2 gate is a real risk; the fallback covers the data-
fabric role but introduces multi-mechanism cost for the other
six patterns.

## Assessor view (SCA-R)

Zenoh as universal substrate is a CM-2 (baseline configuration
— the substrate is part of the baseline) and SC-7 (boundary
protection — federation is via boundary-node-mediated routers,
not via per-system gateways) decision. AU-2 (event logging) is
served by the audit-logger interceptor
([`0508`](0508-audit-logger-interceptor.md)) at the substrate
level. The phoenix-native discipline depends on this substrate
for its discovery and role-declaration mechanisms.

The deferred-publication discipline is a separate strategic
choice — we adopt it internally now, we publish externally
only after readiness criteria are met. SCA-R review is welcome
on the technical decision regardless of publication timing.

## Trigger conditions for revisit

- Phase 2 gate produces a no-go. Activate the NATS JetStream
  fallback for the data-fabric role; re-decide the other six
  patterns.
- A specific partner federation refuses Zenoh-based federation
  (likely path: the partner's authorization framework
  pre-dates Zenoh acceptance). Use boundary-node mediation to
  hide the substrate from the partner side.
- Zenoh upstream support deteriorates (vendor disappearance,
  governance failure). Switch to NATS or another substrate;
  the seven-pattern framing survives the swap.

---

*See `GLOSSARY.md` for terms and acronyms.*
