# ADR 0111 — Vendor-friendly license posture: open core, permissive default, anti-commercial licenses disqualified

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-27 |
| Repo scope | Multi |
| Supersedes | — |
| Superseded by | — |
| Related | [`0000-why-adrs-and-procurement-resilience.md`](0000-why-adrs-and-procurement-resilience.md), [`0104-no-prime-capture.md`](0104-no-prime-capture.md), [`0106-sovereign-tooling-preference.md`](0106-sovereign-tooling-preference.md), [`0110-license-posture-agplv3-forked-components.md`](0110-license-posture-agplv3-forked-components.md), `../project/license-audit-2026-04-27.md` |

## TL;DR (for a layperson)

We want three things at once: real open-source benefit (anyone can
read, audit, contribute, fork), a healthy vendor ecosystem (vendors
can sell support, modify the code, build proprietary extensions on
top, and make money doing it), and the ability for some parts of
the larger ecosystem — partner integrations, mission-specific
classified components, vendor "secret sauce" — to be proprietary.
Some open-source licenses are friendly to that mix; some
specifically forbid it. This ADR locks in the friendly defaults for
everything we control, names the licenses that disqualify a
dependency, and queues a one-time audit of the dependencies we
already have.

## Context

Three pressures meet here:

1. **Open-source benefit** — already locked in by [ADR 0000](0000-why-adrs-and-procurement-resilience.md)
   and [ADR 0106](0106-sovereign-tooling-preference.md). Source
   availability, sovereign control, partner-share-without-NDA.
2. **Vendor profitability** — partners and contractors who service,
   integrate, extend, and resell our system must be able to make
   money doing so. The ecosystem is healthier if vendors have a
   commercial path. This is also explicit project policy: we do
   not want to *prevent* vendor profit (that's a different ADR
   away from where we are), we want to *enable* it while keeping
   the open core open.
3. **Proprietary ecosystem layer** — some components in the larger
   ecosystem will inevitably be proprietary (vendor sensors, mission
   integrations, classified payload code, partner secret sauce).
   The license posture must let proprietary code coexist next to
   open code without forcing one to convert the other.

These pressures are mostly compatible — but several specific
licenses in the modern open-source landscape are *designed* to
disrupt one of them. SSPL, Commons Clause, and restrictive Business
Source License variants explicitly target vendor commercialization
of the licensed software. Strong network copyleft (AGPL §13) is
*compatible* with vendor profit but only via the
service-and-support model — it forecloses the
"keep proprietary modifications closed" strategy. The boundary is
fine and contributors can land us on the wrong side of it without
realizing it.

This ADR exists to make that boundary unambiguous and reviewable.

## Decision

### D1 — Default license for our own code: **Apache 2.0**

All code we author in this repository (and any future repository in
the project) is licensed Apache 2.0 unless an explicit ADR records
an exception. Apache 2.0 chosen over MIT/BSD because of the
explicit patent grant — material protection for vendors building
on our code. Every new top-level repository gets `LICENSE` (Apache
2.0) and `NOTICE` files at root in the same PR that lands the
repository.

### D2 — Preferred-license filter for third-party dependencies

Tool-and-library selection (per [ADR 0106](0106-sovereign-tooling-preference.md)
five-question filter) extends with a license-tier preference:

- **Tier 1 — Permissive (Apache-2.0, MIT, BSD-2/3-Clause, ISC).**
  Default choice. Vendors can build proprietary extensions; no
  obligation back. Almost the entire stack we run lives here.
- **Tier 2 — Weak copyleft (LGPL-2.1+, MPL-2.0).** Acceptable.
  Vendors can build proprietary extensions by linking dynamically
  (LGPL) or by keeping vendor code in separate files (MPL).
- **Tier 3 — Strong copyleft (GPL-2.0/3.0).** Acceptable for
  dev-tools and CLI utilities; rejected as a dependency that gets
  linked into a binary we distribute. (We typically don't
  distribute binaries — we distribute container images, which is
  arguably "distribution" under GPL terms. Treat as Tier 4 for
  any code that ends up *inside* a container we ship.)
- **Tier 4 — Network copyleft (AGPL-3.0).** Acceptable only at
  process boundary (separate container, no library linking, no
  Go `replace` directives). Vendors can profit via service /
  support / hosted offerings; vendors *cannot* keep proprietary
  modifications of the AGPL component itself closed. Per [ADR 0110](0110-license-posture-agplv3-forked-components.md)
  D4. Currently in stack: netbird-mgmt (modified upstream).
- **Tier 5 — Disqualified by default.** Anti-commercial /
  vendor-hostile licenses. See D3.

### D3 — Anti-commercial license disqualification list

Disqualified by default for any dependency that ends up in our
product (binary, container, or shipped artifact):

- **SSPL (Server Side Public License, Elastic
  pre-relicense).** Forces a vendor offering the software as a
  hosted service to release the *entire* service-stack source
  under SSPL. Vendor-hostile by design.
- **Commons Clause** (typically applied as a rider to MIT/BSD/
  Apache). Adds a "no Sale" restriction on top of an otherwise-
  permissive license. Vendor-hostile by design.
- **Elastic License v2 (ELv2)** — restricts managed-service
  offerings; license is not OSI-approved.
- **Business Source License (BSL) when restrictions cover our
  use case.** BSL is *conditionally* acceptable per [ADR 0106](0106-sovereign-tooling-preference.md)
  §"BSL is acceptable" — but only when the BSL grant covers our
  use case and the change-date conversion target is OSS. BSL with
  restrictions that block vendor-managed-service offerings is
  Tier 5 for any vendor that we expect to integrate. Read the
  specific BSL grant on a per-dependency basis.
- **PolyForm Shield / Strict / Internal-Use** — designed to
  prevent commercial use by competitors; defaults to Tier 5.
- **Custom "non-commercial" or "research-only" licenses.**
  Tier 5.
- **No-license / "all rights reserved"** code. Tier 5; same
  legal posture as a closed-source dependency without a contract.

A Tier-5 dependency may be adopted only with an explicit ADR that
(a) names the alternatives considered, (b) confirms the license
covers our specific use case in writing, (c) names a trigger
condition for replacement.

### D4 — Process-boundary discipline applies generally

D4 of [ADR 0110](0110-license-posture-agplv3-forked-components.md)
(process-boundary aggregation for AGPL components) generalizes:
**any Tier 3 or Tier 4 dependency runs as a separate process,
never linked as a library into our own services or into a Tier 1
component.** This is what protects vendors who build proprietary
extensions on top — their integrations sit on the API side of the
boundary, never inside the AGPL/GPL binary, so their code is not
implicated by the copyleft license.

### D5 — Vendor extension surface

The intentional extension surface for vendors is:

- **Service / support / hosting** — even AGPL components support
  this profit model (Red Hat, Confluent, Elastic-
  pre-relicense). No license obstruction.
- **Proprietary peer code on the mesh** — partners run their own
  services that talk to ours via mesh networking, Zenoh, mgmt API,
  REST, gRPC. Their code is theirs; never derived from our
  AGPL components.
- **Proprietary shims that bridge vendor-specific protocols to
  Zenoh / our APIs.** Per [ADR 0209](0209-shim-pattern.md),
  shims are the standard pattern for vendor cruft absorption.
  Shims may be open-source (we prefer it; the TAK-to-Zenoh
  reference is) or proprietary (vendor's choice). The vendor's
  shim is process-boundary-isolated from any AGPL component by
  the standard architecture.
- **Proprietary integrations + mission payloads.** Same shape:
  process-boundary, API-mediated, no source-level dependence on
  AGPL/GPL code we ship.

### D6 — Audit obligation

A one-time license audit of the existing dependency stack is
queued at `../project/license-audit-2026-04-27.md`
and scheduled as a Phase 2D task. Per-dependency tier classification +
flagging of Tier-5 licenses to remove or escalate. Output goes back
into this ADR as either "no exceptions found" or as a list of
explicit Tier-5 ADRs.

### D7 — Recurring license-watch hook

After the one-time audit, license tier shifts (e.g., a vendor
relicenses upstream the way HashiCorp did
with BSL) are caught by:

- A pin-regression CI hook (per [ADR 0703](0703-pinned-digests-and-pin-regression-ci.md)
  pattern) that re-runs license detection (`go-licenses`,
  `cargo-deny`, `pip-licenses`, `npm-license-checker` —
  language-appropriate) on every dependency bump.
- The findings register pattern (per ADR 1011) — when an upstream
  relicense is observed, file a finding with the trigger condition
  (mirroring F-010).
- Periodic (every 6 months) re-scan of the full dep tree with the
  audit framework.

## Alternatives considered

- **MIT or BSD as the default for our own code instead of
  Apache 2.0.** Apache 2.0 wins on the explicit patent grant.
  MIT/BSD have no patent language; vendors building on top assume
  the patent risk. Rejected (Apache 2.0).
- **AGPL or GPL as the default for our own code.** Would force
  every vendor extension into the same license — defeats the
  vendor-profitability requirement. Rejected.
- **Permissive-only (Tier 1 strictly).** Ignores the value of
  Tier 2-4 dependencies that already exist in our stack and
  serve us well (netbird-mgmt is the obvious example; PostgreSQL
  is Tier 1 already). Rejected; the tiered preference + process-
  boundary discipline is the right shape.
- **No explicit Tier 5 list.** Lets a future contributor pull in
  an SSPL or Commons Clause dependency without realizing the
  vendor-impact. Rejected; the explicit list is reviewable.
- **Defer the license audit to "as time permits."** This is how
  license tier shifts catch organizations off-guard. Rejected;
  the audit is queued as a Phase 2D task.

## Consequences

**Positive.**

- The vendor-profit story is explicit and defensible. Partners
  and contractors can read this ADR and know what they can build
  proprietary, what they must keep open, and where the boundaries
  are.
- The Tier-5 disqualification list is an early warning that
  prevents accidental adoption of vendor-hostile licenses.
- The audit obligation closes a real risk surface (we don't
  currently know with certainty whether every transitive
  dependency is Tier 1-4).
- Apache 2.0 is the default DoD-and-allied-procurement-friendly
  license and the OSI / Linux Foundation house preference. Avoids
  a future re-license event for our own code.

**Negative.**

- The license audit is real engineering work (estimated 3-5 days
  per major language stack — Go, Rust, Python, Node).
- The Tier-5 disqualification list will need maintenance —
  licenses come and go; new vendor-hostile constructions appear
  every few years.
- A one-time mistake (vendor adopts a Tier 5 license between our
  audit and their next release) requires triage and possibly
  replacement. Mitigated by D7's pin-regression hook.

## Assessor view (SCA-R)

NIST SP 800-53 SR-3 (supply chain) is served better by license-
posture clarity than by license-posture ambiguity — assessors
already know that AGPL, SSPL, and Commons Clause have different
posture implications. The four-tier preference and Tier-5
disqualification list is a reviewable decision artifact that
demonstrates we have engineered around the variety in the open-
source ecosystem rather than treating "open source" as a
monolithic checkbox.

For partner federation: this ADR makes the
license posture an explicit input to partner negotiation. A
partner who cannot accept AGPL has a documented escape ramp
(ADR 0110 Trigger Conditions); a partner whose stack contains
SSPL components is a documented integration risk.

## Trigger conditions for revisit

- **License audit completes** and produces findings that require
  decisions (Tier-5 dependency to remove or escalate). Action:
  per-finding ADR or remove-and-replace work item.
- **A specific component we depend on relicenses to a Tier-5
  license** (mirror of the netbird AGPL relicense, but for a
  permissive-now / restrictive-later component). Action:
  emergency replacement spike + finding under ADR 1011.
- **A vendor partner identifies a license that blocks their
  commercial path.** Action: revisit Tier 4 / 5 boundaries,
  consider whether to replace the dependency or to adjust the
  partner integration shape (e.g., shim pattern absorbs the
  problem).
- **OSI or FSF approves a new vendor-friendly license worth
  promoting.** Action: extend D2 tier preferences if useful.
- **Apache 2.0 itself becomes problematic** (extremely unlikely
  in a 5-year window but worth naming). Action: re-evaluate
  default for our own code.

---

*See `GLOSSARY.md` for terms and acronyms.*
