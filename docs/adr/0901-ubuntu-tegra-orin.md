# ADR 0901 — Ubuntu-for-Tegra 22.04 + Ubuntu Pro (FIPS+STIG) for Orin

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0605-op-tee-key-custody.md`](0605-op-tee-key-custody.md), `0902-goat-drop.md`, [`../design/orin-os-decision.md`](../design/orin-os-decision.md), `../operations/pilot-bringup.md` |

## TL;DR (for a layperson)

The NVIDIA Jetson Orin nodes (used at compute / sensor sites)
run Ubuntu-for-Tegra 22.04 with Ubuntu Pro (FIPS + STIG). This
is NVIDIA's Jetson-specific Ubuntu plus the FIPS / STIG
compliance overlay. We use the mutable-root variant; LUKS lives
on the NVMe with key custody via OP-TEE / Tegra-SE where the
JetPack version supports it.

## Context

Orin host class has different constraints from x86:

- Specific Linux kernel + driver stack (L4T) supplied by NVIDIA.
- JetPack release cadence; current target JetPack 6.1 / L4T
  R36.4.7.
- Hardware-rooted secure element (Tegra-SE) accessible via
  OP-TEE.
- Mutable root required by JetPack tooling / OTA.

## Decision

Orin pilot runs:

- **OS**: Ubuntu-for-Tegra 22.04 (mutable root).
- **JetPack**: 6.1 / L4T R36.4.7 currently.
- **Compliance channel**: Ubuntu Pro (FIPS + STIG).
- **Storage encryption**: LUKS on NVMe.
- **Key custody**: OP-TEE / Tegra-SE where feasible
  ([`0605`](0605-op-tee-key-custody.md)).
- **Pilot bringup**: rewritten runbook at
  `../operations/pilot-bringup.md`.
- **Open questions** tracked in
  [`../design/orin-os-decision.md`](../design/orin-os-decision.md).

Migration tracked in Block 6D of
`../project/implementation-plan.md`.

## Alternatives considered

- **Kairos on Orin.** Could not deliver FIPS 140-3 + DISA
  STIG. Dropped per `0902`.
- **Stock NVIDIA Jetson Linux without Ubuntu Pro.** Loses
  FIPS / STIG channel. Rejected.
- **Pure Ubuntu (not Ubuntu-for-Tegra).** Loses NVIDIA
  driver support. Rejected.

## Consequences

**Positive.** Compliant compliance channel + native
NVIDIA driver stack + hardware-rooted secure element.

**Negative.** Mutable root limits dm-verity coverage. JetPack
release cadence is decoupled from upstream Ubuntu's. Both
acceptable.

## Assessor view (SCA-R)

Ubuntu-for-Tegra + Ubuntu Pro serves CM-2 (baseline
configuration), CM-6 (configuration settings — STIG),
SC-13 (FIPS-validated kernel module), SC-28 (LUKS),
and SI-7 (Secure Boot + measured boot). Ubuntu Pro's
FIPS-validated module is the load-bearing artifact for
the Jetson host class.

## Trigger conditions for revisit

- JetPack version update changes secure-element surface.
- Ubuntu Pro support for Ubuntu-for-Tegra changes.
- A specific authorization requires a different Jetson OS
  baseline. Negotiate.

---

*See `GLOSSARY.md` for terms and acronyms.*
