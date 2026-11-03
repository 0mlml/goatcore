---
name: ADR 0712 — Production relay instance class — c7i.2xlarge for the 8-shard goat-relay architecture
description: Lock c7i.2xlarge as the production relay instance class for the new goat-relay tier (replacing t3.micro stock-netbird-relay via blue-green cutover). The 8-shard architecture from ADR 0709 D11 + L8 needs ≥8 vCPU to actually exercise the lever stack; t3.micro's 2 vCPU caps shard count at 2 of 8 and wastes CBPF / RSS / per-shard-routing capacity. Single-placement empirical evidence from M-040; placement-variance characterization is its own track.
status: Proposed
type: adr
supersedes: none
sub-series-tag: relay-engineering
---

# ADR 0712 — Production relay instance class — c7i.2xlarge for the 8-shard goat-relay architecture

| Field | Value |
|---|---|
| Status | Proposed (2026-05-04) — flips to Accepted on operator-approved blue-green deploy |
| Audiences | [`technical contributor`, `layperson sponsor`, `SCA-R`] |
| Sub-series | [`relay-engineering`] |
| Repo scope | `terraform/modules/goat-relay/`, `terraform/main.tf`, `terraform-prod/` (per-tree mirror at deploy time) |
| Cross-refs | [ADR 0709](0709-block-33-relay-rewrite-architecture.md) (8-shard architecture), [ADR 0218](0218-relay-substrate-bare-metal-strategy.md) (substrate strategy — VMs now, bare-metal at production-shape), [ADR 0830](0830-spike-tolerant-relay-capacity.md) (warm-stopped fleet), M-040 (empirical), F-040 (placement variance still open), F-042, F-045, `docs/design/relay-blue-green-cutover.md` |

## TL;DR (for a layperson)

The new Rust relay (`goat-relay`) is built around an 8-shard architecture
— eight independent worker pipelines that the kernel spreads incoming
connections across so that no single CPU core is a bottleneck. The
existing production relay VPS is `t3.micro`, which has 2 vCPU. Two
shards of eight is six shards left on the floor, plus the lever stack
(per-shard routing tables, NIC RSS, eBPF affinity steering) doesn't
have anything to steer onto.

We ship the new relay onto `c7i.2xlarge` (8 vCPU, 16 GiB, up to 12.5
Gbps NIC) so the architecture can actually exercise itself in
production. The cost goes from ~$7.50/month per relay (t3.micro) to
~$262/month per relay (c7i.2xlarge). Three production relays:
~$720/month delta. The blue-green cutover (legacy tier stays running
during overlap, then retires) plus rollback path are in
`docs/design/relay-blue-green-cutover.md`.

The choice is not ambiguous between class siblings. `c6i.2xlarge` is
roughly comparable but skip-generation Intel; `c5n.2xlarge` brings a
25 Gbps NIC at +~$90/mo (overkill at current envelope); `c6gn` (ARM
Graviton) is materially cheaper but needs arm64 image work that isn't
landed yet. We chose c7i for current-generation Intel with adequate
NIC headroom and a clear upgrade path inside the same family.

## Problem

[ADR 0709](0709-block-33-relay-rewrite-architecture.md) D11 amendment
2026-05-01 landed multi-shard with `SO_REUSEPORT`: the binary defaults
to `--shards <num cores>`, each shard pins to its own core, and the
kernel distributes accept events / UDP datagrams across shards by
source-tuple hash. Block 33C.1 lever L8 (CBPF affinity-aware steering)
is queued on top.

Today's production relay VPS class is `t3.micro` — 2 vCPU, burstable
CPU credits, 1 GiB RAM, "up to 5 Gbps" network. It was the right choice
for stock-netbird-relay (Go binary, single OS thread doing useful work,
GOMAXPROCS=1 tuned per M-006).
It is **structurally too small** for the 8-shard goat-relay
architecture in production:

- `--shards` defaults to `num_cpus::get()` — on t3.micro that's 2.
  The architecture compiles and runs, but six of the eight intended
  shards never exist.
- The compounding levers in [ADR 0709 D11](0709-block-33-relay-rewrite-architecture.md)
  + Block 33C.1 (NUMA-aware shard pinning, CBPF affinity steering,
  multi-shard `SO_REUSEPORT_CBPF`) all assume N≥core-count shards
  with cross-core distribution. On 2 vCPU there is one logical-core
  pair; the multi-shard discipline reduces to "two shards on
  hyperthread siblings" — no NUMA, no cross-core dispatch, no real
  test surface for the levers.
- T-class CPU credits are the wrong shape for sustained relay
  forwarding. A 16-flow iperf3 ramp burns the credit balance; once
  exhausted, the relay drops to baseline-credit (10% of vCPU) and
  the steady-state ceiling collapses. Bench cells sometimes mask this
  because credits start at full; production sees the tail.

M-040
captured the first post-F-042
ALPN-fix matrix on `c7i.2xlarge` in one AWS region — single placement,
four cells (docker × WS, docker × QUIC, systemd × WS, systemd × QUIC).
The docker × QUIC cell read **405 / 789 / 916 / 954 Mbps** at 1 / 4 /
8 / 16 parallel flows. Single-shape cell-to-cell variance ~5-10%. This
is the empirical anchor for the sizing decision.

F-040
remains active: characterizing AWS c7i placement variance across ≥3
fresh-placement runs is a separate track. Single-placement evidence is
adequate for this sizing ADR — the ~3-4× pre-fix WS-on-different-
placements spread was on a substantially under-provisioned shape; the
variance question is a within-c7i.2xlarge characterization that does
not change the shape choice. The cost model in
`goat-net-scaling.md` §"Cloud cost
model" inherits a separate provisional caveat that this ADR does not
resolve.

## Options considered

### Option A — `c7i.2xlarge` (CHOSEN)

8 vCPU (4 physical cores + hyperthreading), 16 GiB RAM, up to 12.5
Gbps NIC, 4th-gen Intel Xeon Scalable (Sapphire Rapids). On-demand
~$262/mo per relay (similar ±~10% across the production regions).

**Why it fits:**
- 8 vCPU lets `--shards 8` actually run with one shard per logical
  core, exercising the multi-shard lever stack as designed.
- 12.5 Gbps NIC is multiple times the M-040 single-placement ~954 Mbps
  16-flow ceiling — NIC is comfortably not the bottleneck.
- 4th-gen Intel + AVX-512 + AES-NI + Intel QAT-friendly silicon path
  (kTLS path benefits from per-record AEAD acceleration; rustls + ring
  use AES-NI when present per [ADR 0709 D8](0709-block-33-relay-rewrite-architecture.md)).
- Sustained-CPU class (no T-class credit cliff).
- Same on-demand class is generally available across all three
  current production regions and the planned hub region.

### Option B — `c6i.2xlarge`

Skip-generation 3rd-gen Intel (Ice Lake). Same 8 vCPU / 16 GiB / "up
to 12.5 Gbps" shape. ~$245/mo. Comparable for our workload — the
~$17/mo savings does not justify the skip-generation choice when
c7i is GA in all our regions. If c7i unavailable in a region, c6i is
the documented fallback.

### Option C — `c5n.2xlarge`

8 vCPU / 21 GiB / **25 Gbps NIC** (enhanced networking via Elastic
Network Adapter). ~$352/mo (+~$90 vs c7i). Overkill at current
envelope — M-040 ceiling is ~954 Mbps single-placement, two orders
of magnitude under the NIC. Reserved as a future bump if multi-pair
characterization shows NIC saturation on c7i.

### Option D — `c6gn` (ARM Graviton2 / Graviton3)

Materially cheaper (~$170/mo for c6gn.2xlarge) and Graviton's
per-byte efficiency story is real. **Blocked on arm64 build chain
work that isn't landed.** [ADR 0707](0707-rust-toolchain-pinning.md)
+ Chainguard image build at [`ops/goat-relay/Dockerfile`](../../ops/goat-relay/Dockerfile)
need an arm64 build matrix. AGX Orin (the on-prem mgmt-host platform)
is arm64 already, so the toolchain landing is on the path regardless;
this ADR does not gate on it. Re-evaluate at the same trigger as the
[ADR 0218](0218-relay-substrate-bare-metal-strategy.md) Phase-2
verdict.

### Option E — `t3.large` / `t3.xlarge`

Cheaper than c7i (~$60-120/mo) but burstable. Same credit-cliff
pathology as t3.micro under sustained throughput. Rejected.

### Option F — `m7i.2xlarge`

Same 8 vCPU / 32 GiB / up to 12.5 Gbps NIC as c7i.2xlarge with double
the memory at higher per-month cost (~$300/mo). Memory is not the
binding constraint — the relay's working set is bounded by routing
table + per-connection state, well under 4 GiB at the 600/3000/6000
peer envelope (ADR 0215).
Pure cost penalty.

## Decision

**Production relay instance class = `c7i.2xlarge`.**

Default in `terraform/modules/goat-relay/` `variables.tf`:
`instance_type = "c7i.2xlarge"`. Per-region fallback to `c6i.2xlarge`
allowed when c7i is unavailable; both pass the 8-vCPU floor and run
the same lever stack identically.

The legacy stock-netbird-relay tier on `t3.micro` is **not** in scope
of this ADR — that tier stays exactly where it is during the blue-green
cutover window described in `relay-blue-green-cutover.md`,
then retires once the goat-relay tier is operationally proven.

## Rationale — the 8-shard architecture forces the floor

[ADR 0709 D11 amendment 2026-05-01](0709-block-33-relay-rewrite-architecture.md)
shipped multi-shard `SO_REUSEPORT` with `--shards <n>` defaulting to
`num_cpus::get()`. Block 33C.1 lever L8 (CBPF) lifts that to
affinity-aware steering. Block 33C.1 lever L5 (io_uring SQPOLL +
multishot recv) per [ADR 0711](0711-tokio-uring-migration-io-uring-sqpoll.md)
benefits proportionally to per-shard CPU. The lever stack is
designed to compound across N≥8 shards.

The instance class is the platform on which this architecture either
gets exercised or doesn't. 2 vCPU caps `num_cpus::get()` at 2; the
binary runs (correctness-preserving), but the architecture becomes
inert. Six of the eight intended shards never exist. CBPF affinity
has no meaningful target distribution. NUMA-local pinning has no
NUMA topology to exploit. **The lever stack's value is structurally
near-zero on 2-vCPU shapes.**

That's the controlling argument. Cost is real (~$720/mo for three
relays) but bounded; the architectural waste of running an 8-shard
binary on a 2-vCPU host is the larger expense.

## Cost analysis

| Item | t3.micro (legacy) | c7i.2xlarge (new) | Per-relay delta |
|---|---|---|---|
| Compute (on-demand) | ~$7.50/mo | ~$262/mo | +~$255/mo |
| EBS (20 GiB gp3) | ~$1.60/mo | ~$1.60/mo | 0 |
| Elastic IP | $0 (in-use) | $0 (in-use) | 0 |
| Egress | charged by volume | charged by volume | unchanged at same throughput |
| **Per-relay total** | **~$9.10/mo** | **~$263.60/mo** | **+~$254.50/mo** |
| **Three relays** | **~$27.30/mo** | **~$790.80/mo** | **+~$763.50/mo** |

Round-numbers: ~$720-770/mo delta for the three-relay tier, depending
on per-region pricing (regions vary ~±5-10%).

**Blue-green overlap window cost.** During cutover the legacy tier
stays online while the goat-relay tier proves out. ~$30-90 cumulative
delta during the overlap window (legacy tier is small; the dollar
impact of overlap is dominated by the new tier, not double-counted
legacy). Operator decides overlap duration based on observed metrics
(see runbook).

**Reservations.** Decision proceeds against on-demand pricing.
Reserved-instance / Savings-Plans optimization is queued post-
operational-soak (~30-90 days) when usage shape is stable. RI savings
on c7i.2xlarge are ~30-40% at 1-year no-upfront — meaningful at
fleet scale, not load-bearing for the sizing decision.

**Blue-green retirement.** Once the goat-relay tier is operationally
proven and the legacy tier retires, the steady-state cost = three
c7i.2xlarge relays (~$790/mo) plus whatever fleet expansion happens
during Phase 3. The delta vs. today is the sizing decision; the
retirement schedule is operator-fired post-cutover.

## Consequences

**Positive:**

- 8-shard architecture exercised in production as designed. Block
  33C.1 lever L8 (CBPF) and L5 (io_uring SQPOLL) have something to
  steer onto / batch under. The post-fix QUIC ceiling at 16f from
  M-040 (~954 Mbps single-placement) is the empirical floor; multi-
  pair characterization and ≥3 fresh-placement variance
  characterization work proceed against this shape.
- NIC headroom is ~12× the current ceiling. Phase 3 horizontal scale
  (multi-peer / partner federation push) does not face NIC-side
  bottlenecks at the per-host level until well past 1 Gbps sustained.
- Sustained-CPU class eliminates the t3.micro credit-cliff failure
  mode under prolonged operations.
- Same family is GA across all current production regions plus the
  planned hub region; future fleet expansion does not face
  per-region instance-class availability as an explicit gate.

**Negative:**

- ~$720-770/mo recurring cost for the three-relay tier — order of
  magnitude over t3.micro. Justifiable given the architecture, but
  worth tracking against per-Tbps cost-model figures in
  `goat-net-scaling.md` §"Cloud
  cost model" (which inherit their own provisional caveat).
- Hypervisor-shared substrate. Lever stack levers L5 / L8 + H1 / H2
  give larger gains on bare-metal per [ADR 0218](0218-relay-substrate-bare-metal-strategy.md).
  The Phase-2 bare-metal POC at Block 34J / M-010 will measure the
  delta; if bare-metal ≥3× same-shape VM, [ADR 0218](0218-relay-substrate-bare-metal-strategy.md)
  Phase 3 fires and primary regions migrate. This ADR's c7i.2xlarge
  decision stays correct *until* that trigger; bare-metal becomes
  the substrate then.
- F-040 placement variance still open — single-placement M-040 is
  adequate evidence for the sizing direction, not for absolute
  ceiling claims at production. Three-fresh-placement re-bench is
  queued (own track); cost model + operator-facing throughput
  guidance inherit the provisional caveat until that closes.

## Trigger conditions for revisit

- **F-040 closure shows c7i.2xlarge ceiling materially below the
  M-040 single-placement number** (e.g., per-placement central
  tendency at 400-500 Mbps 16f rather than ~950): the cost model
  doubles, but the sizing decision still holds (8 vCPU is still the
  floor for 8 shards). Re-evaluate the +~$90/mo c5n.2xlarge upgrade
  as a single hop to relieve the variance.
- **Fleet aggregate sustained > 1 Gbps for any one region**: triggers
  [ADR 0218](0218-relay-substrate-bare-metal-strategy.md) Phase 3
  bare-metal migration; this ADR's VM choice ages out for that region.
- **arm64 toolchain lands** (Chainguard image, CI matrix, Cargo
  cross-compile): re-evaluate Option D (c6gn) on cost grounds. Same
  family; same lever stack applies; cost saving is meaningful at fleet
  scale.
- **NIC saturation observed** in multi-pair characterization:
  bump to c5n.2xlarge (+~$90/mo, +12.5 Gbps NIC headroom) before
  switching family.
- **Production CPU utilization at <30% sustained over a 30-day window
  post-cutover**: re-evaluate downsizing to c6i.large or c7i.xlarge
  (4 vCPU). Note that downsizing under 8 vCPU re-introduces the
  shard-architecture-inert pathology — this trigger is "we never
  needed the architecture" rather than "we need less of it."

## Assessor view (SCA-R)

The instance-class decision is documented against empirical evidence
(M-040, single-placement, full evidence chain in `docs/reports/2026-05-04-goat-relay-tier4b-M-040-*.jsonl`)
and against the architectural prerequisite (ADR 0709 D11 +
Block 33C.1 lever L8 require
N≥core-count shards). The placement-variance question is
acknowledged and queued separately rather than rolled into the sizing
decision; this preserves the audit chain — the sizing call holds
under any of the F-040 variance outcomes (the architecture's vCPU
floor is a function of shard count, not single-instance throughput).
Cost numbers are documented against on-demand pricing with
reservations queued post-soak. The blue-green cutover preserves
rollback (legacy tier stays live during overlap) and is described in
the linked design doc; the cutover is operator-fired with a documented
checklist, not an automated promotion.

---

*See `GLOSSARY.md` for terms and acronyms.*
