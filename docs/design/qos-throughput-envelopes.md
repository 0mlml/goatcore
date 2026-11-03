# QoS throughput envelopes — per-actor expected-rate bands (declare → reveal → enforce → dynamic)

> **60-second summary:** Fabric health already shows *what each producer's rate is*
> (`ProducerFootprintV1`, [ADR 1034](../adr/1034-fabric-health-surface.md)). It cannot yet say whether
> that rate is **normal for that actor** — there is no per-actor *expectation* to judge a footprint
> against. This doc adds a **throughput envelope** per registered actor — `{min, mean, high, max}` for
> both msgs/s and bytes/s — and sequences its use across four maturity steps: **declare** (a rough-guess
> default attached at registration, editable by the registering party, never gating), **reveal**
> (`goat top` shows each producer as LOW / within-range / HIGH / OVER its envelope — `LOW` (below `min`)
> is a fault signal too, not just `OVER` — built now), **enforce**
> (a producer above `max` is throttled or cut — near-term, sequenced, not built here), and **dynamic**
> (envelopes self-tune to live network bandwidth and actors adapt continuously, not from static
> registration-time values — the long-term north star). It is the per-actor expectation layer of the
> QoS umbrella ([ADR 1033](../adr/1033-zenoh-qos-prioritization-reserved-mission-slice.md)).

| | |
|---|---|
| Status | Proposed — see [ADR 1045](../adr/1045-qos-throughput-envelopes.md). **Declare + reveal** build now; enforce + dynamic sequenced. |
| Anchor ADRs | [ADR 1045](../adr/1045-qos-throughput-envelopes.md) (this) · [ADR 1033](../adr/1033-zenoh-qos-prioritization-reserved-mission-slice.md) (QoS umbrella: reserve/reveal/enforce) · [ADR 1034](../adr/1034-fabric-health-surface.md) (the footprint this judges) · [ADR 1046](../adr/1046-goat-top-live-fabric-dashboard.md) (the reveal surface) |
| Reference designs | [`fabric-health-surface.md`](fabric-health-surface.md) · [`zenoh-qos-and-prioritization.md`](zenoh-qos-and-prioritization.md) · [`goat-top.md`](goat-top.md) · [`data-access-cop.md`](data-access-cop.md) (the portal registry SoT) · [`zenoh-brokerless-consequences.md`](zenoh-brokerless-consequences.md) |

---

## 1. The gap

Fabric health meters every producer's live rate and `goat top` ranks producers by it. But "is `omniwatch`
at 1 msg/s + 2 KB/s **behaving normally**, or is it about to flood?" is unanswerable today, because there
is no per-actor **expectation** on record. Rate without an expected range is a number without a verdict.

The brokerless fabric ([`zenoh-brokerless-consequences.md`](zenoh-brokerless-consequences.md)) has no
central QoS enforcer, so the QoS strategy ([ADR 1033](../adr/1033-zenoh-qos-prioritization-reserved-mission-slice.md))
is **reserve / reveal / enforce**. A per-actor expectation is the missing input to *reveal* (show whether
a producer is within its declared band) and *enforce* (act when it floods past its ceiling).

## 2. Vocabulary (defined before mechanism)

- **Actor** — a registered fabric participant that produces traffic: a portal-registered vendor/org and
  its slot identity. The unit an envelope attaches to.
- **Throughput envelope** — an actor's expected traffic, as four ascending levels on **two axes**:

  | level | meaning |
  |---|---|
  | `min` | floor of *expected* operation — **below it is a fault signal, not benign idle** (see the `LOW` band) |
  | `mean` | typical steady-state |
  | `high` | upper edge of *normal* — sustained operation can reach here |
  | `max` | the ceiling — beyond it is a flood; the enforce trigger |

  Each level is a pair: **msgs/s** and **bytes/s** (data/sec). So an envelope is 8 numbers.

- **Band** (derived, for display + enforcement): given a live rate `r` and an envelope —
  `LOW`/`UNDER` (`r < min`) · `normal` (`min ≤ r ≤ high`) · `HIGH`/elevated (`high < r ≤ max`) ·
  `OVER` (`r > max`). Computed independently for msgs/s and bytes/s; the **worst** of the two governs the
  row's verdict.

  **`LOW` is a fault signal, not benign quiet.** An actor below its expected floor is *under-producing
  what it declared* — which usually means something is wrong: the producer crashed or stalled, lost its
  upstream feed, got disconnected, throttled itself, or is misconfigured. It has many possible causes, so
  `LOW` says "look here," not "this specific thing failed." (An actor that is *legitimately* event-driven
  or bursty should declare `min = 0`, so correct silence never trips `LOW` — the envelope, not the band
  logic, encodes "is quiet normal for me.") `LOW` and `OVER` are the two off-nominal ends; `HIGH` is the
  in-range warning that precedes `OVER`. Enforcement (Step 3) acts only on `OVER` (a flood); `LOW` is a
  health signal to surface and investigate, never something to throttle.

The envelope is a **declared expectation**, not a measured fact — it is honest about that (it can be
wrong/stale; the dynamic step closes that). It is per-actor governance data, distinct from the per-class
QoS reservation (C0..C3, [ADR 1033](../adr/1033-zenoh-qos-prioritization-reserved-mission-slice.md)):
class says *which lane*, envelope says *how much this actor should be doing in it*.

## 3. Step 1 — Declare (build now): rough default, editable, never gating

The registry of record is the portal ([`data-access-cop.md`](data-access-cop.md) — manifests SoT +
roster). The envelope is a **field on each actor's registration record**.

- **Rough-guess default at registration.** Every registration is assigned a default envelope so the data
  always exists. v1 heuristic (deliberately rough — it is a placeholder to be corrected, not a model):
  a per-class baseline keyed off the actor's primary class (a C2 situational telemetry actor defaults
  lower than a C3 bulk actor), with msgs/s and bytes/s scaled together. Where the actor already has live
  footprint history at registration, seed `mean` from the observed rate. The exact numbers are tunable
  and live in config, not code.
- **Editable by the registering party** — at registration or any time after — via the portal (an
  `envelope` block in the registration payload + an edit endpoint). The party who knows their workload
  best owns the numbers.
- **Never gates registration.** A registration with no envelope supplied succeeds and keeps the default.
  This is the ZAGROS-sandbox / near-term posture: unrestricted in terms of *declaring* throughput; the
  declaration is advisory until the enforce step.
- **Served like the alias registry** — a portal API (`GET /api/envelopes`, or folded into the existing
  registration/roster response) that consumers (starting with `goat top`) fetch and cache, degrading
  gracefully to "no envelope" when the portal is unreachable.

**Invariant (mirrors the slot-alias layer, [ADR 1039](../adr/1039-slot-alias-display-layer.md)):** the
envelope is **governance/display metadata** — it does not change the wire, the ACL, the cert, or the
namespace. Declaring it is not authorization to send that much; it is a statement of intent.

## 4. Step 2 — Reveal in `goat top` (build now)

`goat top` ([ADR 1046](../adr/1046-goat-top-live-fabric-dashboard.md)) already shows each producer's live
msgs/s + bytes/s. It additionally fetches the envelope registry (same pattern as the alias registry) and,
per producer row, renders the **band**:

- `LOW` — `< min`: cyan/amber, "below expected floor — possibly stalled, disconnected, or upstream-starved;
  investigate." (Suppressed for `min = 0` actors, which declare that quiet is normal for them.)
- `normal` — within `[min, high]`: unstyled / green.
- `HIGH` — in `(high, max]`: amber, "approaching ceiling."
- `OVER` — `> max`: red, "flooding past its declared max" (this is the visual precursor to the enforce
  step — you *see* the flood before the fabric *acts* on it).
- no envelope known (portal down, or actor predating the rollout) → `—`, honest, never a faked verdict.

Honesty discipline is inherited wholesale from the env-health surface: a band is only shown when both the
live rate **and** the envelope are grounded; otherwise `—`. The envelope numbers themselves are
viewable (so an operator can see *why* a row is HIGH/OVER, and that the envelope may simply be set too
low — a cue to edit it). Display-only; the wire/ACL are untouched.

## 5. Step 3 — Enforce on flood (near-term — SEQUENCED, not built here)

When a producer sustains `> max`, the fabric should **throttle or cut** it rather than let it flood. This
is the *enforce* pillar of [ADR 1033](../adr/1033-zenoh-qos-prioritization-reserved-mission-slice.md) and
is **explicitly out of scope for the declare+reveal build**; it is sequenced as the next work item.

Rough shape (to be designed in its own pass): enforcement lives **where we control the path**, keyed on
**authenticated peer identity** (never a forgeable in-band marker) —
- the **relay** rate-limits/sheds an identity that exceeds its `max` (consistent with ADR 1033's
  identity-keyed reservation);
- **owned-host egress** (`tc`) caps a local producer;
- escalation: sustained gross overage → mesh-evict (the `goat admin peer cut` emergency path already
  exists).
It must be hysteretic (act on *sustained* overage, not a single spike), observable (the action is itself
revealed on the bus, like everything else), and reversible. **Build gate:** only after declare+reveal are
live and the envelopes have been corrected away from the rough defaults — enforcing against a bad default
would cut legitimate traffic. Sequenced; not now.

## 6. Step 4 — Dynamic, network-aware QoS (long-term north star — ROUGH PLAN)

The end state: envelopes are **not static values posted once at registration** but **continuously tuned**,
and actors **adapt their own behavior** to hit them against **live network conditions**. This is the
"final north star for dynamic and universal QoS management." It is genuinely harder than the steps above
and is sketched here as the destination, not specified.

The progression and the hard parts:

1. **Static declared (Steps 1–3).** Envelopes are fixed numbers; enforcement is against the declared
   ceiling. Good enough to reveal + stop floods.
2. **Observed-baseline tuning.** The fabric-health meter already has every actor's rate history; a
   passive roll-up can *suggest* envelope corrections (drift the default toward observed behavior),
   surfaced for the party to accept. Still actor-local; no network context yet.
3. **Network-aware budgets — the context-building problem.** The real target is envelopes expressed as a
   *share of available bandwidth on the actor's current network*, not absolute numbers — because the same
   actor on a fat backbone link vs a constrained tactical-edge link should behave very differently. This
   requires the fabric (and the actor) to know **which network an actor is currently on** and **that
   network's live headroom**. The raw material is starting to exist — mesh-link health
   ([ADR 1042](../adr/1042-mesh-link-health-domain.md)) knows direct-vs-relayed + latency per link, and
   the fabric-health surface knows aggregate load — but composing them into a per-actor *network context*
   ("you are on the relayed region-B path which is at 80% — your effective max just dropped") is the nuanced,
   sophisticated work. This is where "universal" lives: every actor, on whatever network, gets a budget
   appropriate to *that* network's current state.
4. **Closed-loop adaptation.** Actors (or their sending/receiving libraries) consume their live
   network-aware budget off the bus and **change their own send behavior** to fit it — downshift sample
   rates, switch to the `volatile`/drop class, batch — instead of being externally throttled. Enforcement
   becomes the backstop for misbehavers, not the primary mechanism. This is the universal dynamic QoS end
   state: the fabric continuously negotiates each actor's behavior against real, per-network conditions.

The path is deliberately incremental: each step is useful alone, and none requires the next to ship. The
context-building (step 3) is the research-grade piece and will get its own design when we reach it.

## 7. How it composes

- **QoS umbrella ([ADR 1033](../adr/1033-zenoh-qos-prioritization-reserved-mission-slice.md)):** the
  envelope is the per-actor input to *reveal* (now) and *enforce* (next); class reservation is orthogonal
  (lane vs amount).
- **Fabric health ([ADR 1034](../adr/1034-fabric-health-surface.md)):** supplies the live per-producer
  rate the band is computed against; its history is the raw material for observed-baseline tuning (Step
  4.2).
- **Portal registry ([`data-access-cop.md`](data-access-cop.md)):** the SoT for the declared envelope,
  exactly as it is for aliases and manifests.
- **`goat top` ([ADR 1046](../adr/1046-goat-top-live-fabric-dashboard.md)):** the first reveal surface;
  panoscope/`goat health` can adopt the band later.
- **Brokerless safety ([`zenoh-brokerless-consequences.md`](zenoh-brokerless-consequences.md)):** declare
  + reveal add no central component; enforce stays identity-keyed at points we already control (relay /
  owned host), re-introducing no broker.

## 8. Build map (honest)

| Step | Scope | Status |
|---|---|---|
| 1 Declare | portal: envelope field + rough default at registration + edit endpoint + `GET` API | **built** (Go; `go test` green) |
| 2 Reveal | `goat top`: fetch envelope registry + per-row band (LOW/normal/HIGH/OVER, honest `—`) + incremental producer filter | **built** (Rust; extends ADR 1046; band + filter unit-tested) |
| 3 Enforce | relay / owned-host throttle + mesh-evict on sustained `> max`, identity-keyed | **sequenced — its own design pass, not built here** |
| 4 Dynamic | observed-baseline tuning → network-aware budgets (context-building) → closed-loop adaptation | **north star — rough plan only (§6)** |
