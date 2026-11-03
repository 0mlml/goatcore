# ADR 0213 — Convenience services vs core services — promotion threshold

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [`0108-scope-discipline-own-the-transport.md`](0108-scope-discipline-own-the-transport.md), [`0813-service-registry-pattern.md`](0813-service-registry-pattern.md) |

## TL;DR (for a layperson)

Our platform makes it cheap to deploy small services on the mesh
— status pages, leaderboards, planning tools, demonstration apps.
We make liberal use of that ease. But "easy to deploy" is not the
same as "this becomes a core service we maintain forever." Most
of those services are temporary or non-critical. The few that
become core require a deliberate review to confirm we want to
take on permanent ownership in deployed environments.

## Context

The service registry pattern ([`0813`](0813-service-registry-pattern.md))
makes adding a new hosted service trivial — a single HTML or Go
binary, one row in `services.yaml`, and it runs with TLS, DNS,
and ACL coverage. The platform encourages this; demonstrating the
hosting pattern is part of how the platform proves itself.

Without scope discipline, "trivial to deploy" silently becomes
"the team now operates 30 services in deployed environments." That
is the path to losing engineering focus from the transport layer
([`0108`](0108-scope-discipline-own-the-transport.md)).

## Decision

Hosted services fall into three categories:

| Category | Maintenance posture | Examples |
|---|---|---|
| **Consciously temporary** | Not maintained; no SLO. May continue to work indefinitely but is not underwritten. | Planning visualizations, an effort tracker, ad-hoc demo apps. |
| **Maintained but non-critical** | Kept running for operational awareness. Loss does not affect mission-critical paths. | isitup, smoketest, gatewatch dashboards. |
| **Core / canonical** | Permanent staffing, maintenance, and security obligation in deployed environments under operational tempo. | Today: a shared operational-picture service and a sensor integration. Soon: TAK Server federation and a partner-system bridge. |

**Promotion to core requires deliberation.** A new core service
goes through:

1. **Nomination.** Named sponsor, named operational requirement.
2. **Evaluation.** Does it meet the test "essential minimum needed
   to accomplish the mission"? If not, it stays in maintained-but-
   non-critical or consciously-temporary.
3. **Selection.** Recorded as an ADR (one per core service). The
   ADR documents the maintenance commitment, the staffing
   implication, the deployed-environment monitoring requirement,
   and the failure consequence.

The promotion threshold is **high by design**. Most candidates
stay in the lower two categories.

**Convenience services do not skip the deliberation by accident.**
A consciously-temporary service that becomes load-bearing must be
explicitly promoted (with the ADR) or explicitly sunsetted; drift
into "everyone depends on it but no one owns it" is the failure
mode we are preventing.

## Alternatives considered

- **Treat every hosted service as core.** Spreads engineering
  effort thin. Rejected.
- **No distinction; everything is best-effort.** Loses the
  ability to commit to mission-critical services credibly.
  Rejected.
- **Per-service maintenance tier without explicit promotion.**
  Drift is invisible. Rejected.

## Consequences

**Positive.** The platform stays usable for low-cost
demonstrations and convenience services without that ease
silently expanding our maintenance surface. Core services are
explicitly committed; their failure mode is owned.

**Negative.** Deliberation is overhead. We accept; the
overhead is small per-service and prevents a much larger
ongoing cost.

## Assessor view (SCA-R)

The promotion threshold is part of the system's CM-7 (least
functionality) and PM-9 (risk management strategy) posture.
The categorical distinction is reviewable: an assessor can ask
"what services are core?" and get a definitive answer with ADR
backing. Consciously-temporary and maintained-but-non-critical
services are scoped as best-effort; their failure does not
cross the system's mission-critical SLO boundary.

For assessment purposes, only core services trigger CP-2
contingency planning, IR-4 incident response coverage, and
SLA-bearing monitoring obligations.

## Trigger conditions for revisit

- A consciously-temporary service unexpectedly becomes load-
  bearing without going through promotion. Treat as a drift
  incident; either promote or sunset.
- Core service count exceeds the operational team's
  maintenance budget. Re-evaluate; either staff up or
  demote a service.

---

*See `GLOSSARY.md` for terms and acronyms.*
