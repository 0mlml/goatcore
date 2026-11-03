# Fabric health surface — `goat-fabric-health` + `goat health`

> **60-second summary:** A small always-on meter (`goat-fabric-health`) subscribes to all fabric
> traffic, rolls it up by **traffic class** and by **producer**, compares against operator-set
> per-class capacity targets (the measured knee), and publishes two protobuf topics back onto the
> bus: `ops/fabric/health/<site>` (fabric weather — per-class load vs target + a 5-state honesty
> color that never false-greens: green/amber/red only when grounded, else idle/unmetered) and
> `ops/fabric/footprint/<peer>` (each producer's own rate, class shares, rank, near-limit flag).
> Any producer or consumer reads it with `goat health`, so network health and one's own impact are
> visible to everyone — the precondition for informed self-governance on a brokerless fabric. This
> is the **Reveal** pillar of the QoS design ([ADR 1033](../adr/1033-zenoh-qos-prioritization-reserved-mission-slice.md)),
> built first because it delivers value without the heavy relay-enforcement work. It meters; it
> does not yet enforce.

| | |
|---|---|
| Status | Proposed — see [ADR 1034](../adr/1034-fabric-health-surface.md). Build increments in §7. |
| Generalized by | [`environment-health-surface.md`](environment-health-surface.md) ([ADR 1036](../adr/1036-environment-health-surface.md)) — this is the **fabric domain** of a cross-domain env-health contract (network + time + future). The honesty model + `ops/<domain>/health/<scope>` convention here are the pattern that generalizes; `FabricHealthV1` folds into the shared `EnvHealthV1` envelope (as `FabricDetailV1`) at the coordinated `V2` bump noted in §9/§11. The panoscope web view deferred in §6 lands there, once, for all domains. |
| Anchor ADRs | [ADR 1034](../adr/1034-fabric-health-surface.md) (this component) · [ADR 1033](../adr/1033-zenoh-qos-prioritization-reserved-mission-slice.md) (QoS umbrella — Reveal pillar) · [ADR 1022](../adr/1022-zenoh-denial-observability.md) (sibling monitor) · [ADR 1032](../adr/1032-zenoh-volatile-non-persisted-namespace-class.md) (why not reuse the audit-subscriber) · [ADR 0858](../adr/0858-zenoh-image-upstream-pivot.md) (normal peer, no fork) · [ADR 1012](../adr/1012-load-testing-discipline.md) (capacity targets) |
| Reference designs | [`zenoh-qos-and-prioritization.md`](zenoh-qos-and-prioritization.md) (the class model + the three pillars) · [`zenoh-audit-external-subscriber.md`](zenoh-audit-external-subscriber.md) (the code template) · [`zenoh-loadtest.md`](zenoh-loadtest.md) (the knee) · [`data-access-cop.md`](data-access-cop.md) (the web rendering home) |

---

## 1. Why this exists

Brokerless self-governance only works if participants can *see* — fabric health and their own
contribution to it ([`zenoh-qos-and-prioritization.md`](zenoh-qos-and-prioritization.md) Pillar 2).
Today there is no participant-visible surface; only operator-side Prometheus, which a producer
cannot read and which never tells a producer its own footprint. This builds the surface, on the
bus, readable by anyone.

It is the **first** thing built off the QoS umbrella because it is cheap (one meter peer + a CLI
reader), high-leverage (transparency makes most misbehavior self-correct), and independent of the
heavy relay-reservation work.

## 1a. Measurement trust — no red herrings (the non-negotiables)

This surface will be **widely trusted and acted on**, so a measurement that *looks* meaningful
but isn't is worse than no measurement — a false "green" gets believed, and someone relies on
capacity that isn't there. Five rules are load-bearing; none is optional:

1. **Honest "unknown" beats false "green."** The worst red herring is a confident green with
   nothing behind it. The state set is therefore five values, not three:
   `green` = *measured healthy under real load*, `amber`/`red` = measured pressure, **`idle`** =
   too little traffic to confirm anything (UNCONFIRMED — *not* "healthy"), **`unmetered`** = load
   seen but no capacity reference. A class with no traffic, or a class with no target, **never**
   reads green. When the whole fabric is quiet, the dipstick reads `IDLE`, not `GREEN`.

2. **Transparent grounding travels with every reading.** Each reading carries its basis —
   **sample count `n`**, the window, the site/vantage — so a reader can reconstruct the
   counterfactual ("amber means load is ~80% of the *configured* target; the target is a
   load-test *estimate*, not a live ceiling"). A reading you can't ground is reported as `idle`,
   not asserted.

3. **Measured vs inferred vs assumed are not equal.** Live **load** is *measured*; the **capacity
   target** is *assumed* (periodically load-tested, fed in as config); **headroom** is *inferred*
   from the two and is only as good as the assumption. The surface keeps these distinct (headroom
   is `—` when there is no target; the target is always shown next to the load so the comparison
   is visible, never hidden inside a single number).

4. **Dipstick simplicity on top, grounding one level down.** One glance, hard to misread: a single
   overall state (`GREEN`/`AMBER`/`RED`/`IDLE`) with its `n` + window on the same line. The rich
   per-class / per-producer detail is drill-down, not the headline.

5. **Ride operational traffic; probe only when quiet.** Health is derived **passively** from the
   traffic already flowing — zero added load on the mission (this is the meter's whole design: it
   subscribes and counts, it never injects). The only time we generate *notional* traffic is the
   **idle-probe** (§9, roadmap): when a path is quiet, a tiny synthetic probe confirms a
   `idle` path's health (latency/throughput), and **backs off the instant real traffic
   reappears** — the idle capacity is the entire probe budget. We never compete with operational
   traffic to measure it.

These rules govern not just v1 but every layer added later — a measurement that cannot meet them
is reported as `idle`/`unknown`, never dressed up as green.

## 2. Why a separate meter, not the audit-subscriber

The audit-subscriber already subscribes `**` — but it now **excludes `volatile/**`**
([ADR 1032](../adr/1032-zenoh-volatile-non-persisted-namespace-class.md)), the highest-volume
class, so it structurally cannot measure total load. It is also compliance-critical; adding a
publish-back loop would widen its blast radius. So the meter is a **separate** normal mTLS peer,
templated on the audit-subscriber's session + subscriber loop + per-producer accounting, **minus
persistence** — it keeps only rolling counters and writes nothing durable.

## 3. What it observes and computes

For every sample on `**`:

- **Class** ← the top-level prefix, via the [ADR 1033](../adr/1033-zenoh-qos-prioritization-reserved-mission-slice.md)
  mapping: `cmd/`/`task/`/`ack/`/`ops/fabric/health` → **C0 control**; `data/`/`release/` → **C1
  mission**; `agents/`/`telemetry/`/`health/` → **C2 situational**; `volatile/**` + bulk → **C3
  bulk**. Unmapped prefixes → C2 by default.
- **Producer** ← the key's identity segment (`<peer>/…`, or `volatile/<peer>/…` for the volatile
  class — the second segment there).
- Accumulate sample-count + byte-count into per-`(class, producer)` counters that reset each
  publish — so the **window equals the publish interval** (default 5 s).

On a timer (default every 5 s), it computes and publishes:

- **Per class:** sample-rate, byte-rate, the **sample count `n`** behind the window (grounding,
  §1a), the configured **capacity target** (msgs/s + bytes/s), **headroom %** = `1 − load/target`,
  and the 5-state honesty **color** (§1a): `green`/`amber`/`red` only when *grounded* (rate ≥ an
  idle floor **and** a target is set; green < 70 %, amber 70–90 %, red > 90 % of target); `idle`
  when the class carries too little traffic to confirm (`headroom` reported as unknown, not a
  false 100 %); `unmetered` when load is seen but no target is set. The **overall** dipstick is
  the worst *grounded* pressure, or `idle` when no class is grounded — never a false green.
- **Per producer:** its sample-rate + byte-rate, its share of each class it publishes to, its
  **rank** among producers (by byte-rate), and a **near-limit** flag (advisory until caps exist —
  set when the producer's class is amber/red and it is a top-N contributor).

Class + producer are **observed, never self-reported** — the meter sees ground truth, so a
misbehaving producer cannot hide or misclassify its footprint.

## 4. The topics (protobuf)

`ops/fabric/health/<site>` carries `FabricHealthV1`; `ops/fabric/footprint/<peer>` carries
`ProducerFootprintV1`. Both are **C0** (published `Block` + high priority) so the health signal
is never starved by the load it reports on. Schema (canonical in
`ops/zenoh-trial-peers/fabric-health/proto/fabric_health_v1.proto`):

```protobuf
message ClassHealthV1 {
  string class = 1;            // "C0".."C3"
  double sample_rate = 2;      // msgs/s, rolling-window
  double byte_rate = 3;        // bytes/s
  double target_sample_rate = 4;
  double target_byte_rate = 5;
  double headroom_pct = 6;     // 0..1 when grounded; -1 = unknown (idle/unmetered)
  string color = 7;            // green | amber | red | idle | unmetered  (§1a honesty states)
  uint64 sample_count = 8;     // grounding: samples behind this window (n)
}
message FabricHealthV1 {
  string site = 1;
  int64  ts_unix_ms = 2;
  repeated ClassHealthV1 classes = 3;
  string overall_color = 4;
  uint32 active_producers = 5;
  uint32 window_secs = 6;
}
message ClassShareV1 { string class = 1; double sample_rate = 2; double byte_rate = 3; }
message ProducerFootprintV1 {
  string peer = 1;
  int64  ts_unix_ms = 2;
  double sample_rate = 3;
  double byte_rate = 4;
  repeated ClassShareV1 shares = 5;
  uint32 rank = 6;             // 1 = largest by byte-rate
  bool   near_limit = 7;       // advisory until caps exist
}
```

## 5. Capacity targets (the knee) — operator config

Per-class targets (msgs/s + bytes/s) are operator config, seeded from the measured ceilings
([ADR 1012](../adr/1012-load-testing-discipline.md) — M-052/M-053 + the pending ceiling sweep).
They are *what we choose to protect*, not a physical limit, and the WAN segment is measured not
guaranteed ([ADR 1033](../adr/1033-zenoh-qos-prioritization-reserved-mission-slice.md)). They live
in the ansible role defaults / group_vars so a site can tune them.

## 6. The consumer — `goat health`

The first reader, in `goat-cli`:

- `goat health` — subscribe `ops/fabric/health/**`; render fabric weather: a per-class bar (load
  vs target) + color, overall color, active-producer count. One screen, glanceable.
- `goat health --watch` — live-refresh.
- `goat health --me` / `--peer <id>` — also subscribe `ops/fabric/footprint/<peer>` and show that
  producer's footprint (rate, class shares, rank, near-limit) — "you are #2 in C3 at 18 %; back
  off." `--me` resolves the caller's own peer id from its profile.

Panoscope web rendering (the throughput sibling of the Data-Access COP picture,
[`data-access-cop.md`](data-access-cop.md)) is a later increment.

## 7. Build increments

1. **Meter + schema + design — DONE.** `goat-fabric-health` service (Rust, on the
   audit-subscriber template) + the `.proto` + this doc + [ADR 1034](../adr/1034-fabric-health-surface.md).
   Compiles; publishes the two topics from a live `**` subscription.
2. **Consumer — DONE.** `goat health` subcommand in `goat-cli` — fabric weather (per-class load
   vs target + green/amber/red) one-shot or `--watch`; `--me` / `--peer <id>` adds that producer's
   footprint. Decodes the protobuf with prost-derive structs matching the `.proto`; TTY-colored,
   pipe-safe.
3. **Build + deploy path — DONE.** `Dockerfile` (two-stage Chainguard) + `fabric-health-image.yml`
   (per-arch amd64/arm64 → `ghcr.io/<org>/goat-fabric-health`); the **`fabric-health` ansible
   role** (defaults + pre-flight cert checks + compose template + image pin `sha-d4ebcc3@sha256:7c7eb823…`
   + `deploy-history.yml`), the `fabric-health-deploy.yml` playbook, and nova-a group_vars
   (seed capacity targets — replace with measured ceilings). Persists nothing (no S3/MinIO/.env).
   The operator runs the playbook; this is turnkey up to that.
4. **Operator deploy + live-fire (gated).** Deploy on nova-a; confirm `goat health` shows weather,
   a synth-vendor firehose moves the color toward amber/red and shows up as that producer's
   footprint, and the topic renders in panoscope → `M-NNN`.
5. **Idle-probe (roadmap — the only active measurement).** When a path reads `idle` (no
   operational traffic to ride), emit a tiny notional probe to *confirm* its health
   (round-trip latency, and a short sized burst for throughput/underlay), gated strictly on
   idle and backing off the instant real traffic reappears (§1a rule 5). Rides the existing
   load-test apparatus ([`zenoh-loadtest.md`](zenoh-loadtest.md), the `loadtest/**` namespace,
   [ADR 1012](../adr/1012-load-testing-discipline.md)) in a continuous **dip-stick** mode rather
   than a saturating benchmark. Upgrades a quiet path's reading from `idle` (unconfirmed) to a
   grounded, probe-sourced result — never run when the network is busy.
6. **Later.** Panoscope web view; feed the footprint stream into the [ADR 1022](../adr/1022-zenoh-denial-observability.md)
   monitor's rate-threshold detection; fold underlay (Layer B, §9) into the surface (a
   `FabricHealthV1` → `V2` schema bump); per-site federation at scale.

## 8. What this is not

- **Not enforcement.** It meters and reveals; it does not reserve capacity or cap producers (the
  Reserve pillar — the heavy relay work, [ADR 1033](../adr/1033-zenoh-qos-prioritization-reserved-mission-slice.md)).
  The near-limit flag is advisory until caps exist.
- **Not persistence.** It writes nothing durable — a live meter, rolling counters only.
- **Not a producer's self-report.** Ground-truth observation only.

## 9. Measurement layers — what v1 measures, and what it does NOT

Fabric health spans three layers measured by three different mechanisms. **v1 of this surface
covers only Layer A.** Naming the boundary keeps the surface honest and the roadmap clear.

| Layer | What | Measured by | In `goat-fabric-health` v1? |
|---|---|---|---|
| **A — data-fabric offered load** | msg/s + bytes/s per class + per producer, vs a capacity target | **this meter** (one subscriber vantage, rolling window) | ✅ |
| **B — underlay** | host/relay CPU·mem·NIC·disk; WireGuard peer health (handshake age, last-seen, rx/tx); WAN/Starlink RTT·loss·throughput | the observability stack (node-exporter → OpenObserve/Prometheus, [ADR 0815](../adr/0815-observability-stack.md)) + WireGuard/netbird stats + [`snitch.md`](snitch.md); WAN is **not continuously metered today — a gap** | ❌ (separate domain) |
| **C — capacity (the knee)** | the route-only / rocksdb / MinIO / relay throughput ceilings | the **load-test apparatus** ([`zenoh-loadtest.md`](zenoh-loadtest.md), [ADR 1012](../adr/1012-load-testing-discipline.md)), **periodic not live** | ❌ (seeds the Layer-A targets as config) |

Honest consequences: Layer A measures *offered load as one subscriber sees it* — **not** pub/sub
delivery success / loss / latency, and at true scale it under-counts pure peer-to-peer flows that
never reach the meter. **Capacity is not live-measured** — it is a periodically load-tested ceiling
fed in as config, against which live load is compared. The Layer-B underlay signals exist but in
the obs stack, **not on the bus** — folding them in (so `goat health` shows one picture) is a
named next increment (a `FabricHealthV1` → `V2` schema bump + a `goat-cli` + panoscope update).
*(Note: the [ADR 1033](../adr/1033-zenoh-qos-prioritization-reserved-mission-slice.md) umbrella
describes `ops/fabric/health` eventually carrying relay-utilization + WAN-envelope; those are
roadmap fields, not v1 — `FabricHealthV1` is Layer-A-only by design here.)*

## 10. Visualize, diagnose, react

- **Visualize:** `goat health` (terminal, Layer A — built); panoscope web (Layer A, the throughput
  sibling of the Data-Access COP [`data-access-cop.md`](data-access-cop.md) — later); Grafana/
  OpenObserve (Layer B — exists); load-test HTML reports (Layer C).
- **Diagnose:** the expected-vs-actual producer monitor ([ADR 1022](../adr/1022-zenoh-denial-observability.md))
  answers "who went silent / who is over-rate"; stethoscope / flashlight / `goat inspect --tui`
  drill down. Cross-correlating A + B + producer behaviour is **manual across surfaces today**; a
  unified health COP is the future integration.
- **React:** (a) cooperative — producers read `goat health` and back off (advisory); (b) detect→cut
  — the monitor flags over-rate → mesh-evict ([ADR 1025](../adr/1025-zenoh-dynamic-tenant-authz-slot-pool.md)/[ADR 1031](../adr/1031-credential-revocation-mesh-first-offboarding.md));
  (c) **enforce** — the relay reservation + per-peer caps (the Reserve pillar) is the only
  *automatic* teeth and is **not built**; (d) capacity — sustained red is a capacity-planning
  signal (add relay capacity / admission-gate, [ADR 1012](../adr/1012-load-testing-discipline.md)).

## 11. How `goat-cli` grows with this

`goat health` v1 is the seed and is intentionally minimal. Each layer added is a coordinated
update across the meter's schema, `goat-cli`, and panoscope: folding underlay (Layer B) in is a
`FabricHealthV1` → `V2` bump + a `goat health` render update; a health lens in the existing TUI
picker (`goat ui` / `inspect --tui`) is a natural add; diagnose/react ties into stethoscope + the
monitor. The protobuf is versioned (`…V1`) precisely so the consumer can evolve without breaking
older readers.
