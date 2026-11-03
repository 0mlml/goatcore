# ADR 0509 — Phase 2 gate criteria — four questions before Phase 3 commit

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Last amended | 2026-04-30 (added Question 4 — substrate-boundary abstraction test) |
| Repo scope | goat core |
| Related | [`0500-zenoh-data-fabric.md`](0500-zenoh-data-fabric.md), [`0210-zenoh-universal-substrate.md`](0210-zenoh-universal-substrate.md), [`0512-rapid-adaptation-with-ai.md`](0512-rapid-adaptation-with-ai.md), [`1006-phase-gates.md`](1006-phase-gates.md) |

## TL;DR (for a layperson)

Before we commit to Zenoh as the universal substrate for the
whole system, we will run a Phase 2 evaluation and answer
four specific questions. Only if all four answers are
satisfactory do we move forward to Phase 3 (where Zenoh becomes
the substrate for everything). If any question fails, we fall
back to NATS JetStream — but the fourth question makes sure the
"fallback" is real, not a phrase that hides a 15-block cascade.

## Context

[`0210-zenoh-universal-substrate.md`](0210-zenoh-universal-substrate.md)
commits to Zenoh as the universal peer-interaction substrate,
*conditional* on Phase 2 outcome. Without an explicit gate, the
"conditional" framing dissolves into momentum; we end up
committed without ever having answered "is it actually working?"

The gate forces the answer.

## Decision

The Phase 2 gate is the four questions defined verbatim in
`../design/zenoh-data-fabric.md`
"Phase 2 gate criterion." Before Phase 3 commits to DDS bridge
and CDS conditioning, these four questions must be answered with
pilot data:

1. **Did the interceptor model produce testable code in practice,
   or did it degrade into config-like shims?**
2. **Did the operational burden (monitoring, debugging, upgrades)
   match the "one system, one config" promise, or did it sneak
   in new failure modes?**
3. **Did Rust interceptor development velocity hold up for the
   team — not compared to theoretical alternatives, but as an
   absolute measure of "can we ship features on this stack"?**
4. **Is the substrate-boundary abstraction clean enough that the
   named NATS fallback would be a substrate swap rather than a
   15-block cascade?** Test of decoupling, not of NATS
   specifically. The conventions Phase 3 commits to (agent-class
   peers per ADR [`0512`](0512-rapid-adaptation-with-ai.md),
   schema declaration + schema-enforcer, queryable-by-default,
   citation discipline, lineage, partner release namespaces)
   need to sit on an interface the named fallback could satisfy
   — not be structurally bound to Zenoh's specific
   key-expression / queryable / storage shape such that "fall
   back to NATS" would actually mean rewriting the conventions.
   If the answer is "structurally bound," the documented
   fallback is no longer a fallback; Phase 3 is a one-way
   commitment, and either the gate criteria are revised to
   reflect that or the conventions are refactored to clean the
   boundary before Phase 3 commits.

**Pass:** commit to Phase 3+ DDS bridge + CDS conditioning scope
([`0210`](0210-zenoh-universal-substrate.md) universal-substrate
expansion).

**Fall-back (documented in advance, not re-litigated):** if the
gate fails, the tactical data plane falls back to NATS JetStream
+ Go consumers per the design doc. Decided in advance so we don't
re-debate when tired.

The gate is reviewed at the end of Phase 2D — see
`../project/zenoh-implementation.md`
for the running tracker. The integdev environment exists to let
us answer these questions under realistic distributed conditions
(router federation over WireGuard, island-mode partition, cross-
router query, storage consistency under reconnect) — see
`../design/zenoh-data-fabric.md`
"Why integdev not single-router" rationale.

> **Correction note (2026-04-25).** The original draft of this ADR
> invented three different gate questions (30-day uptime,
> partition-recovery test, three phoenix-native peer classes)
> that are not in the canonical design doc. The three questions
> above are the actual gate text. Original draft authored without
> opening the design doc.

**Enhancement (landed 2026-04-25):** the canonical design doc
now lists **supporting evidence** the panel uses to inform its
qualitative answers — operational soak (≥30 days), partition-
recovery test (≥4 hours), phoenix-native peer count (≥3 peer
classes), interceptor catalog. The supporting evidence is *not*
the gate criteria; it's the empirical record the panel
deliberates against. See
`../design/zenoh-data-fabric.md`
"Phase 2 gate criterion" — the section now explicitly
distinguishes the four qualitative gate questions from the
empirical evidence that informs them.

**Amendment (landed 2026-04-30):** Question 4 added — the
substrate-boundary abstraction test. Motivation: a Phase 2
review surfaced that the conventions Phase 3 commits to
(agent-class peers, schema-enforcer, queryable-by-default,
lineage, partner release namespaces) currently sit *structurally*
on Zenoh's key-expression / queryable / storage shape, with no
clean interface boundary. Without Question 4, "pass the gate"
could mean "all three qualitative questions answered yes, and
the named NATS fallback is a phrase that would actually require
15 blocks to be rewritten." The new question forces a concrete
check: walk each Phase-3 convention, write down the interface
shape, and verify the named fallback could satisfy it. Anchored
by a new "Substrate-boundary audit" supporting-evidence item in
the design doc.

## Alternatives considered

- **Implicit gate (revisit if it doesn't feel right).** Allows
  momentum to dissolve the conditional framing. Rejected.
- **Quantitative gate (throughput / latency / uptime targets).**
  Real but secondary; they don't surface the load-bearing
  qualitative judgements about *sustainability of the stack*
  that drove the design-doc questions.

## Consequences

**Positive.** Decision is forced and recorded with evidence.
Fallback is pre-decided. Future review is reproducible.

**Negative.** A pass-fail gate forces a binary call. We
accept; partial-pass would dissolve into political
negotiation.

## Assessor view (SCA-R)

The gate is itself a CA-3 (system interconnection) and CM-3
(change control) artifact: the decision to commit to Zenoh
across the system has reviewable evidence behind it. The
fallback is a CP-9 (system backup — alternative substrate)
component.

## Trigger conditions for revisit

- The gate is executed (pass or fail). Update Phase 3 plan
  accordingly.
- A criterion proves to be the wrong question. Revise the
  gate explicitly; do not silently relax.

---

*See `GLOSSARY.md` for terms and acronyms.*
