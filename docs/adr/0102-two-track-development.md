# ADR 0102 — Two-track development: capability ahead of accredited

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | Multi |
| Supersedes | — |
| Superseded by | — |
| Related | `0100-cato-not-monolithic-ato.md`, [`1006-phase-gates.md`](1006-phase-gates.md) |

## TL;DR (for a layperson)

We deliberately run two versions of the project at any given time:
the version that is currently approved for official use, and a
version that is six to twelve months ahead of it where new
capabilities get developed and tested. The newer track flows into
the accredited track on a steady cadence. Without the forward track,
the accredited track ages out and stops being useful. Without
discipline in the inheritance, the forward track becomes shelfware.

## Diagram



Source: `diagrams/0102-two-track-development.mmd`.

## Context

Defense IT programs that approve a single configuration tend to
ossify around it. Real engineering happens out-of-band — in
exercises, MPE, coalition unclass environments, lab benches, partner
test events — but the accredited baseline does not benefit, because
those out-of-band runs do not flow back. Eventually the gap is so
large that the accredited version is no longer the version anyone
wants to deploy, and the program either re-accredits painfully or
the field abandons it.

The fix is to make the forward track an explicit, named workstream
with an explicit, named inheritance into the accredited track. Then
both tracks have governance, and the question "is this work worth
doing?" has a deterministic answer: "does it land in the next
inheritance?".

## Decision

goat operates on two tracks simultaneously.

| Track | Audience | Governance | Cadence |
|---|---|---|---|
| **Capability track** | Lab benches, exercises (MPE, coalition unclass), partner test events, validation envs (HA, integration, proof-of-concept, CGNAT validation) | This repo's `main` branch + ephemeral cloud envs | Continuous |
| **Accredited track** | Sites authorized for SBU-E / CMMC operation | Tagged releases that have cleared cATO pipeline gates | Quarterly inheritance from capability track, or sooner for security fixes |

Rules:

1. **Capability track lives in the same repo.** No fork, no shadow
   repo. Branches and ephemeral environments are first-class. The
   accredited track is a tag set, not a directory.
2. **Inheritance is explicit.** Every quarterly inheritance produces
   a named tag (`accredited-v<date>`) with a delta narrative naming
   what changed and citing the cATO pipeline evidence (signed images,
   scan results, test evidence) for each change.
3. **The capability track is always ahead.** If a quarterly
   inheritance produces no delta, that is a signal the capability
   track has stalled — investigate and either land work or trim
   scope.
4. **Security fixes can fast-path.** A pipeline-validated security
   fix can land on the accredited track outside the quarterly
   cadence, with a delta narrative of its own.
5. **Validation envs are how the capability track earns its
   inheritance.** The validation environments are not
   side projects; they are the quality gate the capability track
   passes through before inheriting.

## Alternatives considered

- **Single-track development.** Default in many DoD programs. Either
  the accredited version stagnates (because new work is too risky to
  ship) or every release re-runs the full accreditation gate
  (uneconomic). Rejected.
- **Forward fork in a separate repo.** Adds context-switching cost
  and makes inheritance a rebase exercise, which is where drift
  enters. Rejected.
- **No accredited track until the capability track is fully mature.**
  Means we do not deliver authorized capability for years. Rejected.
- **Capability track as a research-only artifact, never inherited.**
  Becomes shelfware. Rejected.

## Consequences

**Positive.** New work has a path to deployment. The accredited
track stays current. The two tracks can coexist in one repo without
forking. Validation environments earn their cost by being the
inheritance gate.

**Negative.** Quarterly inheritance discipline is real work. We
must maintain delta narratives, run the cATO pipeline against the
inheritance candidate, and sometimes back out work that does not
clear the gate. We accept the cost.

The discipline can fail silently if the inheritance becomes a rubber
stamp. We mitigate by requiring an SCA-R-readable delta narrative
per inheritance and by using the [`1006`](1006-phase-gates.md)
phase-gate framework to track whether quarterly inheritances are
actually shipping new capability.

## Assessor view (SCA-R)

The two-track model maps onto NIST SP 800-137 ISCM continuous
monitoring (the capability track) plus traditional configuration
management (the accredited track). NIST SP 800-128 CM-3 (change
control) is served by the explicit inheritance step; CM-4 (security
impact analysis) is served by the delta narratives.

For the assessor, the relevant artifact per inheritance is: the
named tag, the delta narrative, the cATO pipeline evidence (SBOM,
signed images, scan results), and the evidence from validation
envs (test reports, soak evidence). `0850`
formalizes this packaging.

## Trigger conditions for revisit

- Quarterly inheritance is missed for two consecutive quarters.
  Investigate root cause; do not silently extend the cadence.
- The capability track ships a change that the accredited track
  cannot accept (pipeline failure, scan failure, control regression).
  Treat as an inheritance reject; do not loosen the gate.
- A partner authorization model demands a different cadence
  (e.g. monthly, annual). Adjust the cadence; preserve the two
  tracks.

---

*See `GLOSSARY.md` for terms and acronyms.*
