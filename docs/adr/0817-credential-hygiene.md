# ADR 0817 — Credential hygiene — WireGuard / SSH / TLS rotation cadence

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0307-setup-key-rotation.md`](0307-setup-key-rotation.md), [`0805-policy-engine-active-passive.md`](0805-policy-engine-active-passive.md) |

## TL;DR (for a layperson)

Beyond setup-key rotation (already covered in
[`0307`](0307-setup-key-rotation.md)), the system has many
other long-lived credentials: WireGuard peer keys, SSH host
keys, TLS certificates, internal service tokens. We rotate
each on a defined cadence and have automated tooling to make
the rotation routine. Approval flows through the policy
engine.

## Context

Long-lived credentials accumulate risk. Setup-key rotation
covers enrollment credentials; this ADR covers everything
else: WireGuard peer keys (rotated less frequently than
setup keys but still rotated), SSH host keys, internal TLS
certs, service-to-service tokens.

## Decision

Per-credential-class rotation cadence and tooling:

| Class | Cadence | Tool |
|---|---|---|
| **WireGuard peer keys** | 12 months (or on suspected compromise) | `ops/ansible/scripts/rotate-wg-keypair.sh` |
| **SSH host keys** | 12 months | Ansible `host-keys` role |
| **Internal TLS certs** | 90 days (Let's Encrypt automation) | Traefik + cert-manager equivalents |
| **Service-to-service tokens** | 90 days | Per-service rotation hook |
| **Operator tokens** | 30 days | Kanidm token expiry |
| **YubiKey-PIV root CA** | reviewed annually; rotated when needed | Offline ceremony |

Approval flows through the policy engine
([`0805`](0805-policy-engine-active-passive.md)) for
high-impact rotations (root CA, service tokens, mass-peer
rotation).

Tracked as Block 18D in
`../project/implementation-plan.md`.

## Alternatives considered

- **No rotation cadence; rotate on incident only.**
  Long-tail risk. Rejected.
- **Aggressive rotation everywhere (e.g. 30 days for WG
  keys).** Operational cost out of proportion to risk
  reduction. Rejected.
- **Manual rotation via runbooks.** Drift. Rejected;
  scripts are the source of truth.

## Consequences

**Positive.** Credential exposure is bounded. Rotation is
routine, not panic. Approvals are auditable.

**Negative.** Discipline cost. Acceptable; mostly automated.

## Assessor view (SCA-R)

Credential hygiene serves IA-5 (authenticator management —
authenticator lifecycle), AC-2 (account management),
SC-12 (key establishment — rotation supports key freshness),
and AU-2 (rotation events audited).

## Trigger conditions for revisit

- A credential class compromise drives a one-off mass
  rotation. Document; review the cadence after recovery.
- Operational evidence shows a cadence is wrong (too short
  / too long). Adjust; document.

---

*See `GLOSSARY.md` for terms and acronyms.*
