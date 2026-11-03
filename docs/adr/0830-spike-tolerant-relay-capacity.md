---
name: ADR 0830 — Spike-tolerant relay capacity via warm-stopped EC2 + pre-baked AMI
description: Decide the strategy for rapidly provisioning relay throughput capacity on demand without paying for long-lived idle high-throughput instances. Stopped-EC2-with-pre-baked-AMI is the chosen anchor; ECS Fargate / Lambda / spot fleet are evaluated and rejected with reasons. Sub-spike + ADR + distributed load test sequence captured.
status: Proposed
type: adr
supersedes: none
sub-series-tag: relay-engineering
---

# ADR 0830 — Spike-tolerant relay capacity via warm-stopped EC2 + pre-baked AMI

| Field | Value |
|---|---|
| Status | Proposed (2026-05-03) — flips to Accepted on spike + initial-impl PASS |
| Audiences | [`technical contributor`, `layperson sponsor`, `SCA-R`] |
| Sub-series | [`relay-engineering`] |
| Repo scope | `terraform/`, `ops/ansible/roles/`, new `ops/relay-spike-fleet/` |
| Cross-refs | [ADR 0709](0709-block-33-relay-rewrite-architecture.md), [ADR 0218](0218-relay-substrate-bare-metal-strategy.md), M-026 (PROVISIONAL — see F-040), `docs/design/goat-net-scaling.md` §"Cloud cost model" |

## Problem

> **Per-instance baseline is provisional.** The M-026 single-run
> point estimate (3.56 Gbps) is almost certainly a QUIC-transport
> run (see F-040
> + F-041).
> Same-day M-040 WS-transport replications on c7i.2xlarge produced
> 772 / 956 Mbps — much lower because of TCP-over-TCP HOL collapse.
> QUIC-tagged re-bench is blocked on a daemon-side force-QUIC patch.
> The shape of this ADR's decision (warm-stopped fleet beats
> always-on at-rest) is robust to the per-instance number; only the
> absolute instance count and dollar figures move (~1.5-2× if
> QUIC-on-c7i central tendency replicates closer to ~2 Gbps).

M-026 verdict puts a single c7i.2xlarge at 3.56 Gbps aggregate relay throughput (single-run point estimate; see provisional caveat above). The cost model in `goat-net-scaling.md` §"Cloud cost model" shows 1 Tbps capacity = ~282 instances = ~$93.9k/month on-demand compute (egress separate) — both figures inherit the provisional caveat.

The operational reality: relay capacity is **mostly unused** in steady state, then **needs surge** during peak events. Paying for 1 Tbps of always-on capacity to handle occasional spikes burns ~$1M/year per Tbps for capacity that runs at ~5-15% utilization on average.

We need a strategy to **rapidly provision relay throughput on demand** without long-lived idle high-cost instances.

## Options considered

### Option A — Stopped EC2 + pre-baked AMI (CHOSEN)

Pre-bake an AMI containing:
- goat-relay container image preloaded into local docker
- Tier-1 sysctls in `/etc/sysctl.d/`
- H2 GRUB drop-in (mitigations=off)
- H3 hugepages reservation
- F-023 modules-load.d/goat-relay-tls.conf
- IAM instance profile + SSM agent baseline

Maintain a fleet of EC2 instances in **stopped** state. Cost = EBS storage only (~$3/instance/mo for 30 GB gp3) + zero compute. Cold start: ~30-60s wall-clock from `ec2 start-instances` to relay-ready.

**Trigger:** spike-detect mechanism (CloudWatch alarm on existing-relay throughput / connection count, OR manual operator command, OR scheduled pre-operation warmup) calls `ec2 start-instances` on N instances + waits for SSM-online.

**Cost:** capacity-at-rest = ~$3/instance/mo. Capacity-in-use = standard on-demand. Net: ~95% cost reduction during idle periods.

### Option B — ECS Fargate

Container-as-a-service. Provisioning ~30s, no EC2 to manage.

**Rejected**: Fargate doesn't support `network_mode: host`. goat-relay's F-027 requires host networking for kTLS ULP visibility (`/proc/sys/net/ipv4/tcp_available_ulp`). Bridged networking silently bypasses kTLS — losing the L1 hand-off lever that's part of every M-009..M-026 measurement. Also: Fargate networking has its own overhead layer (ENI per task, vpc-cni-equivalent) that interferes with multi-shard `SO_REUSEPORT` semantics.

### Option C — Lambda for fan-out

15-minute execution limit, no host networking, 6 MB response cap, no kernel-module access. Fundamentally unsuitable for sustained data-plane relay traffic.

**Rejected**: data plane is by definition multi-second to multi-hour. Lambda's quotas violate basic relay properties.

Could in principle be used for spike-trigger plumbing (lambda fires on CloudWatch alarm → calls `ec2 start-instances`) — but that's just a trigger, not the relay itself. Trigger plumbing is a thin layer on top of Option A; document under that, not as a standalone option.

### Option D — Spot fleet

Cheap (~$28/Gbps/mo vs $94 on-demand) but eviction-prone. Relay sessions don't tolerate sudden termination — peers would re-handshake, mesh would reconverge, but in-flight WireGuard sessions get dropped.

**Rejected as primary, kept as second-tier**: spot is the right shape for **batch / exercise / non-mission-critical surge** where 30s of session loss on eviction is acceptable. Not the right shape for **mission-traffic surge** where session continuity matters. Layered: Option A handles mission-traffic surge; spot handles low-stakes burst (training exercises, internal load tests).

### Option E — On-prem bare-metal (per ADR 0218)

Cheapest per Gbps at large scale (~3-5× cheaper than EC2). Procurement / hosting / replacement overhead. Long lead time to add capacity.

**Complementary, not competing**: Option A covers cloud spike-tolerance. ADR 0218 covers production baseline. Use both per workload posture.

## Decision

**Option A (warm-stopped EC2 + pre-baked AMI) as the cloud spike-tolerance anchor**, with Option D (spot) layered for non-mission-critical burst, and Option E (bare-metal per ADR 0218) as the parallel non-cloud production posture.

## Consequences

### Positive

- **~95% cost reduction during idle periods** (EBS storage only when stopped)
- **30-60s cold-start latency** is fast enough for "operator-detected spike" timescales (minutes-to-hours notice typical)
- **Pre-baked AMI eliminates** ~10 min of cloud-init + docker pull on each cold start
- **Same goat-relay binary + host-tuning stack** as our measured M-026 verdict — no new code path to validate
- **Composes with ADR 0218** bare-metal posture: cloud handles surge, bare-metal handles baseline

### Negative

- **AMI maintenance overhead**: AMI must be rebuilt on every goat-relay binary update + sysctl change + kernel update. CI cadence required.
- **Stopped-instance limit per region**: AWS limits running instances per region (default 20-100 depending on instance class). For 1 Tbps spike capacity (~282 instances) we'd need either limit increase requests or multi-region split.
- **Cold start is NOT instant**: 30-60s is fast enough for human-detected spikes but not for machine-detected sub-minute spikes. For sub-minute response, need a hot pool of N running instances on top.
- **Spike-trigger plumbing is the new operational surface**: CloudWatch alarms + lambda + IAM permissions to call `ec2 start-instances`. New code to maintain, new failure modes to test.

### Mitigations

- **AMI build via Packer in CI** — rebuild AMI on every main-branch push that touches goat-relay binary or host-tuning role. Tag with git SHA; terraform `data.aws_ami` looks up by tag.
- **Multi-region pre-baked AMI** — copy AMI to all production regions; spike fleet split per region per workload.
- **Hybrid pool**: maintain N hot-running instances (~5% of peak capacity) for sub-minute response + warm-stopped fleet (~95% of peak) for minutes-scale surge.
- **Graceful drain on scale-down**: when shrinking the fleet, mark instances `draining` first, let existing sessions complete, then `ec2 stop-instances`.

## Sub-spike + initial-impl plan

1. **Spike (1-2 days)**: build the pre-baked AMI + stopped-EC2 + on-demand-start prototype. Measure: cold-start time (target <60s wall-clock from `ec2 start-instances` to relay accepting connections), AMI size, EBS cost at rest, end-to-end latency from spike-trigger CW alarm to N instances ready.
2. **Initial impl (3-5 days)**: terraform module for spike-fleet (N stopped instances + ASG-equivalent for triggered scale-out + IAM), Packer pipeline for AMI rebuild on CI, CloudWatch alarm + Lambda spike-trigger.
3. **Distributed load test**: extend bench-as-a-service framework (cgnat-validation-style 100-peer scaffold) to drive realistic spike load and verify the spike-fleet scales up cleanly under it. Separate ADR / track per the session-prompts handoff.

## Cross-refs

- [ADR 0709](0709-block-33-relay-rewrite-architecture.md) — Block 33 architecture
- [ADR 0218](0218-relay-substrate-bare-metal-strategy.md) — bare-metal posture (complementary)
- M-026 — capacity-per-instance baseline
- `../design/goat-net-scaling.md` §"Cloud cost model" — economics
- F-027 — why ECS Fargate was rejected
- `../project/spike-tolerant-relay-capacity-prompts.md` — handoff session prompts (spike + initial-impl + bench expansion + architecture overview)

---

*See GLOSSARY.md for acronyms.*
