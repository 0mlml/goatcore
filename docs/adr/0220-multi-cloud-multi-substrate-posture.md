# ADR 0220 — Multi-cloud / multi-substrate posture for the in-flight transit tier

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-05-04 |
| Repo scope | goat core (also informs every future consumer-repo deploying transit infrastructure) |
| Supersedes | — |
| Superseded by | — |
| Related | [`0000-why-adrs-and-procurement-resilience.md`](0000-why-adrs-and-procurement-resilience.md), [`0110-license-posture-agplv3-forked-components.md`](0110-license-posture-agplv3-forked-components.md), `0215-scalability-continuity-scope.md`, [`0216-goat-net-federation-and-site-planes.md`](0216-goat-net-federation-and-site-planes.md), [`0218-relay-substrate-bare-metal-strategy.md`](0218-relay-substrate-bare-metal-strategy.md), [`0219-multi-repo-iac-state-separation-coordinated-compliance.md`](0219-multi-repo-iac-state-separation-coordinated-compliance.md), `0316-wg-cp0-mixed-cloud-and-local-relay-fleet.md`, [`0604-luks-data-at-rest.md`](0604-luks-data-at-rest.md), [`0611-confidential-outsourcing-pattern.md`](0611-confidential-outsourcing-pattern.md), [`0821-mission-partner-sharing-pattern.md`](0821-mission-partner-sharing-pattern.md), [`0900-ubuntu-lts-luks-x86.md`](0900-ubuntu-lts-luks-x86.md). Companion (separate) ADR: [`0221-anycast-relay-tier.md`](0221-anycast-relay-tier.md). |

## TL;DR (for a layperson)

Cloud relays + bridges on one hyperscaler across several regions cover single-region failure but not "the provider itself has a bad day," "the provider gets sanctioned," "the provider is the wrong sovereign substrate for a partner's data," or "the provider gets DDoS-attacked at the IP level." The fix is to deploy the same in-flight transit tier (relays + bridges) across **multiple structurally independent providers** — multiple commercial CSPs (AWS + GCP + Azure), bare-metal vendors (Hetzner / OVH / Equinix Metal), and **partner-procured infrastructure under partner authority**. Data-at-rest stays sovereign on edge hardware regardless. This ADR commits to the posture; the per-provider modules + first migrations are tracked under Block 58. A separate effort ([ADR 0221](0221-anycast-relay-tier.md)) handles Anycast, which is related but not gated on this.

## Context

Three converging forcing functions made the existing single-CSP-multi-region posture insufficient:

**1. Procurement resilience as first-class invariant ([ADR 0000](0000-why-adrs-and-procurement-resilience.md)).** The project's anchor ADR commits to fork-and-patch escape valves for every load-bearing dependency. The cloud transit tier was the largest dependency without such an escape valve — several regions of one CSP are still one account, one set of legal terms, one sanctions surface, one supply chain.

**2. Sovereign-partner federation ([ADR 0216](0216-goat-net-federation-and-site-planes.md), [ADR 0821](0821-mission-partner-sharing-pattern.md)).** The federation-plane and partner-data-sharing patterns assume infrastructure can sit under partner authority. When a partner federates, "we both run in the same AWS region" is a weaker trust claim than "infrastructure under your authority terminates the in-flight path before it reaches our side." Allied-nation and partner-org-procured substrate is a strategic capability, not a tactical optimization.

**3. Multi-region failure ≠ multi-CSP failure.** The 2017 us-east-1 S3 outage and the 2021 us-east-1 networking event both took down half the public internet despite "multi-region" being the recommended fix. AWS-wide control-plane failures, route hijacks against AWS prefixes, and sustained DDoS against named endpoints (a handful of fixed instance IPs is easy to saturate) are all undefended by multi-region. Different CSPs = different hypervisors = different supply chains = different blast radii.

**4. Performance opportunity ([ADR 0218](0218-relay-substrate-bare-metal-strategy.md)).** Bare-metal substrate unlocks the full Block 33 lever stack (kTLS + io_uring SQPOLL + AF_XDP + NUMA-aware shard pinning + `mitigations=off`); on hypervisor VMs several of these levers shrink or vanish. Multi-substrate is a performance answer as well as a resilience answer.

The existing posture in ADR 0316 (mixed cloud-EC2 + local-physical wg-cp0 fleet) already accepts substrate diversity for the *control* plane. This ADR generalizes the same pattern to the *data* plane (relays + bridges) and explicitly extends it to **dev/test** as well as prod. Dev/test cannot validate multi-substrate behavior on a single-substrate substrate.

## Decision

Adopt **multi-cloud / multi-substrate** as the in-flight-transit-tier posture for both prod (`terraform-prod/`) and dev/test (`terraform/`). Specifically:

### 1. Substrate classes (in priority order)

| Class | Examples | Strength | Sovereignty | Phase |
|---|---|---|---|---|
| Sovereign edge | Edge hardware at site | Maximum sovereignty; bytes never leave site | Sovereign | Holds the management plane and at-rest workloads |
| Partner-procured | Partner racks / VMs / bare-metal under partner authority | Partner sovereignty; jurisdictional advantage | Partner-sovereign | **New under this ADR** — Block 58.4 |
| Bare-metal | Hetzner / OVH / Equinix Metal / Vultr Bare Metal | Closer to sovereign; performance lever ([ADR 0218](0218-relay-substrate-bare-metal-strategy.md)) | Commercial, but single-tenant + jurisdictionally diverse | **New under this ADR** — Block 58.2 |
| Hyperscaler CSP | AWS, GCP, Azure | Operational maturity; rapid spin-up; CSP-managed-service capabilities | Commercial, multi-tenant | Default class; GCP for Block 45B confidential outsourcing; Azure for IL5 federation later |

The relay + bridge tier MUST run on **at least two structurally independent substrate classes** in production, and **at least two structurally independent providers** within or across those classes. Target end-state: a hyperscaler + (one bare-metal provider) + (where applicable) partner-procured.

"Structurally independent" means: different ASNs, different physical infrastructure, different legal jurisdictions, different supply chains. Two AWS regions are not structurally independent. AWS + GCP are. AWS + Hetzner are. AWS + a partner rack in a partner nation are.

### 2. Dev/test parity

Dev/test (`terraform/`) MUST be exercisable on every substrate class prod uses. A multi-cloud capability that has never been validated end-to-end in dev/test is theoretical. Specifically:

- Each pre-built escape-valve module (Block 58.2) has a corresponding dev/test bringup target.
- The CGNAT-aware validation harness ([cgnat-validation-env.md](../design/cgnat-validation-env.md)) extends to spin peers across multiple substrates so multi-substrate behavior is observable.
- New ephemeral validation envs (Wave-3-style) explicitly choose substrate per their validation goal — testing AWS-region-failure recovery uses AWS-only; testing CSP-coercion failover requires multi-CSP.

This is a deliberate departure from "dev/test optimizes for cost / iteration speed; prod optimizes for resilience." At our small team size, divergence between dev/test and prod is a category of failure we cannot afford.

### 3. Substrate-portable build artifacts (no per-CSP source forks)

Every load-bearing transit binary (`goat-relay`, `goat-bridge`, future Anycast-aware variants) must be substrate-portable: same Rust/Go binary, same container image, same Ansible role. Per-substrate config lives only in:

- `terraform/<env>/providers/<provider>/` — provider-specific Terraform modules.
- `ops/ansible/inventory/<provider>-<env>.ini` — per-provider inventory.
- `ops/ansible/group_vars/<provider>_<env>_hosts/` — per-provider variables (image AMI / OS image / network setup).

If a substrate cannot run the canonical binary, it is disqualified — not specialized. (Cloudflare Workers as a relay substrate is the named example; ruled out previously on workload-shape grounds.)

### 4. Per-substrate state separation, no spanning stacks

Per [ADR 0219](0219-multi-repo-iac-state-separation-coordinated-compliance.md): per-repo state, no cross-repo lock contention. **Generalized here:** never one Terraform stack spanning two CSPs / two bare-metal providers. Each substrate gets its own Terraform module + state + lock. Cross-substrate orchestration happens at the deployment-script level (`ops/relay/deploy-relay.sh`-style orchestrators), not inside Terraform.

Reasons: Terraform's CSP abstraction is leaky in practice, provider-specific knowledge accumulates regardless, blast radius of state corruption stays bounded.

### 5. Partner-procured substrate as a first-class deployment target

Define and codify the **Partner Transit Substrate Contract** (Block 58.4): a partner provides hardware (rack / VM / bare-metal) meeting a minimum spec, gets a relay binary + Ansible role + offline-CA-signed cert, runs it under their authority. Same shape as the [Partner Reference Architecture](https://github.com/usareur-af/goatdocs/blob/main/docs/design/partner-reference-architecture.md) but for transit infrastructure rather than data sharing.

Trust model: the partner-procured node is a **transit endpoint**, not a data-at-rest store. It encrypts everything it relays (WireGuard + TLS) and never holds plaintext mission data. The partner provides hardware + power + transit; we provide configuration + supervision via the same mechanisms used for self-operated nodes.

This is the **biggest strategic gain** in this ADR. It unlocks deployment shapes that no commercial multi-cloud arrangement can match.

### 6. Capability-driven CSP picks (unchanged from existing commitments)

Some CSP picks are driven by capability access, not substrate diversity. These commitments are already made and continue under this ADR:

- **GCP Confidential VMs** for confidential outsourcing pilot ([ADR 0611](0611-confidential-outsourcing-pattern.md), Block 45B).
- **Azure Confidential AI** as Phase-3+ option for IL5 federation participation ([ADR 0216](0216-goat-net-federation-and-site-planes.md)).
- **AWS GovCloud (IL4/5/6)** as a federation participant per [ADR 0216](0216-goat-net-federation-and-site-planes.md) — never a centralizer.

These rides on top of the multi-substrate posture; they do not replace it.

### 7. Hard prohibitions

- **No CSP-managed-mystery on data-at-rest.** Site plane stays sovereign. Block 45B's three decoupling invariants apply at every CSP boundary, not only confidential compute.
- **No multi-CSP per single Terraform stack** (per §4 above).
- **No spanning state lock across substrates.**
- **No CSP-locked-only build paths** (per §3 above).
- **No claiming "multi-cloud" when you mean "multi-region within one CSP."** This ADR defines the term: "multi-cloud" = at least two structurally independent substrates in production.

## Substrate selection criteria

When deciding where a new transit node should land, evaluate in this order:

1. **Sovereignty fit.** Is this for partner federation? → Partner-procured > bare-metal-EU > hyperscaler-EU > hyperscaler-US.
2. **Resilience class.** Is this part of an Anycast prefix or a per-region fallback? Anycast nodes need at least 3 substrate-diverse PoPs; per-region fallbacks need substrate diversity from the primary.
3. **Performance class.** Is this on the Block 33 hot path? → Bare-metal > hyperscaler-with-SR-IOV > hyperscaler-VM.
4. **Operational class.** Is rapid spin-up / tear-down a priority? → Hyperscaler > bare-metal > partner-procured.
5. **Cost class.** All else equal, prefer cheaper. Bare-metal is cheaper than hyperscaler-VM at sustained throughput. Partner-procured is essentially free for transit; their hardware + power.

The goal: every new transit node has a stated rationale. Block 32 ([engineering-health scorecard](https://github.com/usareur-af/goatdocs/blob/main/docs/design/engineering-health-assessment.md)) carrying-cost metric tracks whether the substrate mix is paying off.

## Out of scope (explicit non-decisions)

- **Site-plane substrate.** The sovereignty split already commits site-plane to sovereign hardware. This ADR is about **in-flight transit only**.
- **Specific provider rankings.** Provider selection per use case lives in the design doc + Block 58 sub-tasks, not in this ADR. The ADR commits to the posture; specifics evolve.
- **CDN tier.** Cloudflare's role for DNS + Let's Encrypt + (future) edge protection is unchanged. This ADR does not commit to or rule out future Cloudflare use.
- **Anycast.** Separate ADR ([0221](0221-anycast-relay-tier.md)). Anycast benefits from multi-substrate but is not gated on it.
- **Federation-plane substrate.** [ADR 0216](0216-goat-net-federation-and-site-planes.md) already addresses; this ADR does not modify.

## Consequences

**Positive:**

- Procurement-resilience escape valves operational, not theoretical.
- Strategic-capability advantage (partner-procured substrate) is a unique offering vs vanilla DoD-cloud postures.
- DDoS absorption + multi-region-outage protection at substrate level.
- Performance ceiling rises (bare-metal Block 33 lever stack).
- Dev/test gains the ability to validate multi-substrate scenarios end-to-end.

**Negative / accepted trade-offs:**

- **Per-CSP / per-provider Terraform module surface grows.** Mitigated by §3 substrate-portable build artifacts: only Terraform / inventory / group_vars vary per substrate; binary + container image + Ansible role do not.
- **Operational toil grows.** Each substrate has its own provisioning quirks, IAM model, network model, billing model. Block 32 carrying-cost metric is the explicit signal for "is this becoming unaffordable?"
- **Cross-substrate diagnostic surface.** A failure mode that touches two substrates is harder to debug than one. Mitigated by Block 27 wide-event observability — `correlation_id` carries across substrate boundaries; investigation surface is one query, not five.
- **Chicken-and-egg with partner-procured substrate.** A new Partner Transit Substrate Contract starts with no adopters. Mitigated by sequencing: contract first (Block 58.4 paper artifact), one named partner pilot (Block 58.5), evaluate.

**Operational:**

- Initial Block 58 work creates dormant escape-valve modules for ≥3 alternative substrates without making them load-bearing. Production cutover to a second substrate is a follow-up decision triggered by capacity / sovereignty / failure events.
- Monthly review at engineering-health scorecard cadence: are the dormant modules still buildable? Have substrate APIs drifted? Module rot is a real risk and the scorecard is the forcing function against it.

## SCA-R framing (Senior Cloud Architect — Reviewer)

The textbook multi-cloud-for-resilience pattern is Anthos / multi-cloud Kubernetes / cross-cloud database replication / etc. This ADR explicitly does NOT adopt that pattern, because the pattern's value-prop assumes the workload itself spans clouds. Our transit tier is a fleet of small stateless replaceable nodes; the value of multi-cloud here is not "the workload runs across clouds" but "the workload can run on any cloud, and we run instances on enough independent ones that no single CSP failure / sovereign event / DDoS can take all of them down."

The novel piece — partner-procured substrate as a first-class transit deployment target — has no good analog in commercial DoD architectures, where infrastructure is overwhelmingly hyperscaler-or-self-hosted. The partner-procured pattern only makes sense when: (a) the substrate handles only encrypted in-flight bytes, never plaintext data; (b) the deploying organization has supervisory mechanisms that work across legal-authority boundaries (offline-CA signing, Ansible-over-mesh, audit-by-construction); (c) the partner relationship is both diverse enough to be politically interesting and stable enough to commit infrastructure. The goat system meets all three. Worth pointing out as a strategic capability, not a tactical optimization.

The dev/test parity insistence is unusual — most multi-cloud posture statements quietly except dev/test. The reasoning here ("at our team size, divergence between dev/test and prod is a category of failure we cannot afford") is sound and worth explicit naming.

## References

- [`docs/design/multi-cloud-multi-substrate.md`](../design/multi-cloud-multi-substrate.md) — design doc operationalizing this ADR
- `docs/project/implementation-plan.md` Block 58 — execution plan
- `docs/design/cloud-environments.md` — env taxonomy this ADR extends
- [ADR 0218](0218-relay-substrate-bare-metal-strategy.md) — bare-metal-specific decision (the relay tier's performance answer; this ADR is the strategic parent)
- ADR 0316 — control-plane mixed-fleet (the pattern this ADR generalizes to data plane)
- [ADR 0611](0611-confidential-outsourcing-pattern.md) — confidential outsourcing (uses GCP/Azure/AWS independently of substrate-diversity rationale)
- [ADR 0821](0821-mission-partner-sharing-pattern.md) — partner data sharing (the relationship pattern partner-procured substrate operationalizes)
- See GLOSSARY.md for acronym expansions used in this ADR (CSP, ASN, ADR, SCA-R, IL4/5/6, etc.).
