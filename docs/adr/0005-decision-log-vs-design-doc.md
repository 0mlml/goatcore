# ADR 0005 — ADR vs design doc vs runbook — what goes where

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | Multi |
| Supersedes | — |
| Superseded by | — |
| Related | [`0001-adr-template.md`](0001-adr-template.md), [`0002-adr-lifecycle.md`](0002-adr-lifecycle.md) |

## TL;DR (for a layperson)

The repository has three kinds of long-form documents that look similar
but serve different purposes. ADRs record *why we made a choice*. Design
docs describe *how a subsystem works today*. Runbooks describe *how an
operator does a specific task*. We keep them separate so each can be
updated on its natural cadence without dragging the others along.

## Context

goat has accumulated ~150 documents under `docs/` covering
design, operations, compliance, project tracking, and reference
material. Several are ADR-shaped without being labelled as such (e.g.
`design/container-orchestration.md`,
[`design/orin-os-decision.md`](../design/orin-os-decision.md)). Others
are design specs that contain embedded decisions which were never
extracted. The result is that the rationale for many architectural
choices is buried inside multi-thousand-word design docs and not
discoverable on its own.

The fix is to separate decision rationale (durable) from system
description (lives or dies with the code) from operator instructions
(specific to a task) and put them in three different places.

## Decision

Three artifact types, three locations, three update cadences.

| Type | Location | Purpose | Update cadence |
|---|---|---|---|
| **ADR** | `docs/adr/` | The *why*: the decision, the alternatives, the residual risk, the trigger to revisit. Immutable once Accepted (see [`0002`](0002-adr-lifecycle.md)). | New ADR when the decision changes. |
| **Design doc** | `docs/design/` | The *how* of a subsystem as it works today. Architecture diagrams, component lists, data flows, schemas, configuration examples. Mutable; lives or dies with the code. | Updated in the same PR as code changes that change the design. |
| **Runbook** | `docs/operations/` | The *how-to* for an operator: deploy, debug, rotate, recover. Step-by-step. | Updated when a step changes. |

Promotion rules:

1. **An embedded decision in a design doc gets promoted to an ADR**
   when (a) someone has to ask "why did we choose this?" more than
   once, or (b) it would be load-bearing for an SCA-R review, or
   (c) it has rejection-worthy alternatives.
2. **An ADR does not duplicate the design doc.** It states the
   decision and links to the design doc for the *how*. If the
   design changes but the decision does not, the design doc
   updates and the ADR stays.
3. **A runbook does not contain decision rationale.** If an operator
   needs to know *why* a step exists, the runbook links to the ADR.

What promotes to an ADR:

- A choice with named alternatives that were considered and rejected.
- A choice that an SCA-R, sponsor, or new contributor would predictably
  ask "why?" about.
- A choice with non-trivial consequences if reversed.
- A waiver, exception, or accepted risk.

What does not promote:

- Implementation details that follow obviously from a higher-level
  decision.
- Configuration values (those go in code or design docs).
- Project status, milestone tracking, or phased plans (those stay
  in `docs/project/`).

## Alternatives considered

- **One unified `docs/decisions/` for all three artifact types** —
  collapses the cadence distinction; design churn would force ADR
  re-review. Rejected.
- **ADRs as Git issues / GitHub Discussions** — ephemeral, not
  citable in a compliance package, dies with platform changes.
  Rejected.
- **No design docs, only ADRs** — ADRs are not the right shape for
  describing how a subsystem currently works (data flows,
  configuration, schemas). Rejected.

## Consequences

**Positive.** Each artifact has a clear update trigger. Design docs
can churn without re-litigating accepted decisions. ADRs stay
reviewable because they are short and decision-focused.

**Negative.** Authors must classify their writing before producing
it. We accept the cost; the classification is usually obvious. The
existing `docs/design/` directory contains hybrid documents that
will be split over time as their embedded decisions get promoted —
this is tracked in [`INDEX.md`](INDEX.md) per ADR.

## Assessor view (SCA-R)

The artifact-type separation maps directly onto NIST SP 800-171 /
800-53 documentation expectations: ADRs satisfy the rationale-and-
risk-acceptance dimension (CA-x, RA-3, PM-9), design docs satisfy
the system description dimension (PL-2 System Security Plan
components), and runbooks satisfy the operational procedure
dimension (CP-x, IR-x, MA-x). An assessor reading the ADR series
gets the answer to "why does this look like this?" without having
to derive it from the implementation.

## Trigger conditions for revisit

- A fourth artifact type emerges with a distinct update cadence
  (e.g. test plans, threat models). Add it explicitly; do not
  fold it into one of the existing three.
- Repeated drift between an ADR and its referenced design doc.
  Investigate: usually means the ADR was over-specified or the
  design doc is stale.

## Amendment 2026-08-13 — a how-to for getting concurrence is a CONTRIBUTING concern

**The three promotion rules in the Decision section stand as written.**
This amendment adds a fourth:

4. **A how-to for getting concurrence on a decision is a runbook /
   `CONTRIBUTING.md` concern, not an ADR.** The mechanism — a decision
   expressed as a reviewed, merged file — is already established by this
   ADR and [`0109`](0109-git-collaboration-trunk-based.md); the
   step-by-step for contributors lives in
   [`CONTRIBUTING.md`](../../CONTRIBUTING.md) under "Proposing a
   decision that needs concurrence."

Recorded as a dated amendment rather than an in-place edit to the rule
list: this ADR is Accepted, and [`0002`](0002-adr-lifecycle.md) rule 1
holds that an Accepted ADR takes substantive additions by amendment or
supersession so a prior citation of its content stays true. Origin: PR #56.

---

*See `GLOSSARY.md` for terms and acronyms.*
