# ADR 1190 — `[multi-agent-orchestration]` A cold-start marker resumes the next session by itself

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-10-27 |
| Repo scope | Multi |
| Supersedes | — |
| Superseded by | — |
| Related | [ADR 0839](0839-continuous-handoff-baton.md), [ADR 0013](0013-master-worktree-read-only-by-convention.md), [ADR 0006](0006-multi-agent-human-concurrency-governance.md) |

## TL;DR (for a layperson)

When a session stops with work unfinished, it leaves a small note in the repo. The next session that opens there reads the note once, deletes it, starts whatever was running before, and carries on with the work before anything else. Nobody has to remember restart steps after a break. Without a note, the check takes a few milliseconds and does nothing.

## Context

ADR 0839 carries a session's state across a break: the handoff baton commit holds the hypothesis, what was tried and the next step. What it does not do is start anything. After a break the operator was handed steps to remember: start the local services, check a PR, resume a paused queue (operator, 2026-10-27: "don't be producing bespoke custom processes that I need to personally recall and do on restarts; I'm too busy, I'll never remember it"). A session start hook can do those steps, if something tells it what they are, and the cost of checking must stay near zero for the many sessions that have nothing to resume.

## Decision

- **The marker** is `<repo>/.claude/coldstart.md`, gitignored, per machine. It has a `## Run` section (start-up commands safe to run unattended: local services only) and a `## Then` section (what the next session does first, in order; it points at the latest baton commit for state).
- **The module** `ops/scripts/coldstart` owns the marker: `check` (run at session start) and `leave` (write a marker).
- **Check and run.** The SessionStart hook `.claude/hooks/session-start-coldstart.sh`, matched to `startup|resume` only, calls `coldstart check`; a compaction or `/clear` inside a running session is not a new session and must not restart anything. No marker: it exits after one file test. A marker: it moves it to `.claude/coldstart.consumed/<UTC stamp>.md` before acting (it fires once; a second hook that loses the race does nothing), starts the `## Run` lines in the background with a log beside the consumed marker, and hands the `## Then` text to the session as `additionalContext`.
- **Leave behind.** `/closeout` leaves a marker when its verdict is Not clean or anything was stopped for the break; a session whose cold start could not finish re-arms it with what is left.
- **The boundary with ADR 0839:** state stays in the baton commit; the marker holds only what to start and a pointer to the baton. A marker never carries state that could drift from history.
- **Safety:** `## Run` never holds a send, merge, deploy, delete, or anything acting on people, devices or shared infrastructure; those are `## Then` steps the session performs with its own checks.
- Default-on; per-person opt out with `GOAT_COLDSTART=off` in `.envrc.local.d/`.

## Alternatives considered

- **Instructions in the handoff document for the operator to follow.** What existed; the operator does not and should not remember them.
- **State in the marker** (a `.claude/handoff/<track>.md` file). Rejected in ADR 0839 because it drifts from history; the marker carries only start-up actions and a pointer.
- **Always start services at session start.** Wrong for most sessions (inspection, docs) and costly; the marker makes it opt-in per break.
- **A user-level hook only** (`~/.claude/settings.json`). Works for one person; a project hook makes it the repo's behaviour for everyone.

## Consequences

- A break costs the operator nothing; the next session resumes in the time the start-up commands take.
- The check costs about 20-40 ms per session start without a marker (measured 2026-10-27).
- A marker fires once; consumed markers and their run logs stay under `.claude/coldstart.consumed/` for inspection.
- Risk: a `## Run` line that is not idempotent could misbehave on a repeat; the rule above keeps `## Run` to safe start-up commands, and the module runs them detached so a failure never blocks the session.
- Reversibility: remove the hook from `.claude/settings.json`; delete `ops/scripts/coldstart` and `.claude/skills/coldstart/`.

## Acronyms / glossary

- **Marker** — the one-shot note `.claude/coldstart.md` this ADR defines.
- **Baton** — the handoff commit of ADR 0839.
