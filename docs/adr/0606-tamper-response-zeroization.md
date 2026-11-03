# ADR 0606 — Tamper response — physical chassis intrusion → key zeroization

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0604-luks-data-at-rest.md`](0604-luks-data-at-rest.md), [`0805-policy-engine-active-passive.md`](0805-policy-engine-active-passive.md), [`0810-tamper-response.md`](0810-tamper-response.md) |

## TL;DR (for a layperson)

In contested environments, an attacker may physically open a
server's chassis to extract data. We respond to that by
having the hardware itself zero out the disk encryption keys
the moment the chassis intrusion sensor trips. After the keys
are zeroed, the encrypted data on disk is unrecoverable. This
is the same pattern used by FIPS 140 Level 3+ devices, applied
at the system level.

## Context

Physical intrusion is a real threat in deployed environments.
Standard data-at-rest encryption ([`0604`](0604-luks-data-at-rest.md))
protects against drive theft after an orderly shutdown, but
not against a powered-on chassis being opened and the running
keys extracted from RAM.

Zeroization on chassis-intrusion-sensor trip is the
counter-measure: hardware-level event triggers immediate
crypto-key wipe.

## Decision

Tamper response is implemented as a tiered system:

- **Hardware:** chassis intrusion sensor (where the host
  hardware supports it).
- **Boot chain:** UEFI Secure Boot + signed shim
  ([`0809`](0809-boot-chain-integrity.md)) detects unsigned
  modifications.
- **Runtime daemon:** systemd unit listens on the intrusion
  sensor's GPIO; on trip, runs:
  - LUKS header wipe (`cryptsetup luksErase`).
  - In-memory key zeroization via kernel API.
  - Forced kernel panic / power-off.
- **Arming / disarming** flows through the per-site policy
  engine ([`0805`](0805-policy-engine-active-passive.md)).
  Tamper response is *armed* in deployed sites; operators
  must explicitly disarm before opening the chassis for
  maintenance.

Implementation tracked as Block 18F in
`../project/implementation-plan.md`.
Detailed design in [`0810`](0810-tamper-response.md).

## Alternatives considered

- **No tamper response.** Acceptable for non-contested-
  environment deployments; not acceptable for forward sites.
- **Full destructive wipe (drive overwrite).** Slower than
  LUKS header wipe; LUKS-header wipe achieves equivalent
  unrecoverability instantly because the master key is
  unrecoverable.
- **Manual operator-pulled-the-lever response.** Too slow;
  fails the threat model.

## Consequences

**Positive.** Physical intrusion in a contested environment
results in unrecoverable data within seconds. Operator
arming-disarming flow prevents accidental wipes during
maintenance.

**Negative.** Operator must remember to disarm before
maintenance. We accept; the policy-engine arming workflow
gates this and produces an audit trail.

The tamper response is *destructive*. False positives on the
intrusion sensor trigger a real wipe. We accept the trade-off
for forward sites; non-forward sites can run the system
disarmed.

## Assessor view (SCA-R)

Tamper response serves PE-3 (physical access control —
detection), PE-6 (monitoring physical access), and SC-28
(protection of information at rest — destructive response).
This is consistent with the FIPS 140 Level 3+ tamper-response
pattern. Mapping onto NIST SP 800-53 PE-x is direct; the
specific control for "tamper-detection-triggered cryptographic
zeroization" is not in the standard set, so the ADR is the
authoritative description.

## Trigger conditions for revisit

- A specific deployment requires tamper response off
  (lab / training environments). Use the disarm flow.
- A new host class without intrusion sensor needs tamper
  response. Add an alternative trigger (e.g. accelerometer-
  based detection for portable hardware).

---

*See `GLOSSARY.md` for terms and acronyms.*
