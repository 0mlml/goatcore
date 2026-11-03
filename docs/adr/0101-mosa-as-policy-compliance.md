# ADR 0101 — MOSA as policy compliance, not just engineering taste

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | Multi |
| Supersedes | — |
| Superseded by | — |
| Related | `0100-cato-not-monolithic-ato.md`, [`0104-no-prime-capture.md`](0104-no-prime-capture.md), [`0106-sovereign-tooling-preference.md`](0106-sovereign-tooling-preference.md) |

## TL;DR (for a layperson)

US law (10 USC 4401) requires defense systems to be built using a
Modular Open Systems Approach. We are doing this anyway because it is
the only architecture that has ever produced working large-scale
distributed systems. But we cite the law, in writing, in our
submission packages — because that citation is the strongest
defense against future program-management pressure to fold the
system into a proprietary integration platform.

## Context

The Modular Open Systems Approach (MOSA) is established US law and
DoD policy: 10 USC 4401, DoD Instruction 5000.02 paragraph 5.b, and
the OUSD(R&E) Modular Open Systems Approach Implementation Guide.
The policy requires that major defense acquisition programs use:

- Modular system designs with severable components.
- Open standards and interfaces, with rights to modify.
- Government-defined interface controls.
- Support for competitive substitution of components.

Two structural facts make MOSA load-bearing for this project:

1. **MOSA is the policy-level analog of the engineering choice we are
   already making.** Our open-source-first technology strategy
   (see [`0106`](0106-sovereign-tooling-preference.md)) is MOSA in
   substance.
2. **MOSA is the strongest legal defense against prime capture.**
   Without it, a downstream program office can adopt a proprietary
   integration layer and the system becomes a bilateral integration
   pile. With it cited, that pivot is a violation of acquisition law.

The borking-then-freezing pattern depends on prime capture (see
[`0104`](0104-no-prime-capture.md)). MOSA citations are how we make
that capture institutionally costly.

## Decision

We treat MOSA as a *load-bearing* element of every architectural and
procurement decision, not as a flavor of engineering preference.
Specifically:

1. **MOSA citation in every submission package.** Every
   authorization narrative names the MOSA pathway and
   maps the architecture onto its four required properties (modular
   design, open standards, government-defined interfaces, competitive
   substitution).
2. **Component severability is a hard requirement.** Every
   architectural decision passes the test "can this component be
   substituted by a competitive alternative without breaking the
   system?" Where the answer is no, we either redesign or
   document a deliberate exception (see `0855`).
3. **Government-defined interfaces, not vendor-defined.** Our
   integration surface (the protocols other systems use to plug in)
   is defined by us, not by vendors we integrate with. Where we
   adopt a vendor protocol (e.g. TAK), we adopt it as a public
   standard and absorb vendor cruft via the shim pattern
   (see [`0209`](0209-shim-pattern.md)).
4. **MOSA is named in resistance to platform abstractions.** When a
   sponsor or partner proposes a proprietary integration platform,
   our response cites MOSA explicitly: "this would violate 10 USC
   4401's competitive-substitution requirement; here is the
   open-protocol alternative."
5. **The MOSA citation is durable across personnel changes.** It
   lives in the README,
   and in the ADR series so a future PM cannot accidentally walk
   past it.

## Alternatives considered

- **Treat MOSA as background context, not as load-bearing.** Default
  industry posture; produces systems that pass MOSA review on paper
  while being structurally bilateral. Rejected — defeats the purpose.
- **Cite MOSA only in the accreditation submission, not in
  engineering ADRs.** Compliance-narrative-only treatment; loses
  the engineering effect. The point of MOSA is that it informs
  *engineering* decisions; ADRs are where those decisions live.
  Rejected.
- **Build to MOSA without citing it.** Loses the legal defense
  against prime capture. Rejected.

## Consequences

**Positive.** Every architectural review has a clear test
(severability, open interface, competitive substitution). When a
proprietary integration layer is proposed downstream, the ADR series
provides the citation that makes the pivot institutionally
expensive. Submission packages map cleanly onto MOSA review
artifacts.

**Negative.** MOSA discipline is harder than ad-hoc integration. A
specific request to integrate with a vendor on the vendor's terms
will sometimes be answered with "no — we'll adopt the open protocol
they support, and absorb the rest in a shim." That answer is right
but less convenient.

## Assessor view (SCA-R)

MOSA review is conducted by the program's MOSA Implementation
Officer (typically separate from the SCA-R, but coordinated with
them). Our MOSA submission cross-references the ADR series at the
component level: every proprietary protocol bridge is documented as
a shim with named open-protocol alternatives, every integration
point is documented as a government-defined interface, every
substitutability claim is tied to a specific named alternative
component. NIST SP 800-160 vol. 1 system-engineering principles
align with MOSA and provide the cross-walk for assessors who are
more familiar with NIST than with the OUSD(R&E) MOSA guide.

## Trigger conditions for revisit

- 10 USC 4401 is amended in a way that changes the four required
  properties.
- DoD policy is updated such that MOSA is no longer the load-bearing
  framework (unlikely).
- A specific architectural decision creates a non-substitutable
  component without an ADR-documented exception. Treat as a
  drift incident, not as grounds to revisit.

---

*See `GLOSSARY.md` for terms and acronyms.*
