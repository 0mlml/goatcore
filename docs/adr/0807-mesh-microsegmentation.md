# ADR 0807 — Internal mesh microsegmentation

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goatPolicy |
| Related | [`0805-policy-engine-active-passive.md`](0805-policy-engine-active-passive.md), [`0806-egress-proxy-cascade.md`](0806-egress-proxy-cascade.md), [`0214-failure-isolation-four-pattern.md`](0214-failure-isolation-four-pattern.md) |

## TL;DR (for a layperson)

Egress controls govern what mesh peers can reach *outside*
the mesh. Microsegmentation governs what peers can reach
*inside* the mesh — peer-to-peer access is decided per-pair
per-protocol, not allowed by default. This is the
lateral-movement equivalent of egress: a compromised peer
can only reach the specific peers/services its policy
allows.

## Context

A flat mesh trusts every enrolled peer to talk to every
other enrolled peer. This is the same mistake as flat
perimeter networks at a smaller scale. A compromised peer
can attack every other peer freely.

Microsegmentation closes the gap: per-pair, per-protocol
ACLs decide who can reach whom. The discipline is the
internal counterpart to egress
([`0806`](0806-egress-proxy-cascade.md)).

This supersedes the older "Block 1C dashboard config"
framing (which was just static ACLs) and reuses the policy
engine ([`0805`](0805-policy-engine-active-passive.md)) for
change approval.

## Decision

Microsegmentation across the mesh:

- **Default-deny intra-mesh.** ACLs explicitly grant
  per-peer-pair, per-protocol access.
- **NetBird ACLs** as the primary enforcement mechanism.
- **Group-based authoring.** Peers are members of groups
  (`role:router`, `role:peer`, `role:partner-X`); ACLs are
  group-to-group.
- **Time-bounded exceptions** for short-term access; auto-
  expire.
- **Approval workflow** through the policy engine
  ([`0805`](0805-policy-engine-active-passive.md)).
- **Audit:** every ACL change is recorded.
- **Failure isolation pairing**: microsegmentation is one
  layer of the failure-isolation four-pattern framework
  ([`0214`](0214-failure-isolation-four-pattern.md)) — the
  fault-domain network isolation pattern.

Implementation tracked as Block 18A.

## Alternatives considered

- **Flat mesh (no microsegmentation).** Lateral movement
  unrestricted. Rejected.
- **Per-peer firewall rules instead of mesh-level ACLs.**
  Distributed enforcement; harder to audit. Rejected.
- **Dashboard-config-only ACLs (Block 1C original framing).**
  Static; no approval flow; harder to audit. Rejected.

## Consequences

**Positive.** Lateral-movement attack surface bounded.
Compromised peers cannot freely reach every other peer.
Audit is centralized.

**Negative.** ACL authoring is real work. Group-based
authoring + the policy engine workflow keep it manageable.

## Assessor view (SCA-R)

Microsegmentation serves AC-3 (access enforcement), AC-4
(information flow enforcement — internal flows), SC-7
(boundary protection — internal mesh boundaries), and AU-2
(audit). The pattern is consistent with NIST SP 800-207
Zero Trust and CISA microsegmentation guidance.

The comparative-posture artifact identifies
"microsegmentation" as a dimension where goat
is ahead.

## Trigger conditions for revisit

- Operational evidence shows ACL count is unmanageable
  (>500 rules per site). Group-based consolidation;
  do not abandon microsegmentation.
- A specific federation partner cannot operate behind
  microsegmentation. Adjust per-partner; do not
  default-allow.

---

*See `GLOSSARY.md` for terms and acronyms.*
