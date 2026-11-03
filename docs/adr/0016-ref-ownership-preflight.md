# ADR 0016 — `[multi-agent-orchestration]` Ref-ownership pre-flight for cross-worktree writes

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-05-06 |
| Repo scope | Multi (every repo where multiple worktrees share a single `.git/`) |
| Supersedes | — |
| Superseded by | — |
| Related | [`0006-multi-agent-human-concurrency-governance.md`](0006-multi-agent-human-concurrency-governance.md), [`0011-cluster-worker-execution-environment.md`](0011-cluster-worker-execution-environment.md), [`0012-session-bus-event-layer.md`](0012-session-bus-event-layer.md), [`0013-master-worktree-read-only-by-convention.md`](0013-master-worktree-read-only-by-convention.md), [`0015-smallest-change-on-shared-substrate.md`](0015-smallest-change-on-shared-substrate.md) |

## TL;DR (for a layperson)

Multiple "worktrees" of one git repo all share the same git database underneath. That means if one agent runs `git rebase` on a branch, every other agent that has *that same branch* checked out in their own folder sees the history change underneath them. There's no equivalent of branch-protection inside a single clone — git assumes one human at a time. This ADR adds the missing safety net: before any operation that would rewrite a ref (commit ID), check whether that ref is currently checked out in any other worktree. If yes, pause and tell the operator — don't silently disrupt the other agent.

## Context

The 2026-05-06 PR-#2-merge-rebase incident: agent in the master worktree ran `git rebase origin/main` on `main`. Every other worktree in the same clone — three locked agent worktrees + two track worktrees — shares `.git/refs/heads/main`. The rebase rewrote 25 commit SHAs and the original SHAs ceased being ancestors of `main`'s new HEAD. Any of those other worktrees that had branches based on the original SHAs effectively saw a phantom history change.

GitHub's branch-protection mechanism prevents this kind of force-push at the *push* layer. There is no analog inside a single clone. Git itself doesn't refuse to rebase a branch checked out elsewhere because git assumes the operator knows what they're doing. With a single human operator that's reasonable; with multiple agent sessions sharing the clone, it's the wrong default.

The session-bus ([ADR 0012](0012-session-bus-event-layer.md)) catches the *announcement* — "I'm about to rebase main" — only if the actor remembers to broadcast. This ADR catches the *symptom* — "this rebase will rewrite a ref another worktree depends on" — at the moment of action, regardless of whether the bus was used.

## Decision

Adopt a **ref-ownership pre-flight check** that fires before any operation rewriting a local ref. If the ref is currently checked out in another worktree, the operation pauses and requires explicit operator confirmation.

**Pre-flight check algorithm:**

1. Identify the ref(s) the upcoming operation would mutate.
2. Run `git worktree list --porcelain` to get all worktrees in this clone.
3. For each worktree, parse its checked-out branch (`branch refs/heads/<name>` line).
4. If any *other* worktree has the target ref checked out → pre-flight FAILS.
5. On FAIL: print which worktree is holding the ref + which session if known; pause for explicit operator confirmation phrase ("yes proceed despite worktree X holding ref"); abort otherwise.
6. On PASS: emit `work bus info --scope <ref> --msg "<op> in <this-worktree>"` per [ADR 0012](0012-session-bus-event-layer.md); proceed.

**Operations covered.** Initial set (expand as edge cases surface):

| Operation | Refs mutated |
|---|---|
| `git rebase <upstream>` | currently-checked-out branch |
| `git reset --hard <ref>` | currently-checked-out branch |
| `git commit --amend` | currently-checked-out branch |
| `git pull --rebase` / `git pull` (with merge) | currently-checked-out branch |
| `git branch -f <name> <ref>` | named branch |
| `git update-ref` (any flavor) | named ref |
| `git push --force` (local-side) | local refs touched, but the push-side is also a remote concern |
| `git filter-branch` / `git filter-repo` | possibly many refs |

**Operations NOT covered (intentionally).** `git fetch`, `git checkout` (changes worktree contents not ref content), `git switch`, `git stash` (creates a new ref but doesn't rewrite existing ones), creating a new branch, normal `git commit` (advances HEAD by one — see "fast-forward exemption" below).

**Fast-forward exemption.** A normal forward-only commit on the agent's own branch is not "rewriting" — it's advancing. Pre-flight fires only on operations that change a commit's parent (which is what makes a SHA "rewrite" something the rest of the system saw).

**Implementation surface.** Two enforcement layers:

1. **CLAUDE.md guidance.** The rule lands in CLAUDE.md as an always-loaded behavioral constraint. Agents are expected to perform the check before invoking the listed operations.
2. **`work` CLI helper.** Add `work refcheck <ref>` that returns 0 (safe) or non-zero (held elsewhere, with details). Higher-leverage future: wrap dangerous git operations in `work git-rebase`, `work git-reset`, `work git-amend` that perform the check by construction.

The CLAUDE.md guidance is v1 (low cost, immediate). The CLI wrap is v2 (Block 48N.3 sub-block).

**Composition with [ADR 0013](0013-master-worktree-read-only-by-convention.md).** ADR 0013 says "don't do destructive ops in the master worktree." This ADR adds the *generalized* check: regardless of which worktree you're in, if the ref you're about to rewrite is held by another worktree, pause. The two ADRs catch overlapping but not identical cases.

**Composition with [ADR 0012](0012-session-bus-event-layer.md).** When pre-flight passes, the agent emits a bus `info` event so other sessions get the post-hoc record (and the next prompt of any affected session sees the operation as system-reminder context). When pre-flight fails, the agent emits a bus `warn` event before pausing for operator confirmation, so the holder of the conflicting worktree sees the contention.

## Alternatives considered

- **Status quo (no check).** Rejected — incident evidence. Default git behavior is one-human-at-a-time-and-they-know-what-they're-doing; that's not the multi-agent regime.
- **Hard-block via git hook.** Considered, deferred. Pre-receive hook on local refs is non-trivial because git's hook surface for ref-update doesn't naturally inspect other worktrees. Library-call wrap (`work` CLI) is simpler and reaches the same destination.
- **Block all rewriting operations on shared branches (not just held ones).** Rejected — too restrictive. If no other worktree holds the ref, rewriting is fine. The check is "is anyone else watching this ref?", not "is this ref ever shared?".
- **Use kernel `flock` on `.git/refs/heads/<name>`.** Rejected — git doesn't take this lock itself, so an opt-in flock would only protect against other opt-in actors. The cooperation surface needs to be at the action layer (the `work` CLI), not the filesystem layer.
- **Lean entirely on [ADR 0012](0012-session-bus-event-layer.md) + [ADR 0015](0015-smallest-change-on-shared-substrate.md).** Rejected — the bus is opt-in (you have to write to it before acting); the smallest-change rule is about interpretation, not about checking. This ADR is the substrate-level check that catches whatever the upstream rules miss.

## Consequences

**Positive.**
- The 2026-05-06 incident class — silent ref rewrite affecting unaware sessions — becomes a structural pause, not a silent disruption.
- Operator retains override ability via explicit confirmation phrase; rule isn't a hard wall.
- Composes with — does not replace — ADRs 0012/0013/0015.
- The check is mechanical and cheap (`git worktree list --porcelain` is O(worktrees), microseconds).
- The override path is auditable: operator's "yes proceed despite X" phrasing is logged.

**Negative.**
- Operator hits the pause when they legitimately want to rewrite a held ref (e.g. recovery from a known-bad rebase). One additional confirmation step.
- The check is honor-system at v1 (CLAUDE.md guidance) — an agent that ignores the rule and calls `git rebase` directly bypasses it. v2 (CLI wrap) closes that gap.
- Doesn't catch ref mutations done by tools the agent doesn't proxy through (`git`-binary direct invocations from a script). Reach is bounded by what the agent actually executes.

**Residual risk.**
- A worktree exists but is itself idle (no agent session active in it). Pre-flight still fails — agent has to confirm despite no real conflict. Acceptable false-positive rate; better to over-warn than under-warn.
- Pre-flight check itself races with another session checking out a branch. Race window is tiny and resolves on the next operation; acceptable.
- An operation that touches multiple refs (e.g. `git filter-repo`) is partially-checkable. Mitigated by enumerating the refs upfront and checking all of them.

## Assessor view (SCA-R)

This ADR sits in **AC-3** (access enforcement — checks whether the ref-mutating operation is access-coordinated with other concurrent agents), **CM-3** (configuration change control — adds a pre-action gate around ref rewrites), and **SI-7** (software / firmware integrity — protects ref integrity against unintended cross-worktree disruption). No CMVP-relevant crypto change.

## Trigger conditions for revisit

- v2 CLI wrap (Block 48N.3) lands → tighten this ADR's text to assume CLI wrap is the default invocation path; CLAUDE.md guidance becomes secondary.
- Pre-flight false-positive rate (warn-without-real-conflict) exceeds operator tolerance → tighten the "what counts as conflict" rubric (e.g. only fail if the holding worktree has been active in the last N minutes).
- A pattern of legitimate force-rewrites emerges (e.g. recovery operations) → add a recovery-mode bypass that the operator can invoke once with structured logging.
- Multi-clone or multi-machine ref coordination becomes load-bearing → revisit the per-clone scoping; consider remote ref-ownership signaling.

---

*See `GLOSSARY.md` for terms and acronyms.*
