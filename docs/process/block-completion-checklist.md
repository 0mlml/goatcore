# Block-completion checklist

> **Status:** Tier-1 in effect (author self-review). Tier 2 + Tier 3 land on the schedule defined in [`docs/design/code-review-process.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/code-review-process.md).
>
> **ADR anchor:** [`1015-code-review-discipline.md`](../adr/1015-code-review-discipline.md)

## How to use this file

1. **Fork it.** Copy this file to `docs/project/block-<name>-completion-review.md` (e.g. `block-50e-completion-review.md`). Don't edit this template.
2. **Walk every row.** Twelve rows across four sections. Each row needs one of: a file path + line number; a commit SHA; a test verdict (`PASS` / `FAIL` / `SKIP`); a link to a finding (`F-NNN`); a measurement (`M-NNN`); a soak evidence file in `docs/reports/`; or — for a known gap — an explicit pointer to the follow-on Block.
3. **Be honest about gaps.** Visibility is the goal; throughput is the constraint. A row that fails honestly + files an `F-NNN` and a follow-on Block is worth more than a row that says "covered" without evidence. Sloppy Tier-1 reviews that cite rows without real evidence are worse than no review.
4. **Commit the result.** The completed review file is committed alongside the release commit. The `work release` step warns if no review file is found in the commit graph since track pickup. **No review file = block not released.**
5. **Declare a verdict.** End the file with one of: `Released` / `Released-with-known-gaps + pointer to follow-on Block` / `Held-pending-fix`.

## Template

Copy from here down (delete this comment block when forking):

---

# Block <name> — Completion Review (Tier 1)

| Field | Value |
|---|---|
| Block | <Block NN<letter> per implementation-plan.md> |
| Track | <track name from active-work.yaml> |
| Author | <agent or human> |
| Review tier | 1 (self-review) |
| Reviewed on | <YYYY-MM-DD> |
| Released as | `Released` / `Released-with-known-gaps` / `Held-pending-fix` |
| Final commit | `<sha>` |
| Tier-2 review | `pending` / `<link to cross-review file>` |

## Section A — Closeout sweep

### Row 1 — Closeout-status row check

Every implementation-plan sub-task in this Block is marked Done / Skipped / Carried-forward with citations.

| Sub-task | Status | Evidence |
|---|---|---|
| <50X.1 short-name> | Done / Skipped / Carried-forward | `<commit sha>` or `Block 50<follow-on>` pointer |
| ... | ... | ... |

### Row 2 — Plan-vs-shipped truth-up

If a sub-task shipped differently from the plan row, the plan row is updated to match what shipped, with a deviation note in the row body.

- Sub-tasks that shipped per plan: <list>
- Sub-tasks that deviated: <list with the truth-up commit SHA, e.g. "50X.4 deviated from 'use existing helper' to 'new internal package'; truth-up at `<sha>`">

## Section B — Test fitness (the gap that bit us)

### Row 3 — Functional-vs-edge-case classification

Every change in this Block is classified as one of: **functional** / **edge-case-hardening** / **error-surfacing** / **test-only** / **doc-only**. Functional means user- or operator-visible happy-path behavior change. Edge-case-hardening means timeouts, panic recovery, leak detection — only changes behavior on degraded/exceptional paths.

| Change | Category | One-line rationale |
|---|---|---|
| <change 1> | functional / edge / err / test / doc | <why> |
| ... | ... | ... |

### Row 4 — Test coverage per category

- **Functional changes** must have a unit (or integration) test that exercises the behavior change. State the test file + the commit SHA where the test landed.
- **Edge-case-hardening** has at minimum: lint green (`.golangci.yml` strict), build green, `goleak.VerifyTestMain` present where a Go test substrate exists.
- **Error-surfacing / test-only / doc-only** self-validates.

| Change | Test file + verdict | Notes |
|---|---|---|
| <functional change 1> | `<path>:<test name>` PASS / FAIL / NOT TESTED | <if NOT TESTED, link to F-NNN + follow-on Block> |
| ... | ... | ... |

### Row 5 — Known-gap filing for un-testable items

If a functional change can't be unit-tested in this Block (missing test substrate, needs docker-in-docker, etc.), an `F-NNN` finding is filed with explicit pointer to the follow-on Block.

| Gap | F-NNN | Follow-on Block |
|---|---|---|
| <gap 1> | `F-NNN` | `Block 50<follow>.X` |
| ... | ... | ... |

If no gaps: state "None — every functional change has a test."

### Row 5.5 — Fail-loud on silent-failure modes

Every change that depends on a runtime invariant — a config-file mode, a bucket lifecycle setting, a directory layout being present, an allowlist being non-empty, an upstream default that we override — has either a fail-loud assertion or a structured drop/skip event. The bar is: **a future operator running this code on a host where the invariant was silently violated must see the failure, not work around it.**

The class of bugs this row catches is everything where "no log line said anything was wrong, the system just quietly stopped doing what we expected":

- F-039 — `chmod 0644` on hosts file before `mv`, no log on failed parse
- F-050 — explicit `ZO_LOCAL_MODE_STORAGE=s3`, no fall-through to disk default
- F-074 — bundle role `Verify bundle binary present` task fails-loud when layout is absent
- Block 22K.8 — `assert wg_cp0_allowed_peers | length > 0` refuses to render an empty allowlist
- F-075 — luks-unlock role's `register:` name-collision silently dropped probe results

Things that count as fail-loud:

- Ansible `assert:` task on the invariant (preferred for declarative substrate)
- Go-side panic-on-impossible / `log.Fatal` at boot (preferred for runtime singletons)
- Structured drop/skip event on the wide-event substrate (preferred for in-flight per-message decisions; Block 27 producer pattern)
- Pre-flight gate task that fails the playbook before any side effect (preferred for orchestrators)
- Test that asserts the assertion fires (the assertion itself isn't useful if a refactor silently removes it)

| Invariant | Fail-loud mechanism | Test that the assertion fires |
|---|---|---|
| <invariant 1> | <where in code> | <test path:name PASS / NO TEST + F-NNN> |
| ... | ... | ... |

If this Block introduced no new runtime invariant, state "None — no new invariants in scope."

If a known silent-failure mode was found but not fixed in this Block, file an `F-NNN` and follow-on Block (Row 5 shape).

## Section C — Drift sweep

### Row 6 — Docs-vs-code grep sweep

`grep` for stale references to renamed components / paths / hostnames; verify CLAUDE.md pointers match current source-of-truth locations.

- **Greps run:** <list the greps with results, e.g. "`grep -rn db-policy-engine ops/` returns 0 paths under `ops/` (correct — service doesn't exist yet); CLAUDE.md was reframed to match in `<sha>`.">
- **Stale references found + fixed:** <list with commit SHA>

### Row 7 — CLAUDE.md updates

If this Block moved a source-of-truth or added a load-bearing component, CLAUDE.md is updated **in the same Block**.

- **CLAUDE.md updated:** YES / NO (no change required)
- **Update commit:** `<sha>`
- **Sections changed:** <list>

### Row 8 — ADR updates

If architectural, ADR(s) authored or superseded; INDEX row added; per-tag sub-series carried.

- **ADRs authored:** <list with status>
- **ADRs superseded:** <list>
- **INDEX rows added:** <list>
- **Sub-series tags applied:** <list>

## Section D — Evidence + hygiene

### Row 9 — Findings register

Any external-system gotcha discovered during this Block becomes an `F-NNN` with INDEX row added.

- **Findings filed:** <list with `F-NNN` IDs>
- **Findings register row count = file count:** verify with `ls docs/findings/F-*.md \| wc -l` matches `grep -c "^| \[F-" docs/findings/INDEX.md` for unique IDs

### Row 10 — Measurements register

Any perf claim is backed by an `M-NNN` measurement; numbers without an `M-NNN` are explicitly framed as "preliminary" or "estimated."

- **Measurements filed:** <list with `M-NNN` IDs>
- **Perf claims without M-NNN:** <list, framed as preliminary>

### Row 11 — Validation-state tracker

Any soak run, probe, or env provisioning verdict updates `docs/project/validation-state.md` in this Block.

- **Validation-state updates:** <list with line ranges or section names>
- **No validation activity in this Block:** YES / NO

### Row 12 — Commit hygiene

- **Track trailers:** every commit in this Block carries `[track: <name>]`. Verified via `git log --grep=<track-name> | wc -l` matches the Block's commit count.
- **Path-form commits:** every commit used `git commit <path>` form (not `git add` + `git commit`) to bypass the concurrent-staging window observed in Block 50E (`bf199f3`, `cccb67b`). Verify via commit author intent + commit-stat narrowness.
- **GPG-signed:** every commit signed; verify via `git log --show-signature -<n>`.
- **No skipped hooks:** no `--no-verify`; no `--no-gpg-sign`. Verify via commit history.

## Section E — Verdict

### Final declaration

State one of:
- **`Released`** — every row above passes with evidence; no follow-on work required.
- **`Released-with-known-gaps`** — rows above are honest; gaps are filed as `F-NNN` and follow-on Block(s); fast-follow Blocks have entered the work tracker.
- **`Held-pending-fix`** — Sev-1 gap surfaced that the author judges should block release; track stays in `active` status, not released.

### Self-confidence statement

Three sentences:
- What's solid in this Block.
- What's tentative or carries risk.
- What needs follow-up.

### Tier-2 hooks

State the rows where Tier-2 cross-review attention would be highest-value:

- <row + why>
- ...

---

(end of template)

## Authoring notes for agents

When you fork this file:
- **Use `git commit <path>` form throughout the Block.** The concurrent-staging race that bit Block 50E (`bf199f3`, `cccb67b`) was caused by `git add` + `git commit` capturing whatever else was in the index. Path-form bypasses that window.
- **Cite, don't assert.** "Tested" is not evidence; `<path>:<test name> PASS` is evidence.
- **Honest classification beats generous classification.** Marking edge-case-hardening as functional inflates the "tested" denominator unfairly; marking functional as edge-case-hardening hides the gap. Be precise per Section B Row 3.
- **The verdict is honest self-assessment.** Tier 1 is the author signing off on themselves. Tier 2 will catch if you cited a row without real evidence; that's a worse outcome than honestly declaring `Released-with-known-gaps`.
