# ADR 0403 — FIDO2 for break-glass and admin authentication

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | Multi |
| Related | [`0402-kanidm-for-accounts.md`](0402-kanidm-for-accounts.md), `0852-fido2-vs-cac-waiver.md`, `../compliance/waivers/fido2-vs-cac.md` |

## TL;DR (for a layperson)

The standard DoD authenticator is the CAC (Common Access Card).
We use FIDO2 hardware security keys instead — small USB devices
that prove possession by an unphishable cryptographic protocol.
This is a deliberate departure from CAC and it needs a
formal waiver. The reasons: FIDO2 works on field hardware that
has no CAC reader, supports air-gapped break-glass enrollment,
and is operationally faster.

## Context

CAC has two structural problems for our environment:

1. **Hardware availability.** CAC readers are not present on
   every device class we deploy. Adding them per-device is
   expensive and fragile.
2. **Network dependency.** CAC validation traditionally needs
   reachability to OCSP/CRL services; our partition-tolerant
   posture cannot accept that dependency.

FIDO2 solves both: hardware tokens (YubiKey 5 series) are USB-A
or USB-C, work everywhere, and the WebAuthn protocol does *not*
require online validation per use (the trust is in the
registration record at the relying party).

## Decision

FIDO2 hardware authenticators are the primary authenticator for:

- **Operator login** at every mgmt-host (PAM via Kanidm).
- **Break-glass access** under FIDO2 hardware token escrow.
- **Admin operations** (sudo, OIDC SSO).
- **Offline-CA ceremonies** (`0406`)
  use YubiKey PIV separately for signing operations.

Per-operator: at least one primary token + one backup token,
both registered. Tokens are issued in a controlled ceremony.

CAC support remains *available* via Kanidm where partner
federations require it; FIDO2 is the *primary* path.

The waiver narrative is at
`../compliance/waivers/fido2-vs-cac.md`.

## Alternatives considered

- **CAC primary, FIDO2 fallback.** Inverts the operational
  shape we need; CAC reader availability is the bottleneck.
  Rejected as primary.
- **Password + TOTP.** Phishable; weaker than FIDO2. Rejected.
- **Smart-card software.** Loses hardware-rooted trust.
  Rejected.

## Consequences

**Positive.** Phishing-resistant. Hardware-rooted. Works on
all device classes. Partition-tolerant. Faster operator login
than CAC. Two-token-per-operator discipline mitigates lost-
token risk.

**Negative.** Formal waiver required (`0852`).
Operators must safeguard tokens; lost tokens require a
documented re-issue ceremony.

## Assessor view (SCA-R)

FIDO2 serves IA-2 (identification and authentication) and IA-5
(authenticator management) at NIST SP 800-63B AAL2/AAL3 when
paired with hardware tokens. The waiver
(`../compliance/waivers/fido2-vs-cac.md`)
documents the deviation from DoD CAC default. Compensating
controls: two-token-per-operator policy, documented re-issue
ceremony, audit-logged registrations.

NSA published guidance acknowledging FIDO2 / WebAuthn at AAL3
levels under specific configurations; our configuration
matches.

## Trigger conditions for revisit

- A specific authorization explicitly forbids non-CAC
  authenticators in the boundary. Negotiate; consider hybrid
  CAC + FIDO2 deployment for that boundary specifically.
- DoD CAC infrastructure adopts FIDO2-equivalent semantics.
  Retire the waiver and align.

---

*See `GLOSSARY.md` for terms and acronyms.*
