# ADR 0200 — Identity-based mesh overlay (vs perimeter network)

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [`0300-netbird-as-mesh-control-plane.md`](0300-netbird-as-mesh-control-plane.md), [`0303-wireguard-data-plane.md`](0303-wireguard-data-plane.md) |

## TL;DR (for a layperson)

Traditional military networks rely on a perimeter — a wall around
the network with a few gates. Once inside, devices trust each other.
We do not work that way. Every device in goat has a
cryptographic identity, and access is decided per-pair, per-device,
per-action. There is no "inside" to be inside of. This is the
modern zero-trust pattern, and it is the only pattern that survives
contact with denied / degraded / intermittent / limited-bandwidth
environments.

## Context

Classical perimeter networks (the typical DoD enclave model) put
trust in network location: traffic inside the perimeter is
trusted, traffic crossing the perimeter is inspected. This breaks
under three pressures relevant to us:

1. **No reliable perimeter at the tactical edge.** Forward sites
   share infrastructure with partners, attach to commercial
   satellite transport, and host devices that move between
   sites. A perimeter is a fiction.
2. **Coalition federation.** Partner networks
   sit outside any DoD perimeter we could establish; they require
   per-system, per-data-flow trust decisions, not network
   location.
3. **Island-mode operation.** A site cut off from the management
   plane must continue making per-pair access decisions. There is
   no "central perimeter" to consult.

The fix is the architecture the modern internet adopted:
identity-based, with cryptographic per-device identity, per-pair
authorization, and per-flow encryption.

## Decision

goat is an identity-based mesh overlay. Specifically:

- **Every peer has a WireGuard keypair** generated on-device,
  private key never leaves the device.
- **Peer identity is the public key plus the management-plane
  enrollment record** (peer name, role, allowed routes, allowed
  ACLs).
- **Authorization is per-peer-pair**, decided by ACL evaluation
  at the management plane and pushed to peers as policy.
- **All peer traffic is encrypted by WireGuard end-to-end** —
  there is no unencrypted overlay.
- **The overlay is flat** — peers at different sites communicate
  directly via WireGuard P2P, with single-hop relay fallback
  when P2P fails ([`0306`](0306-p2p-with-relay-fallback.md)).
- **Network location is not trust.** A peer's IP, site, or
  transport medium does not grant access; only its identity
  does.

The full architecture is documented in
`../design/architecture.md` and
`../design/security-architecture.md`;
this ADR records the strategic choice.

## Alternatives considered

- **Classical perimeter (DoD enclave model).** Cannot survive
  partner federation, island mode, or moving devices. Rejected.
- **Software-defined perimeter (SDP) with brokered access.**
  Closer to identity-based, but typically broker-centric, which
  fails island mode. Rejected.
- **Pure VPN concentrator topology.** Single point of failure;
  no P2P; no island mode. Rejected.

## Consequences

**Positive.** Survives partner federation, island mode, moving
devices, and management-plane outages. Authorization is decidable
per-pair without central consultation once policy has propagated.
Maps directly onto NIST SP 800-207 Zero Trust Architecture.

**Negative.** Identity issuance, rotation, and revocation become
load-bearing. Setup-key rotation
([`0307`](0307-setup-key-rotation.md)), credential hygiene
([`0817`](0817-credential-hygiene.md)), and offline enrollment
([`0308`](0308-offline-enrollment-cbor-bundles.md)) all carry
identity-management weight that a perimeter model would put on
firewalls.

## Assessor view (SCA-R)

NIST SP 800-207 ZTA is the primary frame. Specific control
mappings: AC-3 (access enforcement), AC-4 (information flow
enforcement), AC-17 (remote access), IA-2 (organizational user
identification), IA-3 (device identification and authentication),
SC-7 (boundary protection — applied per-flow not per-perimeter),
SC-8 (transmission confidentiality and integrity).

The system has *no* implicit-trust network zone. Every flow is
authorized by identity and encrypted in flight. This is a
substantively stronger posture than median DoD enclave networks
on AC-3 / AC-4 / SC-7 — see
`../compliance/comparative-posture.md`.

## Trigger conditions for revisit

- A federation partner explicitly requires a perimeter-shaped
  entry point (uncommon; typically resolved by the bridge VPSes
  acting as the mediation layer).
- Identity-management costs become unsustainable at scale (we
  expect sub-1000 peers per site for the foreseeable future;
  revisit at 5000+).

---

*See `GLOSSARY.md` for terms and acronyms.*
