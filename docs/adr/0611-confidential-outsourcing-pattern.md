# ADR 0611 — `[security-ascent]` Confidential outsourcing pattern (commercial-cloud managed services via attested enclaves)

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-05-02 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | `0610-confidential-computing-targeted-workloads.md` (internal CC anchors), [`0216-goat-net-federation-and-site-planes.md`](0216-goat-net-federation-and-site-planes.md), [`0513-wasm-conditioning-sandbox.md`](0513-wasm-conditioning-sandbox.md), [`0821-mission-partner-sharing-pattern.md`](0821-mission-partner-sharing-pattern.md), [`0512-rapid-adaptation-with-ai.md`](0512-rapid-adaptation-with-ai.md) |

## TL;DR (for a layperson)

There are managed services in commercial cloud — frontier-model inference, GPU training clusters, large-scale analytics, globally-available pub/sub — that we cannot replicate on sovereign hardware at reasonable cost. Confidential computing (CC) lets us use those services without trusting the cloud provider with plaintext: data leaves goat *only* encrypted to a cryptographic key bound to a verified enclave running a known-good build, the workload runs inside that enclave, results come back the same way. This ADR establishes the architecture pattern (boundary, attestation, namespace conventions) and authorizes a narrow first pilot using **synthetic data only** to prove the primitives end-to-end.

## Context

Three pressures converge:

1. **Workload classes goat cannot host on the edge.** Frontier-model inference, fine-tuning at scale, terabyte-scale historical analytics, and globally-routable confidential pub/sub all require infrastructure (H100/H200 GPUs, distributed storage with sub-millisecond cross-region routing, MLOps tooling) that sovereign hardware at our scope envelope (ADR 0215) cannot deliver.
2. **The market is moving.** All three hyperscalers are building CC stories explicitly aimed at sovereign customers — GCP "grey cloud", AWS Nitro Enclaves + Bedrock confidential, Azure Confidential AI. The marketplace will solve the harder parts (multi-CSP attestation, FIPS-validated TEEs, per-tenant key custody) on a cadence we cannot accelerate ourselves; what we control is whether goat is *positioned* to absorb that capability when it lands.
3. **Existing posture has the bones.** [ADR 0513](0513-wasm-conditioning-sandbox.md) (WASM at boundary), [ADR 0821](0821-mission-partner-sharing-pattern.md) (release-namespace pattern), [ADR 0216](0216-goat-net-federation-and-site-planes.md) (federation vs site plane), and the policy engine ([Block 17A.5](../design/egress-proxy.md) §6) already supply the egress + ingress + audit + lineage primitives. Confidential outsourcing composes on top — it is not a new substrate.

The trap to avoid: applying CC broadly. Most goat workloads either handle ciphertext only (relays, bridges) or run on sovereign hardware (mgmt-plane, observability, Zenoh router). CC there adds attestation key-custody and perf tax without changing the threat model. The narrow application of CC for our own internal workloads is ADR 0610 (Block 45A: boundary node, offline-CA validator, policy engine, Tegra TrustZone). This ADR is the *other* half — using CC at the **boundary** to commercial cloud, where the data-in-use threat is the cloud provider itself.

## Decision

Adopt **confidential outsourcing** as a first-class architectural pattern, decoupled from operational sovereignty by construction:

**D1 — Five-layer reference architecture.** L0 sovereign anchor (unchanged). L1 boundary with bidirectional release-bridge built on the [Block 28 WASM sandbox](../design/wasm-conditioning-sandbox.md) pattern. L2 attestation root + key custody (per-CSP verifier inside sovereign anchor; offline-CA-signed short-lived attestation cert per known-good enclave build). L3 CC workload inside an attested enclave on the chosen CSP. L4 connectivity via a `cc-bridge` peer class paired with sovereign-side boundary peer over WireGuard. Full mechanics in [`docs/design/confidential-outsourcing.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/confidential-outsourcing.md).

**D2 — Three decoupling invariants.** (a) Every byte crossing L1 is **encrypted at the boundary using FIPS-validated crypto** regardless of what the enclave does internally. (b) Inbound data is treated as untrusted-origin and traverses the same WASM conditioning + schema-enforcer as partner data; the enclave is architecturally a "partner." (c) The CC environment **cannot be a hard dependency** of any operational service — only a producer/consumer of opt-in topics. If those three hold, ripping out a CC integration is a config change, not a refactor.

**D3 — FIPS departure scope.** Inside the enclave, FIPS-validated crypto is **not required**. The departure is purposeful, scoped, and sealed by L2 attestation (only a known-good build receives the data-encryption key). Operational FIPS posture for the rest of the system is unchanged. This is consistent with the project posture that deliberate, decoupled FIPS departures are acceptable when isolated and purposeful.

**D4 — Reserved namespaces.** Outbound: `release/cc-<csp>-<service>/...` (mirrors existing `release/<partner>/...`). Inbound: `import/cc-<csp>-<service>/...` (new reserved prefix; mirror of release). Lineage edges across the boundary mark provenance with the attestation digest captured at the time of crossing.

**D5 — Per-CSP attestation verifier.** A small Rust crate per CSP under `cc-bridge/attestation/<csp>/` (Nitro signed PCR doc, GCP vTPM quote, Azure THIM); each verifier emits an offline-CA-signed cert only when the enclave measurement matches a reproducible-build pin from Block 40. Verifier code is in goat trunk; the CC build artifact (the workload that runs inside the enclave) lives in a separate per-pilot repo to keep blast radius contained.

**D6 — Pilot-first sequencing.** The pattern lands as **Pilot A — frontier-model proxy with sealed prompt/answer** (Block 45B, `docs/project/cc-pilot-frontier-inference.md`) using **synthetic data only**. Pilot B (globally-available confidential pub/sub for cross-cloud federation) and Pilot C (model fine-tuning on synthetic tactical corpus) are deferred follow-ons. Operational data flow through this pattern is gated on Pilot A producing verdict-bearing evidence (driver + measurement entry + decision review) and explicit policy-engine approval.

**D7 — Pilot CSP: GCP Confidential VMs + Vertex AI Confidential Inference.** Selected over AWS Nitro Enclaves for the pilot because the GCP "grey cloud" narrative is the strongest sovereign-customer signal in market, and Vertex AI Confidential Inference provides the cleanest end-to-end enclave-resident frontier-model story today. The L1/L2 architecture is written CSP-agnostic; re-instancing on AWS or Azure is verifier + bridge code, not a redesign.

**D8 — Out of scope until evidence shifts.** FIPS-validated crypto inside enclaves; multi-CSP simultaneous deployment (Pilot B); operational data crossing the boundary; using CC for goat's own workloads (that is ADR 0610). Each becomes in-scope only on explicit revisit.

## Alternatives considered

- **Skip CC entirely; replicate managed services on sovereign hardware.** Rejected — H100/H200 clusters and globally-routable pub/sub at the cost-and-engineering envelope of our scope (ADR 0215) is not realistic; opportunity cost is exclusion from frontier-model and large-scale-analytics workloads.
- **Use commercial cloud non-confidentially with operational data.** Rejected — violates sovereignty posture, cannot survive partner / sponsor / SCA-R review, and provides no story under cloud-provider compromise.
- **Wait until CC tooling matures further.** Rejected — the architecture pattern can be settled now with synthetic-data pilots; doing so positions goat to absorb the capability as the market produces it. Waiting until tooling is "perfect" forfeits the pattern-shaping window.
- **Adopt a single hyperscaler's full CC stack (Confidential GKE Nodes + Confidential VMs + Vertex Confidential).** Rejected as architecture commitment — the L1/L2 boundary must be CSP-agnostic so we are not locked in. Selecting GCP for *the pilot* is different from blessing GCP as architectural ground truth.
- **Treat the enclave as an extension of the sovereign anchor (trust it).** Rejected — undermines D2 decoupling. The enclave is architecturally a *partner*; it conditions inbound traffic the same way and depends on attestation evidence per crossing.

## Consequences

**Positive.** Unlocks workload classes (frontier inference, large-scale training, globally-available pub/sub, terabyte analytics) that goat structurally cannot host. Composes cleanly with existing primitives (WASM at boundary, mission-partner-sharing namespace pattern, federation/site-plane split, vendor data-services charter). Positions goat to absorb hyperscaler CC capability as it lands without architecture redesign. Provides a structured answer to the recurring sponsor question "how do you intend to use commercial cloud beyond relays?"

**Negative.** Adds a new architectural surface (attestation verifiers per CSP, cc-bridge peer class, reserved namespaces). Per-CSP attestation tooling is immature; expect 2-3× the integration cost of a typical bridge for the first CSP, less for subsequent. Side-channel risk on TEEs (steady drip of academic attacks on SEV-SNP / SGX) raises the bar but does not absolutize protection. FIPS composition is not solved — any FIPS-bound workload must remain in the sovereign anchor.

**Residual risk.** (a) Cloud-provider compromise of TEE firmware that we cannot detect via attestation — mitigated by reproducible-build pin + per-cross attestation evidence in lineage, but not eliminated. (b) Egress-side WASM conditioning misses a pattern in synthetic-data pilot that would matter in operational data — mitigated by gating operational data behind a separate decision review per workload class. (c) Vendor lock-in via attestation tooling that has no portable equivalent — mitigated by CSP-agnostic L1/L2 architecture, accepted as a known cost for the pilot CSP.

## Assessor view (SCA-R)

This ADR documents a **deliberate, decoupled, scoped FIPS departure** within an enclave whose boundary is sealed by FIPS-validated crypto and verified by reproducible-build attestation rooted in our offline CA. Control mappings: **NIST SP 800-171 r2** §3.13.8 (cryptographic protection during transmission — preserved at L1 boundary), §3.14.7 (system monitoring — wide-event emission per [Block 27](../design/observability-wide-events.md) carries CC-boundary crossings); **NIST SP 800-53 r5** SC-12 (cryptographic key establishment — attestation-bound DEKs per crossing), SI-7 (software/firmware/information integrity — reproducible-build attestation pin), AC-4 (information flow enforcement — release-bridge + WASM conditioning + policy engine approval). The synthetic-data-only pilot scope means no controlled unclassified information traverses the boundary during this ADR's lifetime; operational-data traversal is a separate authorization decision per workload, gated on pilot evidence. Waiver references: none required for the pilot scope; pilot-to-operational transition will require either an explicit FIPS-departure waiver per workload or a path to a FIPS-validated TEE if one becomes available.

## Trigger conditions for revisit

- **Pilot A completes** — flip Status from Proposed to Accepted (pilot succeeded, primitives proven) or back to Proposed with named gaps + remediation plan (pilot revealed structural issue), per `docs/project/cc-pilot-frontier-inference.md` §"Decision review."
- **A hyperscaler ships a FIPS-validated TEE** — D3 (FIPS departure scope) re-opens; some workload classes may move from "outside FIPS posture inside the enclave" to "inside FIPS posture end-to-end." Update D3 + ADR 0610 cross-reference.
- **Cross-CSP attestation portability matures** (e.g. via remote-attestation standardization with industry traction) — D7 (pilot CSP) becomes a non-decision; the architecture should pick up the portable form.
- **Operational data is proposed to cross the boundary** — separate workload-class authorization required; this ADR's pilot scope explicitly excludes operational data, so admitting it requires a new decision instance.
- **A side-channel attack on the chosen TEE family produces practical exploit** — D2 invariants hold the blast radius (sovereign anchor unchanged), but attestation-pin update + CSP-vendor remediation cycle becomes a known operational toil. Track via Block 47 continuous-compliance probing.
- **Block 19 boundary node FIPS PQC path lands** — composition with this pattern (FIPS PQC at boundary, non-FIPS inside the enclave) gets concrete; revisit D3 wording.

Pointer to glossary: see "Attestation," "TEE / SEV-SNP / TDX," "Nitro Enclaves," "Confidential VM," "Reproducible build," "Release namespace," "WASM conditioning."
