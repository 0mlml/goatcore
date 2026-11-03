# ADR 0214 — Failure-isolation four-pattern framework

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [`0206-island-mode-by-construction.md`](0206-island-mode-by-construction.md), `../design/failure-isolation.md`, `../project/failure-isolation-rollout.md` |

## TL;DR (for a layperson)

When one piece of the system fails, we want the failure to stay
in that piece — not to spread across the whole system. We use
four standard patterns for confining failures: bulkhead, circuit
breaker, graceful degradation, and blast-radius minimization.
Every service in the system is matched to a specific combination
of these patterns.

## Context

A naïve distributed system has implicit shared-fate dependencies:
one host's disk fills up, the database stops accepting writes, the
management plane stops responding, every peer that depends on the
mgmt plane logs failures, log shipping backs up, observability
gets noisy, alerts fire across the fleet, and an operator can no
longer tell what is actually failing. This is the alert-storm
failure mode that eats SOC effectiveness.

The fix is *deliberate isolation*: each service must be evaluated
against four standard patterns and tagged with which patterns
apply.

## Decision

Every service in the system is reviewed against the four
failure-isolation patterns codified in
`../design/failure-isolation.md` §2:

| Pattern | What it confines | Implementation mechanisms |
|---|---|---|
| **Bulkhead** | Isolates failure domains so one component's collapse doesn't drag in others | Docker Compose `deploy.resources.limits` + `mem_limit` per service; per-service network namespaces; per-VLAN segmentation for fault-domain network isolation. |
| **Circuit breaker** | Stops a caller from retrying a broken peer until the peer recovers | Per-dependency timeout + retry budget; e.g. signal-server's per-peer Forward timeout. |
| **Graceful degradation** | Defines a "I'm broken but still useful" mode for each critical service | Optional-dependency posture (critical service runs without optional dep); paging-on-abnormal flags that downgrade output instead of corrupting it. |
| **Blast-radius minimization** | Limits the scope of a bad change | Canary deploys; staged rollouts; auto-rollback; coordinated upgrade orchestrator (`ops/ansible/coordinated-upgrade.yml`). |

Per-service matrix is maintained in
`../design/failure-isolation.md`;
rollout tracking in
`../project/failure-isolation-rollout.md`.

The four patterns are *not* mutually exclusive — most services
adopt two or three. The matrix is the contract.

**Implementation status (per `failure-isolation-rollout.md`, 2026-04-25):**
all Phase 2a items (Alertmanager paging, container resource
limits, dead-man's-switch) are **Not started**. Phase 2b items
(signal-server circuit breaker, admit-audit auto-quarantine) are
**Not started**. The patterns are committed; the rollout has not
begun.

> **Correction note (2026-04-25).** The original draft of this ADR
> listed four patterns — "paging on abnormal", "cgroup bulkheads",
> "fault-domain network isolation", "optional-dependency posture"
> — that conflate *implementation mechanisms* with the four
> canonical *patterns* in failure-isolation.md §2 (bulkhead /
> circuit-breaker / graceful-degradation / blast-radius). The
> original draft also claimed Phase 2a "stubs in place"; the
> rollout doc says Phase 2a is not started. Both errors were
> introduced because this ADR was authored without opening the
> canonical design doc.

## Mechanisms column — landed 2026-04-25

The implementation-tier shorthand from the original draft is now
a **Mechanisms** column on the per-service matrix in
`../design/failure-isolation.md` §3
alongside a **Patterns** column naming which of the four canonical
patterns each service relies on. The two columns are now required
on every new service row.

## Alternatives considered

- **No formal isolation framework; rely on operator vigilance.**
  Produces alert storms; operator vigilance is finite. Rejected.
- **Single isolation mechanism (e.g. just cgroups).** Insufficient
  — different failure modes need different mechanisms. Rejected.
- **Adopt circuit-breaker library at the application layer.**
  Useful complementary pattern; does not substitute for the four
  patterns. Held as a possible Phase 3 enhancement.

## Consequences

**Positive.** Failures stay where they originate. Alert noise
drops because adjacent services do not cascade. Operator
debugging is easier because the failure surface is bounded.
The matrix is reviewable.

**Negative.** Per-service tagging is real work. We accept; the
matrix lives in one place and gets reviewed in PRs.

The optional-dependency pattern requires careful upfront
design — a critical service that *thinks* it can run without
its optional dependency must actually be tested doing so.
Phase 2b drills exercise this.

## Assessor view (SCA-R)

The four-pattern framework serves SC-5 (denial of service —
single-component failures are absorbed), SC-7 (boundary
protection — fault-domain isolation maps onto network
boundaries), CP-2 (contingency — degraded-functional posture
under partial outage), and SI-7 (software, firmware, integrity
— paging-on-abnormal prevents corrupt-output propagation).

The per-service matrix in
`../design/failure-isolation.md`
is the assessor's review artifact; CM-8 (component inventory)
benefits from the matrix being canonical.

## Trigger conditions for revisit

- A failure-isolation incident demonstrates a pattern is
  inadequate. Update the matrix; do not retire patterns
  without demonstrated replacement.
- Service count grows past the matrix's reviewable size
  (~50). Consider per-tier sub-matrices.
- A new failure mode emerges that none of the four patterns
  addresses. Add a fifth pattern explicitly; do not retrofit
  existing ones.

---

*See `GLOSSARY.md` for terms and acronyms.*
