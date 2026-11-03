---
adr: 0825
title: Replay substrate — operator-facing rewind over wide events + audit + lineage
status: Proposed
date: 2026-04-30
authors:
  - contributor-x
audiences:
  technical: how the replay peer reconstructs and re-emits historical fabric state, and why nothing new is stored
  layperson: why operators should be able to ask the same question of yesterday as they ask of today
  scar: how replay preserves audit-evidentiary integrity (mode B) and re-runs partner conditioning rather than bypassing it
repo_scope: goat core
glossary: docs/adr/GLOSSARY.md
---

# ADR 0825 — Replay substrate

## Status

Proposed.

Promotion to Accepted is gated on:

1. Block 37 phase A (mode A reconstruction) ships and runs through a 2 hr validation-tier soak with zero replay-vs-live cross-talk on the original key expressions.
2. Block 37 phase D (mode B audit-grounded reconstruction) returns measured latency under the canonical-tier soak window.
3. Scout integration (37F) demonstrates a full Scout query that opens, queries, and closes a replay session with citation-graph integrity preserved.

## Context

After-action review on a goat site today is days of engineer-hours
hand-stitching across stores. Wide events
([ADR 0822](0822-wide-event-observability-otlp-openobserve.md)) are
joinable by `correlation_id`. Audit-integrity storage
([ADR 0811](0811-audit-log-integrity.md)) is durable and verifiable.
Lineage adjacency
([`../design/lineage.md`](../design/lineage.md)) carries the
derived-from / derived-to graph. All three exist independently. None
serves as a substrate for re-derivation of past topic state.

What operators want — and what Scout
([`../design/scout.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/scout.md)) needs to be useful for
historical questions — is a substrate that, given a time interval,
re-emits the past as if it were live, on the same key expressions,
with the same schemas, at original timestamps' relative pacing. The
operator's existing tools (dashboards, ATAK clients via federation,
Scout) render replayed sessions identically to live ones, modulo the
isolation marker that prevents cross-talk.

## Decision

**A `replay/` peer class reconstructs past fabric state from wide
events + audit + lineage, and re-emits it on a `replay/<session_id>/...`
namespace. Replayed emissions are isolated from live consumers by
namespace prefix and by schema-enforcer enforcement. Three
reconstruction modes — wide-event-only (default, fast),
audit-grounded (slower, evidentiary), lineage-aware (parallel
metadata stream) — are operator-selectable per session.**

Detailed shape, session lifecycle, mode characteristics, and storage
budget are in [`../design/fabric-replay.md`](../design/fabric-replay.md).
Highlights:

- **No new substrate.** Replay reads from existing OpenObserve / MinIO / lineage-RocksDB; writes only ephemeral session metadata.
- **Isolation by construction.** Replayed emissions ride `replay/<session_id>/...`, not the original keyexpr. Subscribers explicitly opt in.
- **Replay sessions are themselves audit-logged.** No silent past-state inspection.
- **Partner-release replays re-execute WASM conditioning** — replayed events through `release/<partner>/...` paths run the partner's signed WASM module against the replay's events with the same fuel / memory / time bounds as live. Replays do not bypass conditioning.
- **Scout integration.** Scout opens a replay session for time-bounded questions; the `session_id` flows through Scout's citation graph.

## Consequences

### Positive

- **After-action review collapses from days to operator-self-service.** Operator runs `cmd/replay/start` with a time window and inspects the resulting session via existing tools.
- **Scout becomes time-aware.** Scout reasons over historical fabric state with the same citation discipline and machinery it uses for live state.
- **Training and red-team use cases land for free.** A captured real session replays into a training environment without bespoke recording infrastructure.
- **Schema-migration testing.** A new schema-enforcer interceptor can run against yesterday's traffic before production promotion.
- **Composes with chaos.** Block 38 chaos peer can probe invariants against replayed historical state — answers "did this invariant hold last Tuesday?"
- **Composes with time-quality.** Replayed emissions preserve the quality envelope (Block 36); operators ask quality questions of past data the same way they ask of live data.

### Negative

- **Substrate dependency growth.** Replay depends on OpenObserve + MinIO + lineage-RocksDB being healthy. A replay session during a substrate degradation returns partial results — the session metadata records the partial-completeness.
- **Mode B latency.** Object-Lock-protected reads against MinIO are slower than OpenObserve queries. The first 37D measurement run will produce the actual number; mitigation if too slow is async-result-with-callback, not interactive.
- **Schema-evolution edge cases.** A topic whose schema bumped between original emission and replay needs a deterministic rule. Default: replay carries the original schema version; consumers handle multi-version subscription per `../design/fabric-schema-evolution.md`. Confirm against a real schema bump in the first soak.
- **Engineering cost.** Reconstruction engine is non-trivial. Estimate ~6-8 wks total across 37A-37F. Mitigated by phased delivery: 37A delivers value before the rest ships.

### Neutral

- **No cross-site replay in v1.** Each site replays its own state. Cross-site stitching by `correlation_id` is a Phase 3+ extension if operational demand surfaces. Site-loss durability is unchanged: the audit-integrity tier is the durable record, not replay.
- **No evidentiary claim for mode A.** Mode A is fast operator-helpful; mode B is audit-grounded. Evidentiary contexts must use mode B; the session metadata records the chosen mode.
- **Replay is optional infrastructure.** A site without OpenObserve does not get replay. The discipline does not block such a site; it just means after-action review reverts to hand-stitching there.

## Alternatives considered

### A. Build replay into OpenObserve as a query-result transformation

Rejected. OpenObserve is a query store, not a publish/subscribe substrate. Operator tools and Scout speak Zenoh; making them all speak OpenObserve queries is a step backward. Re-emitting on Zenoh keeps the substrate uniform.

### B. Capture all emissions to a dedicated replay store

Rejected. The audit-integrity tier already captures (durably) and the wide-event tier already captures (cheaply, queryable). A third store would duplicate the substrate and triple the storage budget for marginal gain.

### C. Operator-facing query UI that hand-stitches behind the scenes

Rejected as the only mechanism. A query UI is fine for ad-hoc operator inspection (and OpenObserve already provides one). Replay-as-Zenoh-session is the mechanism that enables Scout / training tools / dashboard reuse without re-implementing each.

### D. Defer replay until Phase 4

Rejected on operational grounds. After-action review hand-stitching is already a known operational cost and gets worse as the fabric grows. Replay's substrate dependencies (OpenObserve + audit + lineage) all exist or are landing in Phase 3; no substrate gating remains. The marginal cost of building replay now is the engineering time; the marginal cost of waiting is recurring after-action-review hand-stitching across every Phase 3 incident.

## Implementation tracker

Block 37 in `../project/implementation-plan.md`:
37A mode A reconstruction; 37B fabric-peer wiring; 37C mode C
lineage-aware; 37D mode B audit-grounded; 37E pause/resume + variable
playback rate; 37F Scout integration; 37G cross-site (deferred).

## Cross-refs

- Design: [`../design/fabric-replay.md`](../design/fabric-replay.md)
- ADR [`0822`](0822-wide-event-observability-otlp-openobserve.md) — wide-event substrate (primary input)
- ADR [`0811`](0811-audit-log-integrity.md) — audit-integrity tier (durability backstop)
- ADR [`0508`](0508-audit-logger-interceptor.md) — Zenoh audit-logger
- ADR [`0513`](0513-wasm-conditioning-sandbox.md) — partner WASM modules re-execute on replay
- [`../design/scout.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/scout.md) — primary consumer
- [`../design/lineage.md`](../design/lineage.md) — lineage propagation
- [`../design/continuous-architectural-chaos.md`](../design/continuous-architectural-chaos.md) — chaos probes can run against replayed state
- [`../design/time-quality-attestation.md`](../design/time-quality-attestation.md) — replayed emissions preserve envelope

## Glossary pointer

See `GLOSSARY.md` for project acronyms used in this ADR.
