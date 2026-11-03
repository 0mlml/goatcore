---
adr: 0514
title: Time-quality attestation propagated through the fabric
status: Proposed
date: 2026-04-30
authors:
  - goat maintainers
audiences:
  technical: how the time-quality envelope rides every emission and what consumers can filter on
  layperson: why we cannot treat all timestamps as equally trustworthy at the tactical edge
  scar: how this composes with FIPS / NTS / GNSS posture without inventing new validated cryptography
repo_scope: goat core
glossary: docs/adr/GLOSSARY.md
---

# ADR 0514 — Time-quality attestation propagated through the fabric

## Status

Proposed.

Promotion to Accepted is gated on:

1. The envelope schema + Rust SDK + Go SDK helpers land and are exercised by at least three first-wave producers (Block 36 phase A-B).
2. The schema-enforcer interceptor extension lands, runs against existing fabric traffic for ≥1 validation-tier soak, and produces zero false-positive rejections of legitimate emissions.
3. The lineage `worst_case_time_quality` attribute lands and the lineage peer's new query verbs return correct propagation graphs against synthetic load.
4. At least one consumer (Tipper or Scout or partner-release bridge) actively uses `time_quality` in a filter / weighting decision against a real workload.

## Context

GNSS denial and NTS spoofing are routine threats in the operational
environments goat targets. The project already has substantial NTP
work — three-tier NTP strategy
([ADR 0804](0804-three-tier-ntp.md)), per-peer chrony / NTS daemon
under [`universal-ntp-hygiene.md`](../design/universal-ntp-hygiene.md),
admit-time NTP attestation under
[`zenoh-ntp-integration.md`](../design/zenoh-ntp-integration.md), per-
peer health agent at [`time-hygiene/`](../../ops/time-hygiene/).

What we do not have is a *fabric-layer* representation of how
trustworthy each emission's timestamp is. Two consequences in
production:

1. **Fusion math is silently lossy.** A track derived from a
   GNSS-locked sensor and a self-clocking sensor has merged-confidence
   characteristics that no downstream consumer can compute, because
   the inputs do not advertise their quality. A receiver that degrades
   from GNSS lock to NTS-only shows up downstream as jittering tracks,
   because nothing discounts its tracks accordingly.
2. **Cross-plane debugging is hand-stitched.** When a downstream
   consumer reports an anomaly, the operator must hand-trace which
   sensor produced the input, look up that sensor's NTP health
   history, decide whether quality at emission time was adequate,
   and infer the propagation through any derivation chain. This is
   exactly the kind of cross-plane joining that wide events
   ([ADR 0822](0822-wide-event-observability-otlp-openobserve.md))
   exist to make machine-tractable.

The data-dictionary discipline
([`data-dictionary.md`](../design/data-dictionary.md)) and the
mandatory `attest/schema/<topic>` declaration give us the place to
encode the field. The schema-enforcer interceptor gives us the place
to enforce it. This ADR codifies adding the field as a required
contract.

## Decision

**Every emission on the Zenoh fabric carries an inline `time_quality`
envelope. The schema-enforcer interceptor rejects emissions missing
the field. Per-topic declarations may set a `minimum_source` floor
that the interceptor enforces.**

Envelope shape, source ladder, and propagation rules are codified in
[`time-quality-attestation.md`](../design/time-quality-attestation.md).
Highlights:

- **Required fields:** `source` (enum, ladder), `sigma_us`,
  `last_external_lock_at`, `seconds_since_external_lock`,
  `drift_model_confidence`.
- **Source ladder (worst → best):** `self_clocking` < `peer_attested`
  < `chrony_steered` < `nts_verified` < `gnss_locked`.
- **Lineage propagation:** worst-case along edges; new attribute on
  lineage adjacency; new query verbs on the lineage peer.
- **Per-topic floor:** schema declaration carries optional
  `minimum_source`; interceptor enforces.

Producer side: the per-node `ntp-health-agent` already tracks the
inputs. It gains a UDS endpoint that returns the current envelope as
a single syscall; producer SDK helpers (Rust + Go) cache the result
for ≤100 ms and attach it to every emission.

Consumer side: filters on `(source, sigma_us)` are a first-class
predicate. Tipper anomaly weighting, Scout citation rendering, and
mission-partner release floors all use the envelope.

**Why this lands as a propagation contract, not a new validated
cryptographic mechanism.** No new CMVP claim; no new clock-sync
protocol; no new key custody. The signature on the envelope (when
present) reuses the same ed25519 chain as existing publisher trust
([ADR 0511](0511-publisher-trust-model.md)). The envelope is plain
metadata — what makes it load-bearing is the schema-enforcer's
willingness to reject below-floor emissions and the consumers'
willingness to act on quality dimension.

## Consequences

### Positive

- **Fusion correctness becomes evidence-backed.** Downstream consumers stop treating self-clocking and GNSS-locked inputs identically. Tipper's false-positive rate during night-time drops as the drift-model-confidence dimension enters the weighting.
- **Cross-plane debugging via wide events.** OTLP attribute mapping `dbb.time_quality.*` makes "show me every emission with quality below floor in the last hour" a single SQL query.
- **EW resilience defensible.** When a partner asks "what's your story on GNSS denial?", the answer becomes "every emission carries quality envelope; below-floor emissions are rejected at the fabric layer; here's the per-peer propagation graph; here's the soak data showing quality degradation paths."
- **Composable with replay.** Replayed emissions (Block 37) preserve their original quality envelope; "what was quality at incident time?" is a substrate-native question.
- **Composable with chaos.** A chaos probe (Block 38) confirms the floor enforcement is live in production.

### Negative

- **Producer migration cost.** Every producer that emits onto the fabric needs the SDK helper or hand-rolled equivalent. Mitigated by phased rollout (Block 36E backfills) and by the SDK absorbing the work for new producers.
- **Schema-enforcer becomes a tighter gate.** A bug in the enforcer's validation logic could DLQ legitimate emissions. Mitigated by the validation-tier soak gate and by the DLQ being inspectable in real time.
- **Producers without good local time-source insight.** A producer running on a peer with no GNSS, NTS, or peer attestation can only emit `source = self_clocking` with growing sigma. This is correct: the system is not pretending. But it does mean some operationally important sensors may produce below-floor emissions for some topics, requiring either floor relaxation (per-topic) or hardware upgrades (CSAC procurement).

### Neutral

- **Bundle path unchanged.** Offline-enrollment bundles continue to carry only signing-time signatures; the first emission after enrollment carries the envelope as normal.
- **Per-peer NTP work unchanged.** The envelope propagates state already tracked by `ntp-health-agent`; no new clock-sync protocol.

## Alternatives considered

### A. Per-topic free-form quality field, no enforcement

Rejected. Producers without a contract drift apart over time; consumer code becomes per-source defensive. The schema-enforcer-enforced floor is what makes the discipline real.

### B. Quality as a separate sidecar topic, not in-band

Rejected. The temptation to drop the sidecar topic during partition / load is structural; in-band guarantees the dimension travels with the data. The cost in bytes is bounded (~80 bytes per emission).

### C. Wait for SPIFFE / X509 / hardware time-attestation

Rejected as the v1 mechanism. Hardware time attestation (TPM-PCR-bound timestamping) is real but procurement-heavy and not a Phase 2-3 prerequisite. The envelope approach gives us 90% of the operational value at near-zero hardware cost; hardware attestation can layer in later as `source` ladder additions.

### D. Treat time quality as Tipper's problem

Rejected. Tipper is one consumer among many. Putting the quality dimension in-band lets Scout, partner-release, fusion math, and human operators all use it without each re-implementing a quality model.

## Implementation tracker

Block 36 in `../project/implementation-plan.md`:
36A envelope schema + UDS endpoint + SDK helpers; 36B schema-enforcer
extension + DLQ; 36C data-dictionary spec update + per-topic floors;
36D lineage attribute + query verbs; 36E producer backfill; 36F
consumer wiring (Tipper / Scout / partner-release).

## Cross-refs

- Design: [`../design/time-quality-attestation.md`](../design/time-quality-attestation.md)
- ADR [`0804`](0804-three-tier-ntp.md) — three-tier NTP
- ADR [`0511`](0511-publisher-trust-model.md) — publisher trust model
- ADR [`0508`](0508-audit-logger-interceptor.md) — Zenoh audit-logger interceptor (envelope is auditable by construction)
- ADR [`0822`](0822-wide-event-observability-otlp-openobserve.md) — wide events (OTLP attribute mapping)
- [`../design/data-dictionary.md`](../design/data-dictionary.md) — schema-declaration discipline
- [`../design/lineage.md`](../design/lineage.md) — propagation graph
- [`../design/tipper.md`](https://github.com/usareur-af/goatstuff/blob/main/docs/design/tipper.md) — first weighted consumer
- [`../design/mission-partner-sharing.md`](../design/mission-partner-sharing.md) — partner-release floor enforcement

## Glossary pointer

See `GLOSSARY.md` for project acronyms used in this ADR.
