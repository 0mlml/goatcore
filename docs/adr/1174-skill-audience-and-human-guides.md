# ADR 1174 — Skills declare an audience, and human-facing skills gain a guide that points at the canon

- **Status:** **Proposed** (2026-09-10, operator). Docs and skills only; nothing in a live goatnet changes on merge.
- **Date:** 2026-09-10
- **Deciders:** operator
- **Related:** [`skill-audience-model.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/skill-audience-model.md) (the pattern in full) · [ADR 1132](1132-goat-gym-training-surface.md) (the gym, where guides publish) · [Diátaxis](https://diataxis.fr)

## Context

Skills (`​.claude/skills/<name>/SKILL.md`) are written for an agent. The set is growing, and some skills describe work a person also does or supervises while others only choreograph the agent's own process. Nothing recorded which was which, so there was no principled place to put a human counterpart — and the naive counterpart, a hand-written "human version" of a skill, is a second "THE procedure" that drifts from the first, violating doctrine P1.

## Decision

**Every skill declares an `audience` of `ai` or `both`, and every `both` skill carries a `GUIDE.md` that points at the one canonical doc rather than restating it.**

1. **`audience` frontmatter, two values.** `ai` — the skill only choreographs the agent's own workflow, no human counterpart. `both` — the skill wraps a real-world capability a person can perform or supervise. The discriminator, applied per skill: *would a person ever do this, or need to understand it to oversee it?*
2. **`GUIDE.md` is a doorway, not a document.** Where a canonical runbook or design doc already exists, the guide is a thin pointer to it. Where none exists, the human how-to is authored once and the guide points at that. `SKILL.md` and `GUIDE.md` are both thin wrappers over one source; a guide that copies the canon is the anti-pattern.
3. **Diátaxis naming.** The design doc / ADR / runbook is *reference + explanation*; `SKILL.md` is the automation's *how-to*; `GUIDE.md` is the human *how-to / tutorial*. Guides publish through the gym (ADR 1132), the *tutorial* quadrant, not a new site.
4. **CI enforces it.** `ops/ci/check-skill-audience.py` fails a `SKILL.md` with no valid `audience`, and a `both` skill with no `GUIDE.md`.

Initial classification: `ai` — `iso`, `closeout`, `bus`, `design-work`, `handoff`, `inchworm`, `claude-md-frugality`; `both` — `env-door`, `fastlane`, `goatnet-standup`, `lab-site-net`, `graduate`, `goat-cli`, `record`.

## Consequences

- A human reaching for an operational capability has a named entry-point, and it always lands on the single source, so it cannot drift.
- One new field per skill, one new file per `both` skill, and one CI check. The reference instance is `lab-site-net` — the only `both` skill with no prior doc, which proves the "authored once" branch.
- The gym gains task-level guides to assemble into role lanes without a second publishing system.

## Alternatives considered

- **A human package per skill, copied from the skill.** Rejected: a second source of truth that drifts (P1).
- **No tag, decide case by case.** Rejected: the decision isn't recorded, so the gap can't be checked and the set can't be bucketed as it grows.
- **A standalone human docs site.** Rejected: the gym already hosts per-role human material with an outside-reader gate; a parallel site duplicates that machinery.
