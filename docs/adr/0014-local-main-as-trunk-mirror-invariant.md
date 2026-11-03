# ADR 0014 — `[multi-agent-orchestration]` Local-main-as-trunk-mirror invariant — bounded unpushed-commit budget + commit-time DCO/lint

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-05-06 |
| Repo scope | Multi (every repo where local main is shared across worktrees / sessions) |
| Supersedes | — |
| Superseded by | — |
| Related | [`0006-multi-agent-human-concurrency-governance.md`](0006-multi-agent-human-concurrency-governance.md), [`0011-cluster-worker-execution-environment.md`](0011-cluster-worker-execution-environment.md), [`0013-master-worktree-read-only-by-convention.md`](0013-master-worktree-read-only-by-convention.md), [`0109-git-collaboration-trunk-based.md`](0109-git-collaboration-trunk-based.md), `../project/implementation-plan.md` Block 66 |

## TL;DR (for a layperson)

Most git workflows assume your local copy of `main` is roughly the same as the public copy on GitHub — maybe one or two commits ahead while you're typing. Tools like `git rebase` are designed around that assumption. If your local `main` is 25 commits ahead of GitHub because nobody has pushed yet, the same `git rebase` rewrites 25 commit IDs instead of one — and any other agent looking at the same local copy sees its history change underneath it. This ADR makes a rule: local `main` is a *mirror* of the GitHub copy, not a staging area. Anything that's not pushable yet (because it's missing a sign-off, fails lint, etc.) shouldn't be sitting on `main` — it belongs on a feature branch with a PR. The two enforcement teeth: (1) cap unpushed commits at 2; (2) run the DCO + lint gates at commit time, not push time, so unpushable commits never land on `main` in the first place.

## Context

The 2026-05-06 PR-#2-merge-rebase incident had local `main` 25 commits ahead of `origin/main`, with all 25 missing Signed-off-by trailers and one introducing golangci-lint failures. The drift was the *amplifier*: the rebase rewrote 25 SHAs instead of 0-2, and the structural blockers (DCO + lint) meant nobody could push the drift to remediate it. Local `main` was being used as a deep staging queue, not a trunk mirror.

[ADR 0109](0109-git-collaboration-trunk-based.md) is the trunk-based-development decision but it operates at the *push* layer (PR-gated merges, branch protection once threshold trips). It says nothing about local-main hygiene *between* commits and pushes. The implicit assumption — local main ≈ origin/main — is what makes the entire ecosystem of git tooling safe to use. When the assumption is silently false, ordinary operations become high-blast-radius.

Block 66 is operationalizing branch-protection-readiness. This ADR is the missing companion: branch protection is a *push-time* gate; this ADR is the *commit-time* discipline that makes the push-time gate trustworthy.

The 2026-05-06 incident was triggered by an unrequested `git rebase`, but the *severity* came from the drift. With ≤2 unpushed commits the same rebase rewrites ≤2 SHAs and is barely noticed. The drift is the multiplier on every category of mistake.

## Decision

Local `main` is a **mirror of `origin/main`** with a bounded staging budget. Two enforcement teeth:

**Tooth 1 — Bounded unpushed-commit budget on `main`.**

| State | Limit | Action when exceeded |
|---|---|---|
| At rest (between operator-driven sessions) | 0 unpushed commits | Operator pushes or rebases-into-feature-branch before walking away |
| During an active session | ≤2 unpushed commits | Push or open a PR before adding a third |

Three+ unpushed commits is a smell that the work belongs on a feature branch, not directly on `main`. The pre-PR-protection convention of "commit directly to main" doesn't change — it just acquires a budget.

**Tooth 2 — Commit-time DCO + lint gates.**

Pre-commit hook (configured per-repo) runs:

| Gate | What it catches | Failure behavior |
|---|---|---|
| DCO check (Signed-off-by trailer present) | Missing `-s` flag | Block commit; tell operator to use `git commit -s` |
| `golangci-lint` on staged Go files | gosec / errcheck / unconvert violations introduced in this commit | Block commit; show specific findings |
| `shellcheck` on staged shell files | Per Block 50F.4 expansion | Block commit |
| `bats tests/build.bats -f "no credentials"` on staged tracked files | Cred leak per the existing test | Block commit |

Each gate is structurally local and fast (<5s for incremental check). Skipping is forbidden per CLAUDE.md ("NEVER skip hooks") — if a gate is wrong, the gate gets fixed, not bypassed.

**The principle.** If the gate would block a `git push` later, the gate must block the `git commit` *now*. Otherwise the commit lives on `main` as a structural blocker that nobody can push, accumulates drift, and amplifies the blast radius of every subsequent operation.

**Drift recovery.** If local `main` is already over-budget when this ADR lands, the recovery is `git rebase --signoff origin/main` + per-commit lint cleanup (commits introducing lint failures are amended; those that don't are passed through). Operator-fired, in the master worktree under soft-lock per [ADR 0013](0013-master-worktree-read-only-by-convention.md). Done once; the ongoing discipline keeps it from recurring.

**Composition with [ADR 0109](0109-git-collaboration-trunk-based.md):** that ADR governs *what merges to main* (PR-gated, signed, conventional commits). This ADR governs *what stays on local main* (bounded, push-ready, mirror-shaped). The two are orthogonal and stack.

**Composition with Block 66:** Block 66's branch-protection-readiness is the push-time gate. This ADR is the commit-time gate. Together: a commit is local-mirror-clean → push succeeds → branch-protection rule passes → merges to origin/main as a real PR (post-Block-66-flip).

## Alternatives considered

- **Status quo (unbounded local-main drift, push-time gates only).** Rejected — incident evidence + structural blast-radius argument. Drift is the amplifier; capping drift is the lever.
- **Push every commit immediately.** Rejected — too rigid; sometimes operator intentionally batches 2-3 related commits before pushing. Budget ≤2 is the compromise.
- **Run gates at push time only.** Rejected — that's what we have today, and it failed. Push-time-only means structural blockers accumulate on local main between gate runs.
- **Run gates at session-end time.** Rejected — same problem as push-time-only; the blast radius window is open between session-end events.
- **Allow the drift but instrument it (warn at >5 unpushed).** Rejected — warnings without enforcement become noise. The bound has to be enforced or it's not a bound.
- **Eliminate direct-to-main commits entirely (force PRs for everything).** Considered, deferred to Block 66 trigger. Once branch protection is on, this is automatic. Until then, this ADR provides the equivalent discipline at commit time.

## Consequences

**Positive.**
- Blast radius of any future ref-rewriting operation on `main` is bounded to ≤2 SHAs.
- Push at any moment is structurally safe; no "I'd push but it's blocked by hidden gates."
- Pairs cleanly with [ADR 0109](0109-git-collaboration-trunk-based.md) and Block 66 — commit-time + push-time gates compose into a coherent contract.
- Operator gets DCO + lint feedback at the moment of commit, not minutes later when CI fails.

**Negative.**
- Pre-commit hook adds 1-5 seconds per `git commit`. Acceptable.
- Operator who wants to commit a WIP "I'll fix the lint later" must commit on a feature branch instead. This is the right friction.
- Existing 25-commit drift requires a one-time recovery (rebase --signoff + lint cleanup) before the ADR's discipline is achievable.

**Residual risk.**
- Pre-commit hook misconfigured per-clone — gate doesn't fire, commits accumulate as drift. Mitigated by a `make verify-hooks` target that operator runs at clone-time.
- A gate has a bug and falsely blocks a valid commit — surface as F-NNN finding; fix the gate; never `--no-verify` to bypass.
- Operator legitimately needs to commit-and-immediately-push N>2 related commits (e.g. landing a sequenced refactor) — push the first 2, then continue. Workflow-friendly.

## Assessor view (SCA-R)

This ADR sits in **CM-3** (configuration change control — commits to `main` carry pre-commit-validated trailers + lint), **AU-2** (auditable events — Signed-off-by trailer is part of the audit chain), and **SI-7** (software / firmware integrity — gate enforcement on integrity-relevant code). No CMVP-relevant crypto change. The DCO trailer requirement is project-policy, not regulatory; aligns with Linux Foundation DCO 1.1.

## Trigger conditions for revisit

- Branch protection on `main` flips on (post-Block-66 readiness fire) → some of this ADR's discipline becomes automatic at the push layer; revisit which gates remain commit-time-only.
- Commit-time gate latency exceeds 10s consistently → optimize gates or move expensive checks to push-time.
- A new gate class becomes load-bearing (e.g. license scan per `license-audit-ci-spec.md`) → add to the commit-time set.
- Operator workflow surfaces a legitimate need for >2 unpushed commits at rest (e.g. coordinated release prep) → revisit budget; consider per-branch budget config.

---

*See `GLOSSARY.md` for terms and acronyms.*
