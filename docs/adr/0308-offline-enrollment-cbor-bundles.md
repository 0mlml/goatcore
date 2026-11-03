# ADR 0308 — Offline enrollment — CBOR bundles signed by offline CA

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0201-enrollment-vs-mesh-op-separation.md`](0201-enrollment-vs-mesh-op-separation.md), `0406-offline-ca-yubikey-piv.md`, [`../design/offline-enrollment.md`](../design/offline-enrollment.md), [`../operations/enrollment-bundle-runbook.md`](../operations/enrollment-bundle-runbook.md) |

## TL;DR (for a layperson)

Sometimes a device has to join the mesh without being able to
reach the management plane — for example, when it's air-gapped
on the way to a forward site. For that case, we ship the device
with a small file (a "bundle") that contains everything it needs
to be enrolled, signed by an offline certificate authority that
lives on a YubiKey. A small "validator" program on the device
reads the bundle and provisions the peer automatically. The
bundle expires after 90 days so a stolen one is bounded in
exposure.

## Context

The PACE Contingency path requires an enrollment mode that does
*not* require management-plane reachability at enrollment time.
The conventional setup-key flow ([`0307`](0307-setup-key-rotation.md))
fails here.

The fix: ship a self-contained, signed bundle with the device.
The bundle carries everything the validator needs to provision
the peer on first boot.

## Decision

Offline enrollment uses signed CBOR bundles:

- **Format:** CBOR (Concise Binary Object Representation) for
  compactness and unambiguous parse semantics.
- **Contents:** pre-shared WireGuard config + signed ACL
  capability + 90-day validity window + peer name + role.
- **Signature:** Ed25519, signed by the offline CA on YubiKey
  PIV (`0406`).
- **Validator:** sidecar daemon on the management host;
  consumes bundle, validates signature against the CA root,
  checks expiry, provisions the peer record into Postgres
  (Spock replicates the row to other sites — the validator
  writes locally, Spock handles cross-site).
- **Revocation (Phase 2A — current):** CRL distributed via
  HTTP pull from a single issuance endpoint; validator
  refuses bundles whose serial is in the CRL. Per
  `offline-enrollment.md` §6.1, this is the Phase 2A model.
  Spock-replicated CRL distribution is Phase 3+. The original
  draft of this ADR conflated the two — corrected here.
- **Expiry:** 90 days hard cap; bundles after that auto-reject.

Implementation: `ops/enrollment/` (CA tooling, validator,
CRL pipeline, device consumer). Ansible role:
`ops/ansible/roles/enrollment-validator/`. Runbook:
[`../operations/enrollment-bundle-runbook.md`](../operations/enrollment-bundle-runbook.md).

This supersedes the narrower "pre-shared WG bootstrap" concept;
the bundle pattern is more general.

## Alternatives considered

- **Pre-shared WG-only.** Loses ACL capability binding; loses
  expiry. Rejected.
- **Reach mgmt plane via partner network on first boot.**
  Doesn't work in PACE Contingency; loses the property we
  need. Rejected as primary; remains the default
  enrollment path.
- **Online-only enrollment, refuse PACE Contingency.**
  Operationally untenable. Rejected.

## Consequences

**Positive.** A device shipped with a bundle can self-provision
on first boot without management-plane reachability. The 90-day
expiry bounds exposure. The CRL allows revocation of issued
bundles. The CA on YubiKey PIV gives strong custody of the
signing key.

**Negative.** Bundle issuance is now a CA-ceremony operation
([`../operations/enrollment-bundle-runbook.md`](../operations/enrollment-bundle-runbook.md)).
Operator-only and audited. We accept the operational discipline.

## Assessor view (SCA-R)

Offline enrollment serves AC-2 (account management — peer
enrollment), IA-5 (authenticator management — bundle as
authenticator), CP-2 (contingency — PACE-Contingency
enrollment), AU-2 (audit — bundle issuance and consumption are
audited), and SC-12 (key establishment). The Ed25519 / YubiKey
PIV anchor gives a hardware-rooted trust path.

The 90-day expiry is the compensating control for the offline-
distribution risk surface.

## Trigger conditions for revisit

- Bundle issuance volume justifies automation beyond the CA
  ceremony. Investigate; do not compromise the CA-key
  custody.
- A federation partner accepts only their own enrollment
  protocol. Negotiate per-partner; do not abandon offline
  enrollment.

---

*See `GLOSSARY.md` for terms and acronyms.*
