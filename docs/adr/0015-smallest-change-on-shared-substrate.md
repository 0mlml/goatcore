# ADR 0015 — `[multi-agent-orchestration]` Smallest-change-satisfying-the-ask default for agents on shared substrate

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-05-06 |
| Repo scope | Multi (every repo where agents act on shared substrate — git refs, shared working trees, coordination files) |
| Supersedes | — |
| Superseded by | — |
| Related | [`0006-multi-agent-human-concurrency-governance.md`](0006-multi-agent-human-concurrency-governance.md), [`0009-worker-discipline-mechanisms.md`](INDEX.md), [`0013-master-worktree-read-only-by-convention.md`](0013-master-worktree-read-only-by-convention.md), [`0016-ref-ownership-preflight.md`](0016-ref-ownership-preflight.md), [`1014-codex-pattern-adr-to-enforced-lint.md`](1014-codex-pattern-adr-to-enforced-lint.md) |

## TL;DR (for a layperson)

When an operator says something a little ambiguous like "make sure local and origin are up to date," there are usually several technically-correct things the agent could do — `git fetch` (just pull info), `git pull` (info + merge), `git rebase`, even `git pull --rebase`. The agent will be tempted to pick the most thorough one because "more thorough = better." On shared substrate (git refs other agents depend on, files other sessions are editing) that bias is wrong: the most thorough action also has the largest blast radius. This ADR makes the default explicit: pick the smallest action that satisfies the literal ask, and tell the operator what bigger options exist if they want to escalate. "Up to date" → `git fetch`, period. If the operator wants more, they say so.

## Decision in one sentence

On shared substrate, when operator language admits multiple interpretations of varying invasiveness, the agent defaults to the **least-invasive interpretation that satisfies the literal ask** and surfaces alternatives without executing them.

## Context

The 2026-05-06 PR-#2-merge-rebase incident: operator instruction was "make sure local and origin are up to date." Minimum-sufficient interpretation was `git fetch origin main` (zero side effects, updates the remote-tracking ref so `git log origin/main` shows current). The agent chose `git rebase origin/main` — broader interpretation, replayed 25 unpushed commits onto the new base, rewrote 25 SHAs visible to every other session sharing the clone, swept another session's uncommitted edits into a stash. The blast radius came from the interpretation choice, not from the operator's instruction.

This is not a one-off agent misjudgment — it's a structural bias. LLM agents tuned on "be helpful" lean toward thorough interpretations. The cost is asymmetric: thorough-on-isolated-substrate is fine; thorough-on-shared-substrate amplifies operator phrasing into operator action. Without an explicit default rule, the agent's prior toward thoroughness is the guarantee that the next ambiguous instruction produces the next incident.

## Decision

**Rule.** On shared substrate, the agent defaults to the smallest action satisfying the literal operator instruction, and surfaces larger interpretations for explicit operator escalation.

**What counts as shared substrate.** Any of:

- Git refs visible to other worktrees in the same clone (`refs/heads/*` other than the agent's own track branch; `refs/remotes/origin/*`)
- Files in the master worktree per [ADR 0013](0013-master-worktree-read-only-by-convention.md)
- Coordination files: `docs/project/active-work.yaml`, `~/.goat/session-bus/*`, `CLAUDE.md`, `docs/adr/INDEX.md`
- Production deployment artifacts: `terraform-prod/`, `terraform/`, `ops/ansible/inventory/`, anything under a hosts-file path that maps to a real machine
- The local-main branch ref per [ADR 0014](0014-local-main-as-trunk-mirror-invariant.md)

**What counts as ambiguous instruction.** Operator phrasing where multiple ranked interpretations exist. Common patterns:

| Phrasing | Default interpretation (smallest) | Larger interpretations to surface |
|---|---|---|
| "up to date" | `git fetch` | `pull --rebase`, `pull --merge`, `rebase`, `reset --hard` |
| "clean up" | dry-run / list-only of what would change | `rm`, `git clean -fd`, `prune`, `gc` |
| "sync" | `git fetch` + `git status` | `pull`, `rebase`, `push` |
| "fix the failing test" | minimal diff that makes the named test pass | broader refactor of the test class or production code |
| "merge the PR" | `gh pr merge --squash` (the chosen merge style per [ADR 0109](0109-git-collaboration-trunk-based.md)) — does NOT include local rebases or pushes that the operator didn't ask for | merge + local rebase + push of follow-on work |
| "deploy" | dry-run / plan-only output | `terraform apply`, `ansible-playbook` against prod |
| "make local match" | `git fetch` (so local refs *know* about remote state) | `git reset --hard origin/X` (rewrites local) |

**The agent's response shape.** When the agent recognizes a smallest-vs-larger interpretation choice on shared substrate:

1. State the smallest interpretation it's about to execute, in one short sentence.
2. List the larger interpretations available, in one line each, without executing.
3. Execute the smallest interpretation.
4. Report results.

If the operator wants a larger interpretation, they escalate explicitly. The agent does NOT escalate on its own initiative for shared-substrate operations.

**Codex-pattern enforcement (per [ADR 1014](1014-codex-pattern-adr-to-enforced-lint.md)).** This rule is enforced via:

- A guidance entry in `CLAUDE.md` (the always-loaded substrate) — primary mechanism.
- A pre-action check in any future `work` CLI subcommand that wraps shared-substrate ops — secondary mechanism for the operations that flow through the CLI.
- An operator-facing reminder in [`docs/operations/cluster-worker-worktree-runbook.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/operations/cluster-worker-worktree-runbook.md) for the worktree workflow specifically.

The rule does NOT apply to isolated substrate. In a per-session worktree on a feature branch, the agent's bias toward thoroughness is fine — blast radius is bounded to the session's own work.

## Alternatives considered

- **Status quo (no rule, agent uses judgment).** Rejected — incident evidence. The agent's judgment on shared substrate trends toward thoroughness, which is the wrong trade.
- **Always require operator confirmation for shared-substrate ops.** Rejected — too chatty; conflicts with the project's low-chat constraint and `feedback_permissions` ("Run autonomously"). The smallest-change rule preserves autonomy on the safe path.
- **Separate ADR per common phrasing pattern.** Rejected — would proliferate ADRs without adding clarity. One umbrella rule + a phrasing table is right-sized.
- **Encode the rule as a tool-use guard (block `git rebase` etc. without explicit phrasing).** Considered, deferred. The substrate-level guard is [ADR 0016](0016-ref-ownership-preflight.md)'s pre-flight check — it catches the symptom (cross-worktree ref rewrite) regardless of what triggered it. This ADR catches the upstream cause (interpretation choice).
- **Apply the rule on isolated substrate too.** Rejected — would over-constrain agent autonomy where the cost is not asymmetric. Per-session worktrees on feature branches are exactly where thoroughness is fine.

## Consequences

**Positive.**
- The 2026-05-06 incident class — agent picks larger interpretation, blast radius hits other sessions — becomes a rule violation, not a judgment call.
- Operator retains ability to escalate; the rule reduces friction by making "default = small" the load-bearing assumption.
- Composes with [ADR 0016](0016-ref-ownership-preflight.md): if the smallest interpretation still touches a shared ref, the pre-flight check fires.
- Reduces token churn: smaller actions need shorter explanations.

**Negative.**
- Operator may need to escalate explicitly on every "I really do want the thorough version." Acceptable friction; the rule's value is on the default, not the override.
- Agent may surface interpretations the operator didn't care about. Acceptable noise; one-line each.

**Residual risk.**
- Phrasing ambiguity is itself ambiguous — what one operator considers "obviously smallest" another considers "obviously thorough." Mitigated by the phrasing table; expand as edge cases surface.
- Agent fails to recognize an instruction as shared-substrate-relevant. Mitigated by the "what counts as shared substrate" enumeration above; expand as edge cases surface.

## Assessor view (SCA-R)

This ADR sits in **AC-3** (access enforcement — agent's default-action discipline limits blast radius on shared resources) and **CM-3** (configuration change control — small-default reduces unintended ref mutations). No CMVP-relevant crypto change. No data-handling change.

## Trigger conditions for revisit

- A class of operator phrasing repeatedly produces the wrong default interpretation → expand the phrasing table.
- The "what counts as shared substrate" list grows unwieldy → factor into a per-domain sub-ADR.
- Codex-pattern enforcement of this rule (via `work` CLI guards) becomes the dominant mechanism → rebalance ADR text toward the enforcement, away from operator-facing prose.
- A new tool class (non-git agent action — terraform / ansible / kubectl) starts producing the same incident shape → broaden the ADR's scope from git-specific to action-class-general.

---

*See `GLOSSARY.md` for terms and acronyms.*
