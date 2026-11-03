# ADR 1045 — QoS throughput envelopes (per-actor expected-rate bands)

**Status:** Declare + reveal **Accepted — deployed + live-fired on ZAGROS 2026-06-15 (M-071):** the portal (`sha-028dc95`) serves `GET /api/envelopes` + `PUT /api/vendors/{anchor}/envelope`, and `goat top` 0.4.6 rendered the per-producer BAND honestly on real ZAGROS producers — a declared actor over its ceilings read `OVER`, one under its floors read `LOW`, one in-range read `OK`, and the ~17 undeclared producers an honest `—`; the `--filter` narrowed with an `N of M shown` count. Display-only (no wire/ACL/cert/storage change). Enforce + dynamic (§5–6) sequenced, not built.
**Design:** [`docs/design/qos-throughput-envelopes.md`](../design/qos-throughput-envelopes.md)
**Related:** [ADR 1033](1033-zenoh-qos-prioritization-reserved-mission-slice.md) (QoS umbrella) · [ADR 1034](1034-fabric-health-surface.md) (footprint) · [ADR 1046](1046-goat-top-live-fabric-dashboard.md) (reveal surface) · [ADR 1039](1039-slot-alias-display-layer.md) (display-only precedent)

## Context

Fabric health publishes each producer's live rate (`ProducerFootprintV1`), but there is no per-actor
**expectation** to judge it against — so "is this producer behaving normally, or flooding?" is
unanswerable. The brokerless fabric has no central QoS enforcer; the QoS strategy (ADR 1033) is
reserve / reveal / enforce, and a per-actor expected-rate band is the missing input to *reveal* and
*enforce*.

## Decision

Introduce a per-actor **throughput envelope** — `{min, mean, high, max}` for both msgs/s and bytes/s —
and adopt it across four maturity steps:

1. **Declare (build now).** The envelope is a field on the portal registration record. Every
   registration gets a **rough-guess default** (per-class baseline, config-driven); the registering
   party may **edit** it at registration or later. It **never gates** registration (advisory until
   enforce). Served via a portal API and consumed like the alias registry. Governance/display metadata
   only — it does not touch the wire, ACL, cert, or namespace (mirrors ADR 1039's display-only
   invariant).
2. **Reveal (build now).** `goat top` fetches the envelope registry and renders each producer's **band**
   — `LOW` (`< min` — a fault signal: under-producing what it declared, i.e. possibly stalled /
   disconnected / starved / misconfigured; suppressed for `min = 0` actors) / `normal` / `HIGH`
   (`> high`) / `OVER` (`> max`) — per the worst of msgs/s and bytes/s, honest `—` when no envelope or
   rate is grounded. Both off-nominal ends are surfaced, not just floods.
3. **Enforce (sequenced — separate pass).** A producer sustaining `> max` is throttled/cut where we
   control the path, keyed on authenticated identity (relay shed / owned-host `tc` / mesh-evict).
   Hysteretic, observable, reversible. **Gated** on declare+reveal being live and envelopes corrected
   off the rough defaults (enforcing a bad default would cut legitimate traffic). Not built here.
4. **Dynamic (north star — rough plan only).** Envelopes self-tune (observed baseline → network-aware
   budgets → closed-loop actor adaptation); actors adjust behavior to a live, per-network budget rather
   than static registration-time numbers. The hard part is context-building (knowing which network an
   actor is on + its headroom). Sketched in the design §6; not specified.

## Alternatives considered

- **Gate registration on a complete envelope.** Rejected — raises the onboarding bar for no near-term
  benefit (the value is in reveal/enforce, which tolerate a default); the ZAGROS-sandbox posture is
  unrestricted declaration.
- **Bake the envelope into the cert/ACL/namespace.** Rejected — it is mutable governance intent, not
  identity or authorization; binding it to the cert would force a re-mint to edit a number. Display/
  governance metadata, like the slot alias.
- **Enforce immediately (skip reveal-first).** Rejected — enforcing against rough defaults would cut
  legitimate traffic; reveal-first lets the defaults be corrected before any cutoff.
- **Self-reported in-band class/marker for the rate budget.** Rejected — forgeable; enforcement must key
  on authenticated identity (consistent with ADR 1033).

## Consequences

- Operators get a verdict, not just a number: `goat top` shows who is within their declared band and who
  is flooding — the visual precursor to automatic flood control.
- A new portal field + API + edit surface; consumers fetch + cache + degrade gracefully (no envelope →
  `—`).
- Declare + reveal add no central component (brokerless-safe); enforce stays identity-keyed at points we
  already control. The dynamic end state is incremental — each step useful alone.
- Rough defaults are explicitly *placeholders to be corrected*; enforcement is gated until they are.
