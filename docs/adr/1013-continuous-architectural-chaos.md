---
adr: 1013
title: Continuous architectural chaos — every documented invariant becomes a probe
status: Proposed
date: 2026-04-30
authors:
  - contributor-x
audiences:
  technical: how the chaos peer encodes invariants as probes and what the framework requires
  layperson: why "the docs say so" is not enough — we want machine evidence the system still does what we claim
  scar: how this strengthens assurance posture beyond point-in-time accreditation
repo_scope: goat core (project-wide)
glossary: docs/adr/GLOSSARY.md
---

# ADR 1013 — Continuous architectural chaos

## Status

Proposed.

Promotion to Accepted is gated on:

1. The probe framework (`goat-chaos`) ships with at least 5 production-active probes covering the highest-stakes invariants (silent control plane, no-public-mgmt-DNS, schema-enforcer, audit-tamper-evidence, setup-key TTL).
2. At least one probe failure in production has produced a draft finding entry that promoted to a permanent F-NNN.
3. The discipline is documented in CONTRIBUTING.md alongside the soak (ADR 1008) and load (ADR 1012) discipline pointers.
4. The Block 32 engineering-health scorecard incorporates a probe-coverage + pass-rate metric.

## Context

ADR [`1008`](1008-soak-testing-discipline.md) established **soak
testing** — correctness under realistic time and load. ADR
[`1012`](1012-load-testing-discipline.md) established **load testing**
— measured ceilings before production-ready or handoff-ready claims.
Together they cover pre-production. Neither covers a real risk:
**post-deployment drift between documented invariants and live
reality.**

goat has unusually high invariant-density. CLAUDE.md alone makes
load-bearing claims about silent control plane, no public mgmt DNS,
schema-enforcer default-deny, peer-key attestation, audit
tamper-evidence, default-deny egress, phoenix-native peer
self-attestation, mandatory data-dictionary declaration, time-quality
floors per topic, and ~40 more. Every one of these is a place where
reality could silently drift from the doc — vendor change, library
upgrade, ops remediation, partial rollback, configuration mistake.

The UserPromptSubmit hook nags about doc-vs-code drift at authoring
time. CI catches drift in pre-merge code. Neither catches drift that
arrives from outside the source tree (a third-party container update
that changes default ports, an AWS service change that opens a
listener, a configuration mistake during incident response).

The project's own comparative-posture commitment
(`../compliance/comparative-posture.md`)
asserts that meaningful claims rest on operational evidence, not
lab-ideal assertions. We hold others to that standard. We must hold
ourselves to it.

## Decision

**Continuous architectural chaos is a required post-deployment test
class. Every load-bearing architectural invariant claimed in CLAUDE.md,
ADRs, or design docs gets at least one runnable probe where mechanically
feasible. Probes run continuously against the live fabric on a per-
probe schedule. Probe results emit on Zenoh + OTLP wide events + the
audit-integrity ledger. Failed probes draft F-NNN finding entries.**

Probe framework, authoring rules, invariant catalog, composition with
existing systems, and density targets are codified in
[`../design/continuous-architectural-chaos.md`](../design/continuous-architectural-chaos.md).
Highlights:

- **Probe shape:** small Go function returning `passed`, `evidence_path`, optional `finding_draft`. Cites source via `InvariantDoc()`.
- **Severity ladder:** Critical (paging) / High (24h SLA) / Medium (weekly) / Informational.
- **Authoring rules:** must cite a doc; must be non-destructive; must self-clean; bounded ≤30s per probe; reproducible evidence; findings-register-shaped output on failure.
- **First-wave catalog:** 15+ probes from 0300-series (mesh), 0500-series (fabric), 0800-series (operations).
- **Density target:** Phase 3 80% of CLAUDE.md-quoted invariants probed; Phase 4 95% with the residual catalogued.
- **Language: Go.** Per ADR [`0112`](0112-tech-strategy-workload-shape-language-boundary.md), chaos is request-shaped (periodic probes, broad library needs).

## Consequences

### Positive

- **Drift becomes machine-detectable.** Documented invariants stop being honor-system; they become enforced.
- **Findings register grows from production reality, not just adversarial review.** Every probe failure surfaces a finding the team would otherwise discover in the next red-team or, worse, in a real incident.
- **Vendor handoff strengthens.** Recipients of handed-off components must pass the relevant probes — discipline travels with the component.
- **Engineering-health scorecard gets a real metric.** "Invariant-fulfillment rate" becomes evidence-backed quarterly reporting.
- **Composes with replay (Block 37).** Chaos probes can run against replayed historical state — answers "did invariants hold last Tuesday?" without rebuilding past state by hand.
- **Closes the comparative-posture loop.** When the project asserts an invariant ("the secure plane is not on the public internet"), the answer to "prove it" is a passing probe with reproducible evidence, not a doc reference.

### Negative

- **Engineering-time cost.** First-wave 15+ probes is ~2-3 wks. Ongoing probe authoring is a soft expectation per ADR-with-implementation discipline (~30-60 minutes per probe for the kinds of invariants we have). Mitigated by the framework absorbing common patterns (HTTP probe, DB query probe, AWS-SG probe) and by probe quality compounding (one good probe replaces many ad-hoc operator checks).
- **False-positive risk.** A probe that fails intermittently due to legitimate transient state (controlled failover, scheduled maintenance) creates noise. Mitigated by per-probe failure-streak threshold, per-probe cool-down windows, and manual silencing windows during planned events.
- **Probe coverage gaps misread as completeness.** A 95% probe-coverage number could lull operators into thinking the 5% does not matter. Mitigated by explicit cataloguing of unprobable invariants (process invariants, sponsor-facing claims, etc.) — the residual is named, not assumed safe.
- **Probe maintenance burden.** A probe whose invariant gets superseded must update or retire. Mitigated by the framework's `InvariantDoc()` requirement: when the doc moves or supersedes, the probe shows up in a "superseded-doc-pointer" sweep.

### Neutral

- **Discipline-not-product.** This is not a tool to buy or a service to deploy; it is engineering practice with a Go binary as the orchestrator. The Go binary is small (~2 kLOC framework + per-probe code).
- **Existing components are not retroactively de-promoted.** Pre-Block-38 components stay accepted; chaos coverage builds incrementally. Block 38 catalog lists which invariants are probed and which are pending.
- **Adversarial review continues.** Chaos is the floor, not the ceiling. Quarterly red-team is unaffected.

## Alternatives considered

### A. Make probe-with-ADR a hard CI gate from day one

Rejected for v1. Soft expectation initially; promote to hard gate if drift accumulates. Hard gates without operational experience produce malformed gates that block legitimate work.

### B. Buy a commercial chaos-engineering platform

Rejected. Most commercial tools (Gremlin, ChaosMesh, etc.) are pre-production fault injectors, not post-deployment invariant probers. Different category. Building the probe framework against goat's specific invariants is the work; no commercial product matches.

### C. Treat chaos as part of soak

Rejected. Soak validates correctness over time. Chaos validates that documented invariants still hold. Different questions, different schedules, different evidence shapes. Conflating them dilutes both. They compose (chaos runs concurrently with soak windows; chaos failures during soak are first-class soak inputs) but stay separate.

### D. Defer until Phase 4

Rejected. Drift starts the moment a system has documented invariants and changing infrastructure — both true today. Block 38 first wave is ~2 wks of work; the cost of waiting is recurring drift that surfaces at the worst time.

### E. Probe everything via Prometheus alerting

Rejected as the only mechanism. Prometheus is excellent for metric-shaped invariants (throughput, error rate, queue depth). Architectural invariants are propositions ("the listener answers no pre-auth bytes from outside-mesh"), not metrics. Some probes do emit Prometheus metrics for trending; the verdict shape is the difference.

## Implementation tracker

Block 38 in `../project/implementation-plan.md`:
38A framework + 5 probes; 38B wide-event + audit + policy-engine
notification; 38C findings-register draft generation; 38D first-wave
catalog (~15 probes); 38E scheduling tuning + per-env override; 38F
engineering-health scorecard integration; 38G ongoing catalog growth.

## Cross-refs

- Design: [`../design/continuous-architectural-chaos.md`](../design/continuous-architectural-chaos.md)
- ADR [`1008`](1008-soak-testing-discipline.md) — sibling test class (correctness over time)
- ADR [`1012`](1012-load-testing-discipline.md) — sibling test class (measured limits)
- ADR [`1011`](1011-findings-register.md) — durable home for probe-discovered findings
- ADR [`0112`](0112-tech-strategy-workload-shape-language-boundary.md) — language assignment (Go for chaos)
- ADR [`0805`](0805-policy-engine-active-passive.md) — notification adapters
- ADR [`0822`](0822-wide-event-observability-otlp-openobserve.md) — wide-event integration
- ADR [`0811`](0811-audit-log-integrity.md) — audit-integrity ledger entries for probe results
- Findings register INDEX
- [`../design/engineering-health-assessment.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/engineering-health-assessment.md) — quarterly scorecard integration
- `../compliance/comparative-posture.md` — comparative-posture standard
- [`../design/fabric-replay.md`](../design/fabric-replay.md) — chaos can probe replayed historical state
- `../project/vendor-handoff-pickup-plan.md` — handoff bundle artifact

## Glossary pointer

See `GLOSSARY.md` for project acronyms used in this ADR.
