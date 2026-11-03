# ADR 1033 — Zenoh QoS + prioritization: a reserved mission-critical slice, enforced by identity, with network health as a first-class surface

**Status:** Proposed (2026-06-08)
**Repo scope:** goat core (Zenoh namespace + client conventions, `goat-relay`, the observability monitor, panoscope / `goat-cli` surfaces)
**Related:** [`zenoh-qos-and-prioritization.md`](../design/zenoh-qos-and-prioritization.md) (the design) · [ADR 0844](0844-zenoh-deployment-topology-and-acl-v1.md) (namespace + ACL — the classification substrate) · [ADR 1012](1012-load-testing-discipline.md) (the measured knee this is sized against) · [ADR 1022](1022-zenoh-denial-observability.md) (the monitor this extends) · ADR 1021 (authenticated relay identity — what the reservation keys on) · [ADR 1025](1025-zenoh-dynamic-tenant-authz-slot-pool.md) / [ADR 1031](1031-credential-revocation-mesh-first-offboarding.md) (mesh-first cut-off) · [ADR 1032](1032-zenoh-volatile-non-persisted-namespace-class.md) (the drop-first spillway) · [ADR 0858](0858-zenoh-image-upstream-pivot.md) (no router fork — a hard boundary) · [ADR 0709](0709-block-33-relay-rewrite-architecture.md) (the relay this extends)

## Plain-language summary

On a fabric with no central broker, there is no single place to enforce quality of service, so
asking every participant to self-regulate is anarchy with good intentions — a single misbehaving
producer can saturate a shared resource and starve the most important traffic. This decision says
we do **not** rely on goodwill. We **reserve** a guaranteed capacity slice for mission-critical
traffic and enforce it where we control the path (the relay, by authenticated identity; the
durable tier; owned-host egress); we make **network health a first-class, on-bus surface** every
producer and consumer can see — including their own footprint — so impact is visible and the
system self-corrects; and we **enforce** with per-peer caps and mesh-eviction. Cooperative QoS
(priority bands, congestion control) is an optimization layered on top, never the guarantee. The
class of any message is bound to its authenticated identity and topic prefix (governed), not to a
priority byte the sender can forge.

## Context

The brokerless P2P design ([ADR 1032](1032-zenoh-volatile-non-persisted-namespace-class.md) §1a)
removes the central throughput funnel — its whole scaling argument. The same property removes the
central QoS enforcer. Today the fabric has allow/deny (ACL), drop-counters under
decouple-and-drop, and manual mesh-eviction — but **no priority bands, no rate limits, no reserved
capacity, and a flat FIFO relay**. A valid-session peer flooding an unauthorized prefix shows the
gap: the only brakes are a counter and a manual eviction.

The requirement: **mission-critical data keeps flowing even if every other participant misbehaves
at once.** Two facts constrain the answer. (1) The relay forwards **opaque inner-WireGuard
ciphertext** — it cannot read Zenoh topic/priority, only the outer DSCP, which the sending host
sets, so DSCP-based priority is forgeable by an untrusted producer. (2) No `zenohd` fork
([ADR 0858](0858-zenoh-image-upstream-pivot.md)) — no in-router scheduler. So enforcement must
live in the producer, the OS/wire, the relay (ours), and the identity/mesh layer, and the
mission reservation must key on something unspoofable.

## Decision

1. **Reserve, don't just prioritize.** A configured **mission-critical capacity slice** is
   reserved at every point we control:
   - **Relay:** reserve a fraction of each shard for flows from **mission-class-authorized peers**
     (authenticated peer-ID, ADR 1021); cap all other peers
     into the remaining best-effort pool with per-peer token buckets (the TURN tier already does
     per-session caps — established practice).
   - **Durable tier:** class-aware admission; bulk drops first; mission sized under the measured
     knee ([ADR 1012](1012-load-testing-discipline.md)).
   - **Owned-host egress:** `tc` guaranteed-rate class for the mission DSCP.

2. **Classification is bound to authenticated identity + namespace, never to a self-set byte.**
   Authorization to publish a high class is the cert/ACL grant ([ADR 0844](0844-zenoh-deployment-topology-and-acl-v1.md));
   the relay reservation keys on the same authenticated identity. Zenoh `Priority`/`CongestionControl`
   and DSCP are the **within-allocation optimization** — they order a peer's own traffic and let it
   self-shed gracefully, but cannot promote it above its grant or jump another peer's reservation.
   A fixed class set: **C0 control/command** (`cmd/`,`task/`,`ack/`, health), **C1 mission**
   (`data/`, `release/`), **C2 situational** (`agents/`,`telemetry/`,`health/`), **C3 bulk/scavenger**
   (`volatile/**`, drop-first — the spillway, [ADR 1032](1032-zenoh-volatile-non-persisted-namespace-class.md)).

3. **Network health is a first-class, participant-visible, on-bus surface.** The monitor + relays
   publish `ops/fabric/health/**` (load vs knee per class, per-class headroom, relay utilization,
   drop rates, measured WAN envelope, a green/amber/red state) and `ops/fabric/footprint/<peer>`
   (a participant's own rate, class shares, throttle/drop status, rank, distance from cap), carried
   in C0 so it is never starved. Rendered in panoscope, `goat health` / the `goat-cli` TUI, and the
   portal — readable by producers **and** consumers. Transparency makes the anarchy self-correcting
   and feeds the detector.

4. **Enforce with caps + eviction; cooperation is optimization, not control.** Detect over-rate
   via the expected-vs-actual monitor extended with per-producer rate thresholds
   ([ADR 1022](1022-zenoh-denial-observability.md)); cap at the relay token bucket; cut off via
   mesh-first revocation ([ADR 1025](1025-zenoh-dynamic-tenant-authz-slot-pool.md) /
   [ADR 1031](1031-credential-revocation-mesh-first-offboarding.md)).

5. **QoS-awareness is built into the client surface,** not hand-rolled — the kit/SDK/`goat-cli`
   default to the correct class for the namespace, so "be QoS-aware" mostly means "use the kit."

6. **Phased:** ship the cooperative + transparency layers first (cheap, self-correct most risk),
   then the relay identity-keyed reservation + per-peer caps last (the hard guarantee, the
   heaviest engineering, the Block 33 / vendor-handoff tier).

## Alternatives considered

- **Pure cooperative self-regulation (priority bands only).** Rejected as the *guarantee* — it is
  exactly the failure case (a flood) where cooperation has broken down. Kept as the optimization
  layer (decision 2).
- **A central QoS broker / gateway every message funnels through.** Rejected — it reintroduces the
  central chokepoint the brokerless design exists to avoid, capping the very scale
  [ADR 1032](1032-zenoh-volatile-non-persisted-namespace-class.md) unlocks, and becomes a single
  point of failure for the control path.
- **DSCP-priority as the reservation key.** Rejected — the relay sees only outer DSCP, which an
  untrusted producer sets, so a flood could self-mark "control." DSCP is kept as a *within-allocation*
  hint and is trustworthy only from owned hosts. The reservation keys on authenticated identity.
- **A custom `zenohd` interceptor/scheduler for rate limiting.** Rejected — violates the no-fork
  boundary ([ADR 0858](0858-zenoh-image-upstream-pivot.md)); enforcement lives at the relay + OS +
  identity layers instead.
- **Operator-dashboard-only health (not on-bus, not per-participant).** Rejected — the requirement
  is that *participants* see their own impact; an operator-only view does not make the anarchy
  self-correcting. Health rides the bus and includes per-peer footprint.

## Consequences

**Positive.** Mission-critical traffic gets a guarantee that does not depend on any producer's
goodwill, enforced where we actually control the path and keyed on unspoofable identity. Network
health and per-participant impact become visible to everyone, making most misbehavior
self-correcting before enforcement is needed. Reuses the namespace/ACL, the measured knee, the
monitor, the volatile spillway, peer-ID identity, and mesh-eviction — little new substrate beyond
the relay scheduler and the health surface. No router fork; no central broker.

**Negative / cost.** The hard guarantee requires real relay engineering (reserved slice + per-peer
token buckets + class-aware scheduling) — the heaviest, latest phase, likely a vendor-handoff item.
DSCP-over-WireGuard preservation needs validation before trusted-host priority can be relied on.
The reservation holds only at points we control — the WAN segment is measured, not guaranteed, and
that limit must be communicated honestly. Per-participant footprint adds an observability stream
(small, C0).

**Neutral.** Composes with the Data-Access COP (health is the throughput sibling of its access
picture) and with capacity planning (an undersized reservation surfaces as shrinking mission
headroom → add capacity / admission-gate, per ADR 1012).

## Open items

- Reserved fraction + per-peer cap defaults (set against measured knees, not guessed).
- DSCP-over-WireGuard preservation mechanics; per-shard vs global relay reservation.
- `ops/fabric/health/**` + `ops/fabric/footprint/<peer>` protobuf schema.
- Auto-eviction vs operator-in-the-loop for sustained over-rate (likely operator-gated first).

## Status

**Proposed** pending ratification.

## Tenant control and abort bands (#1419)

Tenant publisher examples must set `InteractiveHigh` for commands and
`RealTime` for abort/stop messages. Ordinary data stays at `Data`. These are
cooperative scheduler settings, not the identity-based reservation described
above; they do not implement #1313 or establish a delivery guarantee.

The publisher review covers nine health-publisher components under
`ops/zenoh-trial-peers/` and `ops/consistency/rust/`. It retains
`Block` + `InteractiveHigh` for their existing health outputs:

| Publisher | Reason for retaining the band |
|---|---|
| fabric-health | Reports congestion and degraded delivery to participants. |
| mesh-health | Reports reachability needed to diagnose loss of the control path. |
| node-net-health | Reports local network degradation affecting that path. |
| peering-health | Reports federation reachability and peering health. |
| ntp-health-pub | Reports time confidence used to interpret freshness. |
| [presence-health](https://github.com/usareur-af/goatstuff/blob/main/ops/zenoh-trial-peers/presence-health/src/main.rs) | Reports live and recently dropped presence tokens so operators can distinguish quiet peers from absent peers under load. Absence alone is not a health alarm. |
| [time-health](https://github.com/usareur-af/goatstuff/blob/main/ops/zenoh-trial-peers/time-health/src/main.rs) | Rolls host time attestations into a site reading, including stale or unattested hosts, so operators can assess time confidence under load. |
| fabric-crawler | Publishes the census and rollup used to attribute fabric load. |
| goat-consistency-health | Publishes configuration drift reports and the health rollup. |

These observations need to remain available under the load they diagnose.
Keeping their existing C0 policy avoids moving the overload signal behind bulk
data. Tenant commands explicitly share the band; abort messages outrank it.
This is a reason to retain the band, not evidence that current telemetry volume
is harmless. The #1419 contention measurement must retain router metrics and
loss counts as well as latency; a failed measurement leaves acceptance open.

The runnable probe is in
Hello Galaxy.
Use 12 Hz, 150-second cells and three repetitions per band under matching load.
The control-band maximum observed receive gap must be at most 5.1 seconds.
Do not label a local loopback result as the live goatnet proof.
