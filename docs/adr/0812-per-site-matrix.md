# ADR 0812 — Per-site Matrix (Dendrite) for chat layer + policy alerts (SUPERSEDED)

| Field | Value |
|---|---|
| Status | Superseded by [ADR 0835](0835-secure-messaging-substrate-tuwunel.md) on 2026-05-07 |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0805-policy-engine-active-passive.md`](0805-policy-engine-active-passive.md), [`0835-secure-messaging-substrate-tuwunel.md`](0835-secure-messaging-substrate-tuwunel.md) (supersedes), [`0836-matrix-push-gateway-hedwig.md`](0836-matrix-push-gateway-hedwig.md) (push gateway sibling) |

> **Superseded note (2026-05-07):** the scope of "secure messaging
> for goat-net" expanded from "SRE pager notification adapter"
> (~10 paged operators per site) to "all peers in the mesh,
> 1000 users per site" — see [ADR 0835](0835-secure-messaging-substrate-tuwunel.md).
> The "lightweight successor to Synapse" rationale that picked
> Dendrite for the pager use case did not survive the new scope.
> ADR 0835 picks **tuwunel** (Rust, Apache-2.0, official conduwuit
> successor, sponsored by the Swiss government) with a federation
> soak gate and Dendrite named as the explicit fallback if the
> gate fails. The federation-controlled / per-site-homeserver /
> notification-adapter-pattern decisions in this ADR carry
> forward into 0835 unchanged; only the implementation choice
> changed.

## TL;DR (for a layperson)

Each site runs its own Matrix homeserver (Dendrite — a
lightweight Go-based implementation). v1 use case is policy-
engine notifications via a per-site room. Federation between
homeservers is opt-in and controlled. Sites without a
homeserver gracefully degrade to email + dashboard
notifications.

## Context

The policy engine ([`0805`](0805-policy-engine-active-passive.md))
needs a chat-layer notification adapter. Slack / Mattermost /
Discord all come with sovereignty problems. Matrix is the
open-protocol choice; Dendrite is the Go-native server (lighter
than Synapse) and aligns with the rest of our Go stack.

## Decision

Per-site Dendrite homeserver:

- **One homeserver per site.** `<site>.netbird.legacy.example`.
- **v1 use case**: policy-engine notification room
  `#policy-alerts:<site>.netbird.legacy.example`.
- **API**: standard `_matrix/client/v3/rooms/.../send/m.room.message`.
- **Federation**: opt-in and controlled per peering. Each
  homeserver is independent.
- **Notification adapter pattern**: adding/removing chat
  backends is config-only in the policy engine. Sites
  without a Dendrite homeserver gracefully degrade to email
  + dashboard.

## Alternatives considered

- **Slack / Discord SaaS.** Sovereignty + Island Mode
  issues. Rejected.
- **Synapse (Python).** Heavier; less aligned with Go-native
  stack. Rejected.
- **No chat layer; email + dashboard only.** Loses real-time
  conversation channel. Rejected.

## Consequences

**Positive.** Sovereign per-site chat. Standard Matrix
protocol; federation when desired. Notification adapter
pattern means chat is optional per site.

**Negative.** A homeserver per site is one more service to
operate. Lightweight (Dendrite is a single Go binary).

## Assessor view (SCA-R)

Per-site Matrix serves IR-6 (incident reporting — chat as
the operator notification channel), AU-2 (notifications
audited), and CP-2 (contingency — partition-tolerant chat).
The federation control posture is consistent with NIST SP
800-95 web service security where applicable.

## Trigger conditions for revisit

- Operator volume justifies a different chat backend
  (unlikely). Use the notification adapter pattern.
- Dendrite governance changes adversely. Substitute
  Synapse or another implementation.

---

*See `GLOSSARY.md` for terms and acronyms.*
