# ADR 0012 — `[multi-agent-orchestration]` Session-bus event layer — append-only JSONL + UserPromptSubmit hook + `work bus` CLI

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-05-06 |
| Repo scope | Multi (every repo running multi-agent clusters) |
| Supersedes | — |
| Superseded by | — |
| Related | [`0006-multi-agent-human-concurrency-governance.md`](0006-multi-agent-human-concurrency-governance.md), [`0011-cluster-worker-execution-environment.md`](0011-cluster-worker-execution-environment.md), [`0013-master-worktree-read-only-by-convention.md`](0013-master-worktree-read-only-by-convention.md), [`0016-ref-ownership-preflight.md`](0016-ref-ownership-preflight.md), [`../design/session-bus.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/session-bus.md), [`../design/multi-agent-team-management.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/multi-agent-team-management.md) |

## TL;DR (for a layperson)

When two agents are working on the same project at the same time, they currently can't tell each other "I'm about to do something that might affect you." [ADR 0006](0006-multi-agent-human-concurrency-governance.md)'s `active-work.yaml` records *who picked up what task* — that's slowly-changing state. What's missing is a fast event channel: short messages that say "I'm typing in this file right now" or "I just merged that PR." This ADR adds a tiny append-only log (one line per event), plus a hook that quietly checks the log when you submit a prompt and only tells the agent about the events that overlap with its current work. No background polling, no chat between agents, no extra infrastructure — just a file + a hook + one new command on the existing `work` CLI.

## Context

The 2026-05-06 PR-#2-merge-rebase incident is the proximate trigger. Two sessions were active in the master worktree concurrently; one ran `git rebase` while the other was mid-edit on uncommitted files. Stash + rebase succeeded mechanically but the operating session had no way to learn the master worktree was in active use *before* the destructive operation began. The blast radius would have been zero with a one-line broadcast like `{from:other-session, scope:["docs/project/active-work.yaml","CLAUDE.md","ops/ansible/roles/keepalived-mgmt/*"], msg:"actively editing"}` injected as a system reminder before my next turn.

[ADR 0006](0006-multi-agent-human-concurrency-governance.md) governs slowly-changing track-ownership state via `docs/project/active-work.yaml`. That file is checked into git and changes on the order of pickups/releases (minutes to hours). It is the wrong substrate for in-flight signals like "I am about to mutate `main`" or "I just stashed your working tree, here is the stash ref." Trying to encode events as state mutations on `active-work.yaml` would make every working-tree edit trigger a coordination commit — chatty, slow, and a moving target for downstream rebases.

What's needed is a complement: append-only, low-latency, scope-tagged, with bounded reader cost. The reader-side cost concern is specific to LLM agents: any "check on every action" mechanism has to avoid burning prompt-cache tokens on irrelevant chatter, and any "background poll" mechanism has to avoid spinning between turns when the agent is idle waiting for the operator.

## Decision

Adopt an **append-only JSONL session-bus** with three load-bearing pieces:

1. **The bus itself** — `~/.goat/session-bus/<clone-id>/<YYYY-MM-DD>.jsonl`. One JSON object per line. Outside the repo (gitignored at any in-repo location too). Per-clone scoped because different clones of this repo on the same machine are independent coordination domains.
2. **The `work bus` CLI** — extends `ops/work/work` with `bus warn|info|urgent --scope <paths-or-tracks> --msg "..."` (writer) and `bus tail [--since <cursor>]` (reader). Atomic append via single-`O_APPEND` write per ADR 1009-style filesystem discipline.
3. **The UserPromptSubmit hook** — `.claude/hooks/session-bus-tail.sh`, invoked by Claude Code on every user prompt. Reads entries newer than this session's cursor (`~/.goat/session-bus/<clone-id>/<session-id>.cursor`), filters by scope-overlap against the current session's worktree path + currently-checked-out branch + paths touched in the last N commits, and emits filtered entries as system-reminder context for the next turn. Cursor advances only on successful read.

**State vs events split (load-bearing).** `active-work.yaml` keeps its existing role: who holds what track. The bus adds: what is happening *right now* and what *just happened* that someone else needs to know about. The two are not redundant — `active-work.yaml` answers "who can I bother?", the bus answers "what is bothering me?".

**Three severity tiers, three injection rules.**

| Severity | Injection rule | Operator notification |
|---|---|---|
| `info` | Inject only on exact scope match (file path, track name, branch ref) | None |
| `warn` | Inject on loose scope match (parent dir, sibling track in same cluster, ref's reflog ancestor) | None |
| `urgent` | Always inject regardless of scope; route to operator via existing per-site ntfy substrate ([ADR 0010](0010-boundary-classification-escalation-and-medium.md) §"escalation medium") | ntfy push to operator |

**Cost model (bounded by construction).**
- Writer-side: one CLI call, one filesystem append. Microseconds.
- Reader-side: one hook invocation per UserPromptSubmit (a prompt the operator was already going to submit). Inside the hook: one `tail`-like read since cursor, scope-match filtering in-process, system-reminder emission only on overlap. No tokens consumed when there are no relevant new entries.
- Between turns: zero. No background poll, no daemon, no listening overhead.

**Failure mode discipline.** If the hook fails (script error, missing dir, permission), the session continues without the warning. The broadcast still durably exists in the JSONL log for post-hoc reconstruction. Failure mode is *degraded coordination*, not silent corruption.

Full mechanics live in the design doc at [`../design/session-bus.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/session-bus.md). Implementation tracked under Block 48N.

## Alternatives considered

- **Encode events as state mutations on `active-work.yaml`.** Rejected — chatty, every edit becomes a commit, downstream rebases churn, conflates state with events.
- **Central daemon / MCP server with subscriber state.** Rejected for v1 — extra infra, single point of failure, doesn't add capability over the file+hook pattern. Reconsider if multi-clone coordination across machines becomes load-bearing.
- **Reply threads on the bus.** Rejected — `active-work.yaml`'s `track_status_update` field already covers async response. Two-way conversation belongs in the slow-state file, not the fast-event log.
- **Listen-via-background-poll.** Rejected — burns tokens between turns; conflicts with the project's low-chat constraint.
- **Use Matrix per-site or ntfy as the primary substrate.** Rejected for the bus itself (Matrix needs explicit listening; ntfy is one-way push). Both remain valid for the *operator-notification* path on `urgent` events.
- **OS-level filesystem watch + harness injection.** Rejected — requires harness changes; the UserPromptSubmit hook achieves the same effect with substrate already shipped by Claude Code.

## Consequences

**Positive.**
- The 2026-05-06 incident class becomes detectable at the moment of action.
- Composes with — does not replace — `active-work.yaml`; both can be read independently.
- Operator-notification via existing ntfy substrate; no new alerting infra.
- Reader cost is zero between turns and zero when no overlap; sender cost is one CLI call.
- Durable log enables post-hoc reconstruction even when the hook misfires.
- Pattern is per-clone but cross-machine generalization is a flag flip (shared bus path) if it becomes load-bearing.

**Negative.**
- Honor-system writer discipline: an agent that doesn't `work bus warn` before destructive ops produces silence, not a warning. Mitigated by codex-pattern lint ([ADR 0015](0015-smallest-change-on-shared-substrate.md) + [ADR 0016](0016-ref-ownership-preflight.md) require a bus warn for the operations they cover).
- Per-clone scoping means a session in clone A can't warn a session in clone B on the same machine. Acceptable — clones are different coordination domains by ADR 0006's posture.
- The hook fires on every UserPromptSubmit — adds ~tens of milliseconds latency. Acceptable for the value delivered.

**Residual risk.**
- Bus file growth — one JSONL per UTC day; logrotate-equivalent cleanup is part of the operator runbook.
- Cursor file corruption — if a session's cursor gets stale or wrong, it gets a flood of older entries as system reminders. Bounded recovery: delete the cursor file, session re-establishes on next prompt.
- Hook script bugs — addressed via shellcheck (Block 50F.4) and pilot harness (Block 48N.5).

## Amendment (2026-08-11) — as-built delta + delivery contract

**As-built divergences from the text above** (implementation in `ops/work/main.go` + `.claude/hooks/session-bus-tail.sh`): storage is `~/.claude/state/session-bus-<sha256-12>.jsonl` (single file, monthly rotation), not `~/.goat/session-bus/<clone-id>/<date>.jsonl`; events carry `level` + free-text `msg` + auto-captured `track`/`branch`/`cwd`/`session_id`, not the `--scope` token grammar; cursors are byte offsets, not timestamps. The scope-match algorithm was never built — injection filters by level, not scope.

**The write-only failure.** From 2026-05 to 2026-08 the hook existed but was never wired into `.claude/settings.json` — every session posted, none received; delivery happened only when the operator hand-pasted a message (observed 2026-08-11). The reader side of this ADR was effectively unimplemented.

**Delivery contract (now wired).** Posting to the bus is durable record, **not** delivery. Delivery = the reader session's `session-bus-tail.sh` firing at `SessionStart` (catch-up, last 24 h default) and on each `UserPromptSubmit` (incremental via per-session cursor `~/.claude/state/session-bus-cursor-<session-id>`). Injection policy: `urgent`/`warn` always; `info` capped to the 5 most recent (`GOAT_BUS_INFO_CAP`) with an omitted count; empty case = no output = zero tokens; own events never injected. Directed messages — `TO session <id>` / `TO track <name>` in the msg, canonically written by `work bus <level> --to session:<id>|track:<name>` — always inject for the addressee. New `work bus ack <ts>` emits a directed info event back at a message's sender (with a `ref` field) so senders can verify delivery. Subagents inherit no hook context; the parent session is the delivery point and must forward excerpts in spawn prompts. Regression fixture `.claude/hooks/tests/session-bus-tail-tests.sh`; contract narrative [multi-agent-team-management.md](https://github.com/usareur-af/goatdocs/blob/main/docs/design/multi-agent-team-management.md) §6.4.8.

## Assessor view (SCA-R)

This ADR sits in **AC-3** (access enforcement — informs agents about concurrent activity that may affect their access patterns), **CM-3** (configuration change control — bus events around ref-mutating operations are part of the change-evidence chain), and **SI-4** (information system monitoring — the bus IS a monitoring channel for in-flight agent activity). No CMVP-relevant crypto change. Bus contents may include scope paths revealing project structure — the bus file is operator-local and gitignored; not a deployment-secret surface.

## Trigger conditions for revisit

- Pilot harness (Block 48N.5) measures net-positive on at least 2 incident-prevention cases vs WITHOUT-bus baseline → flip Status to Accepted.
- Cross-clone or cross-machine session coordination becomes load-bearing → revisit per-clone scoping; consider shared bus path or central daemon.
- Volume of `urgent`-tier events causes operator notification fatigue → tighten the urgent-tier rubric or add per-operator preference filtering.
- Hook latency observed >100ms regularly → optimize scope-match algorithm or cache cursor in-memory between hook invocations.
- A different LLM harness becomes load-bearing without UserPromptSubmit hooks → port the read-side mechanism (write-side is harness-agnostic).

---

*See `GLOSSARY.md` for terms and acronyms.*
