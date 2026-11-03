# ADR 1006 — Phase gates — explicit transitions across Phases 1 → 5

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | Multi |
| Related | [`0102-two-track-development.md`](0102-two-track-development.md), [`0509-phase-2-gate-criteria.md`](0509-phase-2-gate-criteria.md), `../project/roadmap.md`, `../project/implementation-plan.md` |

## TL;DR (for a layperson)

The project advances through phases — Phase 1 (mesh + mgmt
plane), Phase 2 (data fabric pilot), Phase 3 (production
rollout), Phase 4 (post-production hardening). Each phase
ends with a gate that asks specific questions before the
next phase starts. Without the gates, phases blur and we
end up committed to things we haven't actually validated.

## Context

Project planning at our scale has two failure modes:

1. **No phase structure.** Everything is in flight; nothing
   is decided; momentum dissolves into endless rework.
2. **Phase structure but no gates.** Phases blur; later
   phases consume earlier phases' incomplete work; debt
   accumulates.

Phase gates with named questions force the answer "is
phase N actually done?" to be specific.

## Decision

Phase gates as named decision points:

| Phase end | Gate | Source |
|---|---|---|
| **Phase 1 → 2** | Mesh + mgmt plane working at hadev + integdev. | Wave 1 + Wave 2 evidence ([`../operations/goat-ha-dev-validation.md`](../operations/goat-ha-dev-validation.md)). |
| **Phase 2 → 3** | Three questions on Zenoh ([`0509`](0509-phase-2-gate-criteria.md)). |
| **Phase 3 → 4** | Production rollout completed at first sites; first SBU-E submission lands. |
| **Phase 4 → 5** | Multi-site federation + first partner authorizations. |

Every phase gate produces:

- **Named evidence** — test results, soak evidence,
  partner sign-offs.
- **Decision artifact** — pass / fail / partial-pass; if
  partial, what's deferred.
- **Inheritance into accredited track** ([`0102`](0102-two-track-development.md))
  — phase gate is also the cATO inheritance event when
  applicable.

Phase tracking: `../project/roadmap.md`
+ `../project/implementation-plan.md`.

## Alternatives considered

- **No phase gates.** Implicit momentum; wrong things ship.
  Rejected.
- **Continuous flow without phases.** Useful at the work
  level (sprints / blocks); not at the architecture level
  where gates produce strategic decisions.

## Consequences

**Positive.** Decisions are forced and recorded. Inheritance
discipline gets cATO benefit. Sponsor can ask "what phase are
we in?" and get an answer.

**Negative.** Gate discipline costs effort. Acceptable.

## Assessor view (SCA-R)

Phase gates serve CA-7 (continuous monitoring — inheritance
events), CM-3 (change control — phase transitions are
tracked changes), and CA-2 (security assessment — phase
gates produce assessable evidence).

## Trigger conditions for revisit

- A phase gate is missed for two consecutive cycles.
  Investigate; do not silently extend.
- A phase gate proves to be the wrong question. Revise
  explicitly via supersession.

---

*See `GLOSSARY.md` for terms and acronyms.*
