# ADR 0303 — WireGuard data plane (kernel module + userspace fallback)

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0200-identity-based-mesh-overlay.md`](0200-identity-based-mesh-overlay.md), [`0103-crypto-boundary-discipline.md`](0103-crypto-boundary-discipline.md), [`0602-rosenpass-hybrid-rekey.md`](0602-rosenpass-hybrid-rekey.md), `0854-wireguard-fips-waiver.md` |

## TL;DR (for a layperson)

All traffic between peers travels inside an encrypted tunnel
called WireGuard. WireGuard is small, fast, and well-audited.
Whenever we can, we use the kernel-level version (faster); when
the kernel version is unavailable, we use the userspace version
(slower but works everywhere). WireGuard is not on the official
US government cryptographic-validation list, so a separate path
([Rosenpass + boundary node](0600-two-pq-paths.md)) carries the
post-quantum and FIPS-validated requirements.

## Context

WireGuard's design (small attack surface, modern AEAD ciphers,
key rotation, fast-path kernel implementation) is the right fit
for the intra-mesh data plane. It is not FIPS 140-3 validated
and the conservative reading of NIST guidance excludes its
ciphers from the validated module set. Two structural responses:

1. **Use WireGuard intra-mesh, document the deviation.** The
   intra-mesh data plane gets the speed and simplicity benefit;
   the deviation (`0854`) is
   explicit.
2. **Carry FIPS validation at the boundary node.** Cross-
   boundary traffic (mesh ↔ partner) goes through strongSwan
   + FIPS-validated module ([`0601`](0601-fips-pqc-boundary-node.md)).

This split is the [`0103`](0103-crypto-boundary-discipline.md)
boundary-discipline pattern in operation.

## Decision

WireGuard is the intra-mesh data plane, with this configuration:

- **Kernel module** preferred; userspace (`wireguard-go`)
  fallback when kernel module is unavailable (e.g. some
  containerized environments).
- **Default ciphers** (ChaCha20-Poly1305, BLAKE2s, Curve25519)
  used as-is; we do not negotiate alternates.
- **Rekey every 2 minutes / 60 GiB** per WG default.
- **Rosenpass** ([`0602`](0602-rosenpass-hybrid-rekey.md)) provides
  hybrid post-quantum rekey of WG session keys (intra-mesh PQ
  posture; not a FIPS claim).
- **WG keypair generation** on-device; private key never leaves
  the device.

## Alternatives considered

- **IPsec / strongSwan everywhere.** Heavier, slower, larger
  attack surface. Used at the boundary
  ([`0601`](0601-fips-pqc-boundary-node.md)) where FIPS validation
  is required, not as the universal data plane. Rejected as
  universal.
- **OpenVPN.** Older protocol design, larger attack surface,
  worse performance. Rejected.
- **Custom protocol.** No.

## Consequences

**Positive.** Fast (kernel-level cryptography), small attack
surface, well-audited, modern primitives, low operational
overhead.

**Negative.** Not FIPS 140-3 validated. `0854`
accepts this deliberately and identifies the compensating control
(the boundary node).

## Assessor view (SCA-R)

WireGuard intra-mesh serves SC-8 (transmission confidentiality
and integrity) and SC-12 (key establishment).
`0854` pairs the unvalidated
module with a defined compensating control: the boundary node carries
FIPS-validated cryptography for any cross-authorization-boundary
flow.

The Rosenpass hybrid rekey ([`0602`](0602-rosenpass-hybrid-rekey.md))
provides commercial-grade post-quantum protection for intra-mesh
session keys. NSA guidance on commercial-grade hybrid PQC
(2024) is consistent with this posture.

## Trigger conditions for revisit

- WireGuard or its primitives receive FIPS 140-3 validation.
  Retire the deviation.
- A specific authorization explicitly forbids non-validated
  modules within the boundary. Move WG outside the boundary
  (place every flow through the boundary node) or decline
  the federation.

---

*See `GLOSSARY.md` for terms and acronyms.*
