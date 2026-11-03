# ADR 1118 — Kernelspace WireGuard on Jetson nodes via pinned out-of-tree modules

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-08-08 |
| Repo scope | Multi (goat monorepo + `goat-wireguard-kmod`) |
| Supersedes | — |
| Superseded by | — |
| Related | ADR 0826 (three-Orin topology), ADR 1117 (env doors), ADR 1109 (fleet management), ADR 0109 (git collaboration) |

## TL;DR (for a layperson)

Our Jetson computers run the mesh VPN in software because NVIDIA's Linux build
leaves out the fast in-kernel VPN driver. We now build that missing driver
ourselves, test it on a sandbox machine, and install it across the fleet — the
VPN then runs inside the kernel, which is substantially faster and lighter on
CPU. If the driver is ever missing or broken, the software fallback we run
today takes over automatically, so the worst case is what we already have.

## Context

Every Jetson in the fleet (AGX Orin and AGX Thor management hosts) runs netbird in userspace `wireguard-go` mode.
Probed live 2026-08-08: both tegra kernel lines (`5.15.148-tegra`, L4T 36.4.7;
`6.8.12-tegra`, L4T 38.4.0) have `CONFIG_WIREGUARD` **not set** — the module
was compiled out, along with its `lib/crypto` dependencies. No package
provides it; netbird's Linux interface factory detects the absence and falls
back to userspace TUN. This was an open task on `ha-multi-site.md` since
2026-04.

Userspace WireGuard costs real CPU and caps throughput on ARM (typically
2–4× slower than kernelspace), and leaves the Jetson estate divergent from the
x86 mini-PC nodes, whose stock Ubuntu kernels ship the module.

Both kernels have headers installed, `CONFIG_MODULE_SIG_FORCE` unset (unsigned
out-of-tree modules load, tainting the kernel), and `modules_disabled=0`.

## Decision

- We build `wireguard.ko` + the compiled-out `lib/crypto` dependency modules
  out-of-tree per kernel line, from upstream sources matching the running
  kernel, in the sibling repo **`goat-wireguard-kmod`** (artifact
  factory: build scripts, pinned artifacts, SHA256SUMS, mandatory provenance).
- The artifact set also includes `nft_ct.ko` + `nft_fib{,_ipv4,_ipv6}.ko`:
  in kernel mode netbird's ACL moves from the in-process userspace filter into
  kernel nftables, and the tegra kernels compile those nft expressions out
  too. The deploy role verifies `table ip netbird` exists post-cutover — a
  Connected node without its ACL table is an unfiltered node.
- **2026-08-09 scope extension:** the same factory + role now carry the rest
  of the tegra networking gap set — `tcp_bbr.ko` + `sch_fq.ko` (only
  reno/cubic on the images; BBR needed for satellite uplinks with measured
  packet reordering, F-301) and `xt_TCPMSS.ko` (netbird's MSS-clamp
  rules fail `--set-mss` unknown, leaving NETBIRD-RT-MSSCLAMP silently
  empty). Same mechanism, same pinning, same rollback. BBR as host DEFAULT
  congestion control is a separate role toggle (`wg_kmod_bbr_policy`,
  default off) gated on the F-301 A/B measurement; module install alone
  changes no traffic behavior.
- Artifacts are **exact-kernel-pinned** (vermagic == `uname -r`). A
  `nvidia-l4t-kernel` bump requires a rebuild there *before* the fleet kernel
  moves — a declared deviation owned by the kernel-bump runbook. If a kernel
  bumps without artifacts, nodes fall back to userspace (degraded, not down).
- Deploy is monorepo-owned: role `ops/ansible/roles/wireguard-kmod` +
  playbook `ops/ansible/wireguard-kmod.yml`, `serial: 1`,
  `any_errors_fatal`, verify-before-cutover ordering (modprobe + throwaway
  interface smoke BEFORE the netbird restart; a host that can't load the
  module is left untouched in userspace). Transport = env doors (ADR 1117),
  inventory `ops/ansible/inventory/doors.ini`.
- Validation ladder: sandbox live-fire on nova-a Thor (nova goatnet, left
  running kernelspace as the standing soak) → production cutover per goatnet
  via the playbook.
- Rollback: `NB_WG_KERNEL_DISABLED=true` in the netbird service environment
  (soft, per host), or `-e wg_kmod_state=absent` (full revert to today's
  userspace state).
- No netbird code change: the client auto-selects kernel mode when the module
  loads. Rosenpass PQC is unaffected (it manages the WG PSK, supported
  identically by kernel WireGuard).

## Alternatives considered

- **Stay on wireguard-go** — no work, but a permanent CPU/throughput tax on
  every Jetson and a standing x86/ARM parity gap. Rejected.
- **DKMS on-device rebuilds** — automates kernel bumps but puts a compiler +
  kernel-source dependency on every production node; with exactly two pinned
  kernel lines, prebuilt audited artifacts are simpler. Revisit if kernel
  diversity grows.
- **Vendor-fixed kernels (ask NVIDIA/Canonical for `CONFIG_WIREGUARD=m`)** —
  right long-term answer, not in our control loop; pursued as advocacy, not a
  plan.
- **Module signing now** — `MODULE_SIG_FORCE` is unset on both lines, so
  signing buys nothing today and adds key-custody work. Deferred until the
  STIG posture demands it (path: MOK enrollment in Jetson UEFI + signing in
  the build pipeline).

## Consequences

- Jetson mesh nodes move to kernelspace WireGuard; expected material
  throughput/CPU win (to be measured against the ADR 1045 envelopes).
- New standing coupling: kernel bumps on Jetsons are gated on a
  goat-wireguard-kmod rebuild. Failure mode of forgetting = silent fallback to
  userspace; `goat health` / node-net surfaces can observe the interface type.
- Kernel taints `O` (unsigned out-of-tree module) on Jetson nodes — recorded
  for the compliance narrative; FIPS posture unchanged (neither kernel nor
  userspace WireGuard tunnel crypto is FIPS-validated; the Rosenpass layer is
  orthogonal).
- Fleet view: `jetson-kernel-wireguard.md`.
