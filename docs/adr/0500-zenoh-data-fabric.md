# ADR 0500 — Zenoh as data fabric (Phase 2D pilot; vs NATS JetStream fallback)

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0210-zenoh-universal-substrate.md`](0210-zenoh-universal-substrate.md), [`0509-phase-2-gate-criteria.md`](0509-phase-2-gate-criteria.md), `../design/zenoh-data-fabric.md`, `../project/zenoh-implementation.md` |

## TL;DR (for a layperson)

We are piloting Zenoh as our internal data-distribution layer —
the substrate where sensors, services, and tools publish and
subscribe to data. Zenoh is open-source, designed for distributed
systems with intermittent connectivity, has a strong story for
storage/replay/query, and supports plugin-based extensions.
Phase 2 is the evaluation. If Zenoh meets the gate criteria,
we commit to it system-wide. If not, we fall back to NATS
JetStream.

## Diagram



Source: [`diagrams/0500-zenoh-data-fabric.mmd`](diagrams/0500-zenoh-data-fabric.mmd).

## Context

The data fabric carries internal pub/sub for telemetry, status,
events, and (eventually) sensor / mission data. Requirements:

- Survives intermittent connectivity (peer disconnect-reconnect).
- Has a queryable storage layer (replay, late join).
- Supports interceptors / extensions for audit, transform,
  policy.
- Federates across boundary nodes for partner integration.
- Open-source with active community.

Zenoh meets all five; NATS JetStream meets four (federation
across boundary nodes is more involved). We pilot Zenoh first;
fallback is pre-decided.

## Decision

**Phase 2D pilot:** Zenoh on integdev (2-node EC2 environment)
pinned to upstream 1.0.x:

- Two routers, federated over WireGuard.
- RocksDB storage for `ops/**` keyspace
  ([`0502`](0502-rocksdb-ops-storage.md)).
- Rust audit-logger interceptor
  ([`0508`](0508-audit-logger-interceptor.md)).
- Synthetic load generator at `ops/zenoh/loadgen/`.
- Prometheus scrape + alert rules.
- isitup entry.

**Phase 2 gate:** three questions
([`0509`](0509-phase-2-gate-criteria.md)) decide whether to
commit to Phase 3.

**Fallback (pre-decided):** NATS JetStream + Go consumers if
the gate produces no-go. Covers the data-fabric role; the
universal-substrate framing ([`0210`](0210-zenoh-universal-substrate.md))
needs separate decisions in that case.

The full design + implementation tracker:
`../design/zenoh-data-fabric.md`,
`../project/zenoh-implementation.md`.

## Alternatives considered

- **NATS JetStream.** Held as fallback. Strong streaming
  story; weaker on capability-based discovery and
  interception. Rejected as primary; pre-decided fallback.
- **Apache Kafka.** Heavier operational footprint; not a
  natural fit for intermittent-connectivity peers. Rejected.
- **MQTT broker.** Too narrow (pub/sub only); no
  storage / query / interception. Rejected.
- **DDS as universal substrate.** Used as bridge
  ([`0503`](0503-dds-bridge.md)), not as substrate.
- **Custom built.** Out of scope.

## Consequences

**Positive.** Pub/sub + queryable storage + interceptor model
in one substrate. Rust ecosystem alignment with
`tech-strategy 2026-04-24 update`.
Federation pattern for boundary nodes is straightforward.

**Negative.** Younger ecosystem than NATS or Kafka.
Operational maturity is being established by the pilot.
Phase 2 gate is a real risk; fallback exists.

## Assessor view (SCA-R)

Zenoh as data fabric serves SC-7 (controlled data flow inside
the boundary), AU-2 (audit via the interceptor), and SC-13
(cryptographic protection — Zenoh transport security plus
boundary-node mediation for cross-boundary federation).
The pilot is in `integdev` and is scoped under
[`1003-cgnat-aware-validation-default.md`](1003-cgnat-aware-validation-default.md).

## Trigger conditions for revisit

- Phase 2 gate produces a no-go: activate NATS JetStream
  fallback; re-evaluate the universal-substrate scope.
- Operational evidence shows a different fabric (NATS, Kafka,
  custom) is materially better. Re-evaluate; do not switch
  silently.

---

*See `GLOSSARY.md` for terms and acronyms.*
