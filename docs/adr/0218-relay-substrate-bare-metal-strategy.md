# ADR 0218 — Relay substrate strategy: VMs now, bare-metal at production-shape

| | |
| --- | --- |
| Status | Proposed (2026-05-01) |
| Repo scope | goat core |
| Supersedes | none |
| Superseded by | none |
| Related | [ADR 0709](0709-block-33-relay-rewrite-architecture.md) (Block 33 architecture), [ADR 0217](0217-shim-repo-containment.md) (3×3 horizontal scale-out — pending), [ADR 0700](0700-chainguard-first.md) (container hardening), [ADR 1012](1012-load-testing-discipline.md) (measurement discipline) |

## Context

The Block 33 relay rewrite (per [ADR 0709](0709-block-33-relay-rewrite-architecture.md)) is targeting kTLS + io_uring + shard-per-core to extract per-byte efficiency. Several of the planned levers (Block 33C.1 in `../project/implementation-plan.md`) compound much harder on bare-metal than on shared-tenant VMs:

- **io_uring SQPOLL + multishot recv (L5)** — kernel polls the SQ without per-syscall context switches. On a hypervisor-shared CPU the CPU is already context-switching out for the VM scheduler; SQPOLL's gain (~10-30% at high packet rates per AWS published perf) shrinks. On bare-metal SQPOLL preserves its full benefit.
- **AF_XDP / DPDK userspace networking bypass** — kernel bypass for the data plane. Not available on hypervisor-virtualized NICs without SR-IOV; even with SR-IOV the hypervisor still shares the NIC. On bare-metal the NIC is owned end-to-end.
- **`mitigations=off` (H2)** — disabling Spectre/Meltdown mitigations gives ~10-30% on syscall-heavy paths. Risky on shared-tenant VMs (potential cross-tenant info leakage). Tenable on single-tenant bare-metal where the threat model is "we run only goat-relay on this host."
- **NUMA-aware multi-shard (L8)** — pinning shards to NUMA-local cores gives ~30% on multi-socket bare-metal (e.g., 2-socket × 16-core). Not relevant on a 2-vCPU VM where there's only one NUMA domain.

These four levers in combination would deliver substantially more per-host throughput on bare-metal than on equivalently-sized VMs. M-014 (2026-05-01) verified the rewrite's data path PASSES the ≥110 Mbps acceptance bar on t3.micro VM (3.6× the tuned Go relay), establishing the architecture works. The next question is whether sustaining 10s of Gbps fleet aggregate at production scale is best served by stacking more VMs or fewer bare-metal hosts.

The team is small. Operational simplicity matters. Bare-metal adds ops surface (provisioning, BMC management, firmware updates). It also adds geographic distribution friction: AWS has 30+ regions; bare-metal providers cluster their footprint differently. Self-hosted colo is theoretically optimal for performance + cost but practically a large operational lift the team can't currently sustain.

A staged approach: stay on VMs for the immediate term (33D ships easily at current envelope), measure bare-metal at production-shape (Block 34J / M-010), and only migrate if the per-host gain justifies the ops surface.

## Decision

**Phase 1 — Now (Block 33D):** Stay on AWS VMs, moving to a larger instance class when production load grows. M-014 has proven the rewrite passes acceptance bar on t3.micro shape; no immediate bare-metal need.

**Phase 2 — Block 34J / M-010 production-shape verdict:** Spin **two** bare-metal hosts side-by-side in one region — one Equinix Metal m3.small.x86 (8-core Xeon, 25 Gbps), one Vultr Bare Metal EPYC class (8-16 core, 25-40 Gbps) — plus an AWS c7i.large VM as the hypervisor-virtualized control. Run the same iperf3 ramp + L5/L6/L7/L8/H1-H4 levers stacked on each. Capture HASWELL-style profiling reports per host. ~$370 for ~1 day = real apples-to-apples bare-metal-A vs bare-metal-B vs VM numbers for the same lever stack, and gives the Phase 3 vendor selection a measured rather than paper basis.

**Phase 3 — Decision review post-M-010:** If bare-metal per-host gain is ≥3× the VM at the same lever stack, plan staged migration to bare-metal in primary regions, keep VMs as cheap geo-secondaries. If <3×, stay on VMs and scale horizontally per [ADR 0217](0217-shim-repo-containment.md). **The Phase 3 review consumes the rollup from Block 34K** (`../project/implementation-plan.md` §"34K. Empirical multi-provider substrate comparison") rather than this ADR's paper-math lever-multiplier table — three measurements (throughput parity, distributed-performance RTT/jitter, resiliency over a 30-day window) across the candidate provider matrix, synthesized into a per-region recommendation.

**CSP-agnostic posture:** decision criteria + capacity planning + lever applicability are framed in CSP-agnostic terms (bare-metal vs hypervisor-virtualized; NUMA-aware vs single-domain; SR-IOV vs full PV). Specific vendor selection (Equinix Metal vs OVH HG vs Hetzner vs colocation) lives in `terraform/` per-provider modules. Architecture documentation in this ADR + the linked implementation plan stays portable to any provider.

## Provider candidates (CSP-agnostic, ranked by global coverage)

| Provider | Coverage matching our three relay regions | Per-host class | $/mo (8-core class) |
| --- | --- | --- | --- |
| **Equinix Metal** | all three ✅ — **best match** | m3.small.x86 (8-core Xeon, 25 Gbps) | ~$250 |
| **Vultr Bare Metal** | all three ✅ — **also full match** | EPYC / Xeon 8-16 core, 25-40 Gbps | ~$120-200 |
| OVH | two of three ✅ | HG-15 / HG-30 series | ~$80-150 |
| Hetzner | one of three ✅ | EX-class dedicated | ~$50-100 |
| Self-host colo | wherever we have racks (none currently) | whatever we buy | hardware capex + colo opex |
| AWS dedicated host | matches AWS regions ✅ | dedicated metal but $$$$ | ~$2k+/mo |

Equinix Metal and Vultr Bare Metal both match the full three-region footprint. Equinix is the more mature ops surface (BMC tooling, established Terraform provider, larger enterprise customer base); Vultr undercuts on price (~$120-200 vs ~$250) and has comparable hardware. **Phase 2 POC runs both side-by-side in one region** — apples-to-apples lever-stack comparison gives us a real cost-per-Mbps verdict per provider rather than a paper choice. ~$370 for ~1 day to derisk the Phase 3 vendor selection. **VM-only providers (GCP standard VMs, Vultr Cloud Compute, etc.) are not in scope** — they remain hypervisor-virtualized and inherit the same lever-suppression as AWS VMs; no structural gain to validate.

## Lever multipliers — VM vs bare-metal

| Lever | Bare-metal multiplier vs VM (estimate) | Mechanism |
| --- | --- | --- |
| L5 io_uring SQPOLL | 1.10-1.30× larger gain on bare-metal | Hypervisor scheduler doesn't preempt the SQPOLL kernel thread |
| L6 vectored I/O | ~equal | Both benefit from syscall reduction |
| L7 pre-hashed routing | ~equal | Pure userspace |
| L8 multi-shard + SO_REUSEPORT_CBPF | 1.30× more on multi-socket bare-metal | NUMA-local shard pinning |
| H1 cpupower performance | 1.05-1.15× larger gain on bare-metal | No hypervisor frequency-scaling games |
| H2 mitigations=off | 1.10-1.30× larger gain on bare-metal | Single-tenant security envelope makes it tenable |
| H3 hugepages | ~equal at our packet size | TLB pressure not currently dominant |
| H4 PGO + BOLT | ~equal | Code-side optimization |
| **Combined L5+L6+L7+L8+H1+H2** | **~3-5× per-host throughput at same shape** | Compound multipliers |

A bare-metal host running the full lever stack should sustain 3-5× the same-shape VM running the same lever stack. At $250/mo bare-metal vs ~$80/mo c7i.large = 3× cost for 3-5× throughput → **net per-byte cost win at scale.** Below scale (current envelope) the operational simplicity of VMs beats the per-byte saving.

## Trigger conditions for Phase 3 migration

If ANY of these become true, Phase 3 (bare-metal in primary regions) is the right call:

- Fleet aggregate sustained > 1 Gbps for any one region
- Multi-pair characterization shows VM hypervisor jitter is dominant tail-latency contributor
- Sustained satellite-on-both-ends pairings (per F-014) push relay-mediated traffic past 50% of fleet
- Bench cost optimization (1 bare-metal replaces 4-5 VMs at price parity)

Until one fires, VM-based is the right architectural posture.

## Consequences

- **Operational:** Bare-metal adds BMC + firmware + provisioning surface. Phase 2 POC is bounded to one host so the surface is small. Phase 3 staged migration with VMs as fallbacks.
- **Geographic:** Equinix Metal covers our current three-region footprint. Future regions need to verify provider coverage before commitment.
- **Architectural:** Lever applicability stays CSP-agnostic. The same Rust binary runs on VM or bare-metal. Operator-facing config (`--bind`, `--advertised`, sysctl tunings) is identical.
- **Cost:** Phase 1 unchanged. Phase 2 POC ~$250 one-shot. Phase 3 migration cost depends on how many hosts; at current envelope ~3 bare-metal hosts ($750/mo) replaces the would-have-been ~9 VMs ($720/mo at c7i.large) at break-even cost with materially higher per-host headroom.
- **Vendor lock-in posture:** AWS dependency stays for mgmt-plane, observability, S3, KMS. Bare-metal is purely a relay-substrate question. Easy to multi-vendor: relay regions can be Equinix Metal in some regions + AWS in regions where bare-metal isn't cost-effective.
- **Multi-cloud readiness:** This ADR + the architecture documentation are written CSP-agnostic. The same conclusions apply if we go GCP / Azure / OCI for the mgmt-plane in the future. Provider-specific bits live in `terraform/` per-provider modules.
