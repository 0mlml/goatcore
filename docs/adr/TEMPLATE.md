# ADR NNNN — <short, present-tense decision>

| Field | Value |
|---|---|
| Status | Proposed \| Accepted \| Superseded \| Deprecated |
| Date | YYYY-MM-DD |
| Repo scope | goat core \| goat-bond \| <future-sub-repo> \| Multi |
| Supersedes | ADR-NNNN \| — |
| Superseded by | ADR-NNNN \| — |
| Related | ADR-NNNN, ADR-NNNN |

## TL;DR (for a layperson)

One paragraph. No jargon. What we did, why a non-technical reader should
care, and what would change for them if we had chosen differently. Three
to five sentences. If you cannot write this section without jargon, the
decision is probably not yet clear enough to commit.

## Context

The forces that make this decision necessary. The constraint, the
problem statement, the prior state. Stay specific to this decision —
do not re-litigate the project's overall architecture. Two to four
short paragraphs.

## Decision

The choice itself, in declarative voice, with enough specificity that a
contributor can implement against it without further interpretation.
"We use X. Configuration is Y. The boundary with Z is W." Bullet lists
are fine. This is the load-bearing section.

## Alternatives considered

Brief, fair characterization of each rejected option and the specific
reason it was rejected. One short paragraph or row per alternative.
Fairness matters: an SCA-R reading this should not conclude we
strawmanned the alternatives.

## Consequences

**Positive.** What gets easier or safer.

**Negative.** What gets harder or more fragile. What residual risk we
accept. What we owe the future maintainer.

## Assessor view (SCA-R)

The shape of the decision as it appears to a security control assessor.
Map to NIST SP 800-171 / 800-53 control families where relevant. Name
any waiver this depends on (link to `../compliance/waivers/`). Identify
the accreditation pathway this decision is consistent with (cATO,
CMMC L2, CSfC, etc.). Identify the residual risk and the compensating
control. Three to six sentences.

## Trigger conditions for revisit

Specific, observable conditions under which this decision would be
re-examined. "When host count per site exceeds 20." "When FIPS 140-3
becomes a hard gate." "When vendor X drops the BSL clause." Not "if
something changes."
