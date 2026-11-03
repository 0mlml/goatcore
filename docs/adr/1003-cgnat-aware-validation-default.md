# ADR 1003 — CGNAT-aware validation as default for cloud envs

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`1000-cloud-env-lifecycle.md`](1000-cloud-env-lifecycle.md), [`../design/cgnat-validation-env.md`](../design/cgnat-validation-env.md), `../reports/2026-04-24-cgnat-validation-attempt-1-findings.md` |

## TL;DR (for a layperson)

When we test the system in cloud environments, we deliberately
put the test peers behind a NAT gateway — even though AWS
makes it easy to give every peer its own public IP. Real
production peers usually sit behind CGNAT
(Carrier-Grade NAT) on mobile, satellite and ISP links, and the system behaves differently when peers
can't be reached directly. Testing without CGNAT produces
misleading evidence (an earlier validation environment had only 1
cross-site Forward RPC in 5 days because peers found each other directly via public
IPs).

## Context

An earlier validation environment used EC2 with public Elastic
IPs on each peer. This let peers discover each other directly,
bypassing the signal-server federation path entirely. The
result: 1 Forward RPC over 5 days — almost no exercise of the
load-bearing federation mechanism.

Production peers behind CGNAT will *force* every
peer through the signal-server path. Validation without CGNAT
gives confidence in code paths that production won't exercise.

The fix: cloud validation envs default to CGNAT topology.

## Decision

CGNAT-aware topology is the *default* for every cloud
validation env:

- **NAT gateway**: peers sit behind an AWS NAT Gateway (or
  equivalent), no public IP per peer.
- **STUN required**: peers discover each other through the
  STUN servers ([`0304`](0304-coturn-stun.md)).
- **Federation exercise**: signal-server federation is
  exercised by construction.
- **Public-EIP topology** is allowed only with explicit
  documented reason (e.g. specifically testing the
  public-EIP code path).

The cgnat-validation env is the canonical cloud env exercising
this; current attempt-1 findings are in
`../reports/2026-04-24-cgnat-validation-attempt-1-findings.md`.

## Alternatives considered

- **Public-EIP default.** Easy but produces misleading
  evidence. Rejected.
- **Mix of CGNAT and public-EIP.** Acceptable per-test;
  CGNAT remains default.

## Consequences

**Positive.** Validation evidence reflects production
topology. Signal-server federation gets exercised.

**Negative.** Cloud env setup is slightly more complex
(NAT GW). Acceptable.

## Assessor view (SCA-R)

The CGNAT-aware default serves CA-2 (security assessment —
test conditions match production) and CP-2 (contingency —
validation reflects partition-tolerant production paths).
Test evidence cited in the assessment package is more
credible because the test topology matches production.

## Trigger conditions for revisit

- CGNAT access networks gain end-to-end public reachability
  (unlikely). Re-evaluate.
- Specific code path benefits from public-EIP test (rare).
  Document the exception.

---

*See `GLOSSARY.md` for terms and acronyms.*
