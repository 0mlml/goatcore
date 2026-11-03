# Goat-relay Tier-2 perf spike — AF_XDP + hardware crypto + L5+kTLS+IoBuf integration

| Field | Value |
|---|---|
| Status | Spike (not implementation) — added 2026-05-02 to plan the next perf-engineering quarter |
| Owner | goat core team |
| Scope | Three Tier-2 tracks above the M-018 baseline: AF_XDP kernel-bypass evaluation, hardware crypto offload alignment, L5 io_uring + kTLS + IoBuf-aware-buffers integration |
| Cross-refs | [ADR 0709](../adr/0709-block-33-relay-rewrite-architecture.md) (relay rewrite arch), [ADR 0711](../adr/0711-tokio-uring-migration-io-uring-sqpoll.md) (L5 lever), [ADR 0218](../adr/0218-relay-substrate-bare-metal-strategy.md) (substrate strategy), F-038 (L5 docker integration gap), docs/reports/2026-04-30-ztol-comparative-survey.md §"Gap decomposition" |

## Why a Tier-2 spike doc

Block 33C.1's Tier-1 levers (L1–L8 + H1–H4) close ~2–3× of the per-vCPU efficiency gap with hyper-derp on equivalent VM class. The remaining ~5–7× breaks down across three structural sources (per the comparative survey gap decomposition):

- **Source 1 residual**: missing io_uring features tokio-uring 0.5 doesn't expose cleanly (multishot recv, registered buffers / buffer rings, IORING_OP_PROVIDE_BUFFERS).
- **Source 2 residual**: per-frame `Bytes::from(Vec)` allocation pressure + the L5 shim's owned-vs-borrowed-buffer memcpy on every read/write.
- **CPU generation residual**: Cascade Lake DDR4-2933 vs Granite Rapids DDR5-5200 substrate-level bandwidth difference; addressable via instance class change (c7i / r8i) for production, not via code.

Tier-2 work attacks Sources 1 + 2 head-on with **three engineering tracks**, each gated on a feasibility spike before commit. None of these are tonight's bench scope — this doc plans them so they are tractable when picked up.

---

## Track 1 — AF_XDP kernel-bypass evaluation

### What it is

`AF_XDP` is a Linux kernel facility (4.18+) that lets userspace receive raw packets directly from a NIC's RX queue without going through the kernel's TCP stack. Combined with eBPF, you can attach an XDP program at the NIC driver to filter/redirect packets to userspace buffer rings before any of the kernel's normal protocol processing.

For a relay: instead of `accept()` → `read()` → kernel TCP+TLS → userspace, you'd do XDP redirect → AF_XDP socket → userspace ring buffer → handle TLS in userspace OR pass through to a separate kTLS userspace stack. The kernel TCP path is *gone*.

### Why we'd consider it

- **Eliminates the Source 2 residual entirely**: no kernel→userspace copy, no per-syscall overhead. NIC DMA's directly into pre-pinned userspace buffer pages.
- **Composes with shard-per-core**: each shard owns one NIC RX queue + one AF_XDP socket; perfect locality.
- **Hyper-derp's roadmap mentions it**: AF_XDP is the "next" step beyond io_uring in their lever stack; we'd be following the same trajectory.

### Why we wouldn't yet

- **Not available on hypervisor-virtualized NICs**: AWS / GCP VMs don't expose the NIC driver attach surface AF_XDP needs. SR-IOV with virtio-net helps but is hypervisor-feature-dependent. **Bare-metal substrate (per ADR 0218) is the prerequisite** — the lever has no value on AWS today.
- **Kernel-stack bypass means we re-implement TCP / TLS in userspace.** TLS 1.3 framing without rustls + ring is non-trivial; we'd need to fork rustls into a "raw frames" mode or build a custom AEAD-only path. Substantial scope.
- **Loss of FIPS pathway**: rustls + aws-lc-rs (FIPS 140-3 module) lives inside the kernel's TLS stack abstraction. Userspace TLS bypassing kernel framing complicates the FIPS validation story.

### Spike scope (when we get to it)

A 2–3 day eng spike that:

1. **Stands up an AF_XDP-capable bare-metal box** — Equinix Metal m3.small.x86 with i40e or mlx5 NIC driver. ~$25/day.
2. **Builds a minimal AF_XDP receiver in Rust** — `xsk-rs` crate is the in-tree binding; pre-pin a buffer pool, attach an XDP filter program that redirects port 33073 packets to the AF_XDP socket, count packets in userspace.
3. **Measures raw packet rate**: how many UDP packets/s can we push through pure AF_XDP without TLS, on the same load-generator harness as M-018? Sets the upper bound for what an AF_XDP relay path could deliver.
4. **Pencils the engineering effort** to convert the spike into a production data path: scope estimate for forking rustls or building a custom AEAD path; estimated weeks of work.
5. **Deliverable**: `docs/measurements/M-NNN-af-xdp-feasibility.md` with raw packet rate + spike conclusions; `docs/adr/0712-af-xdp-relay-data-plane.md` Proposed (Accept/Reject after the spike).

### Decision criteria

Adopt AF_XDP as the production data plane only if:

- Spike measures **≥ 2× the io_uring ceiling** on the same bare-metal box, AND
- The userspace TLS surface can be FIPS-attested (or we accept a non-FIPS bare-metal carve-out for the relay specifically), AND
- We have a bare-metal substrate decision (ADR 0218) committed — AF_XDP without bare-metal is moot.

---

## Track 2 — Hardware crypto offload alignment

### What it is

Modern Xeons have AES-NI + AVX-512 + VAES (vector AES) instruction paths that, when correctly aligned, deliver per-byte AEAD throughput multiple GB/s per core. ARM Cortex-A78 / Neoverse N2 have ARMv8 Crypto Extensions doing the same. Whether we hit those paths depends on the crypto provider:

- **rustls + ring**: ring uses AES-NI on x86_64 when `target_cpu` selects Haswell+. Default `target_cpu = generic` in `cargo build --release` does NOT enable the AES-NI path; the AEAD runs in software.
- **rustls + aws-lc-rs**: aws-lc-rs auto-detects CPU at runtime and switches to AES-NI / VAES paths. Faster than ring on modern x86, also FIPS 140-3 validated.

For the goat-relay, today we use ring (per ADR 0709 D8). Per-byte AEAD throughput is bottlenecked by software AES if the binary isn't compiled with `-C target-cpu=native` or similar.

### Spike scope

A 1-day eng spike:

1. **Audit current AEAD path**: build with `cargo build --release` default. Profile `perf record` during sustained load. Inspect symbols in the AEAD hot path — are we hitting `aes_x86_64_asm` (asm fast path) or `aes_block` (software fallback)? Confirm whether AES-NI is being used.
2. **Build with `-C target-cpu=x86-64-v3`** (Haswell baseline + AES-NI guaranteed). Re-profile. Measure throughput delta.
3. **Migrate to aws-lc-rs**: replace `rustls = { features = ["ring"] }` with `rustls = { features = ["aws-lc-rs"] }`. Verify FIPS 140-3 path lights up. Re-profile. Measure delta.
4. **Pencil the FIPS migration scope**: aws-lc-rs's FIPS module needs validation surface — does our supply-chain audit cover it? Block 33D-class scope.
5. **Deliverable**: `docs/measurements/M-NNN-aead-cpu-alignment.md`; potentially flips ADR 0709 D8's "ring" choice to "aws-lc-rs" depending on numbers.

### Decision criteria

Switch from ring to aws-lc-rs in production iff:

- Bench measures **≥ 5% throughput lift** at sustained Gbps load (matches the Source 2 residual reservation), AND
- aws-lc-rs's FIPS validation pathway is operationally accessible (no hidden licensing / build-toolchain blockers), AND
- Build-time / binary-size / dependency-tree impact is within budget.

Otherwise stay on ring + add `-C target-cpu=x86-64-v3` to release builds; the AES-NI path lights up regardless.

---

## Track 3 — L5 io_uring + kTLS + IoBuf-aware buffers full integration

### What it is

The Block 33C.1 L5 lever (per [ADR 0711](../adr/0711-tokio-uring-migration-io-uring-sqpoll.md)) lands tokio-uring's runtime. The current shim ([`goat-relay/src/listener/uring_compat.rs`](../../goat-relay/src/listener/uring_compat.rs)) bridges io_uring's owned-buffer model to tokio's borrowed-buffer model so rustls + tokio-tungstenite can drive the stream unchanged. **This shim adds a per-read/write 32 KiB memcpy** — Source 2 in the gap decomposition.

True L5 value materializes when the entire data path operates on owned buffers, eliminating the shim. Combined with kTLS (so the kernel handles AEAD inline at the FD level) and io_uring's `IORING_OP_PROVIDE_BUFFERS` for kernel-managed buffer pools, the bytes flow NIC → kernel ring → userspace pointer with **zero copies** end-to-end on the data path.

This is what hyper-derp does. To match it, we need three integrations to land together:

1. **Owned-buffer rustls** — either patch upstream rustls to expose an owned-buffer API, or build a "raw frames" rustls wrapper that consumes pre-allocated buffers. ~weeks.
2. **Owned-buffer tokio-tungstenite** — same shape: a tungstenite variant that operates on owned buffers throughout the WS frame parser. ~weeks.
3. **kTLS hand-off on tokio-uring sockets** — currently the ktls crate expects a tokio TcpStream's fd. Need either (a) patch ktls for tokio-uring's TcpStream type, or (b) extract the fd from tokio-uring + custom kTLS setsockopt path bypassing the ktls crate. ~1 week.

Plus the F-038 docker-runtime integration: the L5 path produces a dual-process bind-conflict in chainguard glibc-dynamic when seccomp=unconfined is set. Either resolve the dual-process pattern or ship L5 only to the systemd-native production deploy path (which doesn't go through Docker).

### Spike scope (when we get to it)

A 5–10 day eng spike split across:

1. **F-038 docker integration resolution**: try `user: "0:0"`, alternate runtime images, podman vs docker. Identify the working shape OR confirm L5 is systemd-native-only.
2. **Custom kTLS-on-tokio-uring path**: extract fd from tokio_uring::net::TcpStream, manually call setsockopt(SOL_TLS, TLS_TX/RX) after the rustls handshake completes. Bench this with M-NNN measurement.
3. **Owned-buffer rustls wrapper**: build a thin layer around rustls that exposes `process_owned_buffer(buf: Vec<u8>) -> Result<Vec<u8>, _>` instead of the borrowed-buffer trait. Bench the path.
4. **Owned-buffer tungstenite wrapper**: similarly. (Or skip WS frame parsing if all production peers are on QUIC per F-016.)
5. **End-to-end bench**: M-NNN measurement of L5 + kTLS + IoBuf-aware path on the same M-018 lever stack baseline. Expected: ~3–5× lift over M-018 (closing the bulk of Source 1 + Source 2).
6. **Deliverable**: ADR 0713 (Block 33C.2 IoBuf-aware data path), several measurements, supersedes ADR 0711's "shim is fine" stance.

### Decision criteria

Land Block 33C.2 IoBuf integration iff:

- All three component spikes (F-038 resolution OR systemd-native deploy verified, custom kTLS-on-uring, owned-buffer rustls wrapper) reach passing-tier independently, AND
- End-to-end bench measures **≥ 2× lift over M-018 + L5-shim baseline** (i.e., the io_uring-without-shim path delivers materially more than the io_uring-with-shim M-019 verdict, when M-019 lands), AND
- Engineering effort to reach production-ready is bounded at < 4 engineer-weeks for the full integration.

---

## Sequencing and dependencies

```
                 ┌──────────────────────────────────┐
                 │  Tier-1 (LANDED in M-014..M-018) │
                 │  L1-L8 + H1-H4 + Tier-1 sysctl   │
                 └──────────────────┬───────────────┘
                                    │
              ┌─────────────────────┼─────────────────────┐
              │                     │                     │
              ▼                     ▼                     ▼
     Track 2 Crypto          Track 3 IoBuf+kTLS    Track 1 AF_XDP
     (1 day spike)            (5-10 day spike)     (2-3 day spike)
     INDEPENDENT              GATED on F-038       GATED on bare-metal
     of substrate             docker-resolution    (ADR 0218 commit)
                              OR systemd-native
              │                     │                     │
              └─────────────────────┴─────────────────────┘
                                    │
                                    ▼
                         Cumulative bench
                         (~5-7× over M-018)
```

**Order of operations** (when we have engineering capacity):

1. **Track 2 first** — cheap, fast, may flip ADR 0709 D8 from ring to aws-lc-rs. No dependencies. Can land in week 1.
2. **Track 3 second** — bigger scope; requires F-038 resolution or a systemd-native production target. Multi-week.
3. **Track 1 last** — only meaningful on bare-metal substrate; gates on ADR 0218's bare-metal track being committed.

## What this spike doc does NOT do

- **Doesn't commit any of the three tracks** — they're each gated on their own feasibility spike.
- **Doesn't replace the M-NNN measurements** — each track will produce its own measurement file when picked up.
- **Doesn't override the M-018 + tonight's M-022 baseline** — those remain the production-ready evidence chain. Tier-2 is the path to *more*.
- **Doesn't claim parity with hyper-derp** — see comparative survey gap decomposition. Even with all three tracks landed on bare-metal substrate, we hold residual structural gaps from wire-protocol choice (FIPS-able TLS vs DERP-noise — deliberate trade) and CPU generation (fix via instance class swap, not code).

## Cross-refs

- ADR [`0709`](../adr/0709-block-33-relay-rewrite-architecture.md) — Block 33 architecture (D7-D13)
- ADR [`0711`](../adr/0711-tokio-uring-migration-io-uring-sqpoll.md) — L5 lever (the foundation Track 3 builds on)
- ADR [`0218`](../adr/0218-relay-substrate-bare-metal-strategy.md) — substrate strategy (Track 1 prerequisite)
- F-038 — L5 docker integration gap (Track 3 prerequisite)
- comparative survey §"Gap decomposition" — the four-source decomposition this doc plans to close
- M-018 (commit 4cd5812) — Tier-1 baseline these tracks lift from
- Block 33C.2 (in `docs/project/implementation-plan.md`) — the implementation tracker for these tracks once committed
