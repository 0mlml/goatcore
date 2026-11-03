# ADR 0605 — OP-TEE / Tegra-SE key custody on Orin (where feasible)

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0604-luks-data-at-rest.md`](0604-luks-data-at-rest.md), [`0901-ubuntu-tegra-orin.md`](0901-ubuntu-tegra-orin.md), [`../design/orin-os-decision.md`](../design/orin-os-decision.md) |

## TL;DR (for a layperson)

The NVIDIA Jetson Orin chip has a hardware security feature
called Tegra-SE that can hold encryption keys in hardware so
the operating system never sees them. We use it to hold the
LUKS unlock keys on Orin nodes where this is feasible on the
JetPack version we're running. This is more secure than
storing keys in software because a compromise of the OS
cannot leak the hardware-held key.

## Context

Hardware-rooted key custody is a strong control: the key never
leaves the hardware, so an OS compromise cannot exfiltrate it.
On Orin (Jetson AGX), Tegra-SE backs OP-TEE (Open Portable
Trusted Execution Environment), which can hold and use keys
without exposing them to the rich OS.

Feasibility depends on JetPack / Ubuntu-for-Tegra version
support; this is tracked in
[`../design/orin-os-decision.md`](../design/orin-os-decision.md).

## Decision

On Orin running Ubuntu-for-Tegra 22.04 + Ubuntu Pro
([`0901`](0901-ubuntu-tegra-orin.md)):

- **LUKS unlock key custody** target is OP-TEE (Tegra-SE-backed).
  Per `orin-os-decision.md` §3, **feasibility on JetPack 6.1
  (L4T R36.4.7) is an open engineering item** requiring a spike
  before commitment. The original draft of this ADR presented
  OP-TEE custody as the decided default — the design doc is
  more cautious.
- **Documented fallback per the design doc:** "passphrase +
  physical access control" if OP-TEE custody is not achievable
  on the target JetPack version in the available window. The
  fallback is treated as Phase 3 technical debt, not as the
  primary plan.
- **TPM 2.0 / other modes** per
  [`0604`](0604-luks-data-at-rest.md) where neither OP-TEE
  custody nor passphrase fallback applies.
- **No third-party software** invoked through OP-TEE; only
  the standard OP-TEE OS + key-management TA.
- **Tracked open question** in
  [`../design/orin-os-decision.md`](../design/orin-os-decision.md)
  for the long-term key-custody story (e.g. discrete TPM
  vs Tegra-SE on each Orin host class).

This applies *only* to Orin. x86 management hosts use a discrete TPM 2.0.

## Alternatives considered

- **Software-only LUKS unlock.** Lower assurance. Rejected as
  default on Orin where OP-TEE is feasible.
- **Cloud HSM key custody.** Loses sovereignty / island-mode.
  Rejected.
- **External hardware HSM per node.** Cost / complexity.
  Rejected at our scale.

## Consequences

**Positive.** Hardware-rooted unlock-key custody on Orin.
OS compromise cannot leak the key. Aligns with the
defense-in-depth posture.

**Negative.** OP-TEE on JetPack is less mature than TPM
ecosystems on x86. Operational tooling is thinner. We
accept; fallback to TPM-equivalent modes is documented.

## Assessor view (SCA-R)

OP-TEE / Tegra-SE custody serves SC-12 (key establishment),
SC-28 (data at rest), and SI-7 (firmware integrity — TEE is
measured).

## Trigger conditions for revisit

- JetPack version no longer supports OP-TEE custody (e.g.
  vendor-driven removal). Fall back to TPM 2.0 mode (Orin
  may need a discrete TPM module or a different x86 host
  class for the affected role).
- A new Orin host class ships with a different secure-element
  surface. Update the per-host-class default.

---

*See `GLOSSARY.md` for terms and acronyms.*
