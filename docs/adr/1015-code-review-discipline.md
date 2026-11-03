# ADR 1015 — Code-review discipline for completed blocks

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-05 |
| Repo scope | goat core + every consumer-repo where the same Block discipline applies |
| Supersedes | — |
| Superseded by | — |
| Related | [`0006-multi-agent-human-concurrency-governance.md`](0006-multi-agent-human-concurrency-governance.md) (work tracker), [`0007-multi-agent-orchestration-hierarchy.md`](0007-multi-agent-orchestration-hierarchy.md), `0009-worker-discipline-mechanisms` (Backlog per [INDEX.md](INDEX.md)), [`0109-git-collaboration-trunk-based.md`](0109-git-collaboration-trunk-based.md) (trunk + PR model), [`1006-phase-gates.md`](1006-phase-gates.md), [`1007-wave-style-validation.md`](1007-wave-style-validation.md), [`1008-soak-testing-discipline.md`](1008-soak-testing-discipline.md), [`1011-findings-register.md`](1011-findings-register.md), [`1012-load-testing-discipline.md`](1012-load-testing-discipline.md), [`1013-continuous-architectural-chaos.md`](1013-continuous-architectural-chaos.md), [`1014-codex-pattern-adr-to-enforced-lint.md`](1014-codex-pattern-adr-to-enforced-lint.md). Companion design at [`docs/design/code-review-process.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/code-review-process.md); checklist at [`docs/process/block-completion-checklist.md`](../process/block-completion-checklist.md). |

## TL;DR (for a layperson)

We've been finishing chunks of work and saying "done" without a written
checkpoint. Two recent engineering-quality reviews each found gaps that
*only the next reviewer caught* — the gaps would have been visible in a
five-minute checklist before "done" was declared. This ADR adopts a
small standing process: the author runs a written checklist before
releasing the block; the checklist becomes a commit, not a verbal
gesture. As the project grows, the same checklist gets more eyes (a
second agent or human reviews it before promotion to production-bearing
branches; eventually it becomes the requirements list on a pull
request). One checklist, three tiers, staged adoption.

## TL;DR (for a sponsor)

This codifies the engineering-review discipline we already do
informally. The visible deliverable is a one-paragraph + table file
attached to every block release, written by the author. Cost: ~10
minutes per block. Benefit: gaps that would otherwise be found two
weeks later by a different reviewer get caught while the author still
has context. As staffing grows we add cross-reviewer eyes to the same
checkpoint without replumbing anything.

## TL;DR (for SCA-R)

Block-level review is a standing process anchored in this ADR; the
process design is at [`docs/design/code-review-process.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/code-review-process.md);
the runnable checklist is at [`docs/process/block-completion-checklist.md`](../process/block-completion-checklist.md).
Tier 1 (author self-review, output committed) is in effect today; Tier
2 (independent reviewer) is on availability; Tier 3 (PR review with CI
gates) is gated on per-repo branching adoption per ADR 0109. The
process composes with — does not replace — the existing discipline ADRs
(1006/1008/1012/1013/1014/0006/0009).

## §1 Vocabulary

- **Block** — a unit of implementation tracked under `docs/project/implementation-plan.md`, typically referenced as `Block <NN><letter>` (e.g. Block 50E, Block 22J).
- **Block completion review** — a written artifact at `docs/project/block-<name>-completion-review.md` produced by the author when the block is ready to release; structured per the checklist; cited row by row.
- **Tier** — one of the three review tiers (1 = author self-review, 2 = independent reviewer, 3 = PR review with CI). All three tiers run the same checklist; what changes is who runs it and when.
- **Cross-review** — a Tier-2 review by an independent agent or human, output at `docs/project/block-<name>-cross-review.md`.
- **Released-with-known-gaps** — a release status meaning the block is complete in scope but the Tier-1 review surfaced specific gaps that are filed as `F-NNN` findings or follow-on Block sub-tasks. This is *fair* — visibility, not blocking, is the goal.

## §2 Context

Two recent engineering-quality review waves (2026-05-03 producing Block
50A/B/D, 2026-05-05 producing Block 50E) each shipped released blocks
that the **next** reviewer found gaps in:

- **Wave 1 (50A.2)** — verdict criterion was *"`go test ./...` passes;
  any new leak fails the build."* That criterion is existence-based
  ("does goleak appear?"), not fitness-based ("does the goleak that
  appears actually cover the failure modes?"). Three Go services
  (snitch, admit-audit, enrollment) had zero goleak coverage because
  they had zero tests; the missing-tests-entirely category fell
  through.
- **Wave 2 (50E)** — three genuinely functional changes (snitch
  URL-query auth removal, snitch install.sh fix, gatewatch graceful
  shutdown) shipped with build + reasoning as the only verification.
  No tests, no checklist surfaced the gap before track release. Now
  filed as Block 50F.

In both cases the missing piece is the same: a checkpoint between
"track released" and "next-wave audit" that catches the gap **in
track**. Three forces support adopting that checkpoint as standing
discipline:

1. **Author context is freshest.** The author has just finished the
   work; running through a 12-row checklist takes ~10 minutes and
   surfaces gaps the next reviewer would otherwise spend an hour
   re-discovering.
2. **The discipline doesn't need new infrastructure.** Active-work
   tracker (ADR 0006) already gates `work release`; the review file
   becomes a Section A row in the release commit. No new tooling
   today; CI gates land at Tier 3 when branching does.
3. **The same checklist scales to PR review.** Phoenix-native + Codex
   pattern (ADR 1014) gives us mechanical enforcement of the rows that
   are mechanically checkable; the others stay human-judgment at all
   three tiers. Designing the checklist now to survive promotion to
   PR review later costs nothing and avoids re-design work.

## §3 Decision

### 3.1 Three-tier scaffold, one shared checklist

Adopt a three-tier code-review process anchored on a single shared
checklist:

| Tier | Who | When | Output |
|---|---|---|---|
| 1 — Self-review | Block author | Before `work release` | `docs/project/block-<name>-completion-review.md` |
| 2 — Cross-review | Independent reviewer | After Tier 1; before promotion to production-bearing branch | `docs/project/block-<name>-cross-review.md` |
| 3 — PR review | PR reviewer per CODEOWNERS | At PR open; before merge to main | PR description (Tier-1 fields) + approving review (Tier-2 fields) + CI gates |

Adoption is staged: Tier 1 today; Tier 2 when reviewing-agent or human
availability allows; Tier 3 per-repo when [ADR 0109](0109-git-collaboration-trunk-based.md)
branching lands.

### 3.2 Tier 1 ships immediately and is mandatory for `work release`

From this ADR's acceptance forward, the `work release` step requires a
committed Tier-1 review file at `docs/project/block-<name>-completion-review.md`.
The work CLI (under Block 48C-substrate)
gains a check at release time that warns if no review file is found in
the commit graph since track pickup; warn-not-block initially, promoted
to block once the checklist is well-trodden (revisit at the 5-block
mark).

### 3.3 Tier 2 ships on availability, not as a hard gate today

A Tier-2 review is welcomed whenever a reviewing agent or human is
available and not as a hard gate today. The Tier-1 review file MUST be
structured so that a reviewer can read it cold and verify each row
against linked evidence; this property is checked by the Tier-1 author
as part of the checklist's last row.

The first Tier-2 review will run against **the Block 50G work itself**
— meta-discipline that the review process gets reviewed under itself.

### 3.4 Tier 3 design is declared now, installed later

Tier 3 is **declared now** in the design doc and ADR so that the
Tier-1 + Tier-2 work today naturally sets up for Tier 3 later. Day-1
Tier-3 install (per-repo, when ADR 0109 branching lands) wires:

- PR description = Tier-1 review template
- Approving review = Tier-2 review template
- CI gates from the design doc's enforcement table
- CODEOWNERS routing per file pattern
- Required reviewers (≥1; merge queue at >3 active contributors per
  repo, per ADR 0109)

### 3.5 The checklist composes with — does not replace — existing discipline

ADRs 1006 / 1008 / 1012 / 1013 / 1014 / 0006 / 0009 / 0109 each carry
their own discipline; this ADR operationalizes them at the Block-closure
checkpoint. See the design doc §"Composition with existing discipline"
for the row-by-row mapping.

### 3.6 Severity escalation: release-with-known-gaps is fair

A Tier-1 review that surfaces a Sev-1 gap (e.g. an un-tested functional
change to security-critical code) does **not** automatically block
release. Visibility is the goal; throughput is the constraint. The gap
becomes an `F-NNN` finding or a follow-on Block sub-task, the review
file declares the block `Released-with-known-gaps`, and the fast-follow
Block enters the work tracker. Tighten this rule only if fast-follow
Blocks aren't getting picked up promptly.

## §4 Consequences

**Positive:**
- Both review waves' gap classes (existence-based test verdicts;
  unwritten functional-change tests) are caught by Section B rows 3 +
  4 of the checklist before release.
- A third gap class — silent-failure modes on runtime invariants
  (config-file mode, bucket lifecycle, directory layout, allowlist
  non-emptiness, upstream defaults that we override) — is caught by
  Section B Row 5.5 added 2026-05-08. Reference instances: F-039
  (coredns mode 0600), F-050 (OpenObserve disk-default fall-through),
  F-074 (Block 68 missing bundle layout), Block 22K.8 (wg-cp0
  zero-peer allowlist), F-075 (luks-unlock register-overwrite).
  Pattern documented in CONTRIBUTING.md §"Fail-loud on silent-failure
  modes" so authors apply it during the work, not only at release.
- The retroactive Block 50E review (50G.4) is the demonstration: the
  three test-backfill items become Block 50F naturally.
- When per-repo branching lands per ADR 0109, the Tier-3 install is
  mechanical wiring of the existing checklist; no design work
  pending.
- Composes with [ADR 1014](1014-codex-pattern-adr-to-enforced-lint.md)
  Codex pattern: mechanical rows graduate to PR-block CI gates; the
  rest stay review-time human judgment.

**Costs:**
- ~10 minutes per Block at release time for the Tier-1 review file.
- A Tier-1 review that surfaces no gaps still produces a file —
  positive evidence has cost.
- Tier-2 reviewer-assignment policy is open (see design doc §"Open
  questions"); today we accept "any free reviewer."
- Sloppy Tier-1 reviews (cited rows without real evidence) are worse
  than no review because they create false confidence. Tier-2
  reviews catch this; until Tier 2 lands, the author is on the honor
  system.

**Trade-off accepted:**
- Throughput vs. visibility: the process biases toward release-with-
  known-gaps + visibility rather than block-on-fix. We accept that the
  follow-on Block discipline (work tracker, implementation-plan.md
  Block reservations) handles the actual closure of gaps; the review
  process makes them legible.
- Format choice: the checklist is a Markdown file, not a YAML field or
  a UI form. Markdown matches the existing artifact shape (design docs,
  ADRs, runbooks, F-NNN findings); the cost of a future migration to a
  structured format is small.

## §5 Alternatives considered

**A. Do nothing — keep informal review.** Rejected. Two waves' worth
of evidence shows the same gap class repeats; it will repeat again
without a checkpoint.

**B. Single review file per repo, append-only journal.** Rejected.
The artifact's value comes from being scoped to a Block and citing
that Block's evidence; an append-only journal would either grow
unbounded or require a per-Block sub-section, at which point per-Block
files are simpler.

**C. Skip Tier 1, jump straight to Tier 2.** Rejected. Tier 2 requires
a reviewing agent or human to be available; today's posture is
single-author-per-track most of the time. Tier 1 is what fits today's
staffing; Tier 2 layers on when staffing allows.

**D. Skip Tier 2, jump straight to Tier 3 (PR review).** Rejected.
Tier 3 requires per-repo branching which is gated on ADR 0109's
per-repo trigger (>3 active contributors). Most repos today don't meet
that bar. Tier 2 fills the gap.

**E. ADR-only — no separate design doc + checklist.** Rejected.
Cloudflare's Codex post (cited in ADR 1014) demonstrates that the
operational artifact (their codex.txt, our checklist) is a different
audience and lifecycle than the rationale. Separating them lets each
move at its own cadence: rationale changes rarely; checklist rows
gain mechanical-CI mappings as Tier 3 lands, and gain new rows when
new ADRs add new mechanical rules.

## §6 Open questions

Per the design doc's §"Open questions / future work" — cross-track
review interaction; Tier-2 reviewer-assignment policy; severity
escalation tightening if fast-follow Blocks aren't getting picked up
promptly.

## Glossary pointer

Acronyms used here are defined once in `docs/adr/GLOSSARY.md`.
