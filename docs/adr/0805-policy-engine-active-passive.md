# ADR 0805 — Per-site policy engine (active-passive behind keepalived VIP)

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goatPolicy |
| Related | [`0205-intra-site-vrrp-failover.md`](0205-intra-site-vrrp-failover.md), [`0206-island-mode-by-construction.md`](0206-island-mode-by-construction.md), [`0806-egress-proxy-cascade.md`](0806-egress-proxy-cascade.md), [`0807-mesh-microsegmentation.md`](0807-mesh-microsegmentation.md), [`../design/egress-proxy.md`](../design/egress-proxy.md) |

## TL;DR (for a layperson)

The policy engine is a per-site service that handles approval
workflows for changes that need a human in the loop —
allowing a peer to call out to a specific website, opening
a microsegmentation rule, rotating a credential. It runs as
active-passive on each site's Router-A / Router-B pair
behind a virtual IP. Approvals replicate across sites via
Spock so a partner site can approve. This is sovereign,
Island-Mode-correct, and has nothing to do with GitHub
CODEOWNERS (which would violate Island Mode).

## Diagram



Source: [`diagrams/0805-policy-engine-active-passive.mmd`](diagrams/0805-policy-engine-active-passive.mmd).

## Context

Block 17 (egress proxy, [`0806`](0806-egress-proxy-cascade.md))
needs a per-site approval workflow for time-bounded direct-
CIDR allowlists. Same pattern is foundational for
microsegmentation ([`0807`](0807-mesh-microsegmentation.md)),
credential hygiene ([`0817`](0817-credential-hygiene.md)),
and tamper-response arming ([`0810`](0810-tamper-response.md)).

Initial design used GitHub CODEOWNERS for two-of-N approval;
reversed same-day on review (violated Island Mode — partitioned
site cannot reach GitHub). Replaced with per-site sovereign
policy engine.

## Decision

Per-site `db-policy-engine` Go service:

- **Deployment**: active-passive behind keepalived VIP on
  each site's Router-A / Router-B pair
  ([`0205`](0205-intra-site-vrrp-failover.md)).
- **Storage**: Spock-replicated `egress_policy` +
  `egress_approval` + `egress_request_event` tables.
- **Auth**: OIDC via Dex ([`0401`](0401-per-site-dex-oidc.md))
  — IdP-abstract (standard claims `sub`, `email`, `groups`).
- **Notifications**: three adapters — email (per-site
  SMTP), Matrix (per-site tuwunel homeserver per
  [ADR 0835](0835-secure-messaging-substrate-tuwunel.md),
  room `#policy-alerts:<site>.netbird.legacy.example`), and
  isitup widget. Notification adapter pattern means
  adding/removing chat backends is config-only.
- **Web UI**: `policy.<site>.netbird.legacy.example`.
- **CLI**: `db egress-policy`.
- **Approval modes**:
  - **Normal**: single role-holder approval + 24h SLA
    from submission timestamp; auto-escalates to
    fast-path on breach.
  - **Fast**: two-of-N peer approval, ≠ requestor;
    immediate activation; release valve for urgency or
    SLA breach.
- **Batch submission** supported.
- **Role roster v1**: 1 named per site minimum, 2-3
  preferred; cross-site approval allowed (`cross_site=true`
  in audit).
- **Conflict resolution**: CRDT-style under partition.

Foundational — also services Block 18A (microsegmentation),
18D (credential hygiene), 18F (tamper-response config).

Design at [`../design/egress-proxy.md`](../design/egress-proxy.md) §6.

## Alternatives considered

- **GitHub CODEOWNERS as approval mechanism.** Violates
  Island Mode (partitioned site cannot reach GitHub).
  Rejected on review.
- **Centralized cloud-hosted policy engine.** Loses
  sovereignty / Island Mode. Rejected.
- **No approval workflow; allow direct config changes.**
  Loses audit + change-control discipline. Rejected.

## Consequences

**Positive.** Sovereign, Island-Mode-correct, per-site.
Replicates approvals across sites via Spock. Foundational
for multiple security workstreams.

**Negative.** Operational service to maintain on every site.
Acceptable; the service is foundational.

## Assessor view (SCA-R)

The policy engine serves AC-2 (account management — approval
workflow), AC-3 (access enforcement), AC-4 (information flow
enforcement), CM-3 (change control), and AU-2 (audit). The
dual-path approval (normal + fast) is operationally similar
to the change-advisory-board pattern in NIST SP 800-128 CM-3
guidance, with auditable approval records.

## Trigger conditions for revisit

- Approval volume exceeds operational capacity. Add
  automation for low-risk approvals; preserve human-in-loop
  for high-risk.
- A federation partner requires their approval workflow
  integrated. Use the notification-adapter pattern.

---

*See `GLOSSARY.md` for terms and acronyms.*
