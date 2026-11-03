# ADR 0307 — Setup-key role-scoped TTLs and auto-revoke

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0200-identity-based-mesh-overlay.md`](0200-identity-based-mesh-overlay.md), [`0310-setup-key-kill-switch.md`](0310-setup-key-kill-switch.md), [`../operations/setup-key-rotation.md`](../operations/setup-key-rotation.md) |

## TL;DR (for a layperson)

The setup key is the credential that lets a brand-new device
join the mesh. We never use a single permanent setup key.
Instead, every key is scoped to a role (bootstrap, admin,
operator, peer) with a short, role-specific lifetime. Keys
auto-revoke at expiry, and a kill switch lets us yank everything
in seconds during an incident.

## Context

A long-lived setup key is a long-lived credential, and a
long-lived credential left around becomes the worst breach
vector for any mesh. We adopt the discipline that setup keys
are short-lived, role-scoped, auditable, and auto-revoked.

## Decision

Setup keys have role-scoped TTLs:

| Role | TTL | Use case |
|---|---|---|
| **bootstrap** | 1 day | One-time site bring-up. |
| **admin** | 7 days | Adding a small batch of admin peers. |
| **operator** | 30 days | Per-deployment operator onboarding. |
| **peer** | 7 days | One-time single-device enrollment. |

**Peer TTL clarification recorded 2026-10-03.** The peer default changed from
90 days to 7 days on 2026-08-18, as recorded in the
[rotation plan](../operations/setup-key-rotation.md#role-taxonomy) and implemented
by `goat admin onboard`. The SSM helper uses the same default. A longer window
is a per-enrollment exception and must carry a reason: `netbird.ttl_reason` in
the onboarding manifest, or `--note` for the SSM helper. This records the
existing policy; it does not change a key already issued.

Operational discipline:

- **Helper scripts.** `ops/ansible/scripts/create-setup-key.sh`
  and `revoke-setup-key.sh` are the only sanctioned ways to
  mint/revoke keys; both write audit-log entries.
- **Auto-revoke cron.** `ops/ansible/scripts/setup-key-auto-
  revoke.sh` runs daily and revokes any expired key.
- **Audit trail.** Every key creation and revocation is logged
  to the central audit pipeline.
- **Emergency kill switch** ([`0310`](0310-setup-key-kill-switch.md))
  revokes all active setup keys on demand.

The full plan is in
[`../operations/setup-key-rotation.md`](../operations/setup-key-rotation.md).

## Alternatives considered

- **Single permanent setup key.** Catastrophic breach surface.
  Rejected.
- **Per-peer one-time keys.** Maximum security but operationally
  heavy; we approximate this for `bootstrap` and `admin` roles.
- **OIDC-based enrollment.** Higher operational cost; loses
  Island-Mode property of self-contained enrollment. Held as
  a future enhancement.

## Consequences

**Positive.** Lost / leaked keys have a small blast radius and
short TTL. Auto-revoke keeps the active-key set small. The
helper scripts make doing-the-right-thing the path of least
resistance.

**Negative.** A team that needs many enrollments outside the
normal cadence has to refresh keys more often. We accept;
short TTL is the discipline.

## Assessor view (SCA-R)

Setup-key rotation serves AC-2 (account management — peer
enrollment is account creation), AC-6 (least privilege — TTL
scoping is privilege containment), IA-5 (authenticator
management), and AU-2 (audit). The auto-revoke discipline is
mechanical, not advisory; reviewers can verify by reading the
cron logs.

## Trigger conditions for revisit

- Operational evidence that a TTL is too short or too long
  for a role. Adjust the role-specific TTL.
- A new role emerges (e.g. `partner-federation`). Add it
  explicitly with its own TTL.

---

*See `GLOSSARY.md` for terms and acronyms.*
