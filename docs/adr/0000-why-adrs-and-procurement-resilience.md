# ADR 0000 — Why we use ADRs (and why they are a procurement-resilience tool)

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core (durable across any future split) |
| Supersedes | — |
| Superseded by | — |
| Related | `0100-cato-not-monolithic-ato.md`, [`0102-two-track-development.md`](0102-two-track-development.md), `0850-sbu-e-approval-path.md` |

## TL;DR (for a layperson)

We write down every important decision — what we chose, what we rejected,
and why — in a fixed, short document. This protects the system from the
single most common failure mode in defense IT: the people who chose the
architecture leave, the next people don't know why it looks the way it
does, they "fix" it by adding bureaucratic middleware, and the system
becomes another shelfware program. ADRs are how we keep the original
spirit of the system intact across years, contracts, personnel changes,
and accreditation cycles.

## Context

goat is built on open-source technology to deliver secure
unclassified combat networks. A system like this eventually needs formal
government authorization. Every previous
attempt to build distributed tactical data systems through the official
process has produced the same outcome: a working system at the start, an
APL-frozen snapshot in the middle, and an unmaintainable artifact at the
end. The structural causes are well understood:

1. **Monolithic ATO** approves a snapshot of bits, not a process. Any
   change requires re-accreditation, which incentivizes bundling, which
   produces gold-plated frozen artifacts.
2. **Systems-integrator capture.** A prime owns the integration glue,
   becomes the only entity that can change anything, and freezes the
   upgrade path to protect its margin.
3. **Configuration drift** between the "real" engineering and the
   accredited baseline. Real work happens out-of-band, the accredited
   version stagnates, and the two die on contact.
4. **Personnel rotation.** Three years after the original engineers
   leave, no one can answer "why did we choose this?". The next PM
   pattern-matches the system to whatever they last worked on and
   "fixes" it by adding a familiar abstraction. The fix breaks the
   architecture's load-bearing properties.

ADRs attack cause #4 directly, and pre-empt #1, #2, and #3 by making
the architectural intent legible to the people who decide whether the
system is approved or frozen.

## Decision

Every load-bearing technical, security, and operational decision in
goat is captured as an ADR in [`docs/adr/`](.). Each ADR is:

- **Numbered** — sequential, reserved across topical bands (see
  [`INDEX.md`](INDEX.md)). Numbers are durable across monorepo splits;
  see [`0004-repo-scope-tagging.md`](0004-repo-scope-tagging.md).
- **Tri-audience** — written for the technical contributor, the
  layperson sponsor, and the SCA-R, all in the same document. See
  [`0003-three-audience-writing.md`](0003-three-audience-writing.md).
- **Status-tracked** — Proposed / Accepted / Superseded / Deprecated.
  See [`0002-adr-lifecycle.md`](0002-adr-lifecycle.md).
- **Repo-scope-tagged** — names which sub-repository it travels with
  when the monorepo splits. See
  [`0004-repo-scope-tagging.md`](0004-repo-scope-tagging.md).
- **Decision-only** — design docs in `docs/design/` describe how
  systems work *today*; ADRs record the choice that produced them and
  the alternatives that were rejected. See
  [`0005-decision-log-vs-design-doc.md`](0005-decision-log-vs-design-doc.md).

## Consequences

**Positive.**

- Future contributors and SCA-Rs can pick up cold and assess the system
  against the frame the architects intended, not against the checklist
  they happen to know.
- ADRs are the natural unit of submission for SBU-E / CMMC packages —
  they map directly onto NIST SP 800-171 / 800-53 control narratives.
- Monorepo splits move ADRs wholesale (per repo-scope tag) without
  rewriting them.
- Accreditation submissions can cite ADR numbers as load-bearing
  artifacts; reciprocity from one accreditation to another is supported
  by stable references.

**Negative.**

- ~80 ADRs to author and maintain. Drift between an ADR and the code
  it describes is the dominant failure mode; we treat ADRs the way we
  treat tests — when behavior changes, both the code and the ADR
  change in the same PR.
- A new contributor encounters more documentation up front. We mitigate
  with the TL;DR section in every ADR and the curated reading order
  in `README.md`.

## Assessor view (SCA-R)

This ADR is itself the answer to the question "why does this system
look unfamiliar?" An SCA-R encountering goat is expected
to be looking for: identity-based access control (NIST SP 800-171
3.1.1, 3.5.x), data flow enforcement (3.1.3), audit and accountability
(3.3.x), and configuration management (3.4.x). The ADR series maps
each of these to the specific architectural choice that delivers them
and to the residual risk we accept. The compliance posture document
`../compliance/comparative-posture.md`
gives a dimension-by-dimension comparison to median DoD network
posture. The
`0850-sbu-e-approval-path.md` ADR
maps the ADR series onto the NIST SP 800-171 / CMMC 2.0 Level 2
assessment artifact set.

## Trigger conditions for revisit

- The series exceeds ~150 ADRs and becomes hard to navigate. Revisit
  the topical bands and consider sub-grouping.
- A new accreditation framework is adopted that requires a different
  artifact shape. Revisit the template, not the principle.
- A monorepo split produces a sub-repo with fewer than ~5 ADRs.
  Consider folding back into the parent until the count justifies a
  separate index.

---

*See `GLOSSARY.md` for terms and acronyms.*
