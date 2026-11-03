# ADR 0105 — The ADR series as SCA-R enablement artifact

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | Multi |
| Supersedes | — |
| Superseded by | — |
| Related | [`0000-why-adrs-and-procurement-resilience.md`](0000-why-adrs-and-procurement-resilience.md), [`0003-three-audience-writing.md`](0003-three-audience-writing.md), `0107-comparative-posture-artifact.md`, `0850-sbu-e-approval-path.md` |

## TL;DR (for a layperson)

The Security Control Assessor-Representative (SCA-R) is the
technical assessor whose recommendation determines whether the
system gets approved for use. We treat them as a first-class
audience for our ADR series — and we plan to brief them on the
architecture *before* the formal package lands on their desk. A
SCA-R who has seen the system explained will assess it against
the frame the architects intended. A SCA-R seeing it cold will
assess it against whatever checklist they happen to know, and
that is where the borking starts.

## Context

The Authorizing Official (AO) signs the authorization, but in
practice the AO follows the SCA-R's recommendation unless
something is politically loaded. The SCA-R does the technical
work: reviews the SSP, validates the security control assessment,
and writes the recommendation that the AO countersigns.

The SCA-R has limited time per system. They look for familiar
patterns. When the system pattern-matches to something they have
seen approved before, they assess against that frame. When it
doesn't, they fall back to checklists — and checklists are written
for the median DoD network, not for unusual architectures like
identity-based mesh, cATO pipelines, or open-source EDR.

This means an SCA-R encountering goat cold will see:
WireGuard (not IPsec), NetBird management (not a familiar product),
Falco/osquery EDR (not a DoD-approved closed-source EDR), per-site
PKI (not DoD PKI), FIDO2 (not CAC). Each of those triggers a
"this is non-standard" reaction, and a non-standard reaction
becomes a finding unless the assessor has been walked through the
*why*.

The fix is two-part: (a) make the architecture legible without
in-person briefing, via the ADR series; (b) brief the SCA-R early,
in person, before the package lands.

## Decision

We treat SCA-R enablement as an explicit workstream with two
deliverables.

**Deliverable 1: The ADR series itself.**

- Every ADR contains an Assessor view (SCA-R) section
  ([`0003`](0003-three-audience-writing.md)) that maps the
  decision to NIST SP 800-171 / 800-53 control families,
  identifies residual risk, names compensating controls, and
  links to relevant waivers.
- The control mapping is consistent across the series so an
  assessor reading multiple ADRs sees the same frame.
- Waivers in `../compliance/waivers/`
  are written in SCA-R-readable shape (rule, reason, compensating
  control, residual risk, accepted-by signature, expiry).
- The comparative-posture artifact (`0107`)
  gives the assessor a one-page dimension-by-dimension view of
  where goat is ahead, at par, and behind median DoD
  network posture.

**Deliverable 2: Pre-submission SCA-R briefing.**

- We identify the likely SCA-R as early as possible in the
  authorization track.
- We schedule a 90-minute architecture briefing structured around
  the ADR series, *before* the package lands on their desk.
- The briefing material is the ADR series itself — TL;DRs +
  Decision sections + Assessor view sections — read in topical
  order.
- We brief on the unusual-but-defended choices first: identity-
  based mesh, cATO posture, open-source EDR, sovereign PKI,
  FIDO2 break-glass.
- We follow up with written Q&A in the repo as appendix to
  `0850`.

## Alternatives considered

- **No SCA-R-specific writing; let the standard SSP carry the load.**
  SSP narratives are written to a structure that does not match how
  SCA-Rs actually triage unusual architectures. Rejected.
- **SCA-R briefing only, no written ADR Assessor view sections.**
  Loses the assessor's ability to come back to material; loses
  successor-SCA-R hand-off when staff rotate. Rejected.
- **Wait until the package lands and respond to findings.** Lets
  the assessor's first impression be cold; recovery is much more
  expensive than prevention. Rejected.

## Consequences

**Positive.** The SCA-R is briefed against our frame before they
form one of their own. The ADR series is durable: a successor
SCA-R can pick up cold and still reach the same conclusion. The
findings rate at formal assessment is reduced because the
architecture has already been explained in writing.

**Negative.** Authoring the Assessor view section in every ADR is
work. We accept it because it is the cheapest insurance against
borking-by-finding.

It also means we have to identify and contact the likely SCA-R
proactively, which is a relationship investment, not a paperwork
exercise. This is acquisition-relationship work that engineering
cannot do alone; it requires the program management track to
coordinate.

## Assessor view (SCA-R)

This ADR is itself the explanation for the writing convention you
will encounter throughout the series. Each Assessor view section
in subsequent ADRs maps that decision to:

- The NIST SP 800-171 rev 2 control number(s) it serves.
- The NIST SP 800-53 rev 5 cross-reference where applicable.
- The waiver(s) the decision depends on, with link.
- The residual risk and the compensating control that addresses
  it.
- Where in the cATO pipeline the decision is operationally
  enforced.

`0850` provides the master mapping
of ADRs to control families. `0107`
provides the dimension-by-dimension comparison to median DoD
network posture, identifying where goat is meaningfully
ahead, at par, or behind. We expect the assessor to read both
before any deep technical drill-down.

## Trigger conditions for revisit

- An assessor returns persistent findings that the Assessor view
  sections are not landing. Adjust the section's content guidance.
- A new accreditation framework requires a different assessor
  artifact shape. Add a section; do not retrofit.
- The pre-submission briefing pattern is rejected by a partner.
  Submit cold and document the lessons learned in a successor
  ADR.

---

*See `GLOSSARY.md` for terms and acronyms.*
