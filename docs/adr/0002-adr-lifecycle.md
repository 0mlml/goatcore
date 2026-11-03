# ADR 0002 — ADR lifecycle and status semantics

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | Multi |
| Supersedes | — |
| Superseded by | — |
| Related | [`0001-adr-template.md`](0001-adr-template.md), [`0005-decision-log-vs-design-doc.md`](0005-decision-log-vs-design-doc.md) |

## TL;DR (for a layperson)

Decisions change. ADRs are immutable once accepted — when we change a
decision, we write a new ADR that supersedes the old one and leave the
old one in place. This way the history of the system's reasoning stays
readable, and an assessor reviewing an old artifact can follow the
trail forward.

## Context

A naïve "edit the doc when the decision changes" approach destroys the
audit trail and the rationale chain. Defense accreditation specifically
asks "what changed and why?" — answers that require a stable record of
prior decisions and the events that invalidated them. At the same time,
ADRs that never get retired accumulate noise and confuse new readers.
We need an explicit lifecycle.

## Decision

ADRs have one of four statuses. Transitions are one-way.

| Status | Meaning | Allowed transitions |
|---|---|---|
| **Proposed** | Drafted but not yet adopted. Live in the repo so reviewers can comment. | → Accepted, → Deprecated (rejected) |
| **Accepted** | The current decision. Implementation matches it. | → Superseded |
| **Superseded** | A later ADR replaced this one. The "Superseded by" header field names the replacement. The ADR stays in place; its content is unchanged. | (terminal) |
| **Deprecated** | Withdrawn as never-adopted (e.g., a Proposed ADR that was rejected). Stays in place to preserve the trail. | (terminal) |

Rules:

1. **Accepted ADRs are immutable** except for typo fixes and the
   "Superseded by" header field. Substantive changes require a new
   ADR that supersedes the old one.
2. **Numbers are never reused.** A Deprecated or Superseded ADR keeps
   its number forever.
3. **Supersession is explicit.** The new ADR's `Supersedes` field
   names the old number; the old ADR's `Superseded by` field names
   the new number; both are updated in the same PR.
4. **Status changes are visible in commit history.** Every status
   transition is its own commit so the lifecycle is reconstructable
   from `git log`.

## Amendment 1 (2026-09-04) — the status field leads with the token

The four statuses above were decided on 2026-04-25 and nothing enforced them.
A sweep on 2026-09-04 found the corpus had drifted in one consistent direction:
ADRs were recording **build state** in the **decision status** field —
`**Built, partially adopted**`, `**Phase 1 built in code**`,
`**Plans only — deliberately not built.**`. Each is a true and useful sentence
about implementation. None of them is a lifecycle state, and while the field
holds both, neither question is answerable from it.

That collision has a concrete cost. [`check-adr-status-vs-code.sh`](../../ops/ci/check-adr-status-vs-code.sh)
exists to catch an ADR whose status *under-claims* what is built. It can only
do that if the status field means "is this decision adopted". When the same
field also answers "has the code shipped", the guard is reading two questions
through one string.

**The rule.** The status field MUST lead with one of the four tokens.
Everything after it is free prose and is preserved verbatim — the caveats are
worth keeping and this amendment deletes none of them. Only the first word is
constrained:

| | |
|---|---|
| Rejected | `**Built, partially adopted** — status corrected 2026-08-31` |
| Accepted | `Accepted — **built, partially adopted**; status corrected 2026-08-31` |

The information is identical. The first cannot be ranked by any reader; the
second can.

**Four header formats are tolerated, not blessed.** The field is carried as a
markdown table row (the [`0001`](0001-adr-template.md) template), a
`**Status:**` line, a `- **Status:**` bullet, and YAML frontmatter.
[`ops/ci/adr_status.py`](../../ops/ci/adr_status.py) reads all four, because a
reader that knows only one silently sees a subset — which is how an earlier
audit concluded the field was *missing* from 105 files when it was missing from
none. New ADRs should follow [`0001`](0001-adr-template.md); existing ones are
not being churned to convert a format that parses correctly.

**Enforcement.** [`check-adr-status-vocabulary.py`](../../ops/ci/check-adr-status-vocabulary.py),
wired into the `adr-number-uniqueness` workflow alongside the other three ADR
guards. It never infers a token from prose — an ADR reading `Phase 1 deployed`
is *probably* Accepted, and "probably" is how a decision log ends up asserting
a state nobody ratified. It fails instead. Known non-conformers are baselined
shrink-only in `ops/ci/adr-status-baseline-vocabulary.txt`; one ADR (1045) is
listed there because it records a genuine per-facet split that belongs to its
owner, not to this amendment.

**This amendment changed no lifecycle state.** Eight ADRs were re-encoded
(0401, 0402, 0827, 1066, 1068, 1112, 1150, 1151) and each kept the state its own
text already asserted. Re-encoding 1112 surfaced a stale INDEX row — the file
was corrected on 2026-08-31 and the index was missed in the same PR — which is
the class of drift these guards exist to find.

## Alternatives considered

- **In-place editing** — destroys the audit trail. Rejected.
- **No "Proposed" state, only Accepted from day one** — forces
  decisions to be private until ready, which loses the review-in-PR
  benefit. Rejected.
- **"Rejected" as a separate status** — folded into Deprecated to
  keep the state machine small.

## Consequences

**Positive.** History is reconstructable. Supersession chains are
walkable. Assessors get a stable artifact to cite.

**Negative.** The directory accumulates Superseded ADRs over time. We
mitigate with [`INDEX.md`](INDEX.md) which separates Accepted from
Superseded / Deprecated lists. After ~5 years we may add a `archive/`
subdirectory; do not pre-optimize.

## Assessor view (SCA-R)

The lifecycle gives an assessor a stable reference for any prior
decision. NIST SP 800-171 3.4.1 (CM-2 Baseline configuration), 3.4.3
(CM-3 Change control), and 3.12.x (CA-x Assessment) are all served by
the immutability + supersession-chain pattern. Continuous-ATO
(cATO) narratives can cite a specific ADR-N with full confidence
that the cited content will not change.

## Trigger conditions for revisit

- The number of Superseded ADRs exceeds ~30 and the index becomes
  noisy. Add an archive subdirectory.
- An accreditation framework requires a status not in our state
  machine. Extend, do not fork.

---

*See `GLOSSARY.md` for terms and acronyms.*
