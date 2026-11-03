# ADR 1036 — Environment health surface: generalize fabric health into a cross-domain on-bus viewing contract; time as the second domain

**Status:** Proposed (2026-06-09)
**Repo scope:** goat core (`EnvHealthV1` contract, `goat-time-health` roll-up, `ntp-health-agent` Zenoh publisher, `goat health` consumer, panoscope + stethoscope renderers)
**Related:** [ADR 1034](1034-fabric-health-surface.md) (the fabric health surface this generalizes — its first domain) · [ADR 1033](1033-zenoh-qos-prioritization-reserved-mission-slice.md) (the QoS umbrella; this is its Reveal pillar, widened past fabric) · [ADR 0844](0844-zenoh-deployment-topology-and-acl-v1.md) / [ADR 1035](1035-operator-namespace-ops-canonical.md) (the canonical `ops/` namespace the topics live in) · [`environment-health-surface.md`](../design/environment-health-surface.md) (the design) · [`universal-ntp-hygiene.md`](../design/universal-ntp-hygiene.md) (the time-domain policy + the `ops/time/attest` feed) · F-184 (the time-on-bus gap this closes)

## Plain-language summary

"Is the environment we're operating in healthy?" has more than one domain — **network/fabric** pressure
and **time/clock** discipline today, GNSS/power/thermal later. We already built a good answer for fabric:
a passive meter publishes an honesty-graded weather reading on the bus, read by `goat health`. We did
**not** build one for time — its bus publisher was designed and resolved (2026-04-20) but never wired
(F-184), so today you cannot see network
and time health together anywhere. This decision generalizes the fabric pattern into **one cross-domain
contract** — a single honesty-graded envelope (`EnvHealthV1`), the canonical `ops/<domain>/health/<scope>`
topic convention, and **one renderer per altitude** (`goat health`, a panoscope environment band, a
stethoscope environment target) — and makes **time** its second domain by completing the planned
`ops/time/attest` publisher and adding a viewing roll-up. New domains plug in without new surfaces. It
reveals; it does not enforce.

## Context

[ADR 1034](1034-fabric-health-surface.md) built the fabric health surface: a `goat-fabric-health` meter
rolls `**` up by class + producer and publishes `ops/fabric/health/<site>` with a 5-state honesty color
(green/amber/red/idle/unmetered + sample-count `n`), read by `goat health`. It is the Reveal pillar of
[ADR 1033](1033-zenoh-qos-prioritization-reserved-mission-slice.md), and the pattern — *passive meter →
honesty-graded reading on the bus → glanceable consumer* — is exactly right. But it is **single-domain**
(fabric only) and effectively **CLI-only** (panoscope/stethoscope rendering of even fabric health is
designed-not-built).

The operator need is broader: view **collective environmental health** — network *and* time, later more —
at multiple altitudes. Three substrate facts shape the decision:

1. **Time health was meant to be on the bus and isn't.** [`universal-ntp-hygiene.md`](../design/universal-ntp-hygiene.md)
   §9 resolved (2026-04-20) that `ntp-health-agent` publishes its signed verdict on `ops/time/attest/{host_id}`.
   The publisher was a Phase-3 ~0.5-week item and was never built; only the Unix socket (enforcement) and
   OTLP/HTTP (durable obs) shipped (F-184).
   So time health exists, signed and verdict-graded, but never reaches the bus where it could be co-viewed.
2. **The honesty model and the `ops/` namespace already generalize.** The 5-state never-false-green model
   is domain-agnostic, and `ops/fabric/health/<site>` is domain-first under the canonical `ops/`
   operator namespace ([ADR 0844](0844-zenoh-deployment-topology-and-acl-v1.md) / [ADR 1035](1035-operator-namespace-ops-canonical.md))
   — `ops/<domain>/health/<scope>` is the obvious extension.
3. **Three viewing surfaces already exist.** `goat health`, panoscope, stethoscope — the cost is wiring a
   shared envelope into each, once, against the existing deep-link contract ([`zenoh-surface-deep-links.md`](../design/zenoh-surface-deep-links.md)).

## Decision

1. **One cross-domain contract.** Health for any environmental domain is published on
   `ops/<domain>/health/<scope>` (C0 control, domain-first under `ops/`) carrying a single protobuf
   envelope **`EnvHealthV1`** — a shared honesty header (`domain`, `scope`, `ts`, `overall_color`,
   `sample_count n`, `window_secs`) plus a `oneof detail` for domain-specific structure. The 5-state
   honesty model (green/amber/red only when grounded, else idle/unmetered; `n` on every reading;
   `overall_color` = worst grounded, never a false green) is inherited wholesale from
   [ADR 1034](1034-fabric-health-surface.md).

2. **Fabric is the first domain.** Its existing rows (`ClassHealthV1`) become `FabricDetailV1` inside the
   envelope. Fabric keeps shipping its current `FabricHealthV1` until a coordinated `V2` fold-in (the
   schema bump [`fabric-health-surface.md`](../design/fabric-health-surface.md) already anticipated for
   Layer-B); consumers decode both in the interim. `ops/fabric/footprint/<peer>` is fabric-specific and
   unchanged.

3. **Time is the second domain — built by closing F-184.** (a) Complete the planned `ops/time/attest/{host_id}`
   Zenoh publisher in `ntp-health-agent`. (b) Add a passive `goat-time-health` roll-up reader that
   subscribes `ops/time/attest/**`, rolls per-host, and publishes `ops/time/health/<site>` as
   `EnvHealthV1{domain:"time"}` (one `HostClockHealthV1` per host). The daemon verdict
   (`healthy/warning/degraded/unknown`) maps onto the honesty model (healthy→green, warning→amber,
   degraded→red, unknown/unattested→idle, no-attestation→unmetered); a **stale** attestation downgrades
   that host to `idle` — never a stale green.

4. **Enforcement and viewing planes stay separate.** `ops/time/attest/**` remains the signed, enforcement-grade
   per-host feed (admit-gating, cross-site consensus, router interceptor — unchanged); the roll-up is a
   **non-enforcing** view. OTLP → OpenObserve stays time's durable record; the audit-subscriber stays
   fabric's. This mirrors [ADR 1034](1034-fabric-health-surface.md) §2 (meter ≠ audit-subscriber).

5. **One renderer per altitude.** `goat health` (multi-domain board + `--domain` drill), a panoscope
   **environment band** (per-domain per-site chips; optional node-tinting by clock health), and a
   stethoscope **environment target** (`?domain=…` — the "why is it red", reusing the Check-plugin
   substrate). They link via the Block 94 deep-link contract, extended with one `domain=` param.

6. **Component shape.** `goat-time-health` ships as a sibling meter templated on `goat-fabric-health`
   (same precedent fabric used off the audit-subscriber); consolidation into a single multi-domain
   `goat-env-health` meter is a named later increment, not a precondition.

7. **Reveal only.** No central health authority appears — each node self-publishes, a passive reader
   rolls up a view, nothing gates. Re-introducing a central aggregator would re-introduce the broker the
   fabric rejected ([`zenoh-brokerless-consequences.md`](../design/zenoh-brokerless-consequences.md)).

## Alternatives considered

- **Leave fabric and time on separate planes; just add a time dashboard.** Rejected — the operator need
  is *co-viewing* network and time health at one glance; two unrelated surfaces don't meet it, and it
  forgoes the cheap generalization the honesty model + `ops/` namespace already enable.
- **Keep `FabricHealthV1` and add a parallel `TimeHealthV1`, no shared envelope.** Rejected as the end
  state (each renderer would carry per-domain display logic and new domains would each need bespoke
  rendering), but adopted as the *interim* (fabric keeps `FabricHealthV1` until the coordinated `V2`
  fold-in) to avoid churning shipped fabric code inside this track.
- **A new `ops/time/health` publisher independent of `ops/time/attest`.** Rejected — the signed per-host
  attestation feed is already designed (and partly the point of the time-hygiene work); inventing a
  second time-health source would duplicate it and fork the verdict. The roll-up consumes `ops/time/attest/**`.
- **A central env-health aggregator that computes and enforces fleet health.** Rejected — re-introduces a
  broker/chokepoint the brokerless fabric deliberately rejected, and conflates viewing with enforcement.
- **Render only at the CLI (defer panoscope/stethoscope again).** Rejected — "view at multiple levels"
  is the actual request; building the renderers once against the shared envelope is the leverage, and it
  also delivers the panoscope fabric view ADR 1034 deferred.

## Consequences

**Positive.** Network and time health become co-viewable at three altitudes from one contract; the
fabric pattern is reused, not reinvented; time health finally reaches the bus (closing F-184) with a
faithful honesty mapping; new domains (GNSS/power/thermal) plug in with a `oneof` arm + a renderer arm,
no new surfaces; the panoscope health view ADR 1034 deferred lands once for all domains; completing the
`ops/time/attest` publisher independently unblocks cross-site consensus + the router interceptor.

**Negative / cost.** A new always-on roll-up reader per site (`goat-time-health`: one more service +
image + cert + deploy) plus the Zenoh publisher work in `ntp-health-agent`. An interim with two protobuf
shapes on the wire (`FabricHealthV1` + `EnvHealthV1`) until the `V2` fold-in (one extra consumer decoder).
The viewing roll-up reveals but does not enforce — visibility without teeth (accepted: enforcement is the
separate per-domain plane).

**Neutral.** Composes with the QoS umbrella (Reveal, widened), the Data-Access COP (the access sibling of
this condition picture), and the deep-link contract (one new param).

## Open items

- `EnvHealthV1` / `HostClockHealthV1` exact fields + the time roll-up window + publish-interval defaults.
- Where `goat-time-health` runs (nova-a first; per-site at scale) and whether it federates cross-site.
- The `ops/time/attest` heartbeat interval + attestation-staleness threshold that downgrades a host to `idle`.
- Whether to consolidate `goat-fabric-health` + `goat-time-health` into `goat-env-health` at the `V2`
  fold-in or keep siblings.
- `domain=` param precedence in the deep-link contract relative to `key=`/`vendor=`/`peer=`.

## Status

**Proposed** pending ratification. Generalizes [ADR 1034](1034-fabric-health-surface.md); first build
increment closes F-184.
