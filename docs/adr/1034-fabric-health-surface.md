# ADR 1034 — Fabric health surface: participant-visible network health + per-producer footprint, published on-bus

**Status:** Proposed (2026-06-08)
**Repo scope:** goat core (`goat-fabric-health` service, `goat-cli` consumer, panoscope later)
**Related:** [ADR 1033](1033-zenoh-qos-prioritization-reserved-mission-slice.md) (the QoS umbrella — this is its "Reveal" pillar, built first) · [`fabric-health-surface.md`](../design/fabric-health-surface.md) (the design) · [ADR 1022](1022-zenoh-denial-observability.md) (the expected-vs-actual monitor — sibling observability) · [ADR 0858](0858-zenoh-image-upstream-pivot.md) (the meter is a normal peer, not a router fork) · [ADR 1032](1032-zenoh-volatile-non-persisted-namespace-class.md) (the volatile class the audit-subscriber excludes — why this can't reuse it) · [ADR 1012](1012-load-testing-discipline.md) (the measured knee the capacity targets come from) · [ADR 0844](0844-zenoh-deployment-topology-and-acl-v1.md) (namespace + the `ops/` prefix)

## Plain-language summary

On a brokerless fabric, the only way "everyone be QoS-aware" works is if the fabric continuously
tells every participant the truth about its health **and about that participant's own impact** —
otherwise self-regulation is blind. This decision builds that surface: a small always-on meter
watches all traffic and publishes two things back onto the bus — **fabric health** (how loaded
each traffic class is versus its capacity, with an **honest** green/amber/red state — and an
explicit `idle` when there is too little traffic to confirm health, never a false "green") and **per-producer
footprint** (your own send rate, your share of each class, your rank, whether you're near a
limit). Any producer or consumer reads it in `goat health` (and later panoscope), so impact is
visible to all and the self-governance becomes informed rather than anarchic. This is the
**Reveal** pillar of the QoS design ([ADR 1033](1033-zenoh-qos-prioritization-reserved-mission-slice.md)),
built first because it is the cheapest and highest-leverage piece — it does not require the heavy
relay-enforcement work to deliver value.

## Context

[ADR 1033](1033-zenoh-qos-prioritization-reserved-mission-slice.md) established three pillars —
Reserve, Reveal, Enforce — and that the cooperative/transparency layer ships first. The Reveal
pillar is load-bearing, not decoration: a producer that can see "amber, and I'm the top
contributor to the bulk class" backs off without being told, and the same signal feeds automated
detection. Today the fabric has **no participant-visible health surface at all** — only
operator-side Prometheus, which producers and consumers cannot see and which does not tell a
producer its own footprint.

Two substrate facts shape the build:

1. **The audit-subscriber cannot be the meter.** It already subscribes `**`, but per
   [ADR 1032](1032-zenoh-volatile-non-persisted-namespace-class.md) it now **excludes**
   `volatile/**` — the highest-volume (bulk/C3) class — so it structurally cannot measure total
   load. And it is a compliance-critical service; adding a publish-back loop widens its blast
   radius. So health metering is a **separate** peer.
2. **No router fork** ([ADR 0858](0858-zenoh-image-upstream-pivot.md)). The meter is a normal
   mTLS client peer that subscribes and publishes — no `zenohd` change.

## Decision

1. **A standalone `goat-fabric-health` meter service**, templated on the audit-subscriber (same
   mTLS session + `**` subscriber + per-producer rate accounting) **minus persistence**. It is a
   live meter, not storage: it keeps only rolling-window counters and writes nothing durable.

2. **It publishes two topics, on-bus, in the control class (C0) so health is never starved:**
   - `ops/fabric/health/<site>` — per-class sample-rate + byte-rate, the **sample count `n`**
     (grounding), the configured capacity target per class, headroom %, a per-class and overall
     **honesty state**, active-producer count, timestamp.
   - `ops/fabric/footprint/<peer>` — that producer's sample-rate + byte-rate, its share of each
     class, its rank among producers, and a near-limit flag. One message per active producer.

   Messages are **protobuf** (`FabricHealthV1`, `ProducerFootprintV1`) so panoscope / the
   schema-viewer can render them and consumers decode without guessing.

2a. **Measurement trust — no red herrings (load-bearing).** A surface this widely trusted must
   never emit a false reading. (i) The color is a **5-state honesty signal** —
   `green` = *measured healthy under real load*, `amber`/`red` = measured pressure, `idle` = too
   little traffic to confirm (UNCONFIRMED, *not* healthy), `unmetered` = load seen but no target;
   a class with no traffic or no target **never** reads green, and an all-quiet fabric reads
   `idle`. (ii) **Grounding travels with every reading** (sample count `n` + window + vantage) so
   the counterfactual is reconstructable. (iii) **Measured load vs assumed target vs inferred
   headroom are kept distinct** (headroom is `—` without a target). (iv) **Dipstick simplicity**
   on top, grounding one level down. (v) **Passive-only by construction** — the meter rides
   existing traffic and never injects; the sole active measurement is a **future idle-probe** that
   fires *only* when a path is quiet and backs off the instant operational traffic returns, so we
   never compete with the mission to measure it.

3. **Class + producer are derived from the observed sample** — the class from the top-level
   prefix (the [ADR 1033](1033-zenoh-qos-prioritization-reserved-mission-slice.md) mapping), the
   producer from the key's identity segment. No producer self-reporting; the meter observes
   ground truth.

4. **Capacity targets (the knee) are operator config**, seeded from the measured ceilings
   ([ADR 1012](1012-load-testing-discipline.md)) — per-class msgs/s and bytes/s. Headroom and
   color are computed against them. The targets are *what we choose to protect*, not a physical
   limit; honest per [ADR 1033](1033-zenoh-qos-prioritization-reserved-mission-slice.md) (the WAN
   segment is measured, not guaranteed).

5. **Consumed by `goat health`** (this surface's first reader): fabric weather for anyone;
   `goat health --me` / `--peer <id>` adds that producer's footprint. Panoscope web rendering is
   a later increment.

6. **This is the Reveal pillar only.** It does not reserve capacity or enforce caps (the Reserve
   pillar — the heavy relay work, [ADR 1033](1033-zenoh-qos-prioritization-reserved-mission-slice.md));
   it makes load and impact *visible*, which is the precondition for both cooperative back-off and
   automated enforcement. The footprint's "near-limit" flag is advisory until caps exist.

## Alternatives considered

- **Extend the audit-subscriber to publish health.** Rejected — it excludes `volatile/**` so it
  cannot meter total load, and it is compliance-critical; a separate meter keeps responsibilities
  and blast radius clean. (Noted as a revisit only if running two `**` subscribers proves costly,
  in which case the *metering* loop merges and the audit *exclude* applies only to the
  persistence branch.)
- **Operator-dashboard-only (Prometheus/Grafana).** Rejected as the surface — the requirement is
  that *participants* see their own impact; an operator view does not make self-governance
  informed. Health rides the bus and includes per-peer footprint. (Prometheus is still emitted for
  the obs stack, as a side channel.)
- **Producers self-report their footprint.** Rejected — self-reporting is forgeable and a
  misbehaving producer is exactly the one that would lie. The meter observes ground truth.
- **JSON payloads.** Rejected in favor of protobuf for schema-viewer/panoscope rendering and
  decode-without-guessing, consistent with the fabric's schema convention.

## Consequences

**Positive.** Every participant can see fabric health and its own footprint in `goat health`,
making the brokerless self-governance informed and self-correcting — the cheapest large lever in
the QoS design, shipped without waiting on relay engineering. Reuses the audit-subscriber as a
code template, the namespace as the class axis, and the measured knee as the capacity target.
Normal peer; no router fork. The footprint stream also feeds the [ADR 1022](1022-zenoh-denial-observability.md)
monitor's future rate-threshold detection.

**Negative / cost.** A second always-on `**` subscriber per site (one more service + image +
cert + deploy). It meters but does not enforce — visibility without teeth until the Reserve pillar
lands (accepted: visibility is the precondition and delivers value alone). Capacity targets are
operator-set and only as good as the measurements behind them.

**Neutral.** Composes with the Data-Access COP (health is the throughput sibling of its access
picture) and with panoscope (the later web rendering home).

## Open items

- `FabricHealthV1` / `ProducerFootprintV1` exact fields + the rolling-window length + publish
  interval defaults.
- Whether footprint is one message per producer or a single fleet rollup the consumer filters.
- Where the meter runs (nova-a first; per-site at scale) and whether it federates cross-site.
- Capacity-target seeding from the load-test measurements.

## Status

**Proposed** pending ratification. First build increment of [ADR 1033](1033-zenoh-qos-prioritization-reserved-mission-slice.md).
