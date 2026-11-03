# ADR 1096 — Delegated machine enrollment: late-bound custody mint of system principals

| Field | Value |
|---|---|
| Status | Proposed (2026-07-27, design) |
| Date | 2026-07-27 |
| Related | machine-enrollment.md, identity-grammar.md, [onboard-tier-role-model.md](../design/onboard-tier-role-model.md), [three-stage-candidate-onboarding.md](../design/three-stage-candidate-onboarding.md), ADR 1043, [ADR 1025](1025-zenoh-dynamic-tenant-authz-slot-pool.md), ADR 0412, [ADR 1065](1065-graph-engine-property-graph-read-model.md), [ADR 1088](1088-membership-transfer-engine.md), [ADR 1062](1062-three-stage-delegated-candidate-onboarding.md) |

## Context

The person onboarding flow (ADR 1062) is built and live on ZAGROS, and it always
named a downstream half it deferred: the **machine self-service mint** — a person
bringing their systems online later, late-bound
([onboard-tier-role-model.md](../design/onboard-tier-role-model.md) §5,
[three-stage-candidate-onboarding.md](../design/three-stage-candidate-onboarding.md)
"Relationship to the delegated machine-enrollment thread"). The person half now
exists, so the machine half is unblocked.

The **mesh layer already bounds machines** via the person's `machine_quota`
(NetBird `usage_limit: N`). The gap is one layer up, on the **data fabric**: all of
a custodian's machines publish under that custodian's single Zenoh identity, so they
are **indistinguishable on the wire** — no per-machine cert, no node-class/autonomy
stamp, no custody provenance. The identity grammar (ADR 1043) already defines
everything needed to fix this — `system` as a first-class principal, node-class and
autonomy URI-SAN axes, the `enrolled_by` custody edge — but nothing mints it.

## Decision

A **custodian-driven, self-service machine enrollment flow** that mints a system
principal's own fabric identity against the custodian's standing quota. It is a
sibling of the person flow, not a copy — the person flow spends two human gates to
prove a stranger; a machine's custodian was already vetted, and the
`enrollment_grant` quota is the standing pre-authorization, so **machine enrollment
carries zero review gates** for mission payload.

1. **Trigger = custodian, not a public invite.** The custodian runs
   `goat machine enroll --node-class … --autonomy …` against their remaining quota.
   No front desk, no sponsor, no admin queue.
2. **Mint authority: self-service for function classes, operator-gated for
   infrastructure classes.** `SENSOR`/`EFFECTOR`/`PROCESSOR`/`PLATFORM` are payload
   the custodian owns → self-service. `MGMT-HOST`/`ZENOH`/`SNITCH`/`SERVICES` shape
   the goatnet itself → operator-gated, custodian-attributed — mirroring the person
   flow's escalated-tier carve-out.
3. **Each machine gets its own slot.** A per-machine `<uuid>/<taxonomy>` allocation
   from the pre-cut pool (ADR 1025), URI-SAN stamped with `node-class=`/`autonomy=`
   and the custodian's inherited `aff` — never a share of the custodian's slot.
4. **Portal Zenoh cert is the primary machine identity; SPIFFE only decomposes
   platforms.** SPIRE already composes with the CN-only ACL by CN, so a simple
   system needs no second plane; SPIFFE (ADR 0412) enters only for a `PLATFORM`'s
   sensor/effector/processor sub-workloads.
5. **A mint records the `enrolled_by` custody edge** (actor graph, ADR 1065) and
   emits one `OperatorActionV1` attributed to the custodian.
6. **No `.cbor`, no Ory login identity.** Machines get mesh + fabric identity only;
   they do not sign into web consoles. Affiliation is inherited from the custodian's
   unit, never a mint parameter.

Not a daemon: a library + a `goat machine` CLI leg + the existing certmint/slot-pool
apply path. Build sequence in machine-enrollment.md.

## Alternatives

- **Reuse the person three-stage flow for machines.** Wrong shape: it mints an Ory
  identity + `.cbor` a machine can't use, and spends two human gates the quota already
  paid for. Rejected.
- **Keep machines on the custodian's shared Zenoh identity** (status quo). Leaves them
  wire-indistinguishable — the exact gap this ADR closes. Rejected.
- **Setup-key-only, no fabric identity.** What exists today; bounds the mesh but never
  gives the machine a distinct fabric principal. Rejected as the endpoint (it is the
  starting primitive).
- **SPIFFE as the primary machine identity plane.** Redundant with the portal cert for
  simple systems (SPIRE composes by CN anyway) and a heavier standup; reserved for
  platform decomposition only. Rejected as the default.
- **Fully self-service including infrastructure classes.** An operator-less path to
  mint `MGMT-HOST`/`ZENOH` nodes lets a custodian reshape the substrate unreviewed.
  Rejected in favor of the infra operator gate.

## Consequences

Machines become **wire-distinguishable, custody-attributed principals**: a subscriber
and the ACL can tell a sensor from an effector under the same custodian, and every
system traces to the person who enrolled it. The person and machine flows compose —
one person onboard, many downstream machine mints drawing down one quota. The fabric
gains node-class/autonomy as real, minted axes rather than grammar-only prose.

Costs and risks: a new `goat machine` surface and a quota-accounting store; the
custody edge must stay accurate across revocation (ADR 1031 — does a revoked machine
return its slot to quota? proposed yes, UNVERIFIED) and across cross-custodian
transfer (should ride ADR 1088, not a re-enroll — not yet wired). Self-service mint is
powerful, mitigated by the quota ceiling, the infra operator gate, fail-closed quota
exhaustion, and per-mint audit.
