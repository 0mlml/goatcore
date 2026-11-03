# ADR 0006 — Concurrency governance for multi-agent + human teaming on a shared trunk

| Field | Value |
|---|---|
| Status | Proposed (2026-04-30) |
| Date | 2026-04-30 |
| Repo scope | Multi |
| Supersedes | — |
| Superseded by | — |
| Related | [`0001-adr-template.md`](0001-adr-template.md), [`0002-adr-lifecycle.md`](0002-adr-lifecycle.md), [`0109-git-collaboration-trunk-based.md`](0109-git-collaboration-trunk-based.md) |

## TL;DR (for a layperson)

When several people work on the same code, they use git branches +
pull-requests + dev/test/staging/prod environments to stay out of
each other's way. That works at human speed (a few changes per day
per person). With multiple AI agents working alongside humans on
the same trunk, the throughput is much higher — agent-A and agent-B
can each touch ten files in an hour, and a human can touch another
ten in parallel. Branches alone don't solve this: half the agents
are working on `main` directly because that's where the live state
of the project lives, and even when they branch, they may not know
what other agents are mid-stride on. **We add a thin governance layer
above git** — a file that says "I'm currently working on this track,
here are the files I'm touching, here's when I started" — so any
new participant (agent or human) reads it first and avoids stepping
on in-flight work. We keep all the existing git discipline; this
just sits on top.

## Context

[ADR 0109](0109-git-collaboration-trunk-based.md) committed the
project to trunk-based, PR-gated, signed-commit git collaboration.
That model assumes:

- A handful of human contributors, each touching changes at human
  speed (minutes-to-hours per change).
- Most work happens on short-lived feature branches.
- PR review + CI is the conflict-detection mechanism.
- Merge conflicts are infrequent and resolved at PR time.

The reality of this project is different in three ways:

1. **Agent participation.** Claude Code sessions and other AI agents
   do substantive work — write code, edit docs, run tests, push
   commits. They operate at higher throughput than humans (10× to
   100× the file-touches-per-hour rate).
2. **Trunk-direct commits.** Per [ADR 0109](0109-git-collaboration-trunk-based.md)'s
   "small repo + few contributors" carve-out and the practical
   reality of the project's solo-author phase, much of the work
   lands directly on `main` rather than via PR + branch. PR-on-fork
   is used for upstream work; this repo's velocity has favored
   trunk-direct.
3. **Cross-window concurrency.** A single human contributor often
   has multiple Claude sessions running simultaneously, each focused
   on a different track (Block 33 relay rewrite + Block 35 video
   fabric + Block 21 identity work). Plus their own direct edits in
   editor and terminal.

The 2026-04-30 session that prompted this ADR hit four concrete
collisions in a single day:

- An agent and the user both editing `docs/findings/INDEX.md` to
  add new findings at the same time
- An agent and the user both editing `docs/project/validation-state.md`
  to update status of different blocks at the same time (Block 35
  video-fabric vs Block 33 relay-bench M-003b)
- An agent's commit landing on `main` between the user's edits and
  the user's commit, requiring a re-read mid-edit
- An agent partially-staging files for a commit while the user's
  other window had unrelated changes in the working tree

In each case, git's textual merge handled the bytes correctly. But
the WASTED effort — re-reading files mid-edit, re-doing analysis
that had already been done, double-staging — added up. The collisions
were *visible* (the editing flow paused) but only *after* the
parallel work had already happened.

The need: make in-flight work *visible upfront* so any new
participant sees the field of play before picking up a task.

## Decision

We add a thin governance layer above git: a single live-state file,
a CLI to manipulate it, and conventional hooks that nag (don't block)
when discipline slips. The existing git/PR/CI flow stays.

### Components

1. **`docs/project/active-work.yaml`** — the single source of truth
   for in-flight tracks. YAML (not markdown) because we're committing
   to programmatic hygiene; the format must be machine-readable for
   the CLI and the hooks.

2. **`ops/work/` — Go CLI** named `work`. Single static binary,
   matches the existing pattern at `ops/mgmt-host-bootstrap/`.
   Subcommands:
   - `work pickup <track> [--scope ...] [--eta ...]` — append to active-work
   - `work update <track> [--status ...] [--scope ...] [--note ...]`
   - `work release <track> [--final-commit ...]`
   - `work list [--owner ...] [--status ...]`
   - `work check-stale [--threshold 24h]`
   - `work whoami` — print the canonical owner string for this caller
     (git config user.email, or `claude:<session-id>` for agents)
   - `work commit <track> -m "..."` — convenience: stage + commit with
     the `[track: <name>]` trailer auto-appended

3. **Pre-commit git hook** at `ops/work/git-hooks/pre-commit` (removed 2026-09-08: it read the retired monolithic `active-work.yaml` and its install line overwrote the master-readonly hook; the trailer nag lives in `ops/git-hooks/pre-commit-track-trailer`). Warn-
   not-block. Checks:
   - Does the commit message contain a `[track: <name>]` trailer?
   - If yes, is `<name>` an active track in active-work.yaml?
   - If the committer has any active tracks, does at least one
     `[track: ...]` trailer reference one of them?
   - Are any of the committer's active tracks "stale"
     (no `last_activity_at` update in 24h)?
   On any failure: print a warning to stderr but exit 0. Discipline
   is the goal; blocking commits would slow throughput more than the
   collisions they prevent.

4. **Claude harness hooks** in `.claude/settings.json`. Two hooks:
   - `SessionStart`: read active-work.yaml's `tracks:` section and
     inject a brief summary into the session context. The agent sees
     "here are the 3 tracks currently held — pick a non-overlapping
     one or coordinate before stepping into one of these scopes."
   - `Stop`: remind the agent to either `work release <track>` if
     done, or `work update <track>` if pausing, before the session
     terminates.

5. **CLAUDE.md pointer** — one bullet under the existing process
   discipline section. New agents learn about the system by reading
   CLAUDE.md, hit the bullet, follow to ADR 0006 + the CLI README.

6. **Skill / runbook note** — small entry showing the
   pickup-work-update-release lifecycle. Lives at
   `ops/work/README.md`.

### When this layer applies (and when it deliberately doesn't)

The system is **opt-in coordination**, not a universal tax. The whole
point is speed with reasonable safety — not slowing down to genuflect
to a process before every keystroke.

**Pickup IS warranted when** the work has any of:

- Multi-commit, multi-file, or multi-hour scope
- Scope likely to overlap with what another agent or the human is
  doing (live tracker docs, `validation-state.md`, `INDEX.md` files,
  cross-block design docs)
- Long-running jobs or external systems involved (cloud env spin-up,
  multi-host orchestration, soak runs, integration tests against
  shared substrate)
- A handoff across sessions or actors (Claude session ends, human
  picks up next morning; agent-A starts the work, agent-B continues
  it)
- Anything where another participant landing in the same code
  *unaware* would cost re-work

**Pickup is NOT needed for** (deliberate ad-hoc carve-out):

- Read-only exploration — searching, reading code, asking questions,
  running `git log`, `cargo doc`, etc. No coordination needed for
  reads.
- Single-commit edits in a fresh corner — typo fixes, doc clarifications,
  one-file new captures (a new finding, a new ADR draft sitting on its
  own, a script in a fresh directory)
- New capabilities created from scratch in a previously-empty path —
  `mkdir new-thing/ && commit`. Until others start touching `new-thing/`,
  there's nothing to coordinate.
- Quick session-starts that begin from a query — the agent might end
  up doing work, in which case it picks up at the moment the work
  becomes track-shaped, not at the start of the session.
- Anything that takes <15 minutes and is plausibly invisible to other
  participants

**The promotion rule.** Start without pickup if the work is ad-hoc.
Promote to a tracked track at the first of: scope grew, second
commit landed, you're about to touch a live shared doc, you realize
the work spans >30 minutes, or you're about to start something that
will be visible / interrupted / handed off. The CLI makes mid-flight
promotion cheap: `work pickup <name> --scope "$(git diff --name-only)"`
captures what you've already touched and starts the tracking from
there.

**The pre-commit hook reflects this.** It warns when the committer
has *already* picked up an active track and is committing without a
trailer (forgot to link), or when a trailer references a non-existent
track (typo). It does NOT warn or block when you commit ad-hoc with
no track at all. Ad-hoc commits are a first-class normal mode.

**Explicit opt-out tag.** If you hold an active track but want to
make a genuinely-ad-hoc commit unrelated to it (e.g., a typo fix you
noticed mid-flight), include `[no-track]` (or `[ad-hoc]` as a
synonym) on its own line in the commit message. The pre-commit hook
treats either tag as an explicit "I know what I'm doing" signal and
suppresses the no-trailer nag. The bad-trailer warning still fires
even with `[no-track]` present — that's a typo catch independent
of opt-out.

**The SessionStart hook reflects this.** It tells the agent the
system exists + how to invoke it; it does *not* require pickup before
doing anything. An agent reading the hook output and deciding "this
is an exploratory query, no track needed" is the right call.

**Worked examples — when to pick up vs. not:**

| Scenario | Pickup? |
|---|---|
| User asks "what's our HEAD commit?" → run `git log -1` → respond | No (read-only) |
| User asks "create a new finding F-NNN about X" → write file → commit | No (single-commit fresh-corner) |
| User asks "fix typo in CLAUDE.md" → 1-line edit → commit | No (quick) |
| User asks "build out Block 35 v1" → multi-day, dozens of files | Yes (multi-commit, multi-hour, shared scope) |
| User asks "investigate F-016 mechanism" → reads → patches → tests → soaks | Yes (long-running, external systems) |
| User asks "run the 1h soak on the integration env" → kicks off + watches | Yes (long-running, would block other work on that env) |
| Agent starts as exploratory query, discovers a real bug, writes the fix | Pickup at the *bug-fix* boundary, not at session start |
| User asks "list active tracks" → `work list` → respond | No (the system itself is read-only here) |
| Hold an active track + notice + fix a typo elsewhere → commit with `[no-track]` tag | No (explicit opt-out — see below) |

### Conventions

**Track names** are short kebab-case strings, ideally namespaced by
the implementation-plan block they're part of:

```text
block-35-spike-A-full-soak
block-33-relay-rust-rewrite-step-3
m-003-force-quic-throughput
identity-architecture-21c-production
```

**Owner strings** distinguish humans from agents:

- Human: the contributor's handle (or their git config user.email)
- Agent: `claude:<session-id>` where `<session-id>` is short-form
  the agent picks at session start (e.g., `claude:2026-04-30-video`)
- Pair: `<handle>+claude:<session-id>` when human and agent are
  actively working on the same track in real-time

**Scope** is a list of file paths or directory globs. Doesn't have
to be exhaustive — covers what the owner *expects* to touch. The
hook uses scope only as informational warning input ("you're
committing changes to a path that's in another active track's
scope"), not as a blocker.

**Status values** (small, closed set):

- `active` — work is in progress, owner is responsive
- `blocked:<reason>` — owner has paused for an external reason
  (waiting on operator, env down, etc.)
- `review` — work is at a checkpoint, ready for review or pickup
  by a different actor
- `done` — work is complete; row gets archived to a "Recently
  completed" section after ~7d of dwell time

**Commit-message linkage** — the trailer format:

```text
feat(block-35): Spike A full PASS

[lots of body]

[track: block-35-spike-A-full-soak]
Signed-off-by: <name> <email>
```

The pre-commit hook reads `[track: ...]` trailers; the `work commit`
helper injects them automatically.

**Optional trailer — `[ctx-state: stretched]`** — set by an agent worker
that self-assesses as late-context (≥ ~70% by introspection, or after
auto-compaction has fired once during the session). Default state is
omitted (warm). Routes the commit / PR to a differentiated-review path
per [`design §6.4.7`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/multi-agent-team-management.md) — captain
reviews with extra scrutiny for late-context tells (instruction-drop,
sloppy diff hygiene, hedging on tasks done cleanly earlier in the
session) and may rotate the worker out mid-session rather than fighting
through degraded output. Honor-system: pre-commit hook does not warn on
absence; presence is not a block. Worker types the trailer manually so
the act of typing forces a moment of self-reflection — `work commit`
deliberately does NOT auto-inject this one. Interim mechanism pending
Block 48K pilot harness; may
graduate to a load-bearing convention or be retired in favor of a
different signal once the pilot yields evidence.

**Commit hygiene under shared working tree** — added 2026-05-05.

When multiple agent sessions share one working tree (the dominant pattern
on this project), the working tree contains in-flight files from sessions
other than yours at any given moment. **Sweeping `git add` patterns
silently scoop those in-flight files into your commit**, attached to your
authorship and your `[track:]` trailer. The other session then sees
their files "already committed" but with the wrong context — they re-add
+ re-commit the work, producing a confusing two-commits-per-file history
or, worse, partial / mid-state files frozen as if completed.

**Three-step rule before every commit:**

1. **`git status -s` first.** See everything modified. Don't skip this.
2. **`git add <path>` each file you authored this session, explicitly.**
   Never `git add .`, `git add -A`, or `git add <directory>/`. Each of
   those silently includes other sessions' work.
3. **If you can't tell whether a file is yours, leave it out.** The
   other session will pick it up under their own track. Commit narrow.

This rule applies equally to humans, but humans rarely run multiple
parallel sessions against the same working tree — it's the agent
multi-session pattern that surfaces this failure mode reliably. The
pre-commit hook does not enforce this (it can't tell whose modification
is whose); the rule is honor-system, but the cost of breaking it falls
on coworkers, so it's load-bearing for cluster operation.

**Shared-index hazard (incident af119b9, 2026-05-05) — the rule above is
necessary but not sufficient.** Even with explicit `git add <path>` per
file, the resulting staged set lives in the **shared `.git/index`** —
which other sessions also write to with their own `git add` calls. Once a
file is staged, a `git commit -m <msg>` (no path restriction) commits
**everything in the index**, including paths another session staged
in parallel. The three-step rule prevents you from staging others'
files; it does NOT prevent your `git commit` from sweeping up files
others staged.

Concrete failure: `af119b9` (2026-05-05). One session ran the
three-step rule cleanly (8 explicit `git add` calls). A concurrent
session had separately staged 7 files (its own tracks). The first
session ran `work commit ... -m <msg>` — which executed `git commit -m`
with no path restriction — and the resulting commit landed all 15
files (8 mine + 7 from the concurrent session) under the first
session's track trailer.

**Fix (CLI-enforced):** `work commit` now requires `--paths
<comma-separated-paths>` and uses `git commit -m <msg> -- <paths>`
semantics. The `--` clause restricts the commit to ONLY those paths
from the index, leaving other sessions' staged files untouched (they
remain staged for their own session's commit). Per
`../../ops/work/README.md` §"Shared-index
hazard guard." A `--allow-shared-index` bypass exists for the rare
case where the operator has verified the shared index is theirs alone
via `git status -s`; the bypass prints the file list before committing.

**Discipline expansion:**

- After running the three-step rule, ALWAYS pass `--paths` to
  `work commit`, listing exactly the files you staged in step 2.
- For ad-hoc `git commit` (without the `work` CLI), use `git commit
  -m <msg> -- <paths>` directly. Never `git commit -m <msg>` with no
  path restriction in a shared-tree context.
- Worktrees (per [ADR 0011](0011-cluster-worker-execution-environment.md))
  remove the shared-index hazard entirely — each worktree has its own
  `.git/worktrees/<name>/index`. Adopt worktrees as the substrate's v1
  worker-execution-environment to make the hazard structurally
  impossible, not just procedurally avoided.

### Close-out summary (mandatory on release)

When releasing a track, the owner writes a short plain-English summary
on the `recently_completed` row capturing **goal · work · result**.
This is distinct from the `notes:` field (which carries pickup-time
coordination context, often loaded with internal references); the
summary is the post-hoc record optimized for someone scanning
`recently_completed:` cold.

**Audience: a future participant scanning the file with no project
context.** Concretely:

- No block numbers (`Block 22K`), finding IDs (`F-053`), or ADR
  references (`ADR 0709`) used as load-bearing nouns. Mention them
  parenthetically if useful, but the sentence must read clearly with
  them stripped out.
- No project acronyms used as nouns (`wg-cp0`, `goat-relay`,
  `Spock`) without a one-clause gloss.
- No path references as identifiers (`see ops/foo/bar.go`) — describe
  what the file does, not where it lives.
- One medium paragraph max. Bullets optional. **Short is the goal —
  the brief should read fast.**

**Written into `summary:`** on the `recently_completed` row:

```yaml
recently_completed:
    - name: track-closeout-summary-discipline
      owner: <email>
      picked_up_at: 2026-05-07T...
      released_at: 2026-05-07T...
      final_commit: <sha>
      summary: |
        Added a discipline so that whenever someone closes out a piece
        of work in the project's in-flight tracker, they write a short
        plain-English note saying what the goal was, what they did, and
        what the result was — readable by anyone scanning later without
        needing to know the project's internal vocabulary. The change
        is documentation only: ADR, the work CLI's README, and the
        top-level project guide. No tooling change.
      notes: |
        original pickup-time framing (internal references OK here)
```

The CLI does not enforce summary content today; the hook nags if the
field is missing on release but exits 0 (discipline-not-block, per
ADR 0006's general posture). Adoption pressure: the SessionStart hook
surfaces `recently_completed` summaries when relevant, so a
well-written one pays back in any future session that touches the
same area.

**Format: one short lead paragraph + 2-5 bullets. That is the cap.**
Multi-paragraph essays defeat the purpose; the goal is "scan in 15
seconds." Bullets are concrete (a number, a path, a verdict);
narrative reflection belongs in the commit message, not here.

**Goal: someone scrolling `recently_completed:` rebuilds "what
happened here" in 15 seconds without opening any other file.**

**Surface the same summary in the session reply to the user.** The
YAML row optimizes for future scanning; the chat message at close-out
optimizes for the human in the loop right now. Both surfaces share
the exact same prose — write once, paste in both places. A bare
"track released" line in chat forces the user to open the YAML to
learn what shipped, which is the symptom this discipline is meant to
prevent in the first place. (Added 2026-05-08 after the discipline
was first applied incorrectly — summary went into the YAML but not
into chat; rule clarified. Same-day refinement capped the format at
"one short lead paragraph + 2-5 bullets" after the first compliant
attempt produced a four-paragraph essay.)

### Lifecycle (worked example)

```text
# Agent picks up a new piece of work
$ work pickup block-35-spike-A-full-soak \
    --scope ops/video-fabric/spikes/A-wg-udp/,docs/reports/ \
    --eta 2h \
    --note "30-viewer 1h soak on the integration env"
appended row to docs/project/active-work.yaml; commit?
[Y/n] y
[main 73e27ad] chore(work): pickup block-35-spike-A-full-soak

# Agent works, occasionally updates
$ work update block-35-spike-A-full-soak --note "ramp orchestrator written; running on the integration env"

# Agent commits with track linkage
$ work commit block-35-spike-A-full-soak -m "feat(block-35): Spike A full PASS — driver + verdict"

# Agent releases when done — write summary into the recently_completed row
$ work release block-35-spike-A-full-soak --final-commit 73e27ad
moved track to recently_completed; remember to fill summary: field
[Y/n] y

# Edit docs/project/active-work.yaml, fill in summary: under the new
# recently_completed row. Plain-English goal/work/result, no jargon.
# Commit the summary edit:
$ git add docs/project/active-work.yaml
$ git commit -m "chore(work): close-out summary for block-35-spike-A-full-soak"
```

## Consequences

### Positive

- **Visibility upfront.** A new agent or human sees in-flight work
  before picking up a task, so they don't step on it.
- **Wasted-work avoidance.** Re-reads, re-analysis, double-staging
  drop substantially. The 2026-04-30 session would have had ~0
  collisions instead of 4.
- **Cross-session continuity.** When a session ends mid-track, the
  next session reads active-work.yaml and knows what was in flight.
- **Audit trail.** Track history (pickup → update → release) lives in
  git, queryable by `git log -- docs/project/active-work.yaml`.

### Negative / costs

- **Discipline tax.** Owners must remember to pickup at session start
  and release at session end. The CLI helpers + hooks reduce this to
  near-zero, but it's not free.
- **Stale entries.** Owners forget to release. Mitigation: 24h
  auto-flag in `work check-stale`, plus the SessionStart hook
  proactively listing stale entries.
- **Granularity tension.** Track-level (one row per coherent piece of
  work) is the right level, but where the line falls between
  "one big track" and "five sub-tracks" is judgment. We accept the
  judgment cost; the alternative (file-level locks) is intolerable
  ceremony.
- **Format mismatch with existing live-state docs.** `validation-state.md`
  is markdown; this is YAML. The split is deliberate: validation-state
  is read-by-humans-as-narrative; active-work is read-by-machines-and-
  humans-as-state.

### Non-consequences (deliberate)

- **Does NOT replace git branches / PRs.** All existing git discipline
  per [ADR 0109](0109-git-collaboration-trunk-based.md) stays. This
  layer is additive.
- **Does NOT block commits.** The pre-commit hook warns; it never
  exits non-zero. Throughput is the higher-order good.
- **Does NOT enforce scope.** A track's `scope` is informational. An
  owner who edits files outside the declared scope gets a warning;
  no rejection. Scope drift is normal in exploratory work.
- **Does NOT centralize work assignment.** Anyone can pick up any
  track. The system is *visibility*, not *queueing*.

### Watchlist

- **Adoption rate.** If agents (or humans) systematically forget to
  pickup, the file becomes stale and useless. Revisit at 30d if the
  hooks aren't enough.
- **YAML schema drift.** The schema is versioned (`schema_version: 1`).
  Bumps follow the same discipline as data-dictionary entries
  (Block 23 — registered
  schema at `attest/schema/active-work/v1` if and when this needs
  to participate in the data fabric, which today it doesn't).
- **Multi-repo coordination.** If/when the project splits into
  multiple repos per [ADR 1005](1005-monorepo-separation-map.md),
  active-work.yaml lives per-repo. Cross-repo dependencies declared
  via track `cross_refs:` and read by humans, not enforced
  programmatically.

## Amendment (2026-08-11) — bus delivery contract

The fast-event complement to this ADR's state layer ([ADR 0012](0012-session-bus-event-layer.md), `work bus`) now carries an explicit **delivery contract: posting to the bus is not delivery; the reader-side hook is.** `.claude/hooks/session-bus-tail.sh` — wired in `.claude/settings.json` for `SessionStart` (catch-up) and `UserPromptSubmit` (incremental, per-session cursor) — is the only mechanism by which another session's message reaches an agent without operator hand-relay. Corollaries: a coordination step is not complete just because it was posted; directed messages use the `TO session <id>` / `TO track <name>` convention (canonically via `work bus <level> --to ...`); receipt can be confirmed with `work bus ack <ts>`; subagents inherit no hook context and must be handed bus excerpts in their spawn prompt. Full contract: [multi-agent-team-management.md](https://github.com/usareur-af/goatdocs/blob/main/docs/design/multi-agent-team-management.md) §6.4.8.

## Glossary

See `GLOSSARY.md` for project-specific terms.

## Status flip criterion

Proposed → Accepted on three signals:

1. CLI is built and used for at least 7 days of real work
2. At least one collision-avoidance event is observed and logged
   (someone read active-work.yaml, picked a different track because
   of it)
3. At least one stale-entry auto-flag fired and got resolved
   (proves the staleness mitigation works)
