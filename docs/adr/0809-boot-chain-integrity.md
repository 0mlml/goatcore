# ADR 0809 — Boot chain integrity — UEFI Secure Boot + signed shim + dm-verity

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0604-luks-data-at-rest.md`](0604-luks-data-at-rest.md), [`0900-ubuntu-lts-luks-x86.md`](0900-ubuntu-lts-luks-x86.md) |

## TL;DR (for a layperson)

LUKS encrypts data at rest, but encryption alone doesn't tell
you whether the operating system itself was tampered with.
Boot chain integrity is the complement: every stage of boot
verifies the next stage's signature, all the way from UEFI
Secure Boot through the bootloader (signed shim) to the root
filesystem (dm-verity). If any stage fails verification, the
boot stops.

## Context

LUKS gives confidentiality. Boot chain integrity gives
*integrity* — the OS and root filesystem haven't been
modified offline.

The chain:

- UEFI Secure Boot → verifies bootloader.
- Signed shim → verifies kernel + initrd.
- dm-verity → verifies root filesystem block-by-block at
  read time.

Each stage anchors trust in the next. Without dm-verity, an
attacker with offline disk access could modify root
filesystem contents (read-only system files); dm-verity
detects that on the next read.

## Decision

Boot chain integrity is enforced on production mgmt-hosts:

- **UEFI Secure Boot** enabled in firmware.
- **Signed shim** (Microsoft-signed → Ubuntu shim → Ubuntu
  GRUB → Ubuntu kernel chain).
- **Kernel + initrd signed** by Ubuntu Pro signing keys
  ([`0900`](0900-ubuntu-lts-luks-x86.md)).
- **dm-verity** on read-only system partitions; mutable
  partitions covered by other controls (LUKS for /var,
  audit-integrity tier for logs).
- **TPM 2.0 measured boot** records each stage in PCRs;
  PCR values gate LUKS unlock when in `tpm2` mode
  ([`0604`](0604-luks-data-at-rest.md)).

Tracked as Block 18C in
`../project/implementation-plan.md`.

## Alternatives considered

- **Secure Boot only, no dm-verity.** Misses runtime
  filesystem-tampering. Rejected.
- **dm-verity only, no Secure Boot.** Misses pre-boot
  tamper. Rejected.
- **No boot chain integrity.** Disqualifies the system from
  Moderate-baseline integrity controls. Rejected.

## Consequences

**Positive.** Tampered OS / root filesystem refuses to boot.
Combined with LUKS, both confidentiality and integrity at
rest.

**Negative.** Booting custom kernels requires re-signing or
shim configuration. We accept; the build pipeline produces
signed kernels for our deployments.

## Assessor view (SCA-R)

Boot chain integrity serves SI-7 (software, firmware,
integrity), CM-2 (baseline configuration), and SC-28
(protection of information at rest — integrity dimension).
NIST SP 800-147 BIOS protection guidance applies; UEFI
Secure Boot + measured boot meets the bar.

## Trigger conditions for revisit

- Custom-kernel requirements outpace the signing pipeline.
  Investigate; preserve Secure Boot.
- A host class lacks TPM 2.0. Document the exception;
  preserve Secure Boot + dm-verity.

---

*See `GLOSSARY.md` for terms and acronyms.*
