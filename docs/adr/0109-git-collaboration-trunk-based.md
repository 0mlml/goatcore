# ADR 0109 — Git collaboration: trunk-based, PR-gated, signed-commit

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-04-25 |
| Repo scope | Multi |
| Supersedes | — |
| Superseded by | — |
| Related | [`0103-crypto-boundary-discipline.md`](0103-crypto-boundary-discipline.md), [`0212-goat-vs-bond-split.md`](0212-goat-vs-bond-split.md), [`0700-chainguard-first.md`](0700-chainguard-first.md), [`0703-pinned-digests-and-pin-regression-ci.md`](0703-pinned-digests-and-pin-regression-ci.md), [`1005-monorepo-separation-map.md`](1005-monorepo-separation-map.md) |

> **Maturity note (2026-04-25).** This ADR is *Proposed*, not
> *Accepted*. It is the starting point — a minimum viable change-
> control posture written down so contributors have something to
> read on day one. None of it is stone-cast. While the team and the
> practice settle, the maintainer will personally
> handle commit and PR management and grant exceptions where the
> rule does not yet fit reality. Expect this ADR to be edited
> before it is promoted to *Accepted*; expect the supporting files
> (`CONTRIBUTING.md`, `CODEOWNERS`, the workflow checks) to drift
> alongside it. Disagreement is feedback, not insubordination —
> raise it in PR review.

## TL;DR (for a layperson)

Every change to the codebase goes through the same simple, auditable
pipeline. Each contributor branches off `main`, opens a pull request,
the test suite runs automatically, at least one other person reviews
the change, and only then can the branch merge. Direct pushes to
`main` are not allowed. Commits are cryptographically signed so we can
prove who wrote what. This is how popular open-source projects (we
model on Netbird) keep a clean, reviewable history under heavy
collaboration; it is also what an auditor expects to see for code
provenance and change control.

## Context

goat is moving from a single-author repo toward
multi-team collaboration across several repositories
(goat core, goat-bond, goatCompliance, and
future siblings — see [`1005`](1005-monorepo-separation-map.md)).
Without a documented git workflow, three failure modes appear
quickly:

1. **Untraceable history.** Direct pushes, force-pushes, and
   unsigned commits make `git log` an unreliable witness. This is
   a problem for normal engineering and a much bigger problem for
   the cATO pipeline (`0100`)
   where commit provenance is part of the evidence chain.
2. **Untested merges.** Without required CI, a green test run
   becomes a habit, not a gate. The supply-chain hardening in the
   0700-series and the pin-regression CI in
   [`0703`](0703-pinned-digests-and-pin-regression-ci.md) only
   protect us if those checks are *required*, not advisory.
3. **Cross-repo half-landings.** A single feature can touch
   goat, goat-bond, and goatCompliance.
   Without a coordination convention, an atomic-feeling change
   ships in pieces and the system breaks between merges.

Netbird's open-source repository is a useful reference model:
single `main`, PR-only, GitHub Actions CI matrix, CODEOWNERS,
DCO sign-off, tagged releases, no long-lived feature branches.
We adopt the same shape and add two things Netbird does not need
but our posture does — required commit signing and a documented
cross-repo coordination pattern.

## Decision

**Branching model.** Trunk-based. Every repo has one canonical
long-lived branch: `main`. All other branches are short-lived
(target: < 5 working days from branch to merge). No `develop`,
no permanent release branches. Hotfix branches are allowed but
must merge to `main` and tag from `main`.

**Pull-request gate.** No direct pushes to `main`. Every change
lands via PR. Branch-protection rules on `main` enforce:

- Required status checks (CI must be green): unit tests,
  integration tests, lint, type-check, container-build, and the
  supply-chain checks named in the 0700-series ADRs.
- Required reviewers: at least **one** approval for routine
  changes; at least **two** for changes that touch any path
  listed in `CODEOWNERS` as security-sensitive (crypto,
  enrollment, policy engine, egress, identity, FIPS-PQC
  boundary node).
- Required linear history: merges use **squash-and-merge**
  (single signed commit per PR). Rebase-merge is allowed only
  for stacked PRs where preserving the chain has review value.
  No merge commits on `main`.
- Required up-to-date branch before merge.
- No force-push to `main`. Force-push *to feature branches* is
  allowed (cleaning up a PR's history before merge is normal).
- Required: all conversations resolved.

**Commit provenance.**

- **Signed commits.** Every commit on `main` must be signed
  (SSH or GPG key registered with GitHub; YubiKey-backed where
  possible — same hardware tier as the offline-CA work in
  `0406`). GitHub's "Require
  signed commits" branch protection is on. Squash-merge produces
  a single signed commit on `main`, which is the artifact the
  audit trail relies on.
- **DCO sign-off.** Every commit message ends with
  `Signed-off-by: Name <email>` (Developer Certificate of
  Origin 1.1). DCO is checked by a required status check on
  every PR.
- **Conventional Commits** for PR titles (the squash-merged
  commit message). Format: `type(scope): subject`. Types:
  `feat`, `fix`, `chore`, `docs`, `refactor`, `test`,
  `perf`, `ci`, `build`, `revert`, `adr`. Enforced by a
  required PR-title check. This is what makes the squash-merge
  log human- and machine-readable for changelog and audit.
- **No `Co-Authored-By:` trailers added by tooling.** Pair-
  programmed work is fine; the trailer must be authored by a
  human contributor, not generated by an AI assistant.

**CODEOWNERS.** Every repo has a `CODEOWNERS` file at the root
that maps directories to owning teams. PRs touching an owned path
auto-request review from that team. Security-sensitive paths
require two approvals (above). The list of security-sensitive
paths is reviewed quarterly and named in the repo's
`CODEOWNERS` header comment, not memorized.

**Merge queue.** Once a repo has > 3 active contributors, enable
GitHub's merge queue on `main`. The queue batches PRs, retests
them against the post-merge tip, and prevents the "PR was green
yesterday but conflicts with what landed an hour ago" failure.
Below 3 active contributors, the merge queue is more ceremony
than value; the required-up-to-date-branch check is sufficient.

**Cross-repo coordination.** A change that spans repos must
follow one of these patterns; choose at PR-open time and put it
in the PR body:

1. **Sequenced merge.** Land in dependency order: the producing
   repo merges first, the consuming repo's PR pins the new
   producer version and then merges. PR descriptions cross-link
   the partner PR(s).
2. **Atomic via feature flag.** All repos merge independently
   behind a flag, then a single config flip enables the feature
   end-to-end. Required when a half-landed state would break
   the system.
3. **Coordinated-release tag.** For changes that span > 2 repos
   and need to land together, cut a coordinated-release tag in
   each repo with a shared name (`coord/<feature>-<date>`) so
   the matched set is recoverable.

The default is (1). (2) is the right answer when the boundary
is a runtime contract. (3) is rare; reserve for crypto-boundary
or schema-evolution work.

**Releases.** Tags are cut from `main` only, on a green build.
Tag format: `vMAJOR.MINOR.PATCH` per repo. Release notes are
generated from Conventional-Commit messages on the squashed
commits since the previous tag. Repos that ship as containers
(see [`0700`](0700-chainguard-first.md)) tag the OCI image with
the same version and sign it (Sigstore/cosign per the
0700-series).

**Tooling allowlist.** Each repo's `.github/` (or equivalent)
contains: branch-protection-as-code, CODEOWNERS, required-checks
list, DCO check, signed-commit check, Conventional-Commit-title
check, merge-queue config (when active). Configuration is
checked-in and reviewable; it is not click-ops in the GitHub UI.

## Alternatives considered

- **GitFlow (`develop` + `main` + `feature/*` + `release/*` +
  `hotfix/*`).** Adds long-lived branches and merge ceremony
  that this team does not have the headcount to staff. Modern
  trunk-based + feature flags solves the same problems with
  less surface area. Rejected.
- **GitHub Flow without merge queue.** Works at our current
  scale but breaks predictably at 3-5 active contributors when
  PR-pre-merge state diverges from `main`-tip state. We adopt
  trunk-based now and turn on the merge queue at the trigger
  point rather than retrofit later. Rejected as the long-term
  steady state.
- **Trunk-based with direct push allowed for maintainers.**
  Faster for the maintainer, fatal for the audit trail. The
  cATO posture in `0100`
  requires every change be reviewable; "I trust myself" is not
  a control. Rejected.
- **No commit signing, DCO only.** DCO without signing means
  the sign-off is a claim, not a proof. The marginal cost of
  signing (one-time SSH key setup) is small; the gain is real
  cryptographic provenance. Rejected the unsigned variant.
- **Co-Authored-By for AI-assisted commits.** Conflates
  attribution and tooling. Already standing project policy on
  commit hygiene; reaffirmed here. Rejected.
- **Squash-only vs. rebase-merge vs. merge-commit.** Merge-
  commits clutter `main`'s log and make `git bisect` harder.
  Squash-only is the default for clarity; rebase is allowed
  for stacked PRs where chain review matters. Pure rebase-only
  loses the per-PR boundary on `main`, which we want. Hybrid
  policy chosen.

## Consequences

**Positive.**

- Auditable, signed change history. Every line on `main` is
  attributable to a signed commit produced by an identified
  contributor — the artifact an SCA-R asks for under
  configuration management controls.
- Required CI means the supply-chain hardening, pin-regression
  ([`0703`](0703-pinned-digests-and-pin-regression-ci.md)), and
  multi-scanner regime (`0704`)
  actually gate merges instead of being decorative.
- Cross-repo coordination convention prevents partial-landing
  bugs in the post-monorepo-split world
  ([`1005`](1005-monorepo-separation-map.md)).
- Single trunk + short branches keeps merge conflicts small
  and continuous, which is the well-documented health pattern
  for trunk-based development.

**Negative.**

- Every change has a CI-runtime tax. Mitigation: tests must be
  deterministic and the full required suite must complete in
  under 10 minutes; anything slower is a tracked engineering
  debt and runs in a non-blocking lane until fixed.
- Signed-commit setup is a small one-time cost for new
  contributors. We document it in `CONTRIBUTING.md` and provide
  a `direnv`-friendly setup script.
- Merge-queue introduces a small wait when active. Net win at
  team-size > 3.
- Cross-repo sequenced merge means the consuming repo's PR
  briefly references an unmerged producer PR. Reviewers must
  read both. Acceptable cost.

**Reciprocal obligations.**

- The CI suite is part of the system, not a side project. If a
  required check is flaky, fix the test or remove it from the
  required list — never bypass via `--no-verify` or branch-
  protection override (this is also the rule from the agent-
  collaboration discipline: hooks failing means investigate,
  not skip).
- `CODEOWNERS` is kept current. Stale owners block merges.

## Assessor view (SCA-R)

This decision maps to NIST SP 800-53 **CM-3** (configuration
change control), **CM-5** (access restrictions for change),
**AU-10** (non-repudiation via signed commits), **CM-9**
(configuration management plan), and **SA-10** (developer
configuration management). The DCO is the developer attestation
required under SA-15-style developer-process expectations.
Branch protection enforces separation of duties (no self-
approval, required reviewers ≠ author). Signed commits provide
the non-repudiation evidence; the merge-queue retest provides
the evidence that what was tested is what was merged. There is
no waiver dependency. Compensating controls are not required
because the policy itself *is* the control.

For cATO (`0100`) the signed-
commit + required-CI + PR-review chain is the developer-side
evidence pipeline; it dovetails with the runtime evidence
pipeline (audit-log integrity, multi-scanner regime, runtime
EDR) to produce a continuous-monitoring posture rather than a
point-in-time accreditation.

## Trigger conditions for revisit

- The required CI suite exceeds 15 minutes wall-clock and
  cannot be brought back under 10 minutes within one quarter
  of engineering work. Revisit the required-checks list and
  the deterministic-test budget.
- A repo's active-contributor count exceeds 5 and stacked-PR
  workflows become common. Revisit whether to mandate
  Graphite/`spr`-style stacked-PR tooling instead of leaving
  it optional.
- An external partner or sponsor mandates a different SCM
  workflow (for example, a classified-side mirror with
  different review rules). Document the divergence per
  environment; do not weaken the public-side rules to match.
- A YubiKey-backed signing key custody pattern is required
  beyond what GitHub natively supports. Re-evaluate against
  the offline-CA / OP-TEE custody work in
  `0406` and
  [`0605`](0605-op-tee-key-custody.md).
- The cross-repo "sequenced merge" default produces > 1
  half-landing incident per quarter. Promote feature-flag
  coordination from "use when needed" to default.

---

## Amendment 2026-08-19 — CODEOWNERS routes by role, and lives in `.github/`

This ADR says "every repo has a `CODEOWNERS` file at the root that maps
directories to owning teams." Two parts of that were not true on
the goat repository, and this amendment corrects both.

**Location.** GitHub reads exactly ONE CODEOWNERS file, in precedence
order `.github/` > root > `docs/`. It does not merge them. `goat`
carried two — a root file with 52 rules that had never routed anything,
and `.github/CODEOWNERS`, which is the one GitHub actually used. The
root file is deleted; `.github/CODEOWNERS` is the single table. This is
also what the language boundary demands: a rule you cannot see is worse
than no rule, because it reads as coverage.

**Owning teams.** The file mapped every path to individual accounts, in
practice to a single account below the global line. That is a
bus-factor-of-one on roughly forty paths, and worse than it sounds:
GitHub refuses to let an author approve their own PR, so a path whose
only code owner is its author cannot be approved by anyone. The gate was
satisfiable only by admin bypass, which means it was not a gate.
Ownership now routes to five role teams — maintainers (CTO), platform,
software, product, data — with maintainers on every rule and every rule
naming a team of at least two people. Roles follow the team roster.

**Ordering.** The header comment claimed "first-match-wins." GitHub is
last-match-wins. Because everything routed to one account, the error was
invisible; it became load-bearing the moment different paths had
different owners. Twelve crypto- and identity-ADR patterns sat above a
general `docs/adr/` line and were, every one of them, dead. The file is
now ordered broadest-first with the security-critical block last, and
says so in the header.

**Approval count.** §Requirements asks for two approvals on
security-sensitive paths.
CODEOWNERS can route a path to the platform team but cannot set how many
approvals it takes — that is a branch-protection setting, configured
separately from this file.

---

*See `GLOSSARY.md` for terms and acronyms.*
