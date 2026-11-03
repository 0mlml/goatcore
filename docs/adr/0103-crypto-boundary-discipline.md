# ADR 0103 — Crypto boundary discipline — FIPS at well-defined interfaces

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | Multi |
| Supersedes | — |
| Superseded by | — |
| Related | [`0600-two-pq-paths.md`](0600-two-pq-paths.md), [`0601-fips-pqc-boundary-node.md`](0601-fips-pqc-boundary-node.md), [`0602-rosenpass-hybrid-rekey.md`](0602-rosenpass-hybrid-rekey.md) |

## TL;DR (for a layperson)

Cryptography requirements for federal systems are heavy and slow to
update. If we let those requirements bleed into every part of the
system, the whole system slows down and becomes locked into old
crypto choices. Instead, we keep cryptography at well-defined
interfaces — VPN endpoints, data-at-rest, key management — and the
rest of the application layer treats them as swappable parts. This
is what lets us run modern, agile components like Zenoh, NetBird,
and Rosenpass alongside formally validated crypto at the boundary.

## Diagram



Source: `diagrams/0103-crypto-boundary-discipline.mmd`.

## Context

FIPS 140-3 validation cycles are long (typically 12-24 months from
submission to certificate) and the validated module set lags the
state of the art. If a system's application layer takes hard
dependencies on FIPS-validated primitives, the whole stack is locked
to whatever the validated set contained when the system was
designed. Two structural problems follow:

1. **Crypto agility dies.** The system cannot adopt a newer primitive
   (post-quantum KEM, modern AEAD, hardware-rooted attestation)
   until that primitive shows up validated, which can be years.
2. **The application layer takes on validation burden it shouldn't
   carry.** Application code that only needs a transport with
   confidentiality and integrity ends up reasoning about cipher
   suites, validation status, and FIPS-mode boot chains. The
   application gets slower, more brittle, and harder to audit.

The fix is the same one TLS uses (terminate at the edge), the same
one CSfC uses (defined boundary capability packages), and the same
one mature distributed systems use (separation of concerns):
crypto lives at *interfaces*, not throughout the stack.

## Decision

Cryptography is concentrated at named, narrow boundaries. The rest
of the system treats those boundaries as opaque and substitutable.

The named boundaries:

| Boundary | Where | Validation requirement |
|---|---|---|
| **Cross-boundary network** (mesh ↔ partner network, classified ↔ unclass crossings) | x86 boundary node running strongSwan + wolfSSL/AWS-LC FIPS + ML-KEM ([`0601`](0601-fips-pqc-boundary-node.md)) | FIPS 140-3 validated module |
| **Intra-mesh data plane** (peer-to-peer WireGuard) | WireGuard kernel module + Rosenpass hybrid rekey ([`0602`](0602-rosenpass-hybrid-rekey.md)) | Intentional non-FIPS; a deployment bound to FIPS records it as a standing exception |
| **Data at rest** (mgmt host disks, NVMe storage) | LUKS + NBDE / multi-mode unlock ([`0604`](0604-luks-data-at-rest.md)); OP-TEE / Tegra-SE key custody on Orin where feasible ([`0605`](0605-op-tee-key-custody.md)) | FIPS 140-3 validated kernel module via Ubuntu Pro |
| **Key custody** | YubiKey PIV (offline CA), TPM 2.0 (boot chain), OP-TEE / Tegra-SE (Orin) | FIPS 140-2 / 140-3 device-level certs |
| **Identity / authentication** | FIDO2 hardware attestation; Kanidm for credentials | FIDO Alliance certified attestation |
| **Audit log integrity** | MinIO Object Lock + signed integrity ledger | Hash-chain integrity, no validation requirement |

Application-layer components (signal-server, gatewatch, demo apps,
Zenoh, the management plane itself) consume these boundaries and do
not implement crypto themselves. Where an application generates
randomness, signs an artifact, or encrypts in flight, it does so via
a library that comes from one of the boundary's validated paths.

## Alternatives considered

- **FIPS-validate every component.** Locks the system to whatever
  crypto was validated when components were chosen; kills agility.
  Rejected.
- **No FIPS posture; use modern crypto everywhere.** Disqualifies
  the system from cross-boundary federation with FIPS-required
  partners. Rejected.
- **CSfC as the only crypto framework.** CSfC governs specific
  capability packages (data-at-rest, data-in-transit) but does not
  cover intra-mesh peer crypto or application-layer signing. Use
  it where it applies, not as the universal frame.

## Consequences

**Positive.** The application layer can use modern, fast, agile
crypto (e.g. WireGuard's ChaCha20-Poly1305) without dragging the
system through FIPS validation. The boundary nodes carry the
validation burden, and the rest of the stack stays nimble. New
post-quantum primitives can be adopted at the boundary independently
of whether the application layer is touched.

**Negative.** The boundary nodes are themselves a hard dependency
and a CVE treadmill. We accept the cost in exchange for keeping the
rest of the stack agile.

The discipline requires that we *not* let "FIPS everywhere" pressure
creep into application-layer ADRs. Reviewers must reject decisions
that take on FIPS burden in places that should remain opaque
(e.g. application TLS termination should remain at Traefik, not
move into application code).

## Assessor view (SCA-R)

The crypto boundary discipline maps onto NIST SP 800-53 SC-12
(cryptographic key establishment), SC-13 (cryptographic protection),
SC-17 (PKI certificates), SC-28 (protection of information at rest),
and SC-8 (transmission confidentiality and integrity) — all
concentrated at the named boundaries above. The assessor evaluates
each boundary in isolation, with the application layer treated as
"data flowing across an established cryptographic boundary."

Intra-mesh WireGuard is the only place where a non-validated module
sits inside the authorization boundary. A deployment bound to FIPS
records it as a standing exception, structured around NIST SP 800-175B
framework guidance and NSA guidance on commercial-grade post-quantum
hybrid configurations.

## Trigger conditions for revisit

- A new boundary type emerges (e.g. inter-cluster, cloud-native
  attestation). Add a row; do not retrofit existing boundaries.
- A partner authorization explicitly forbids non-FIPS components
  inside the authorization boundary. Negotiate the boundary
  position; do not attempt to FIPS-validate WireGuard.
- WireGuard or Rosenpass receives FIPS validation. The intra-mesh
  exception retires.

---

*See `GLOSSARY.md` for terms and acronyms.*
