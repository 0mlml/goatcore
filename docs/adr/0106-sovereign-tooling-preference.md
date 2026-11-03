# ADR 0106 — Sovereign tooling preference — open-source first, BSL acceptable

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | Multi |
| Supersedes | — |
| Superseded by | — |
| Related | [`0101-mosa-as-policy-compliance.md`](0101-mosa-as-policy-compliance.md), [`0104-no-prime-capture.md`](0104-no-prime-capture.md), [`0701-iron-bank-not-trust-anchor.md`](0701-iron-bank-not-trust-anchor.md) |

## TL;DR (for a layperson)

We prefer open-source software for almost everything. This is not
because open source is morally better. It is because the
characteristics we actually need — the ability to read the code,
modify it, share it with partners without asking permission, and
keep using it after a vendor disappears — happen to be the
characteristics that well-adopted open-source projects reliably
deliver. A vendor product or a Government-Off-The-Shelf system
meets the bar if it has those same characteristics. In practice,
very few do. Business Source License (BSL) software can qualify
under specific conditions documented here.

## Context

The technology strategy (`../design/tech-strategy.md`)
articulates the requirement at length. The short version: the
requirement is not "open source," the requirement is **transparent,
sovereign, shareable, observable, modifiable, understandable, and
battle-proven**. Any technology that delivers all of those
characteristics meets the bar.

In practice, that set is dominated by well-adopted open-source
projects. The reasons are structural: open-source projects with
active communities accumulate the auditability, the deployment
ergonomics, the documentation, and the cross-organizational
expertise that the requirements demand. Closed-source vendors can
deliver some of those characteristics in isolation but rarely
deliver all of them.

This ADR codifies the preference and names the acceptable exceptions
so that a future contributor or program office cannot accidentally
pivot to a closed-source product on the assumption that open-source
preference was an aesthetic choice.

## Decision

Tool selection is governed by a five-question filter (from
`../design/tech-strategy.md`):

1. Does an existing open-source project solve this?
2. Can we use it with minimum modification?
3. Is it deployable via Terraform, Ansible, and Docker?
4. What happens if it fails?
5. Is this the simplest option that meets the requirement?

A "no" to #1 with a documented "yes" to a closed-source alternative
is allowed only when:

- The closed-source product passes the same characteristic tests
  (transparent, sovereign, shareable, observable, modifiable,
  understandable, battle-proven) at the level relevant to its
  role.
- An ADR documents the choice with named alternatives considered
  and a trigger condition for revisit.

**Business Source License (BSL) is acceptable** under specific
conditions:

- The BSL grant covers our use case (typically: production use by
  a US government contractor, federation with allies).
- The license converts to an OSS-approved license on a defined
  date (BSL "change date").
- We are comfortable with the change-date conversion target.
- An ADR records the dependency.

**Government Off-The-Shelf (GOTS)** meets the bar if the source is
available, modifiable, and the deployment surface is understood.
**Vendor products** meet the bar if any US-government-contracted
organization can inspect the source, build from it, and
interoperate without NDAs that restrict visibility.

**Disqualified by default** (this enumeration is an ADR-level
formalization of the principles in `tech-strategy.md` rather than
a verbatim list from that document — the design doc states the
characteristic test in prose; this ADR turns the most common
disqualifying patterns into reviewable items):

- Closed-source components in the **critical path** (mesh control
  plane, identity, crypto boundary, audit integrity).
- Tools whose vendor restricts coalition-partner sharing.
- Tools that require online phone-home to a vendor service for
  basic functionality (degrades island-mode resilience —
  see [`0206`](0206-island-mode-by-construction.md)).
- Tools where the vendor reserves the right to terminate access
  on policy disagreement.

> **Correction note (2026-04-25).** The original draft of this
> ADR presented the four disqualifications as if they were
> verbatim from `tech-strategy.md`. They weren't — the design
> doc's relevant section originally used a characteristic-test
> framing (transparent / sovereign / shareable / observable /
> modifiable / understandable / battle-proven) and discussed
> the disqualifying patterns in prose only. **Promoted
> 2026-04-25:** the four-item enumeration is now in
> `tech-strategy.md` under the heading "Disqualified-by-default
> patterns" alongside the prose characteristic test. The ADR
> and the design doc now agree.

## Alternatives considered

- **"Open source only" as an absolute rule.** Loses tactical
  flexibility (e.g. Cisco IOS-XE on the canonical site switch).
  Rejected; replaced by the characteristic-based test.
- **No preference; tool-by-tool decisions.** Lets the borking
  pattern in. Rejected.
- **Iron Bank trust signal as a substitute for source review.**
  Iron Bank hosts many insecure containers; not a trust anchor.
  See [`0701`](0701-iron-bank-not-trust-anchor.md). Rejected.

## Consequences

**Positive.** The system is composed of components we can read,
modify, share, and deploy ourselves. Coalition partners can audit
or run it without licensing negotiations. Vendor disappearances
do not bork the system. Supply chain controls (the 0700-series)
can rely on source-level review.

**Negative.** Some categories (specialty radios, some sensor
hardware, certain partner systems) do not have meaningful open-
source alternatives. We accept those at the periphery, never in
the critical path, with explicit ADRs documenting the exception.

The five-question filter takes work to apply. We accept the cost;
it is what prevents accidental closed-source adoption.

## Assessor view (SCA-R)

NIST SP 800-53 SR-3 (supply chain controls and processes), SR-4
(provenance), SA-12 (supply chain protection), and CM-7 (least
functionality) are all served by source-availability. NIST SP
800-160 vol. 1 (engineering trustworthy systems) discusses
transparency as a foundational property; our characteristic-based
test is consistent with that frame.

For specific dependencies, the 0700-series ADRs (supply chain)
identify which components are open-source, which are BSL with a
defined change-date, and which are closed-source-by-exception.
The accepted-risks register `0855`
tracks the latter.

## Trigger conditions for revisit

- A closed-source product genuinely passes the characteristic test
  in the critical path. Rare but possible (e.g. some attestation
  hardware). Add the exception with explicit ADR; do not loosen
  the general rule.
- BSL terms shift in a way that no longer meets our criteria
  (change date pushed out indefinitely, conversion target changed
  to a non-OSS license). Treat as a license incident; replace the
  dependency.
- A contracting vehicle requires use of a closed-source DoD
  product. Negotiate the requirement first; concede only with
  ADR-documented exception.

---

*See `GLOSSARY.md` for terms and acronyms.*
