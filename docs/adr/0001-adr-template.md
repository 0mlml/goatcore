# ADR 0001 — ADR template and required sections

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | Multi (every repo using this ADR series) |
| Supersedes | — |
| Superseded by | — |
| Related | [`0000-why-adrs-and-procurement-resilience.md`](0000-why-adrs-and-procurement-resilience.md), [`0003-three-audience-writing.md`](0003-three-audience-writing.md) |

## TL;DR (for a layperson)

Every decision document in this series follows the same shape. That
shape is enforced because it is the cheapest way to make the documents
useful to all three audiences (engineer, sponsor, security assessor)
without writing three documents.

## Context

ADR series across the industry vary widely. Lightweight ADRs (Michael
Nygard's original 4-section format) optimize for engineering legibility
but leave assessors hunting for control mappings. Heavy ADRs (full RMF
artifact templates) drown the engineering signal in compliance prose.
We need a single shape that serves both, plus a layperson section so
sponsors and program officers can follow along without translation.

## Decision

Every ADR uses the canonical template at [`TEMPLATE.md`](TEMPLATE.md).
Required sections:

1. **Header table** — Status, Date, Repo scope, Supersedes / Superseded
   by, Related. Repo scope is mandatory; see
   [`0004-repo-scope-tagging.md`](0004-repo-scope-tagging.md).
2. **TL;DR (for a layperson)** — three to five sentences, no jargon.
3. **Context** — the forces making the decision necessary.
4. **Decision** — declarative, implementation-grade specificity.
5. **Alternatives considered** — fair, specific rejection reasons.
6. **Consequences** — positive, negative, residual risk.
7. **Assessor view (SCA-R)** — control-family mapping, waiver links,
   accreditation-pathway alignment, residual-risk framing.
8. **Trigger conditions for revisit** — observable, specific.

ADRs are short by default. Target length is ~400 words; cap is ~800
unless the decision is unusually load-bearing (the strategy ADRs in
the 0100 series are allowed to run longer because they are read first
by sponsors and SCA-Rs and benefit from being self-contained).

## Alternatives considered

- **Michael Nygard 4-section** — too thin for SCA-R use; would force
  separate compliance narratives.
- **Y-statements / In-context-of templates** — useful for individual
  decisions, do not survive being read in series by an assessor.
- **MADR (Markdown ADR)** — close to what we use, but its option /
  consequence formality is heavier than we need at our scale.
- **Full RMF artifact** — wrong audience; loses engineering signal.

## Consequences

**Positive.** One template means a contributor can author an ADR
without re-deciding the format every time. Assessors find the same
information in the same place across the entire series. The TL;DR
makes ADRs usable as briefing material.

**Negative.** Format discipline costs ~10 minutes of authoring time
per ADR vs. a free-form note. We accept the cost.

## Assessor view (SCA-R)

The assessor can read the **TL;DR** to get oriented, the **Decision**
section to learn the choice, the **Assessor view** section to find the
control mapping, and the **Trigger conditions** section to understand
under what conditions the decision would be revisited (which is the
question that matters for cATO continuous-monitoring narratives). No
section is optional, so an assessor never has to ask "did they
consider the alternative?" or "what about residual risk?". The series
is written to NIST SP 800-171 rev 2 control families and tagged with
references where applicable.

## Trigger conditions for revisit

- The template grows past ~10 sections (we have over-specified).
- An accreditation framework requires a section we do not currently
  have. Add the section to the template, not as a one-off.
- Repeated reviewer feedback that a section is unread. Drop it.
