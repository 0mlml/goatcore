# ADR 0003 — Three-audience writing convention

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | Multi |
| Supersedes | — |
| Superseded by | — |
| Related | [`0001-adr-template.md`](0001-adr-template.md), `0107-comparative-posture-artifact.md` |

## TL;DR (for a layperson)

Every ADR is written for three readers in one document: the engineer
implementing it, the sponsor or program officer who needs to understand
why we made the choice, and the security assessor who has to certify
it. We do this because writing three separate documents for the same
decision is the path to drift, and drift is the path to losing the
decision's intent during personnel turnover.

## Context

Defense IT documentation typically optimizes for one audience at a
time. Engineers write for engineers. Compliance staff write for
assessors. Briefing slides are made for sponsors. Each is then
translated, re-translated, and partially re-derived by the others —
which is where the original intent of the architects gets lost. By
the time an SCA-R is reading the system, they are looking at a
compliance narrative written by someone who was not in the room when
the decision was made, citing engineering work they have not read.

The fix is not "write better compliance narratives." The fix is
"write the artifact that all three audiences need to consume, once."

## Decision

Every ADR contains three audience-specific sections, in order:

1. **TL;DR (for a layperson)** — three to five sentences, no jargon,
   no acronyms without expansion. A sponsor or program officer should
   be able to read this section and the Decision section and have a
   complete-enough understanding to make a budget or risk call.
2. **Decision + Context + Alternatives + Consequences** — the
   engineering record. Specific, implementation-grade, no
   hand-waving.
3. **Assessor view (SCA-R)** — the control-family mapping, the
   accreditation-pathway alignment, the residual-risk framing, the
   waiver links. Written so an assessor can read this section
   alone and produce a control-narrative artifact without
   interviewing us.

The three sections are non-overlapping. The TL;DR is not a summary of
the engineering record; it is a stand-alone explanation for someone
who will not read the rest. The assessor view is not a re-derivation
of the engineering record; it is the assessor-facing frame on the
same decision.

## Alternatives considered

- **Separate documents per audience** — drift, drift, drift. Rejected.
- **Engineering-only ADRs with a separate compliance package** — we
  did this implicitly for years; the compliance package became
  outdated within months of any engineering change. Rejected.
- **Briefing-style top-level summary, technical detail in appendices**
  — appendices get skipped by exactly the audience we care about
  (engineers reviewing each other's PRs). Rejected.

## Consequences

**Positive.** A single artifact serves all three audiences. Drift
between audience-specific documents is eliminated by construction.
Authoring is more disciplined because the writer must hold all three
audiences in mind simultaneously.

**Negative.** Authoring is harder. The first few ADRs by any new
contributor will need review specifically against the three-audience
standard. We accept the cost.

## Assessor view (SCA-R)

This ADR is itself the convention an assessor will encounter while
reading the series. The Assessor view section in every other ADR is
where the NIST SP 800-171 / 800-53 control mapping, the waiver
references, and the residual-risk framing appear. An assessor can
read only the Assessor view sections across the series and obtain a
complete control-narrative draft. NIST SP 800-171 3.12.4
(Documentation of system security plan) is served directly by this
convention.

## Trigger conditions for revisit

- Repeated SCA-R or sponsor feedback that one audience section is
  unread or unhelpful. Adjust that section's content guidance, do
  not drop the section.
- An additional audience emerges (operator, partner federator) that
  warrants its own section. Add it; do not retrofit the existing
  ones.

---

*See `GLOSSARY.md` for terms and acronyms.*
