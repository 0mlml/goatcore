---
adr: 1012
title: Load testing as a required pre-production test class
status: Proposed
date: 2026-04-28
authors:
  - maintainers
audiences:
  technical: how the discipline gates production-ready and handoff-ready claims
  layperson: why we will not call something production-ready without a measured ceiling
  scar: how this strengthens the assurance posture vs. lab-ideal vendor claims
repo_scope: goat core (post-monorepo split: project-wide)
glossary: docs/adr/GLOSSARY.md
---

# ADR 1012 — Load testing as a required pre-production test class

## Status

Proposed.

Promotion to Accepted is gated on:

1. The first round of fully-automatable load measurements lands in `docs/reports/` with validation-state tracker entries (started 2026-04-28 with the relay-bench env / roadmap 9N).
2. The roadmap items that codify the rest of the automatable surface (block 34) ship.
3. CONTRIBUTING.md gains the discipline pointer.

## Context

[ADR 1008](1008-soak-testing-discipline.md) established **soak testing** as a
required test class for any new component that holds state, replicates across
nodes, or admits production-rate writes. Soak validates **correctness under
realistic time + load**: does the component survive failover, partition,
restart-injection, and steady-state writes without losing data or wedging
state? Smoke (5 min) → validation (2 hr) → canonical (72 hr) → long-haul (30 d).

Soak does **not** answer where the component's **performance ceiling sits**:

- What's the throughput limit per node / per VPS / per CPU core?
- What's the latency profile (p50 / p95 / p99 / p99.9) under sustained load?
- What's the connection-density / state-density ceiling before resource
  exhaustion?
- What does the component do at and past saturation — graceful degradation,
  catastrophic failure, silent data loss, queue buildup?

Without measured answers to these, any claim about scaling is conjecture. Per
the comparison standard codified in
`../compliance/comparative-posture.md`
§1.5, our position is that meaningful claims about a system require
operational evidence under realistic conditions, not lab-ideal benchmarks or
PowerPoint commitments. We hold others to that standard. We have to hold
ourselves to it too.

The trigger that surfaced the gap: an external critique of the
Netbird relay data plane (see
F-014)
forced us to ask "where does our relay's ceiling sit?" and we had no answer.
The full surface assessment (in the strategy thread that produced this ADR)
revealed 25 candidate scaling-bottleneck dimensions across network / data,
storage / state, identity / control, operational, and cross-plane layers —
**most of them untested at the time.** Until we have measured ceilings, we
cannot defensibly claim production-ready or handoff-ready for any component
that depends on them.

## Decision

**Load testing is a required pre-production test class, parallel to and
distinct from soak testing.**

For every load-bearing component the project ships or operates, before
declaring it **production-ready** OR **handoff-ready** (per
`../project/vendor-handoff-pickup-plan.md`),
we measure and record:

1. **Single-component throughput ceiling** at single-node and at typical
   instance / hardware shape. Identify the binding resource (CPU /
   memory / network / disk / connection count / file descriptor / lock
   contention). State the ceiling as a number (Mbps, requests/sec,
   messages/sec, peers, etc.).
2. **Latency under sustained load.** p50 / p95 / p99 / p99.9 at the
   throughput level we expect to operate at. NOT just at saturation.
3. **State / connection-density ceiling.** How many simultaneous
   connections, peers, sessions, open transactions, etc. the component
   holds before something breaks. State the ceiling.
4. **Behavior at and past saturation.** Does it degrade gracefully (queue
   buildup, latency growth, work shedding) or catastrophically (crash,
   silent corruption, deadlock)? Documented in the evidence file and in
   the relevant runbook.

A component without measured answers to all four is not production-ready and
not handoff-ready, regardless of how green its soak verdict is. **No measured
ceiling = no production claim.**

### Discipline

Driver shape mirrors the soak-testing pattern from ADR 1008:

- **Driver:** bash + load-generator (`iperf3`, `k6`, `wrk`, custom Go probe,
  `pgbench`, etc.) + JSONL evidence file
- **Evidence path:** `docs/reports/<date>-<component>-load.jsonl` (parallel
  to `<date>-<component>-soak-evidence.jsonl`)
- **Validation-state tracker entry:** every load-test verdict updates the
  relevant section of `../project/validation-state.md`
  in the same change. Verdict cites evidence by path or by F-NNN.
- **Reusable artifacts:** the load driver lives alongside the soak driver in
  the component's test tree (`ops/<component>/tests/load/` or equivalent)
- **CI:** smoke-tier load run (1 min, low concurrency, lightweight) lands in
  per-PR CI alongside smoke-tier soak; full ceiling measurement is a
  scheduled / pre-promotion run, not per-PR

### Tiers

| Tier | Duration | Purpose | Where it runs |
|---|---|---|---|
| **Smoke** | 1-5 min | Component still answers under nominal concurrent load | Per-PR CI |
| **Ceiling** | 30-120 min | Find the saturation point at one instance shape | Pre-promotion (per-component, per-release) |
| **Sustained** | 4-24 hr | Confirm the ceiling holds without resource leak / degradation | Pre-production-ready |
| **Adversarial** | Variable | Behavior under failure injection + load (combine with soak nemesis) | Pre-production-ready for components handling adversarial input |

### What "load-bearing component" means

A component is load-bearing if **either** is true:

1. It is in the data path of mesh traffic, signal traffic, fabric traffic,
   audit traffic, or telemetry traffic.
2. It is in the control path for enrollment, identity, policy, or
   configuration.

Stateless utilities (a one-shot CLI, a periodic cron job, a
config-template-renderer) are NOT load-bearing in this sense. The discipline
applies to anything operating continuously under mesh-wide or fleet-wide
demand.

### How this composes with handoff posture

`../project/vendor-handoff-pickup-plan.md`
§"Acceptance evidence we expect" gains a load-test-evidence requirement: a
handoff item is not accepted without measured ceilings under our conditions.
Recipient orgs producing only PowerPoint or lab-ideal benchmarks do not pass
acceptance. The bar applies in both directions: we hold ourselves to it for
goat-built components, and we hold recipients to it for handoff-built
components.

### How this composes with comparative posture

`../compliance/comparative-posture.md`
§1.5 (apples-to-apples comparison standard) gains a forward reference to this
ADR: when we publish performance characteristics, the evidence is operational
load-test output under our conditions, not vendor specification sheets. When
critics compare alternatives to goat, the comparison must be made against
load-test evidence the critic produced under equivalent conditions, not
against lab-ideal benchmarks the critic ran in their own environment.

## Consequences

### Positive

- **Defensible scaling claims.** Production-readiness claims become evidence-
  backed, not aspirational. The OSS-strategy receipts thesis (Block 29 +
  32) gets concrete numbers.
- **Handoff items become real.** `vendor-handoff-pickup-plan.md`
  candidate items get explicit acceptance criteria tied to measured improvement
  over the existing baseline (e.g., "Block 33 ships when the Rust relay
  measures ≥3× the Go relay's per-VPS Mbps on the same hardware").
- **Findings surface earlier.** Every load test we have actually run has
  produced a finding. Earlier discovery is
  cheaper than later.
- **Reduces strawman exposure.** Per comparative-posture.md §1.5, demanding
  apples-to-apples is asymmetrically defensible when we have apples and the
  critic has only PowerPoint.

### Negative

- **Engineering time.** Each load driver is 4-12 hours to write + run. With
  ~25 candidate dimensions, a full round of measurements is a multi-day
  sprint per release. Mitigated by tiering (smoke per-PR is cheap; full
  ceiling run is per-promotion).
- **Cloud cost.** Some load tests need ephemeral envs (the relay-bench env
  that produced the first measurement is ~$0.10/hr; PG-connection-flood is
  ~$0.05/hr). Bounded if envs are torn down promptly per
  `../operations/cloud-env-lifecycle.md`.
- **No load-test-environment harness shared across components yet.** Each
  component currently builds its own driver. A shared harness is a
  follow-up; without it, drivers risk drifting in shape. Mitigated by
  this ADR codifying the shape (driver + JSONL evidence + tracker entry)
  even before a shared harness exists.

### Neutral

- This discipline does **not** replace soak. Soak validates correctness;
  load validates limits. Both are required for production-ready or
  handoff-ready.
- Existing components that already ship without load evidence (most of them)
  are not retroactively de-promoted. Roadmap **block 34** stages the back-fill
  in priority order.

## Alternatives considered

### A. Make load testing optional ("nice to have")

Rejected. The comparative-posture standard we just published rests on
operational evidence; making the discipline producing that evidence optional
makes the standard hollow. Optional disciplines are also asymmetrically
unequal — the components that need the discipline most are usually the ones
nobody volunteers to load-test.

### B. Outsource load testing to a vendor

Rejected (for the goat-built components). Per
`vendor-handoff-pickup-plan.md`
§"Risk patterns" — vendor-managed-mystery is a known failure pattern.
Vendors producing performance assertions without driver code we can re-run
is exactly the strawman we want to avoid. We can pull in vendor expertise
on driver design, but the driver lives in our tree and we run it.

### C. Per-PR full ceiling measurement

Rejected. Cost-prohibitive at the per-PR rate (a t3.micro relay ceiling
test is ~$0.10 / 30 min, which sounds cheap until it's running on every
PR for every component). Tiered approach (smoke per-PR, ceiling per-promotion,
sustained per-production-ready, adversarial per-production-ready-for-adversarial)
is the cost-leverage trade-off.

### D. Defer until production runs hot

Rejected. The point of the discipline is to produce the number BEFORE
production runs hot, so that we know the headroom we have rather than
discovering a ceiling under fire. This is exactly the trap F-014 came out
of — assuming "must be fine" without measurement.

## Implementation tracker

Block 34 in `../project/implementation-plan.md`
captures the stand-up: enumeration of load-bearing components, per-component
driver authoring, per-component ceiling measurement, evidence + tracker
entries, ADR promotion to Accepted on completion of the first wave.

## Cross-refs

- ADR [`1008`](1008-soak-testing-discipline.md) — sibling test-class discipline (correctness)
- ADR [`1011`](1011-findings-register.md) — where measurement-surfaced behavior gets durable
- Comparative-posture standard: `../compliance/comparative-posture.md` §1.5
- Vendor handoff plan: `../project/vendor-handoff-pickup-plan.md`
- Validation-state tracker (where verdicts land): `../project/validation-state.md`
- First measurement (in flight 2026-04-28): roadmap §9N relay-bench env, evidence will be at `../reports/2026-04-28-netbird-relay-load.jsonl`
- Trigger finding: F-014

## Glossary pointer

See `GLOSSARY.md` for project acronyms used in this ADR.
