# Zenoh QoS, prioritization, and a reserved mission-critical slice — on a brokerless fabric

> **60-second summary:** A brokerless peer-to-peer fabric has no central place to enforce
> quality of service, so "everyone please self-regulate" is not a control — it is anarchy with
> good intentions. This design replaces that with three things that do not depend on goodwill:
> **(1) Reserve** — a guaranteed capacity slice for mission-critical traffic, enforced at every
> point we actually control (the relay, by *authenticated peer identity*; the durable tier, by
> class-admission; owned-host egress, by `tc`), so mission data survives even if every other
> producer misbehaves. **(2) Reveal** — network health is a **first-class, on-bus surface** every
> producer *and* consumer can read in the tools they already use, including their *own* footprint
> in it, so impact is visible and the system is self-correcting. **(3) Enforce** — per-peer hard
> caps + mesh-eviction stop an abuser; cooperative QoS (Zenoh priority bands, congestion control)
> is the optimization layered on top, never the guarantee. Classification is bound to
> **authenticated identity + namespace** (governed, unspoofable), not to a priority byte the
> sender sets (forgeable). No `zenohd` fork; the enforcement lives in the producer, the OS/wire,
> the relay, and the identity/mesh layer.

| | |
|---|---|
| Status | Proposed (design) — see [ADR 1033](../adr/1033-zenoh-qos-prioritization-reserved-mission-slice.md). Phasing in §12; the cooperative + transparency layers are cheap-and-near, the relay reservation/enforcement layer is the heavy, later piece. |
| Anchor ADRs | [ADR 1033](../adr/1033-zenoh-qos-prioritization-reserved-mission-slice.md) (this decision) · [ADR 0844](../adr/0844-zenoh-deployment-topology-and-acl-v1.md) (namespace + ACL — the classification substrate) · [ADR 1012](../adr/1012-load-testing-discipline.md) (the measured knee this is sized against) · [ADR 1022](../adr/1022-zenoh-denial-observability.md) (the expected-vs-actual monitor this extends) · ADR 1021 (authenticated peer identity at the relay — what the reservation keys on) · [ADR 1025](../adr/1025-zenoh-dynamic-tenant-authz-slot-pool.md) / [ADR 1031](../adr/1031-credential-revocation-mesh-first-offboarding.md) (mesh-first cut-off) · [ADR 0858](../adr/0858-zenoh-image-upstream-pivot.md) (no custom router fork — a hard boundary) |
| Reference designs | `zenoh-data-fabric.md` (namespace = the classification axis) · [`zenoh-loadtest.md`](zenoh-loadtest.md) (decouple-and-drop, the knee) · [`zenoh-volatile-class.md`](zenoh-volatile-class.md) (the drop-first spillway) · `relay-architecture.md` (the shared chokepoint) · [`pubsub-best-practices.md`](pubsub-best-practices.md) §9 (producer-side reconciliation) · [`data-access-cop.md`](data-access-cop.md) (the visibility surface this composes with) |

---

## 1. The problem, stated honestly

The fabric's strength — brokerless, peer-to-peer over WireGuard, no central component every
message funnels through ([`zenoh-volatile-class.md`](zenoh-volatile-class.md) §1a) — is also why
QoS is hard. A broker (Kafka, Solace, a central MQTT) gives you **one** place to enforce global
priority and fairness. We deliberately do not have one. The naive consequence is **anarchic
self-governance**: every participant decides its own send rate, and a single buggy or hostile
producer can saturate a shared resource and starve the paths that matter most.

"Make every participant QoS-aware" is necessary but **not sufficient** — a cooperative producer
honoring priority bands helps the common case, but the design cannot *rely* on cooperation,
because the failure we most need to prevent (a flood degrading the control path) is exactly
the case where cooperation has broken down. The worked example: a peer with a valid session
publishes a torrent under a prefix it shouldn't, and the only brakes are a drop-counter and a
manual mesh-eviction. That must not be the story for
mission-critical traffic.

So this design's bar is: **mission-critical data keeps flowing even if every other participant
misbehaves at once.** Cooperation optimizes; it does not protect.

## 2. What actually shares a resource (the threat surface)

Brokerless changes the threat model in our favor, and naming it precisely tells us *where* to
spend enforcement effort. A producer can only degrade a path it **shares a contended resource
with**. There are exactly three:

1. **A relay** — flows forced onto the relay by CGNAT (`relay-architecture.md`)
   share that relay's cores and uplink. This is the **one shared, ownable chokepoint** and the
   primary enforcement site.
2. **The durable tier** — the per-router RocksDB storage and the audit subscriber are shared
   sinks with measured ceilings (the knee, [`zenoh-loadtest.md`](zenoh-loadtest.md)). Already
   protected by decouple-and-drop; this design makes the *drop order* class-aware.
3. **A common subscriber** — two producers fanning into the same consumer share its ingest
   budget. The consumer protects itself (drop by class on its own subscription).

A **direct P2P flow** that shares none of these mostly harms only the flooder's own links — it
cannot degrade an unrelated path, because there is no broker between them. That is a real and
reassuring property: we do **not** need global rate-limiting on every flow; we need it on the
three shared resources above, plus identity-layer eviction for the actor.

## 3. Three pillars

### Pillar 1 — Reserve (capacity guarantee, not just relative priority)

Relative priority ("mission before bulk") fails under sustained overload from a higher layer:
if the bulk producer is *also* marked high, or simply floods, priority alone doesn't guarantee
mission a minimum. So we **reserve** a slice.

- **At the relay (the enforceable reservation):** the relay authenticates every flow's peer
  identity (peer-ID-bound tokens, ADR 1021). It
  reserves a configured fraction of each shard's capacity for flows from
  **mission-class-authorized peers** (the C2 / edge nodes whose certs authorize `cmd/`, `task/`,
  mission `data/`), and caps all other peers into the **remaining best-effort pool** via per-peer
  token buckets (the TURN tier already does per-session caps — `100 quota / 10 Mbps` — so this is
  established relay practice, not new ground). The reservation is keyed on **who the peer is**
  (authenticated), never on a priority byte the peer set (spoofable — §5).
- **At the durable tier:** class-aware admission — the mission class's expected peak is sized to
  fit under the measured knee ([ADR 1012](../adr/1012-load-testing-discipline.md)), and when a
  sink saturates, **bulk classes are dropped first** (the volatile/`stream` class is already
  drop-by-construction — it is the designed spillway). Mission writes are admitted ahead of bulk.
- **On owned-host egress (Orin edge nodes, relays):** a `tc` HTB/CAKE qdisc with a
  **guaranteed-rate class** for the mission DSCP that can borrow spare capacity but never yields
  its floor to bulk.

**Honest limit — the WAN is not ours to guarantee.** The reservation holds at every point we
control. It does **not** hold across the uncontrolled WAN segment — no design can
reserve bandwidth on a third-party link. What we *can* do there is **measure and
surface** the available envelope (Pillar 2) so operators and producers see the real ceiling and
the capacity plan is sized against measured, not assumed, WAN headroom. We do not paper over this.

### Pillar 2 — Reveal (network health as a first-class, participant-visible surface)

If participants must be QoS-aware, the fabric must **tell them the truth, continuously, in a way
they can act on**. Network health is not an operator-only dashboard — it is a **first-class
on-bus surface** every producer and consumer reads, including their own footprint in it.

- **Fabric-health signal — `ops/fabric/health/**`.** Published by the operator monitor + each
  relay onto the bus itself (dogfood; carried in the control/observability class so it is never
  starved). Carries, per site and per shared resource: current load vs the measured knee per
  class, **per-class headroom remaining**, relay queue depth + utilization, drop rate per class,
  measured WAN envelope, and a single **congestion state** (green / amber / red) so a glance
  suffices.
- **Per-participant footprint — `ops/fabric/footprint/<peer>`.** Each participant can see *its
  own* impact: its publish rate, its share of each class, whether it is being dropped or
  throttled, its rank among producers, and its distance from its cap. The answer to "am I the
  problem?" is one query away.
- **Rendered where people already look:** panoscope (web), `goat health` + a live gauge in the
  `goat-cli` TUI, and the portal — so a *consumer* sees fabric weather and a *producer* sees
  fabric weather **plus its own footprint plus "you are #2 in the bulk class, back off"**. This
  composes with the Data-Access COP ([`data-access-cop.md`](data-access-cop.md)), which already
  observes the access layers non-load-bearingly; health is the throughput sibling of that picture.
- **Why this is load-bearing, not decoration:** radical transparency makes the anarchy
  **self-correcting** — a cooperative producer that can see "amber, and I'm the top contributor"
  backs off without being told, and the same signal feeds the automated detection in Pillar 3.
  Visibility is the cheapest large lever we have.

### Pillar 3 — Enforce (caps + eviction, because trust is not a control)

- **Detect:** extend the expected-vs-actual producer monitor ([ADR 1022](../adr/1022-zenoh-denial-observability.md))
  with per-producer **rate thresholds** — a producer exceeding its class budget is flagged, the
  same machinery that already catches silent-denial and the flood case. The per-producer threshold is
  the actor's **throughput-envelope `max`** ([ADR 1045](../adr/1045-qos-throughput-envelopes.md)) — the
  declared per-actor expected-rate band that `goat top` already *reveals* (`LOW`/normal/`HIGH`/`OVER`);
  this enforce pillar acts on sustained `OVER`, gated until the envelopes are corrected off their rough
  defaults.
- **Cap:** the relay per-peer token bucket (Pillar 1) is the hard ceiling — even a malicious
  producer cannot exceed its allocation on a shared relay.
- **Cut off:** mesh-first revocation ([ADR 1025](../adr/1025-zenoh-dynamic-tenant-authz-slot-pool.md)
  / [ADR 1031](../adr/1031-credential-revocation-mesh-first-offboarding.md)) — instant Netbird
  peer removal kills an abuser's link. This is the brokerless-correct answer to "stop this
  producer": you do not rate-limit a bad actor in a central data-plane component you do not have;
  you **eject them from the mesh**. Already built.

## 4. Traffic classes (classification by identity + namespace, not self-assertion)

A small, fixed set of classes — bound to the **topic prefix**, which is already ACL-governed, so
a producer physically cannot publish into a class above its authorization:

| Class | Prefixes | Delivery | Under contention |
|---|---|---|---|
| **C0 — Control** | `cmd/`, `task/`, `ack/`, `ops/fabric/health/**` | Reliable, never dropped | Reserved slice; strict priority |
| **C1 — Mission** | mission `data/` (tracks, alerts, detections), `release/**` | Reliable | Inside the reserved slice; high priority |
| **C2 — Situational** | lower-criticality `data/`, `agents/`, `telemetry/`, `health/` | Best-effort | Normal; yields to C0/C1 |
| **C3 — Bulk / scavenger** | `volatile/stream`, `volatile/blob`, bulk corpus | Best-effort, **drop-first** | Sheds first at every hop — the spillway |

Two rules make this robust:

1. **The class is a property of the address, not a flag the sender sets.** Authorization to
   publish `cmd/` or mission `data/` is the cert/ACL grant ([ADR 0844](../adr/0844-zenoh-deployment-topology-and-acl-v1.md));
   the reserved relay slice is keyed on that same authenticated identity. A bulk vendor cannot
   promote itself into C0/C1 — it has neither the namespace grant nor the mission-peer identity.
2. **Zenoh priority bands + congestion control are the *within-allocation optimization*, not the
   guarantee.** A producer sets `Priority`/`CongestionControl` per class (C0/C1 →
   `Block`+reliable; C3 → `Drop`+best-effort, so it self-sheds at its own egress first). This
   makes cooperative traffic degrade gracefully; it is layered on top of the identity-keyed
   reservation, which is what actually protects the mission path.

### Worst-case delivery budget (provisional)

The number a tenant needs to size a control timeout ([#1401](https://github.com/usareur-af/goatcore/issues/1401)).
It is provisional: it rests on a rig measurement and tenant trials on zagros, not on a live
measurement of a stalled router.

**Two mechanisms set the worst case, and only one of them is bounded.**

1. *Router stall on a stuck subscriber.* One subscriber that stops draining its socket makes
   the router's non-droppable path (reliable plus `CongestionControl::Block`) wait for up to
   the transmit block deadline before it closes the stuck link. The goat routers run that
   deadline at 2 s (`goat_zenoh_tx_block_wait_before_close_us: 2000000`,
   `ops/ansible/roles/goat-zenoh/defaults/main.yml:128`), shipped by #1228 with 8 rx workers.
   On the rig, that pair held an unrelated victim's worst gap to 2.1 s with 0% loss
   (M-119). The
   mechanism is in `zenoh-router-health.md` §"Transmit queue policy".
2. *Queueing behind goat health telemetry.* goat's health publishers send on `ops/**` at
   `InteractiveHigh` (for example `ops/zenoh-trial-peers/fabric-health/src/main.rs:460`).
   Tenant traffic at that band or below waits behind them under contention
   ([#1419](https://github.com/usareur-af/goatcore/issues/1419)). The 2 s deadline does not
   govern this wait, and nothing bounds it today; only `RealTime` sits above it.

**Conditions on the 2 s figure.** It holds only while fewer destinations are stuck at once
than the router has rx workers (8). The stall is rx-worker-bound, not deadline-bound: M-119's
deadline-only arms, with 2 workers, stalled 42-59 s. M-119 also ran on an uncapped 20-CPU
host; eight rx workers on a CPU-capped container (a Thor with a compose `cpus` cap) is not the
same experiment and was not measured.

**Tenant trials on zagros.** Both ran 12 Hz, 150 s cells from a client-mode peer, with no
injected slow subscriber.

Per-band trial ([#1419](https://github.com/usareur-af/goatcore/issues/1419), `fabric_probe2.py`,
eclipse-zenoh 1.10.1):

| Band | Median | Worst gap | Gaps > 1 s |
|---|---|---|---|
| `real_time` | 54 ms | 2.0 s | 6 |
| `interactive_high` | 53-55 ms | 4.0-5.1 s | 8-11 |
| `data` (Zenoh default) | 211-431 ms | 7.9 s | 25-28 |

Integration report ([#1401](https://github.com/usareur-af/goatcore/issues/1401), 2026-09-22, Pi 5
client, scouting off, no router-side metrics):

- `real_time`: worst case 2.008 s.
- The best-effort arms: worst case 1.994 s and 2.015 s, zero loss.
- `reliable` + `Block` at the data band: worst case 6.0 s, 43 gaps over 1 s.
- A local zenohd on the same host: 4 ms worst case.

The `real_time` and best-effort worst cases sit on the 2 s deadline, which is consistent with
M-119 but does not prove it on a goatnet. The 4.0-7.9 s gaps are consistent with mechanism 2; the trials do not isolate their cause
or demonstrate a failure of the deadline. The same tenant's reliable runs also lost 110 and 176 of 1800 messages in 2 of 5
runs, with no mechanism found; until that is explained, "reliable" is not a delivery guarantee
on this path (#1401).

**Budget per class.** Every C0/C1 row assumes fewer than 8 stuck destinations and an uncapped
router; outside that, no bound is known.

| Class | Published at | Observed worst gap (not a guarantee) | Basis |
|---|---|---|---|
| C0 — Control, C1 — Mission | `RealTime` | ~2 s plus path latency | M-119 rig 2.1 s; #1419 `real_time` 2.0 s; #1401 2.008 s |
| C0 — Control, C1 — Mission | `InteractiveHigh` | ~5 s plus path latency | #1419 `interactive_high` 4.0-5.1 s |
| C0 — Control, C1 — Mission | `Data` (Zenoh default) | ~8 s plus path latency | #1419 `data` 7.9 s; #1401 `reliable` + `Block` 6.0 s |
| C2 — Situational | any | no bound; best-effort | — |
| C3 — Bulk | any | no bound; dropped first | — |

The `InteractiveHigh` and `Data` rows are observed worst gaps from two trials, not bounds:
mechanism 2 has no deadline, so a busier goatnet can exceed them.

**What a tenant should do.**

- Publish control and abort traffic at `RealTime`. It is the only band above goat's health
  telemetry, and the only one whose observed worst case sits on the 2 s deadline.
- Size the control-link timeout to the band you publish at: above ~2 s at `RealTime`, above
  ~5 s at `InteractiveHigh`. Do not publish control traffic at `Data`.
- Keep a local deadman that acts without the fabric. The tenant in #1401 did.

M-119's live leg still owes a frozen-subscriber measurement on a goatnet router. That result
would validate the tested stall scenario, not establish a general delivery bound. Re-measure
the `InteractiveHigh` row after #1419 moves health telemetry below the tenant control band;
keep these figures provisional until the relevant contention and deployment limits are covered.

## 5. Why classification cannot ride a priority byte (the spoofing point)

The relay forwards **opaque inner-WireGuard ciphertext** (`relay-architecture.md`) —
it cannot read the Zenoh topic or priority. The only thing it can classify on at the packet level
is the **outer DSCP**, which the *sending host sets*. So DSCP-based priority is trustworthy
**only from hosts we trust** (our Orin edge nodes); from an untrusted vendor host, the DSCP byte
is attacker-controlled and a flood could mark itself "C0" to jump the queue.

Therefore:

- **The reservation is keyed on authenticated peer identity** (peer-ID-bound tokens, ADR 1021) **+ the namespace ACL**, both unspoofable, **not** on DSCP.
- **DSCP / Zenoh priority is the *within-a-peer's-own-allocation* optimization** — it orders that
  peer's own traffic; it cannot let that peer exceed its allocation or jump another peer's
  reservation.
- On **owned, trusted hosts**, DSCP marking is meaningful end-to-end and `tc` honors it directly.

## 6. The per-hop enforcement map

| Hop | Mechanism | Cooperative or enforced |
|---|---|---|
| **Producer** | Zenoh `Priority` band + `CongestionControl` per class; self-rate-limit (token bucket); read fabric health + footprint and back off | **Cooperative** (optimization; assumed honest only for the common case) |
| **Sending host OS** | DSCP mark by class; `tc` (CAKE/HTB + DRR) guaranteed-rate mission class + fair-queue within class | **Enforced on owned hosts**, cooperative on vendor hosts |
| **Relay** (shared chokepoint) | Reserved slice by authenticated mission-peer identity; per-peer token-bucket cap into the best-effort pool; class-aware scheduling on outer DSCP for trusted peers | **Enforced** — the hard floor for forced-relay flows |
| **Durable tier** | Class-aware admission + drop-first for C3; sized under the knee | **Enforced** |
| **Subscriber** | Drop by class on its own subscription; surface its own ingest pressure | **Enforced (self-protection)** |
| **Identity / mesh** | Detect over-rate (monitor) → evict (Netbird) | **Enforced** — the ultimate cut-off |

The pattern is DiffServ: **per-hop behavior, no central controller** — the same model the
internet uses to do QoS without a broker. Each hop acts locally on class; the composition gives
end-to-end behavior at every point we control.

## 7. QoS-awareness is built into the client, not hand-rolled

The risk in "every participant must be QoS-aware" is that each one implements it differently or
not at all. We remove that risk by **building it into the client surface** so the default is
correct:

- The onboarding kit / SDK examples / `goat-cli` publish at the **right class by default for the
  namespace** (a `cmd/` publish is C0 without the producer thinking about it; a
  `volatile/stream` publish is C3).
- `goat health` (and the TUI gauge) make reading fabric health + own-footprint a one-liner.
- A producer opts *into* a higher class only by having the identity/ACL grant for it — so
  "QoS-aware" mostly means "use the kit," not "implement a scheduler."

## 8. Composition with what already exists

- **Decouple-and-drop** ([`zenoh-loadtest.md`](zenoh-loadtest.md)) already keeps storage from
  freezing the live plane; this makes its drop **class-ordered**.
- **The volatile/`stream` class** ([`zenoh-volatile-class.md`](zenoh-volatile-class.md)) is the
  C3 spillway by construction — already loss-tolerant, already drop-class. No new mechanism; it
  *is* the bottom of this model.
- **The expected-vs-actual monitor** ([ADR 1022](../adr/1022-zenoh-denial-observability.md)) gains
  a rate-threshold dimension and publishes the health/footprint signals.
- **Mesh-first cut-off** ([ADR 1025](../adr/1025-zenoh-dynamic-tenant-authz-slot-pool.md) /
  [ADR 1031](../adr/1031-credential-revocation-mesh-first-offboarding.md)) is the enforcement
  endpoint, unchanged.
- **The Data-Access COP** ([`data-access-cop.md`](data-access-cop.md)) is the natural web home
  for the health rendering — the throughput sibling of its access picture.

## 9. Hard constraints (so the design stays buildable)

- **No `zenohd` fork** ([ADR 0858](../adr/0858-zenoh-image-upstream-pivot.md)). No in-router
  scheduler / rate-limiter / interceptor. Enforcement lives in producer + OS + relay + mesh.
- **Static ACL, dynamism-in-identity** ([ADR 1025](../adr/1025-zenoh-dynamic-tenant-authz-slot-pool.md)).
  Rate limits and reservations are dynamic, so they live at the relay + identity layer + monitor,
  not baked into the static Zenoh ACL.
- **The relay is ours; the WAN is not.** Reservations are enforceable at the relay and owned
  hosts; the WAN segment is measured and surfaced, not guaranteed.

## 10. Threat cases, walked

- **Buggy producer floods C3 (`volatile/stream`).** Self-sheds at its own egress (Drop class);
  relay caps it into the best-effort pool; mission reservation untouched; monitor flags it; health
  surface shows it as top contributor; operator (or auto-policy) evicts if it persists. Mission
  path never degrades.
- **Hostile producer marks a flood as C0 DSCP.** Relay ignores the DSCP for reservation purposes
  (keyed on authenticated identity, which does not authorize C0); flood lands in the best-effort
  pool, capped per-peer; evicted on detection.
- **Two mission producers contend.** Both inside the reserved slice; WFQ fair-shares the
  reservation; if the slice itself is undersized, that is a **capacity-planning** signal (the
  health surface shows mission headroom shrinking) → add relay capacity / admission-gate new
  mission producers, per [ADR 1012](../adr/1012-load-testing-discipline.md).
- **WAN degrades.** No reservation can fix the link; the health surface goes amber/red
  with the measured envelope; producers see it and shed C3; mission keeps its reserved share of
  *what the link allows*.

## 11. Open questions

- The reserved fraction per shard and the per-peer cap defaults — to be set against measured
  knees (ADR 1012), not guessed.
- DSCP-over-WireGuard preservation specifics (inner-ToS → outer mark via `tc`/fwmark on `wg0` +
  physical egress) — validate on the Orin/relay before relying on it for trusted-host priority.
- Whether the relay reservation is per-shard or a global token across shards.
- Exact schema for `ops/fabric/health/**` and `ops/fabric/footprint/<peer>` (protobuf message).
- Auto-eviction policy vs operator-in-the-loop for sustained over-rate (likely operator-gated at
  first; the monitor proposes, a human disposes — until the policy is trusted).

## 12. Phasing

- **Now — cheap, cooperative + transparency (highest leverage per dollar):** the class model +
  namespace mapping + client-default `Priority`/`CongestionControl` + DSCP marking on owned hosts
  + the **fabric-health + footprint surface** (`ops/fabric/health/**`, `ops/fabric/footprint/<peer>`,
  rendered in panoscope / `goat health`). This alone protects the mission path in the cooperative
  common case **and** makes impact visible to all.
- **Medium:** extend the expected-vs-actual monitor with per-producer rate thresholds; the
  congestion-state feedback loop; durable-tier class-aware drop ordering.
- **Heavy (the actual guarantee — relay engineering, the Block 33 / vendor-handoff tier per
  `goat-net-scaling.md`):** the relay's identity-keyed **reserved mission
  slice** + per-peer token-bucket caps + class-aware scheduling. This is the layer that delivers
  the literal "cannot allow any producer to spam the network" guarantee for forced-relay flows.
  Everything above it handles the cooperative 95%; this handles the adversarial 5% on the one
  shared chokepoint.

The ordering is deliberate: ship visibility and cooperative QoS first (they are nearly free and
self-correct most of the risk), then the hard relay reservation last (it is the most engineering
but the smallest residual risk once the first two are live).

## Tenant publisher configuration

Set an explicit Zenoh publisher priority for commands (`InteractiveHigh`) and
abort/stop messages (`RealTime`). Leave ordinary data at `Data`. Preserve the
application's reliability and congestion-control choices. This puts commands
at the existing health band and aborts above it; it does not reserve capacity.

See [ADR 1033's publisher review](../adr/1033-zenoh-qos-prioritization-reserved-mission-slice.md#tenant-control-and-abort-bands-1419)
and the runnable tenant example.
The #1419 live contention measurements remain required before claiming the
reported delivery bound is met.
