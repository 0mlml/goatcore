# ADR 1177 — Graduation is never automated; the daily review is

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-09-10 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | ADR 1120 (release ladder), ADR 1127 (fastlane), ADR 1173 (release candidates), ADR 0006 (concurrency governance); F-514 |

## TL;DR (for a layperson)

A change reaches production only when a named person runs the command, having looked at the evidence. No timer, workflow or agent will ever do that step, because a change that reaches production while nobody is watching is exactly the risk the release ladder exists to prevent. What is automated instead is the looking: one command shows everything that is waiting, owed or ready, with the exact next step, and developers run it at the start and end of every working day.

## Context

The ladder (ADR 1120) moves a candidate through dev, preprod and prod with a soak of at least one day on preprod, and fastlane (ADR 1127) skips the soak against a recorded debt. Both leave a record whose next step falls to a person after time has passed. Nothing marked that moment: `graduate.py status` graded a fastlaned component `aligned`, `check` had no rule about an open hotfix record, and the readiness view on the consistency console is regenerated only when the console is deployed. On 2026-09-10 the first run of a review over main found six open records nobody was watching, two of them ladder candidates open since August with empty evidence and no acknowledging operator, three of them fastlane debts from four days earlier.

The obvious remedy, a timer that graduates a candidate when its soak elapses, was considered and refused by the operator: it would put a change on production without a person having looked at it.

## Decision

1. **No automation graduates, folds or deploys to production.** `graduate.py graduate`, `rc.sh --from prod` and `fastlane.sh` are run by a named person, recorded in `--ack`. No workflow, timer, hook or agent invokes them. This includes agent sessions: a Claude session may run the review and may prepare evidence; it runs a graduation only on the operator's explicit instruction in that session.
2. **The looking is automated.** `ops/release/graduate.py review` lists every open record sorted by what a person must do: ACT NOW (every gate passes), SOAKING (the runnable date and the evidence to fill first), BLOCKED (what is owed and, for a fastlane debt, the day CI goes red), ATTENTION (undeclared skew, a `check` failure). Each line carries the exact command. `ops/scripts/daily-review.sh` wraps it with a stale-tree warning, the developer's tracks and open PRs.
3. **The cadence is part of the delivery process.** Every developer runs the daily review at the start of the day and acts on ACT NOW, and again before closing, when a record they opened must show its evidence filled. The `/daily-review` skill makes a session do the same. CONTRIBUTING.md and the delivery handbook carry the ceremony; `release-graduation.md` §9 is canonical.
4. **What can still fail closed, does.** `check` C9 refuses a tree with a fastlane debt older than three days and no validation evidence; that is a backstop for a missed review, not a substitute for it.

## Alternatives considered

- **Auto-graduate when the soak elapses.** Refused: prod changes unwatched. The soak is a window in which a person looks; ending it by clock defeats it.
- **Alerts only (Prometheus rule to the pager on a soaked candidate).** A push without the list to act from; developers would still open the tool to see what else is owed. Kept as a possible addition once the daily review has run for a while; not required by this decision.
- **Regenerate the console's readiness page on a timer.** Useful, and not enough: a page nobody is obliged to open is where the six records were already visible in principle. The ceremony is the fix; the page can follow.

## Consequences

- A soaked candidate can wait at most one working day plus the review, and the wait is a person's decision, visible on the bus.
- Six records surfaced on the first run need owners now; the bus post of 2026-09-10 names them.
- The review is read-only and cheap; running it twice a day costs seconds. Running it from a stale checkout reviews yesterday's ladder, which the script says out loud.
