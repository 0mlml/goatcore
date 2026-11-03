# ADR 0104 — Government-owned integration glue — no prime capture

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | Multi |
| Supersedes | — |
| Superseded by | — |
| Related | [`0101-mosa-as-policy-compliance.md`](0101-mosa-as-policy-compliance.md), [`0106-sovereign-tooling-preference.md`](0106-sovereign-tooling-preference.md), [`0209-shim-pattern.md`](0209-shim-pattern.md) |

## TL;DR (for a layperson)

The single biggest reason defense IT programs become unfixable is
that the prime contractor ends up owning the integration glue —
the connectors, the deployment scripts, the operational playbooks
— and becomes the only entity that can change anything. Once that
happens, every upgrade requires the prime's permission, and the
upgrade path becomes the thing that gets frozen. We prevent this
by keeping the integration glue, the deployment automation, and
the operational knowledge in government-owned form. Open-source
upstream components are necessary; government-owned glue is
sufficient.

## Context

The defense IT borking pattern has three structural causes (see
[`0000`](0000-why-adrs-and-procurement-resilience.md)):
monolithic ATO, configuration drift, and **systems-integrator
capture** — the third of which is the most expensive to reverse.

Capture happens when the prime contractor is the only entity that
can:

- Modify the integration code that connects the system's
  components.
- Re-deploy the system after a change.
- Diagnose operational failures end-to-end.
- Update the system to incorporate new components.

When all four are true, the prime is operationally indispensable.
The prime then has every economic incentive to keep them all true,
and every incentive to resist architectural moves that would make
the integration glue substitutable. The result is a system frozen
not by accreditation but by economic capture.

Open-source upstream components are necessary but not sufficient. A
prime can build proprietary integration glue around open-source
upstreams and still capture the program. The fix is to require the
glue itself — IaC, deployment automation, runbooks, and operational
playbooks — to be government-owned or non-exclusively licensed.

## Decision

Every artifact that sits *between* upstream open-source components
is government-owned, in-repo, and substitutable. Specifically:

1. **All Terraform, all Ansible, all Docker Compose, all CI
   pipelines live in this repo and are government-owned IP.** No
   reliance on a vendor's proprietary deployment platform.
2. **Integration code (shims, bridges, protocol gateways) is
   in-repo and built from source by us.** Vendor SDKs are consumed
   only at the upstream-component layer; the integration logic that
   weaves them together is ours. See [`0209`](0209-shim-pattern.md).
3. **Custom upstream forks are explicit, named, and built by our
   pipeline.** Where we patch an upstream (e.g.
   `netbird-management:patched-<sha>` — see
   [`0301`](0301-patched-netbird-management-image.md)), the fork
   is in-repo, the build is in our CI, and the patch is documented
   so a contributor can re-derive it.
4. **Operational knowledge is in `docs/operations/`.** Runbooks,
   troubleshooting guides, and incident response procedures live
   in the repo. Vendor-only operational know-how is treated as a
   risk and tracked.
5. **No black-box dependencies in the critical path.** A component
   whose source is not inspectable cannot live in the critical
   path (mesh control plane, identity, crypto boundary, audit
   integrity). It can live in optional or convenience layers
   (e.g. vendor-specific sensor processing) only with an explicit
   exception.
6. **Contractor work product is delivered as merge-ready PRs to this
   repo.** Government Contracting officers should require this
   delivery shape; it is the operational expression of MOSA's
   "government-defined interface" requirement.

## Alternatives considered

- **Open-source upstreams + proprietary glue.** Default for many
  programs that adopt open source nominally. Loses the
  capture-resistance property entirely. Rejected.
- **Government-owned glue + closed upstream components.** Better
  than the inverse but still subject to upstream-vendor capture.
  Rejected as the universal pattern; tolerable case-by-case (e.g.
  a vendor network-switch operating system).
- **Open everything, no government IP.** Fine for the upstream
  layer but loses the citation handle for accreditation and
  procurement. The glue is government-owned because we need to
  *cite* it as government-owned in submission packages.

## Consequences

**Positive.** No single contractor can capture the program. The
integration glue is replaceable, the deployment automation is
replaceable, the operational knowledge is in the repo. A future
program office can re-compete the operational work and the new
contractor can pick up where the previous one left off without
re-learning the system.

**Negative.** More IaC and operational documentation work for
us, up front. We accept the cost. This is the single highest-
leverage investment against the borking pattern.

It also means we cannot accept "we'll deliver the integration glue
in our proprietary platform and you'll never have to see it"
contractor offers. Such offers are how capture happens.

## Assessor view (SCA-R)

NIST SP 800-53 SR-3 (supply chain controls and processes), SR-4
(provenance), SA-12 (supply chain protection), and CM-2 (baseline
configuration) are all served by government-owned glue. The
assessor's question "can a competitive substitution happen without
breaking the system?" is answered yes-by-construction because every
piece of glue is in-repo and the upstream components are
substitutable per [`0101`](0101-mosa-as-policy-compliance.md).

The 0700-series ADRs on supply chain (Chainguard-first, pinned
digests, scanner regime, signed images) are the technical
expression of this strategic decision.

## Trigger conditions for revisit

- A specific contracting vehicle requires that integration code be
  delivered as a vendor IP product. Negotiate; do not concede.
- A vendor refuses to ship a component without bundling proprietary
  deployment tooling. Drop the component or build the glue ourselves.
  Do not take on the bundled proprietary tooling.
- The glue layer becomes unmaintainable due to scale. Refactor in
  place; do not outsource ownership.

---

*See `GLOSSARY.md` for terms and acronyms.*
