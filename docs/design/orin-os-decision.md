# Orin Host OS Decision

**Decision (2026-04-22):** The Jetson AGX Orin production-mesh hosts run **Ubuntu-for-Tegra 22.04 under Ubuntu Pro (FIPS + STIG)** on JetPack 6.1 (L4T R36.4.7). Mutable root. LUKS on the 1 TB M.2 NVMe, with key custody via Tegra SE exposed through OP-TEE Trusted Applications where feasible on this JetPack revision.

Kairos was evaluated and dropped. This doc captures why, what Kairos was doing for us architecturally, and how each of those load-bearing pieces gets replaced under Ubuntu Pro.

## Driver

We need **FIPS 140-3 validated crypto** and **DISA STIG hardening** on the production hosts. These are hard compliance requirements, not preferences.

| Requirement | Ubuntu Pro 22.04 | Kairos |
|---|---|---|
| FIPS 140-3 validated kernel + crypto modules | Yes — `pro enable fips-updates` | No path |
| DISA STIG hardening profile | Yes — `pro enable usg` + official Ubuntu 22.04 STIG | No published profile |
| Ubuntu LTS support / ESM | Yes — Ubuntu Pro | n/a (Kairos builds on various bases) |
| JetPack 6.1 compatibility | Yes — Ubuntu-for-Tegra 22.04 is the JetPack 6 base | Works, but inherits no FIPS/STIG |

Kairos's advantages (immutable rootfs, atomic A/B upgrades, Kcrypt key sealing) are architecturally elegant but cannot be reconciled with the FIPS + STIG requirement. Since compliance is non-negotiable, Kairos goes.

## Ubuntu 22.04 vs 24.04 for the Orin

22.04 was chosen even though 24.04 is newer because:

- JetPack 6.1 is built on Ubuntu-for-Tegra 22.04 — 24.04 is not supported at the L4T level as of this decision.
- Ubuntu Pro FIPS 140-3 validation is more mature on 22.04 (certified earlier; broader module coverage).
- DISA STIG for Ubuntu 22.04 is published and mature; 24.04 STIG is newer.

The x86 NETHOST dev host currently runs Ubuntu 24.04 LTS. That may need to drop to 22.04 to match the Orin compliance posture — flagged as an open question in the migration plan.

## What Kairos Was Doing (and how we replace it)

| Kairos feature | What it was buying | Replacement under Ubuntu Pro 22.04 |
|---|---|---|
| Immutable read-only rootfs | Runtime tamper resistance; runtime integrity monitoring (`os-supply-chain-security.md` X12) | **IMA/EVM** (Integrity Measurement Architecture) + **AIDE** file-integrity baselines. Optional stretch: **dm-verity** on `/usr` only. |
| Atomic A/B upgrade slots (`goat-agent upgrade`) | Recoverable kernel/image upgrades; quick rollback | Stock `apt` + `unattended-upgrades` (security only) + **btrfs or ZFS root snapshots** for rollback. Pre-upgrade snapshot, rollback via GRUB menu entry or manual reflash. |
| Kcrypt LUKS sealing against Tegra SE | FDE key never leaves hardware root-of-trust | **LUKS with key custody in OP-TEE** on Orin — Tegra SE remains the crypto root-of-trust, accessed via an OP-TEE Trusted Application. Implementation is custom; feasibility on JetPack 6.1 is an open engineering item. x86 hosts: `clevis luks bind tpm2` against the onboard TPM 2.0. |
| Image-hash attestation at first boot (X11) | Detect tamper-in-transit; reject substituted hardware | **TPM PCR-based boot measurement** (TPM on x86, Tegra SE PKA/PCR equivalents on Orin) + a baseline of package hashes captured at provisioning; validator sidecar refuses to enroll if measurements diverge. |
| Reproducible Kairos builds (X15) | Raise bar from "trust upstream" to "verify upstream" | Ubuntu Pro + FIPS module binaries are Canonical-signed; GHCR image digest pinning (already planned under Chainguard-first supply chain) covers the container layer. Reproducibility of the OS image is traded for cryptographic provenance from Canonical and Chainguard. |
| Kcrypt key rotation runbook (X17) | Recover from key compromise without reflash | **LUKS key rotation via `cryptsetup luksChangeKey`** + OP-TEE-side rekey procedure (Orin) / `clevis` rebind (x86). Runbook to be authored — same shape as the Kcrypt runbook that was planned. |
| `/oem/` cloud-init as first-boot config path | Host-level config (mounts, sysctl, authorized_keys, hostname) applied once at boot | **Stock Ubuntu cloud-init** (netplan + user-data) baked into a first-boot USB/NVMe image or applied via SSH by Ansible. Greenfield — authored fresh for the new baseline; the original `ops/goat/oem/*.yaml` content was not migrated (directory deleted 2026-04-22). |

## Supply-chain implications

The existing supply-chain doc (`os-supply-chain-security.md`) assumed Kairos as the image trust root. With Kairos out:

- **Control X11** (first-boot attestation): repointed at TPM PCRs + package-hash baseline. Still required, still fail-closed.
- **Control X12** (read-only rootfs monitoring): becomes IMA/EVM + AIDE. Cheaper and simpler than Kairos's immutability; weaker in absolute terms but acceptable under the compliance stack.
- **Control X15** (reproducible Kairos builds): retired. Replaced implicitly by Canonical-signed Ubuntu Pro binaries + Chainguard-signed container images.
- **Control X17** (Kcrypt key rotation): generalized to "LUKS/TPM/OP-TEE key rotation" — same runbook shape, different key-custody mechanism.

`os-supply-chain-security.md` is updated in the same change as this decision.

## Threat-model implications

| Doc | What changes |
|---|---|
| `mgmt-server-security.md` threat #6 (Dex local-key compromise) | Defense layer changes from "Kcrypt-sealed rootfs" to "LUKS + OP-TEE / TPM key custody + IMA/EVM on the Dex service binary + secret mounted from a decrypted volume only at runtime." |
| `mgmt-server-security.md` M7 (setup-key hardware binding) | Device fingerprint drops "Kairos image hash" — replaced with "TPM EK / Orin Tegra SE fingerprint." |
| `pg-spock-security.md` threat #5 (Kcrypt-sealed-volume theft) | Renamed to "LUKS-sealed-volume theft." Defense pivots to OP-TEE / TPM key custody. Rest of the control chain unchanged. |
| `federation-security.md` §5 (adjacent surfaces) | "Kairos OS + supply chain" entry renamed to "Orin host OS + supply chain" and repointed at the Ubuntu Pro / OP-TEE trust chain. |
| `universal-ntp-hygiene.md` §cross-site attestation | "Read-only rootfs on Kairos" note replaced with "IMA/EVM on the ntp-health-agent binary + Ubuntu Pro signed packages." |

## Phase 2 → Phase 3 gate

`ha-multi-site.md` §14 previously made the gate "Kcrypt LUKS sealed against Tegra SE." Under the new plan, the gate is:

1. Ubuntu-for-Tegra 22.04 + Ubuntu Pro attached (FIPS + STIG enabled)
2. LUKS on `/opt/docker-data` with key custody via OP-TEE if feasible, or TPM-analogue fallback documented if not
3. IMA/EVM baseline captured; AIDE scheduled
4. First-boot attestation against TPM/Tegra-SE PCR baseline
5. Offline enrollment bundle validator green

## Migration work

Tracked as a Block in `../project/implementation-plan.md`. High-level:

1. **Re-image the pilot Orin** from Kairos → Ubuntu-for-Tegra 22.04 + Ubuntu Pro. Attach FIPS + STIG. Verify container stack still runs.
2. **Author stock Ubuntu cloud-init + Ansible roles** covering network, sysctl, mounts. Greenfield — the original `ops/goat/oem/*.yaml` content was not migrated (directory deleted 2026-04-22).
3. **Rewrite `docs/operations/pilot-bringup.md`** around the Ubuntu Pro bringup flow (remove `goat-agent`, `/oem/`, `goat@` references).
4. **Implement OP-TEE LUKS key custody** on Orin. This is the one genuinely new piece of engineering work. If it's not achievable on JetPack 6.1 in the available window, fall back to a documented weaker posture (passphrase + physical access control) and treat it as Phase 3 technical debt.
5. **Update threat-model docs** per the table above.
6. ~~Retire `ops/goat/`.~~ **Done 2026-04-22** — directory deleted outright; nothing archived.

## Open questions

- **x86 Ubuntu 22.04 vs 24.04.** The x86 hosts currently target 24.04. If FIPS + STIG applies equally to x86 production hosts, they should drop to 22.04 for parity with the Orin compliance stack. Decision pending verification of Ubuntu Pro FIPS + STIG status on 24.04.
- **OP-TEE TA development cost.** OP-TEE TAs on Tegra are a known-but-uncommon path. Spike needed before committing this as the primary LUKS key-custody mechanism. Alternative: YubiKey PIV slot + `systemd-cryptenroll --fido2-device`-style pattern adapted for the Orin's USB/GPIO realities.
- ~~`ops/goat/` disposition.~~ **Resolved 2026-04-22** — deleted.

## References

- `os-supply-chain-security.md` — updated X11/X12/X15/X17 controls
- `ha-multi-site.md` §14 — updated Phase 2→3 gate
- `mgmt-server-security.md` — updated threat #6, M7
- `pg-spock-security.md` — updated threat #5
- `federation-security.md` §5 — updated adjacent surface
- `fips-pqc-path.md` — updated Orin OS in deployment diagrams
- `../project/implementation-plan.md` — migration block
