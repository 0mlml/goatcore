# ADR 1087 — Advanced-grant catalog: audited request→approval→apply for graduated capabilities

| Field | Value |
|---|---|
| Status | Accepted (design); implementation rides the goat-id-console release train |
| Date | 2026-07-17 |
| Related | ADR 1043 §8 (rename discipline), ADR 1031 (reversible revocation), ADR 1062 (three-stage onboarding), mesh-and-ory-roles.md, [p2p-graduation.md](../operations/p2p-graduation.md), zagros-acl-hardening-plan.md §2c |

## Context

The 2026-07-17 cutovers made several mesh capabilities *graduated* rather
than role-implied: lateral peer reach (`P2P`), and prospectively router
peering and other elevated abilities. Today graduation is a manual runbook
(platform admin edits group membership via the mgmt API). That works but
does not scale past a handful of grants, has no self-service entry point,
and leaves the request/justification off the record.

## Decision

A **grant catalog**: a closed, declared list of graduated capabilities that
a participant can request and an authorized approver can apply, reusing the
onboarding machinery (goat-id-console page + the three-stage
request→approval→apply shape of ADR 1062, with the runner applying the
change and `OperatorActionV1` audit at the chokepoint).

- **Catalog is declared, not ad-hoc**: one file (SoT, CI-fenced like the
  group vocabulary) listing each grant: name, what it maps to (NetBird
  group membership / cert issuance / other), approver tier required,
  whether AAL2 step-up applies, revocation semantics. Initial entries:
  `p2p` (→ `<SITE>-P2P` membership, platform-admin approval) and
  `router-peer` (→ design pending: the group/cert a peered router needs).
- **Per-identity, per-device where the grant is device-shaped** (P2P is a
  peer property; the request names the device).
- **Reversible by default** (ADR 1031): revocation = remove the membership /
  credential; the catalog entry defines the exact undo.
- **Two planes stay separate**: a grant may map to mesh membership or to a
  service permission, but the catalog never mixes one grant across both
  planes implicitly.

## Alternatives

Ambient role-implication (rejected — just removed, it was the problem);
tickets in an external tracker (rejected — off-fabric, no apply automation,
no audit chokepoint); direct dashboard edits forever (rejected — no
request trail, admin-only bottleneck).

## Consequences

goat-id-console grows a "request access" page + approver queue; the runner
gains a small NetBird-group apply leg; the catalog file becomes the SoT the
same way `group-permissions.yml` is for Ory groups. Until it ships, the
manual runbook ([p2p-graduation.md](../operations/p2p-graduation.md)) is
the path.
