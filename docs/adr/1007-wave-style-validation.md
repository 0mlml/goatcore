# ADR 1007 — Wave-style validation methodology

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`1003-cgnat-aware-validation-default.md`](1003-cgnat-aware-validation-default.md), [`1006-phase-gates.md`](1006-phase-gates.md), [`../operations/goat-ha-dev-validation.md`](../operations/goat-ha-dev-validation.md) |

## TL;DR (for a layperson)

We validate the system in named "waves" — Wave 1 (basic mesh
+ mgmt plane), Wave 1.5/1.6/1.7/1.8 (incremental hardening),
Wave 2 (HA + multi-site), Wave 3 (steady-state / soak +
recovery). Each wave has explicit scope, scripts, and
findings documents. This methodology lets us reuse
infrastructure across waves and produce reviewable evidence
each step of the way.

## Context

Without a structured validation methodology, ad-hoc test
runs accumulate without producing evidence assessors can
cite. With waves, each test run is named, scoped, and
documented.

Wave scaffolding (`ops/wave3/`) is reusable for future
ephemeral validation envs.

## Decision

Wave-style validation:

- **Named waves** with explicit scope:
  - **Wave 1**: basic mesh + mgmt plane (1.1 - 1.7
    sub-waves for incremental landings).
  - **Wave 1.8**: pgBackRest paired-stanza smoke test
    (`../operations/pgbackrest-wave18-validation.md`).
  - **Wave 2**: HA + multi-site (Spock + signal-server
    federation + coordinated upgrade).
  - **Wave 3**: steady-state / soak + recovery scenarios
    (§3.1 - §3.9 sub-tests on hadev).
- **Per-wave scripts** in `ops/wave<N>/`.
- **Per-wave findings doc** in `docs/reports/`.
- **Reusable scaffolding** — `ops/wave3/` scripts can be
  re-run on a future ephemeral env with the same shape.
- **Re-scoping** when sub-tests don't fit the original env
  (e.g. Wave 3 §3.2-§3.8 re-scoped to other envs after
  hadev retired).

Pairs naturally with phase gates ([`1006`](1006-phase-gates.md)):
each phase gate cites wave evidence.

## Alternatives considered

- **Ad-hoc validation.** Unreviewable. Rejected.
- **Single big-bang validation.** Loses the incremental
  evidence trail. Rejected.

## Consequences

**Positive.** Reviewable evidence per wave. Reusable
scaffolding. Phase gates cite specific waves.

**Negative.** Methodology discipline costs setup time.
Acceptable; pays back across waves.

## Assessor view (SCA-R)

Wave-style validation serves CA-2 (security assessment),
CA-7 (continuous monitoring), and SI-6 (security function
verification).

## Trigger conditions for revisit

- A wave class doesn't fit the methodology (e.g. one-off
  spike). Document the exception; do not abandon waves.
- Wave scaffolding becomes unmaintainable across many
  ephemeral envs. Refactor into a generic harness.

---

*See `GLOSSARY.md` for terms and acronyms.*
