# thor-flash — operator runbook (NVIDIA AGX Thor Developer Kit)

> **Variant A validated end-to-end 2026-07-14** (flashed from an
> Ubuntu 26.04 host, driven entirely over SSH):
> flash md5-verified on QSPI + NVMe; after the one physical
> power-cycle the board booted Ubuntu 24.04 / L4T R38.4 (GCID
> 43443517) from NVMe with the exact Variant A partition shape
> (951.2 GB APP on `nvme0n1p1` + 10 BSP partitions), DHCP'd, and
> accepted key SSH as the preseeded user. **Variant C (LUKS via
> repartition) REMAINS UNVALIDATED** — the original draft pointed
> `-p "-c bootloader/generic/cfg/flash_t264_qspi_sd.xml"` at a file
> that does not exist; the correct partition XML is
> `tools/kernel_flash/flash_l4t_t264_nvme.xml` per the Thor config
> file's `EXTERNAL_PT_LAYOUT` setting (verified 2026-05-18).
> Do not run Variant C commands verbatim until validated. **Variant D
> (Variant A + post-boot LUKS via USB live boot)** is the canonical
> LUKS path given the Variant C uncertainty — see §"Step 4D" below.
>
> **Flash host newer than Ubuntu 24.04?** Read
> [§ Flash host on Ubuntu 26.04+](#flash-host-on-ubuntu-2604-five-required-workarounds)
> before starting — five host-side workarounds are required or the flash
> fails serially, one stage at a time.

> Self-contained procedure for flashing **Ubuntu 24.04 + JetPack 7 / L4T R38.4**
> onto an NVIDIA AGX Thor Developer Kit from an x86 Linux host on the LAN,
> with an optional **carved LUKS partition** for `/opt/docker-data`. This is
> Step 0 of `nova-bringup.md` — that runbook
> assumes the Thor is already flashed when it arrives at Step 6.
>
> First codified during the **nova-twin** standup 2026-05-17.

## What you end up with

- **OS:** Ubuntu 24.04.3 LTS for Tegra under JetPack 7 / **L4T R38 revision 4.0**
  (Tegra kernel 6.8.12, GCID 43443517, NVIDIA Dec 2025 build).
- **Storage shape (default):** entire `/dev/nvme0n1` consumed by
  the OS — 951 GB rootfs on `nvme0n1p1` plus 10 BSP system partitions for the
  recovery + A/B boot chain. **No LUKS.** `/opt/docker-data` is a plain
  directory on rootfs (per the F-093
  three-tier-storage invariant — `data-root: /opt/docker-data/docker`).
- **Storage shape (LUKS variant — nova-twin):** APP partition capped at
  100 GiB at flash time via `l4t_initrd_flash.sh -S 100GiB`, leaving the
  remaining ~850 GB unallocated. Post-boot: `parted` carves
  `/dev/nvme0n1pN` (next free index after BSP partitions), `cryptsetup
  luksFormat`s it, and `mgmt-host-luks.yml` Ansible role mounts the
  unlocked volume at `/opt/docker-data`. Docker `data-root` lands on the
  LUKSed partition, satisfying F-093 AND giving real disk-level
  encryption-at-rest.
- **Default user:** the operator account with SSH key pre-installed
  + sudo (NOPASSWD limited to bringup commands per
  [`mgmt-host-baseline.yml`](../../ops/ansible/mgmt-host-baseline.yml) Day-1
  posture).

## Pre-flight

### Hardware

- [ ] **Thor**: NVIDIA AGX Thor Developer Kit. Board P4070 = CVM P3834
  + CVB P4071. Powered, on operator's LAN via Ethernet (for post-flash
  reachability), with the included 1 TB NVMe in the M.2 Key M slot.
- [ ] **Flash host**: x86_64 Linux machine with USB-C cable to the
  **device-mode / "Recovery" USB-C port** on the Thor (the other USB-C
  ports are host-mode and will NOT appear on the flash host's `lsusb`).
  Minimum ≥80 GB free in `$HOME` for the BSP workspace + intermediate
  flash images. Ubuntu 22.04 / 24.04 or Linux Mint 22+ tested; Ubuntu
  26.04 works but needs the five workarounds in
  [§ Flash host on Ubuntu 26.04+](#flash-host-on-ubuntu-2604-five-required-workarounds).
- [ ] **No Mac in the flash path.** The Mac admin can SSH-drive the flash
  host but cannot directly flash the Thor (NVIDIA tooling is Linux-only
  due to `usbboot` kernel-module + `udev` rules).

### Flash host workspace (one-time setup)

```bash
# Workspace lives at ~/jetson-flash/Linux_for_Tegra
# If absent on a fresh flash host, download the L4T R38.4 BSP + sample rootfs.
# The direct CDN URLs work WITHOUT the NVIDIA developer login (verified
# 2026-07-13; sizes 1.02 GiB / 1.78 GiB — verify against Content-Length):
#   https://developer.nvidia.com/downloads/embedded/l4t/r38_release_v4.0/release/Jetson_Linux_r38.4.0_aarch64.tbz2
#   https://developer.nvidia.com/downloads/embedded/l4t/r38_release_v4.0/release/Tegra_Linux_Sample-Root-Filesystem_r38.4.0_aarch64.tbz2
# Untar the BSP, then the rootfs INTO Linux_for_Tegra/rootfs/ (sudo tar xpf),
# then apply the NVIDIA proprietary binaries to the rootfs:
#   cd ~/jetson-flash/Linux_for_Tegra
#   sudo ./tools/l4t_flash_prerequisites.sh           # installs apt deps
#   sudo ./apply_binaries.sh                           # bakes NV bins into rootfs
# Headless target? Preseed the default user so first boot needs no console:
#   sudo ./tools/l4t_create_default_user.sh -u <user> -p "$PASS" -n <hostname> --accept-license
#   (bypasses oem-config; then seed rootfs/home/<user>/.ssh/authorized_keys,
#   chown to uid/gid from rootfs/etc/passwd, chmod 700/600)
```

Verification (sanity-check the workspace before flashing):

```bash
cd ~/jetson-flash/Linux_for_Tegra
ls jetson-agx-thor-devkit.conf nvautoflash.sh tools/kernel_flash/l4t_initrd_flash.sh
# All three must exist.
head -1 nv_tegra/bsp_version
# Expect: # SPDX-FileCopyrightText: Copyright (c) 2019 NVIDIA CORPORATION ...
grep '^# R[0-9]' rootfs/etc/nv_tegra_release | head -1
# Expect: # R38 (release), REVISION: 4.0, GCID: 43443517, BOARD: generic, EABI: aarch64, DATE: ...
grep '^VERSION_ID' rootfs/etc/os-release
# Expect: VERSION_ID="24.04"
```

### Network

- [ ] Thor's Ethernet port reachable from the flash host's LAN (post-flash
  the Thor will DHCP up; flash host needs `ping`/`ssh` to it for first-boot
  ACL probe).
- [ ] Operator has a target hostname planned (nova-twin convention:
  `mgmt-nova-twin-01-thor`).

## Step 1 — Recovery mode entry

The Thor must be in **Force Recovery (APX) mode** for `lsusb` on the
flash host to see it. Procedure:

1. Disconnect Thor power.
2. Connect USB-C cable: Thor's device-mode/Recovery port ↔ flash host.
3. Press and **HOLD** the **Force Recovery (REC)** button on the Thor
   front panel.
4. Apply power (press power button) while still holding REC.
5. Continue holding REC for ~2 seconds after power-on, then release.

Verify on the flash host:

```bash
lsusb | grep -i nvidia
# Expect (nova-twin 2026-05-18 captured):
#   Bus 001 Device NNN: ID 0955:7026 NVidia Corp. APX
# Product ID 7026 = AGX Thor (`hwchipid=0x26` = Tegra264). The earlier
# draft of this doc guessed 7023 (Orin); 7026 is the validated Thor value.
```

If no NVIDIA device appears: see [Troubleshooting](#troubleshooting) below.

> **Gotcha — `nvautoflash.sh --help` is NOT a real flag.** The script
> ignores unknown args and proceeds with its full detection flow (which
> includes an RCM-boot to dump EEPROM). Don't "test" it with `--help`;
> read the script source or this doc instead. Also: the script's
> detection step requires **sudo** (it accesses the raw USB device); run
> as `sudo ./nvautoflash.sh` from the start.

## Step 2 — Choose the storage variant

| Variant | Use when | Flash command shape | LUKS landing |
|---|---|---|---|
| **A — default (no LUKS)** | Quick deploy, no encryption-at-rest requirement, full 951 GB rootfs | `sudo ./nvautoflash.sh` (auto-detect) | none — `/opt/docker-data` is plain rootfs dir |
| **C — repartition for LUKS (nova-twin)** | Encryption-at-rest required, want LUKS on `/opt/docker-data` without adding hardware | `sudo ./tools/kernel_flash/l4t_initrd_flash.sh --external-device nvme0n1 -S 100GiB ...` | post-boot `parted` + `cryptsetup` on the unallocated tail of `/dev/nvme0n1` |

> **Variant B — second M.2 NVMe** (mirrors Orin shape) is documented at
> [commit ba6912a6](#) but not codified here yet. Use if you have a Thor
> with a second M.2 Key M slot and a spare NVMe; the existing
> [`mgmt-host-luks.yml`](../../ops/ansible/mgmt-host-luks.yml) role works
> untouched against `/dev/nvme1n1`. Skip the storage-shape changes below.

## Step 3 — Flash (Variant A — default)

```bash
cd ~/jetson-flash/Linux_for_Tegra
sudo ./nvautoflash.sh
# Auto-detects the connected Thor (P4070 = jetson-agx-thor-devkit),
# selects jetson-agx-thor-devkit.conf, flashes both internal storage
# (QSPI, recovery, etc.) and external NVMe with the default partition
# layout (951 GB APP on /dev/nvme0n1p1 + 10 BSP partitions).
# Expected duration: 20-40 minutes depending on USB-C link rate.
```

Watch for the final `*** The target ... has been flashed successfully ***`
line. After that, **disconnect USB-C, power-cycle the Thor**, and proceed
to Step 5 (post-flash verifications).

## Step 3 — Flash (Variant C — LUKS via repartition) — UNVALIDATED

```bash
cd ~/jetson-flash/Linux_for_Tegra
sudo ./tools/kernel_flash/l4t_initrd_flash.sh \
  --external-device nvme0n1 \
  -S 100GiB \
  -c tools/kernel_flash/flash_l4t_t264_nvme.xml \
  --network usb0 \
  jetson-agx-thor-devkit \
  external
# -S 100GiB caps the APP partition (rootfs) at 100 GiB. The XML path
# tools/kernel_flash/flash_l4t_t264_nvme.xml matches the Thor config
# file's EXTERNAL_PT_LAYOUT setting (verified 2026-05-18; the
# earlier draft pointed at flash_t264_qspi_sd.xml which does not exist).
# Expected duration: 25-45 minutes.
```

> ⚠️ **Variant C is UNVALIDATED.** During nova-twin first flash 2026-05-18
> the operator pivoted to **Variant D** (Variant A + post-boot LUKS via
> USB live boot — see Step 4D below) because:
> - The combination of `-S 100GiB` + `-c <external-xml>` + `external`
>   rootdev is documented in `l4t_initrd_flash.sh --help` but the
>   semantics together aren't documented anywhere we could find. Risk:
>   ~30 min wasted on a failed flash.
> - The `EXTERNAL_PT_LAYOUT` XML uses placeholder tokens (`EXT_NUM_SECTORS`,
>   `APP_SIZE`, etc.) that get substituted at flash-time; whether `-S`
>   correctly substitutes into the APP partition entry when the XML is
>   passed via `-c` rather than the default conf-file derivation is
>   unverified.
> - **`nvautoflash.sh`** (Variant A) is heavily-validated by NVIDIA. Lower-risk path.
>
> Validate Variant C in a follow-on session (separate Thor or post-flash
> re-flash of nova-twin) before using it as the documented LUKS path.

## Step 4 — Carve + LUKS the unallocated NVMe tail (Variant C only)

Boot the Thor for the first time (USB-C disconnected, power-cycle).
First-boot oem-config will prompt for username/password — set
**username `<user>`** (matches the bringup runbook's `NOVA_TWIN_A_USER`
default). Then SSH in over Ethernet:

```bash
# On the flash host:
ssh <user>@<thor-dhcp-ip>

# On the Thor:
sudo parted /dev/nvme0n1 print free
# Inspect the partition table. The unallocated tail should be ~850 GB.
# Identify the next free partition number (likely p12 after the 10 BSP
# partitions + p1 APP).

sudo parted -s /dev/nvme0n1 mkpart docker-data ext4 100GiB 100%
sudo partprobe /dev/nvme0n1
# Verify:
lsblk /dev/nvme0n1
# Expect /dev/nvme0n1pN with the right size.

sudo cryptsetup luksFormat /dev/nvme0n1pN --type luks2
# Enter passphrase: $NOVA_TWIN_A_LUKS_PASS (from .envrc.local.d/20-host-access.envrc)

sudo cryptsetup open /dev/nvme0n1pN docker-data
sudo mkfs.ext4 -L docker-data /dev/mapper/docker-data
sudo mkdir -p /opt/docker-data
sudo mount /dev/mapper/docker-data /opt/docker-data
```

Then hand off to Ansible — `mgmt-host-luks.yml` with
`luks_volumes_mgmt_host: [{device: /dev/nvme0n1pN, name: docker-data,
mount: /opt/docker-data}]` makes the unlock + mount durable across
reboots (writes `/etc/crypttab` + `/etc/fstab`).

## Step 4D — LUKS via post-boot USB live-boot offline shrink (Variant D, canonical nova-twin LUKS path)

> Used when you flashed Variant A (no LUKS, full-NVMe rootfs) and want to
> retroactively engage LUKS for `/opt/docker-data` without re-flashing.
> Lower-risk than Variant C because the flash itself uses the validated
> `nvautoflash.sh` path; LUKS comes from a separate offline-shrink ceremony.

**Why offline:** ext4 supports online GROW but not online SHRINK. To
free up NVMe tail space for the LUKS partition, the rootfs must be
unmounted — which means booting from something other than the rootfs.
USB live-boot is the simplest path.

```bash
# Pre-req: Ubuntu 24.04 Server amd64 live ISO on a USB stick.
#   (Thor is arm64 but the live-USB tooling we need is mostly arch-agnostic
#    — confirmed parted + cryptsetup + resize2fs all present on
#    Ubuntu-for-Tegra live media; alternative is "Jetson Linux Boot
#    Recovery" image NVIDIA ships for this purpose.)

# 1. Boot Thor from USB live, drop to a shell.
# 2. Identify rootfs partition (the big one, ~951 GB):
sudo lsblk /dev/nvme0n1
# Expect /dev/nvme0n1p1 = APP, 951 GB (default flash layout)

# 3. Check + shrink the rootfs filesystem (NEVER skip the e2fsck step):
sudo e2fsck -f /dev/nvme0n1p1
sudo resize2fs /dev/nvme0n1p1 100G

# 4. Shrink the partition itself:
sudo parted /dev/nvme0n1
# In the parted REPL:
#   (parted) unit GiB
#   (parted) print                # confirm APP at index 1 starts at ~X GiB and ends at ~951 GiB
#   (parted) resizepart 1 <APP_START + 100>GiB    # e.g. 100GiB end if APP starts at 0
#   (parted) print free            # confirm ~850 GiB unallocated tail
#   (parted) mkpart docker-data ext4 100GiB 100%
#   (parted) print                # confirm new partition (likely index 12+ after the BSP partitions; nova-twin captures the actual number 2026-05-18)
#   (parted) quit
sudo partprobe /dev/nvme0n1

# 5. LUKS-format the new partition:
sudo cryptsetup luksFormat /dev/nvme0n1pN --type luks2 --pbkdf argon2id
# Enter passphrase: $NOVA_TWIN_A_LUKS_PASS (from .envrc.local.d/20-host-access.envrc)
sudo cryptsetup open /dev/nvme0n1pN docker-data
sudo mkfs.ext4 -L docker-data /dev/mapper/docker-data
sudo cryptsetup close docker-data

# 6. Power off USB live, remove USB, boot Thor back into the now-shrunk rootfs.

# 7. Once SSH-able from the flash host, hand off to Ansible:
#    Add to ops/ansible/inventory/group_vars/nova_twin_hosts/vars.yml (queued track):
#       luks_volumes_mgmt_host:
#         - device: /dev/nvme0n1pN
#           name: docker-data
#           mount: /opt/docker-data
#    Then run ops/ansible/mgmt-host-luks.yml — writes /etc/crypttab +
#    /etc/fstab so the unlock + mount survives reboots.
```

## Step 5 — Post-flash verifications (Variants A + C + D)

Required before declaring the flash complete and proceeding to
`nova-bringup.md` Step 6b/6c:

```bash
# On the Thor:
uname -a
# Expect: Linux ... 6.8.12-tegra ... aarch64 ...
cat /etc/nv_tegra_release | head -1
# Expect: # R38 (release), REVISION: 4.0, ...
cat /etc/os-release | grep VERSION_ID
# Expect: VERSION_ID="24.04"
ip a
# Note the DHCP'd Ethernet IP — write into the nova-twin inventory.

# Block 82.D2 followup verifications (capture into evidence):
lsmod | grep -i ether
# Capture the NIC kmod name (likely 'nvethernet' or 'tegra_eqos'); pass
# into the dropbear-initramfs role's NIC kmod parameter.
sudo ubuntu-advantage status 2>/dev/null || echo "Ubuntu Pro not yet attached"
# After attaching Ubuntu Pro + enabling FIPS, verify the Tegra kernel is
# included in the FIPS validation (sub-block 82.D2 ⚠ ubuntu-pro-fips).
```

## Flash host on Ubuntu 26.04+ (five required workarounds)

Live-fired 2026-07-13 from an Ubuntu
26.04 flash host. The L4T R38.4 tooling assumes ≤24.04; on 26.04 the flash fails
serially — each break surfaces only after the previous one is fixed, so
apply **all five up front**. Fixes 4+5 are NVIDIA-code bugs that 26.04
merely exposes (empty tool-path + nested-sudo `secure_path`); 1–3 are
genuine 26.04 platform changes.

1. **`qemu-user-static` is a virtual package** (provided by
   `qemu-user-binfmt`). `l4t_flash_prerequisites.sh` hard-fails on it.
   Install the package list manually, substituting:
   `sudo apt-get install -y qemu-user-binfmt <rest of the script's list>`.
   The dynamic qemu is chroot-safe because 26.04 registers binfmt with
   the `F` (fix-binary) flag — verify `grep flags
   /proc/sys/fs/binfmt_misc/qemu-aarch64` shows `F` before proceeding.
2. **NVIDIA scripts hardcode the name/path `qemu-aarch64-static`**
   (`apply_binaries.sh` → `nv-apply-debs.sh`, and
   `l4t_create_default_user.sh` checks `/usr/bin/qemu-aarch64-static`
   literally). Shim it:
   `sudo ln -sf /usr/bin/qemu-aarch64 /usr/bin/qemu-aarch64-static`
   (plus a copy at `~/jetson-flash/qemu-aarch64-static` satisfies
   `nv-apply-debs.sh`'s first lookup).
3. **OpenSSH 10 removed DSA keys.** The recovery-ramdisk builder
   (`tools/ota_tools/version_upgrade/ota_make_recovery_img_dtb.sh`)
   runs `ssh-keygen -t dsa` with output discarded → bare
   `command is failed` at "Re-generating recovery ramdisk". The
   generated sshd config never references a DSA host key, so delete the
   line: `sudo sed -i '/ssh-keygen -t dsa/d' <that script>`.
4. **`TEGRARCM -- Unable to execute command` ~0.1 s into the burn
   step.** The unified-flash python invokes `tegrarcm_v2` (and friends)
   by bare name; the pipeline re-enters `sudo` internally
   (`l4t_initrd_flash_internal.sh` runs `sudo python3
   flash_bsp_images.py`), and sudoers `secure_path` resets PATH there —
   outer PATH exports never arrive. Fix: symlink the **x86** flash
   tools into `/usr/local/bin` (in `secure_path`):
   `for t in tegrarcm_v2 tegraparser_v2 tegrahost_v2 tegrabct_v2
   tegraopenssl nvbchvalidate nvdtoverlay nvimagegen nvimagesign nvpt
   nvresign nvsimgdump nvskuinfo adb; do sudo ln -sf
   ~/jetson-flash/Linux_for_Tegra/unified_flash/tools/flashtools/flash/$t
   /usr/local/bin/$t; done`.
   **Do NOT put that directory on PATH instead** — it also holds
   *aarch64* copies of `losetup`/`resize2fs`/`e2fsck` (target-side
   payloads); if they shadow the host binaries, `Making system.img`
   dies with `qemu-aarch64: Could not open '/lib/ld-linux-aarch64.so.1'`.
5. **`nvdd does not exist` during the burn step.** Same root cause,
   file-op flavor: `flash_utilities.py`'s constructor never runs in the
   l4t code path, so tool *file references* stay bare class defaults
   resolved against a CWD that moves between regenerated dirs. Append a
   module-level rewrite to BOTH copies of `flash_utilities.py`
   (`unified_flash/tools/flashtools/bootburn_t264_py/` and
   `unified_flash/out/bsp_images/tools/flashtools/bootburn_t264_py/`)
   that rewrites every bare `f_*` class attribute to an absolute path
   into the sibling `flash/` dir when the file exists there. The exact
   snippet used (idempotence-guarded by its trailing marker):

   ```python
   # --- Ubuntu 26.04 host workaround (goat thor-flash, 2026-07-13) ---
   import os as _os
   _flashdir = _os.path.normpath(_os.path.join(_os.path.dirname(_os.path.abspath(__file__)), "..", "flash"))
   for _n in [a for a in dir(flash_utilities) if a.startswith("f_")]:
       _v = getattr(flash_utilities, _n)
       if isinstance(_v, str) and "/" not in _v:
           _c = _os.path.join(_flashdir, _v)
           if _os.path.exists(_c):
               setattr(flash_utilities, _n, _c)
       elif isinstance(_v, list):
           setattr(flash_utilities, _n, [
               _os.path.join(_flashdir, _x) if ("/" not in _x and _os.path.exists(_os.path.join(_flashdir, _x))) else _x
               for _x in _v
           ])
   # --- end workaround ---
   ```

Misc: `tar -tjf` integrity checks fail with `lbzip2: Cannot exec` until
`sudo apt-get install lbzip2` (26.04 tar prefers it); 26.04's Rust
coreutils `tail` rejects `tail -20` — use `tail -n 20` in any driving
scripts.

## Headless / remote-hands recovery (no finger on the REC button)

Validated 2026-07-13 driving the flash over SSH with no physical access
to the Thor:

- **A failed flash attempt does NOT strand the board.** With QSPI/NVMe
  unbootable, the bootrom falls back to USB recovery (APX) on every
  reset — the board re-enumerated as APX after four consecutive failed
  attempts with zero button presses.
- **Board stuck in the RCM-booted flash kernel** (enumerates as
  `0955:7100 NVIDIA Corp. Tegra Device`): it is an adb device.
  `adb reboot` fails in that minimal environment; **`adb shell "reboot
  -f"` works** and drops it back to APX (while unbootable).
- **After a SUCCESSFUL flash, a warm reset re-enters recovery anyway**
  (observed; consistent with the RCM reboot-reason latch surviving warm
  reset — a PMC-scratch-latch explanation is inference, not verified).
  `tegrarcm_v2 --reboot coldboot --chip 0x26` returns 0 but does not
  reset the board (no USB disconnect). **A physical power-cycle is
  required as the final step** — which is what this runbook's Step 3
  prescribed all along. Plan for one power pull at the end; everything
  before it is remote-driveable.

## Troubleshooting

### Thor not visible on `lsusb`

1. **Is Thor powered?** Front-panel light must be on. Recovery mode requires power.
2. **Right USB-C port?** Dev kit has multiple USB-C; only the device-mode
   "Recovery" port appears on host `lsusb`. Check silkscreen labels.
3. **REC button held during power-on?** Pressing it AFTER power-on does
   nothing. Power off, hold REC, apply power, release REC after 2 seconds.
4. **Cable rated for data?** USB-C charge-only cables won't enumerate.
   Use a known-good USB-C data cable (e.g. the one that shipped with the kit).
5. **Flash host's USB controller?** Try a different USB-C port on the host.

### Flash fails mid-way

`nvautoflash.sh` and `l4t_initrd_flash.sh` are mostly idempotent. After
a failure: power-cycle the Thor back into recovery mode, re-run the same
command. If the failure is at the partition-layout step, the on-NVMe
state may be inconsistent — re-flash from scratch (the BSP repartitions
the whole device).

## Followups

- **Block 82.D2 NIC kmod name on Thor** — CAPTURED 2026-07-14: the RJ45 5GbE (`enP2p1s0`, the port you actually use)
  is **`r8126`** (Realtek, v10.016.00-NAPI-PTP-RSS); `nvethernet`
  drives the four MGBE ports (all down on a devkit). Dropbear-initramfs
  NIC kmod parameter for a Thor on the RJ45 = `r8126`. Remaining:
  propagate into `docs/design/nova-goatnet.md`
  Block 82.D2 audit row when that row is next touched.
- **Block 82.D2 ubuntu-pro-fips on Tegra kernel**: verify after enabling
  FIPS + USG; capture in nova-twin bringup evidence.
- **build-image-thor.sh** (Block 68 follow-on): the rapid-bringup path
  (`ops/bootstrap/build-image-orin.sh` + R2 mirror) does not yet support
  Thor. Authoring `build-image-thor.sh` is queued; this runbook is the
  manual fallback until then.
- **Variant B — second M.2 NVMe**: codify the procedure once a Thor with
  the expansion slot is in hand. Existing `mgmt-host-luks.yml` role works
  unmodified against `/dev/nvme1n1`.
- **Variant C validation**: still pending — would need either a
  second nova-twin-shaped Thor or a willingness to re-flash nova-twin to
  prove the `-S 100GiB` + `-c flash_l4t_t264_nvme.xml` combination
  works as expected. Until validated, Variant D is the canonical LUKS
  path.

## Real-time capture log

(omitted)
