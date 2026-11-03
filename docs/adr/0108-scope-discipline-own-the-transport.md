# ADR 0108 — Scope discipline: own the transport, enable the mission

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | Multi |
| Supersedes | — |
| Superseded by | — |
| Related | [`0212-goat-vs-bond-split.md`](0212-goat-vs-bond-split.md), [`0213-convenience-vs-core-services.md`](0213-convenience-vs-core-services.md) |

## TL;DR (for a layperson)

goat is the network — the encrypted mesh that mission
systems run *on*. We are not building sensor analytics, command
and control, or end-user applications. We deliberately stay out of
those layers, even when it would be easy to drift in. This
discipline is what keeps the network reliable: every service we
take permanent ownership of is a service we have to maintain in
deployed environments under operational tempo with limited
people.

## Context

The natural pressure on any infrastructure project is for scope to
expand. A request to add a dashboard. A request to host an analytics
layer. A request to integrate a specific sensor end-to-end. A
request to host a custom application "because we already have the
infrastructure." Each request is individually reasonable. Together
they are fatal: they dilute the engineering focus that makes the
infrastructure reliable in the first place.

goat is the *transport fabric*. The mission belongs to
the systems that run on top of it. The scope discipline is what
prevents us from accidentally becoming a sensor processor or an
application platform — categories where we have no business
operating and no maintenance budget to staff.

The principle is documented at length in
`../design/tech-strategy.md` "Scope
Discipline" section. This ADR captures the *decision* — that scope
discipline is load-bearing — separately from the *description* of
what the discipline looks like.

## Decision

goat's permanent ownership covers exactly:

- The encrypted mesh fabric (WireGuard / NetBird overlay).
- The management plane (enrollment, peer discovery, policy, DNS).
- Infrastructure automation (Terraform, Ansible, deployment
  pipeline).
- Relay and connectivity infrastructure (VPS relays, STUN,
  transport bridges).
- The integration surface — bridge services and protocol gateways
  that let other systems plug in.

We do **not** own:

- Application-layer services (situational-awareness servers, analytics
  platforms, any future mission application).
- End-user applications (dashboards, COP viewers, planning
  tools).
- Sensor processing, data fusion, or analytics.
- Anything a vendor, program office, or unit is better positioned
  to build and maintain.

**Proof-of-concept services are not commitments.** A status page,
a map viewer, a health-check system — we stand them up to
demonstrate that the platform works, then we hand them off when
another organization is ready to own them. Reference
implementations, not products.

**The scope test before any work:** "Is this about the mesh, or
about something that rides on the mesh?" If the latter, our job
is to make sure the mesh supports it — not to build it ourselves.

**Promotion to a "core" service requires deliberation.** Today the
core set is tiny (the mesh control plane components themselves,
plus the federation bridges). The threshold for
adding a core service is high because every core service is a
permanent staffing, maintenance, and security obligation in
deployed environments. See [`0213`](0213-convenience-vs-core-services.md)
for the convenience-vs-core distinction and the promotion
deliberation process.

## Alternatives considered

- **Build whatever is asked for, scope-creep into a full stack
  vendor.** Loses the engineering focus that makes the transport
  reliable. The most common failure mode for infrastructure
  projects. Rejected.
- **Strict transport-only stance, refuse all hosted services.**
  Loses the demonstration value of running services on the mesh
  to validate the platform. Rejected.
- **Variable scope per partner/sponsor.** Produces a fractured
  system where each partner gets a custom application stack we
  cannot maintain. Rejected.

## Consequences

**Positive.** Engineering effort concentrates on transport
reliability. Mission systems that ride on the mesh benefit from a
stable substrate. Hand-offs to specialist organizations
(application vendors, program offices) are a feature, not a
failure.

**Negative.** We sometimes refuse to build things that would be
easy and useful in the short term. We accept the cost; the
alternative is the slow death of a mission-critical infrastructure
project to scope creep.

**Reciprocal obligation:** because we refuse to own application-
layer work, we owe the application layer a *reliable, well-
documented, easy-to-integrate* transport. The shim pattern
([`0209`](0209-shim-pattern.md)) is how we discharge that
obligation for vendor protocols.

## Assessor view (SCA-R)

The scope boundary is the authorization boundary. NIST SP 800-37
boundary definition is served by the explicit "what we own / what
we don't" enumeration. The assessor's question "what data does
this system process and store?" is answered by "transport-layer
metadata, identity, policy, and configuration; *not* mission data
in flight or at rest at the application layer." This narrows the
data-classification scope of the system substantially.

The application layer is *not* unauthorized — it is *separately*
authorized, by the relevant program office or partner. Federation
boundaries to partner systems cross system authorization boundaries
and are governed by the FIPS-PQC boundary node
([`0601`](0601-fips-pqc-boundary-node.md)) and the federation
security design (`../design/federation-security.md`).

## Trigger conditions for revisit

- A sponsor asks to add a permanent core service. Apply the
  promotion deliberation in [`0213`](0213-convenience-vs-core-services.md);
  the answer is usually no.
- A category of mission system genuinely cannot be built by anyone
  else and is required for the mission. Negotiate a hand-off plan
  to a specialist owner; do not absorb permanently.
- A convenience service grows into a critical dependency. Promote
  it deliberately or sunset it; do not let drift make the
  decision.

---

*See `GLOSSARY.md` for terms and acronyms.*
