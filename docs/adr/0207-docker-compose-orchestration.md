# ADR 0207 — Docker Compose orchestration; reject Swarm/Nomad/K8s/k3s/Spin

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [`0206-island-mode-by-construction.md`](0206-island-mode-by-construction.md), [`0900-ubuntu-lts-luks-x86.md`](0900-ubuntu-lts-luks-x86.md), `../design/container-orchestration.md` |

## TL;DR (for a layperson)

We use the simplest container tool we can get away with — Docker
Compose — and explicitly reject the cluster-style alternatives
(Kubernetes, Swarm, Nomad, k3s, Spin) that are the modern default.
At our scale (about 20 hosts across the whole fleet), the simpler
tool wins on every dimension that matters: less code to audit, no
cluster-wide consensus that breaks under partition, faster cold
boot on tactical edge hardware, less for operators to debug.

## Context

The deliberate trade-off review is at
`../design/container-orchestration.md`.
The headline forces:

1. **Island Mode correctness** — orchestrators with cluster-wide
   consensus break under partition.
2. **FIPS 140-3 + DISA STIG.** Each orchestration layer is another
   CVE treadmill and STIG benchmark to own.
3. **Scale ceiling ~20 hosts** at steady-state production.
4. **Cross-arch (amd64 + arm64).** Identical artifacts across
   architectures.
5. **Cold-start on the edge.** Contested-environment power cycle
   must bring the stack back without human intervention.
6. **Resource budget.** Orin: 16-64 GB RAM; x86 hub: 16-32 GB;
   t3.small validation envs: 2 GB. Control planes that cost
   1-4 GB baseline are expensive.
7. **Coordinated multi-site upgrade is already solved** at the
   application layer (Spock + signal-server +
   `coordinated-upgrade.yml`).
8. **Switching cost.** All Ansible roles, smoke tests, runbooks,
   and operator knowledge are Compose-based.
9. **Direct NIC / port / kernel-routing access for load-bearing
   workloads.** WireGuard drives kernel routing tables; VRRP /
   keepalived ([0205](0205-intra-site-vrrp-failover.md)) manages
   L2 VIPs and gratuitous ARP; CoreDNS, the TAK server, and
   MediaMTX run `network_mode: host` today; snitch uses
   `cap_add: NET_RAW`; the relay / bridge native binaries bind
   public ports on the host; chrony binds host networking for
   NTS today and PPS hardware later; the Block 35 video fabric
   pushes sustained line-rate UDP. Orchestrators that abstract
   networking through CNI (K8s / k3s / microk8s) force every such
   workload into `hostNetwork: true` + `privileged: true` +
   raw-cap escape hatches, which defeat the abstractions they
   sell while keeping their operational cost — and split the
   team's operating model into "fits-the-orchestrator" and
   "bypasses-it" tiers. See
   `../design/container-orchestration.md`
   §1 constraint 9.

## Decision

Docker Compose on every management host today. Relay and bridge
VPSes use systemd-native binaries (no Compose layer needed —
single workload class per host).

Explicitly rejected: Swarm, Nomad, Kubernetes (full + k3s +
microk8s), Fermion Spin.

**Active successor track:** Podman + Quadlet — one-service spike
in Phase 2D against an archive-trimmed service inventory. Decide
full migration on the spike's measured switching cost. The
previously-planned "Compose-wrapped-by-systemd" formalization is
**paused** (it duplicates Quadlet's native systemd integration).

This decision supersedes any prior implicit "we're using Compose
because we started with it" framing.

## Course correction (2026-04-25)

Original decision (2026-04-23) treated Quadlet as a vague
"Phase 3+ watchlist." External-collaborator review surfaced two
points that the trade-off review under-weighted:

1. **Every feature listed in the design doc's §4.2 "Compose+systemd
   wrapper" plan is something Quadlet provides natively** (systemd
   service registration, journald lifecycle visibility, per-overlay
   start/stop, boot-ordering integration via `After=`/`Requires=`).
   Building the wrapper is wasted work if we adopt Quadlet within
   the year — and at the timeline implied by trigger #1 (Cosign as
   a runtime requirement) we will.

2. **Cosign signature verification at container-start time is
   already operationally relevant** given our Chainguard-first
   supply chain — not a Phase 3+ concern. Quadlet enforces this
   natively (`Image=…@sha256:…` + signature policy file); Compose
   has no equivalent.

The decision itself (Compose-now-Quadlet-next) holds. What changes:
the §4.2 wrapper work is paused, the Quadlet evaluation is pulled
forward from "Phase 3+" to a concrete Phase 2D one-service spike,
and Cosign-as-runtime-gate is added to the trigger list. The
companion service archival (move non-essentials out of
`ops/serve/`) addresses the under-weighted "10–12 services means
1–2 weeks of migration, not 0.5d × N" point so the spike-then-
migration runs against a smaller surface.

## Alternatives considered

The full evaluation is in
`../design/container-orchestration.md`.
Summary:

| Option | Why rejected |
|---|---|
| **Swarm** | Effectively unmaintained; same control-plane cost as Compose plus dead-end risk. |
| **Nomad** | Single-binary control plane is attractive but requires consensus (Raft); breaks Island Mode. HashiCorp BSL retreat is a tail risk. |
| **Kubernetes (full)** | Operationally expensive; eats RAM budget; cluster consensus breaks Island Mode; CNI abstraction forces every host-net workload (WG, VRRP, host-bound DNS / TAK / MediaMTX, video UDP, chrony, snitch) into `hostNetwork: true` + `privileged: true` escape hatches — splits the operating model. |
| **k3s / microk8s** | Same control-plane shape as full K8s; same Island Mode problem; same host-network escape-hatch problem; the simplification is in operator ergonomics, not the failure model. |
| **Fermion Spin** | WebAssembly-based; promising long-term, immature today. |

## Consequences

**Positive.** Smallest possible orchestrator footprint. No
cluster consensus to break under partition. Cold start is
deterministic (`systemctl start`, Compose pulls images, services
start). All operator knowledge transfers across hosts. STIG
benchmarks for Docker / Podman are well-trodden; we do not have
to own benchmarks for a more complex orchestrator.

**Negative.** No automatic rescheduling on host failure. We
accept; intra-site VRRP failover ([`0205`](0205-intra-site-vrrp-failover.md))
covers the active-passive case, and the application layer
(Spock multi-master, signal-server federation) covers the
distributed-state case. The orchestrator does not need to.

No native rolling-update story. We accept; coordinated upgrades
happen via `ops/ansible/coordinated-upgrade.yml` at the
application layer.

## Assessor view (SCA-R)

Container orchestration is a CM-7 (least functionality)
decision. Docker Compose is the smallest orchestrator that
meets the requirements; rejected alternatives are documented
with named reasons. The systemd-wrapped pattern is consistent
with NIST SP 800-123 server hardening guidance. STIG SRG-APP
(application security) is satisfied by the per-image
hardening discipline (Chainguard, pinned digests, scanners —
see 0700-series); the orchestrator is a thin layer.

The decision aligns with NIST SP 800-160 vol. 1 principle of
"minimize attack surface": each rejected orchestrator was
evaluated against the partition-tolerance requirement; each
failed.

## Trigger conditions for revisit

Per `container-orchestration.md` §6, the documented trigger set
is broader than three items. The full list:

- **Cosign signature verification at container-start time becomes
  a hard runtime requirement (added 2026-04-25).** Compose has no
  native equivalent; Quadlet does. With Chainguard-first as our
  supply-chain posture, this is already trending toward yes.
- **Phase 2D Quadlet spike returns a clean "go" recommendation
  (added 2026-04-25).** The decision-of-record advances on
  measured switching cost, not speculative timing.
- Host count per site crosses ~20.
- STIG compliance becomes a hard gate on the mgmt-host rootful
  Docker daemon that Compose+Docker cannot clear.
- A Spock / Zenoh replacement requires cluster-wide consensus
  in a way the application layer cannot solve.
- Multi-tenant hosting at a single site emerges (today's
  single-tenant assumption breaks).
- The mgmt-plane stack grows past ~20 services per host.
- A compliance regime or partner explicitly requires a specific
  orchestrator.
- **Default posture:** re-derive at end of Phase 3 against fresh
  facts regardless — not a forced switch, just "good hygiene."

The original draft listed three of these and replaced
"re-derive" with "re-evaluate Podman+Quadlet in a parallel
branch; do not switch without dual-running validation" — the
parallel-branch language is operationally heavier than what the
design doc actually prescribes. Stick with the design's
`re-derive against fresh facts` posture.

---

*See `GLOSSARY.md` for terms and acronyms.*
