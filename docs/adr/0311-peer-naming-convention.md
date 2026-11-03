# ADR 0311 — Peer naming convention and stale-peer cleanup

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0307-setup-key-rotation.md`](0307-setup-key-rotation.md), `../operations/peer-naming.md` |

## TL;DR (for a layperson)

Every device on the mesh has a name. We follow a strict naming
convention so a glance at the peer list tells you what each
device is, where it lives, and what it does. Devices that
disappear (decommissioned, lost) have a deliberate cleanup
process to remove them from the registry.

## Context

Without a convention, peer names drift into chaos
(`my-laptop`, `test-server-2`, `foo-fixed`). With several
hundred peers across multiple sites, the registry becomes
unreviewable. Worse, stale peers accumulate — devices that no
longer exist but still have credentials in the registry.

## Decision

Peer names follow the canonical format defined in
`../operations/peer-naming.md`:

```
<role>-<site>-<nn>-<hw>
```

- **role** — what it does (cp, infra, eng, etc.).
- **site** — where it is (alpha, beta, dev, etc.).
- **nn** — two-digit number to disambiguate multiples.
- **hw** — hardware tag identifying the physical box (orin,
  nuc, pi, laptop, etc.).

Both the OS hostname and the NetBird peer name must match.
Refer to the operations doc for the authoritative role and
hardware-tag enumerations and current examples.

> **Correction note (2026-04-25).** The original draft of this
> ADR listed the format reversed as `<site>-<role>-<index>` and
> gave examples in that wrong order (`alpha-router-a`,
> `beta-orin-003`). The canonical format is role-first per
> `peer-naming.md`. Original draft authored without opening the
> operations doc.

Discipline:

- **No ad-hoc names.** A peer that needs a non-conforming name
  is a sign of a missing role; add the role rather than break
  the convention.
- **Stale-peer cleanup** runs weekly: peers with no handshake
  in 30 days are reviewed; 90 days unconditional revocation.
- **Decommission flow.** A site retiring a peer revokes its
  entry explicitly via `ops/ansible/scripts/revoke-peer.sh`;
  do not leave entries lingering.

The full convention with site codes and role names is in
`../operations/peer-naming.md`.

## Alternatives considered

- **No convention.** Registry becomes unreadable. Rejected.
- **UUID-based names.** Reviewable by tooling but not by
  humans; loses the at-a-glance value. Rejected.
- **Free-form names + tags.** Tags drift; humans don't apply
  them consistently. Rejected.

## Consequences

**Positive.** Registry is human-reviewable. Operator can verify
fleet shape at a glance. Decommissioned peers don't accumulate.

**Negative.** Discipline cost; we accept.

## Assessor view (SCA-R)

Naming convention serves CM-8 (component inventory) and AC-2
(account management — accountable enumeration of accounts).
The stale-peer cleanup serves IA-5 (authenticator management —
unused authenticators are revoked).

## Trigger conditions for revisit

- A new role class emerges that doesn't fit existing role
  names. Extend the role list explicitly.
- Cleanup intervals (30 / 90 days) prove too aggressive or
  too lax. Adjust; document the change.

---

*See `GLOSSARY.md` for terms and acronyms.*
