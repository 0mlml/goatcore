# ADR 0513 — WASM conditioning sandbox for externally-authored modules at trust boundaries

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-04-27 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [`0501-rust-interceptors.md`](0501-rust-interceptors.md), [`0500-zenoh-data-fabric.md`](0500-zenoh-data-fabric.md), [`0508-audit-logger-interceptor.md`](0508-audit-logger-interceptor.md), [`0511-publisher-trust-model.md`](0511-publisher-trust-model.md), [`0504-cds-conditioning.md`](0504-cds-conditioning.md), [`0805-policy-engine-active-passive.md`](0805-policy-engine-active-passive.md), [`0811-audit-log-integrity.md`](0811-audit-log-integrity.md), [`0821-mission-partner-sharing-pattern.md`](0821-mission-partner-sharing-pattern.md) |

## TL;DR (for a layperson)

Sometimes we need to run small bits of someone-else's code inside our own trusted services — for example, a partner's rule for how to redact a track before it reaches them. Loading that code as a native plugin would give the partner the keys to our process; refusing the code entirely forces us to accumulate every partner's policy logic in our own codebase. The middle path is **WebAssembly (WASM)**: the partner ships a sandboxed module, our service loads it in a memory-isolated runtime with a tiny set of allowed operations, and the module can only see the one message it is conditioning. Bug or malice, the blast radius is one message and one partner. This ADR carves out exactly that case as the place WASM belongs in the system; everything else stays Rust per ADR [`0501`](0501-rust-interceptors.md).

## Context

ADR [`0501`](0501-rust-interceptors.md) established Rust as the integration extension point for the data fabric and explicitly rejected WASM-based interceptors on operational-maturity grounds. That rejection was correct *for the case 0501 considered*: interceptors **we** author, in a trusted process we control, where adding a WASM runtime adds operational surface without a security benefit (we already trust ourselves).

A second case has emerged that 0501 did not address. With the mission-partner sharing pattern (ADR [`0821`](0821-mission-partner-sharing-pattern.md)) and the vendor data-services-provider charter, code authored by external organizations may need to run at a trust boundary — most concretely, per-partner conditioning logic on `release/<partner>/...` (drop / redact / transform). The two not-good options that remain when WASM is excluded:

1. **Accumulate per-partner policy logic in our own Go/Rust release-bridge.** Every partner rule change becomes a PR cycle in our repo. We become the bottleneck and we own the semantics — exactly the platform-shape ADR [`0512`](0512-rapid-adaptation-with-ai.md) rejects.
2. **Load partner-supplied native code (Rust `.so` / Go plugin / shared library) into our release-bridge process.** Partner now has full process memory, full network, full disk inside our trusted service. Categorically a no.

For *this case*, WASM's trade-off flips. The relevant property is not extension-language flexibility (0501's framing); it is **capability-isolated sandboxing of untrusted code inside a trusted host** — the same property Envoy/Istio filters, Fastly Compute@Edge, Cloudflare Workers, Shopify Functions, and OPA-Wasm rely on at production scale. WASI Preview 2 / Wasmtime is operationally mature enough for this narrow shape today.

## Decision

goat adopts WASM as the **only** mechanism for loading externally-authored code into a trusted host process. Internally-authored extensions remain Rust per ADR [`0501`](0501-rust-interceptors.md).

### 1. Carve-out scope

WASM applies when **all** of the following hold:

- The module author is an **external organization** (mission partner, vendor, mission-software supplier whose code we cannot fully audit on every release).
- The module runs **inside a trusted host process we own** (release-bridge, schema-validator, future CDS conditioning frontend).
- The host can express the module's job through a **narrow byte-in / verdict-out contract** — the module does not need to mediate full process behavior.

Out of scope: our own interceptors (stay Rust), full services (use Firecracker / containers / VMs), in-browser compute (separate UI track).

### 2. Engine

**Wasmtime** (Bytecode Alliance) embedded as a Rust crate in the host service. Pinned by version + digest in the host's `Cargo.toml`. CI tracks Bytecode Alliance security advisories.

**Rejected for now:** Wazero (Go-only, host services here are Rust); Wasmer (less mainstream production track record at our shape); writing our own (non-starter).

### 3. ABI — narrow custom interface, Component Model deferred

First implementation uses a **narrow custom ABI** over Wasmtime linear memory:

| Direction | Function | Purpose |
|---|---|---|
| Module → host | `condition(input_ptr, input_len, scratch_ptr, scratch_cap) -> i64` | Returns `(verdict_code << 32) | output_len`; verdict ∈ `{0=Allow, 1=Deny, 2=AllowWithModification}` |
| Host → module | `host_log(level, msg_ptr, msg_len)` | Forwarded into audit pipeline; never to stdout |
| Host → module | `host_now_unix_ms() -> i64` | Monotonic-coarse time (host-clamped granularity to prevent timing oracles) |

**No network imports. No filesystem imports. No env imports.** The module is a pure function `bytes -> verdict + bytes` with bounded side-channel through the audit log.

The WASI 0.2 Component Model is the long-term direction; deferred for v1 because (a) the ecosystem of partner-side toolchains targeting Component Model is still settling, (b) the narrow ABI is enough for the first proof case, and (c) migration from custom ABI → Component Model is a host-side change once partner toolchains catch up.

### 4. Resource limits

Every invocation runs under:

- **Fuel limit** — default 1,000,000 fuel units (Wasmtime fuel API). Budget per-message, not per-module-load.
- **Memory cap** — default 8 MiB linear memory; modules requesting more are rejected at load.
- **Wall-clock guard** — host-side 10 ms epoch deadline as defense-in-depth against fuel-pricing surprises.
- **Instance-per-call** — fresh module instance per invocation; no shared state across messages. (Module-level static data is loaded once; instance state is not retained.)

Limits are tunable in policy-engine config per partner / per topic.

### 5. Module identity, signing, distribution

- **Signing.** Modules are signed by the partner using their offline-CA-issued key chain (same primitives as [`0308-offline-enrollment-cbor-bundles.md`](0308-offline-enrollment-cbor-bundles.md) and Block 22H peer-key attestation).
- **Distribution.** Signed `.wasm` artifacts land in a per-partner directory (e.g. `/var/lib/wasm-modules/<partner>/<module-name>/<version>.wasm`) plus `.sig` plus a manifest declaring host-API version, expected fuel/memory limits, and the topic-glob the module is permitted to condition.
- **Loading.** Host verifies signature against the partner's CA chain at every load (no in-memory pin); rejects on mismatch / expiry / revocation.
- **Versioning.** Multiple versions may coexist; the policy engine selects which version is active per partner / topic. Promotion is a policy-engine event with the same approval workflow as any other release decision (ADR [`0805`](0805-policy-engine-active-passive.md)).

### 6. Audit-by-construction

Every invocation emits an audit record (per ADR [`0508`](0508-audit-logger-interceptor.md), into the integrity tier of [`0811`](0811-audit-log-integrity.md)) containing:

- `partner_id`, `module_name`, `module_version`, `module_sig_digest`
- `topic`, `input_digest` (BLAKE3), `output_digest` (BLAKE3 — empty on Deny), `verdict`
- `fuel_used`, `wall_clock_ms`, `memory_peak_bytes`
- `correlation_id` (per ADR [`0822`](0822-wide-event-observability-otlp-openobserve.md))

Audit is emitted by the host, not the module — the module cannot suppress it.

### 7. First WASM service — partner-conditioning module on `release/<partner>/...`

The first WASM module shipped under this ADR is the partner-conditioning module described in [`../design/wasm-conditioning-sandbox.md`](../design/wasm-conditioning-sandbox.md), tracked as Block 28 in `../project/implementation-plan.md`. The reference partner-side module is a small Rust crate with a TinyGo equivalent, both as integration examples; partners may use any toolchain that produces a conformant `.wasm`.

### 8. Relationship to ADR 0501

ADR [`0501`](0501-rust-interceptors.md) **stands**. Internal interceptors (audit-logger, schema-enforcer, lineage materializer, future transform/quarantine interceptors authored by the project) remain Rust. ADR 0513 carves out the **untrusted-author / trust-boundary** case as the sole place WASM belongs. The two ADRs compose cleanly: a Rust interceptor that needs to evaluate partner-supplied logic embeds Wasmtime and follows this ADR.

## Alternatives considered

- **Per-partner Go/Rust branches in our own release-bridge.** Author-bottleneck on us; partner policy churn becomes our PR churn; matches the platform-shape we explicitly reject.
- **Native plugin (`.so`) loading from partners.** Categorical no — partner gets full process memory + network + disk inside our trusted host.
- **Out-of-process partner sandbox (containers / Firecracker / gVisor).** Real isolation, but per-message latency budget is too tight (microsecond-class) and the operational footprint of one sandbox process per message-publishing-peer per partner is wrong-shaped. Containers/VMs remain the right tool for *services* we don't trust; WASM is the right tool for *function calls* we don't trust inside services we do.
- **eBPF.** Sibling pattern (verified bytecode in trusted host), but the eBPF verifier is kernel-side and the ecosystem assumes kernel context. Wrong fit for userland fabric extensions.
- **Pure-data DSL (e.g. JSON-Schema for validation, OPA Rego for policy).** Solves a sub-case (validation, allow/deny on field comparisons) but cannot express the per-partner *transform* / *redaction* logic that motivates the carve-out. We may still adopt OPA Rego for pure allow-deny policy cases as a sibling tool; that does not change this ADR.
- **Defer until Phase 3+.** Considered. Rejected because the partner-conditioning need is foreseeable in Phase 3 and the alternative paths (native plugin, our-own-policy) are the wrong shape — better to land the discipline before the first partner-conditioning need rather than after.

## Consequences

**Positive.** External-author code runs under capability-isolation by construction. Partner-policy churn stops driving our PR cycle. Audit-by-construction attaches to every invocation. Composes with offline-CA signing, policy-engine approval flow, and audit-integrity tier already in the system. Future CDS conditioning, vendor schema validators, and ad-hoc agent transforms have a ready pattern.

**Negative.** Adds Wasmtime + signing toolchain + per-partner module-distribution surface to the operational footprint. Partner integrators need a working WASM toolchain (Rust / TinyGo / AssemblyScript) — partial mitigation: we publish reference modules in two languages. Custom ABI means a v2 migration to Component Model when WASI 0.2 partner toolchains mature; we accept the host-side migration cost.

**Residual risk.** WASM is a compiler-and-runtime trust assumption — a Wasmtime CVE could undermine the sandbox. Mitigations: pin Wasmtime version + digest, track Bytecode Alliance advisories on the dependency-bump CI, defense-in-depth via wall-clock guard + instance-per-call, and the **module operates only on the one message it is conditioning** so even an escape is bounded by what reaches the host's per-call linear memory. Catastrophic Wasmtime sandbox break is the residual; we accept this against the alternative of native plugins which would be strictly worse.

## Assessor view (SCA-R)

- **AC-3 / AC-6 (access control / least privilege).** Modules run with no syscalls, no network, no filesystem — the strictest least-privilege posture available without out-of-process isolation.
- **SI-7 (software, firmware, integrity).** Module signature verified at every load against partner CA chain; revocation enforced; no in-memory pinning.
- **SI-3 (malicious code protection).** Sandboxed execution environment + fuel/memory caps + wall-clock guard + instance-per-call together bound malicious-code blast radius to one message and one partner.
- **AU-2 / AU-12 (audit events / generation).** Audit is host-emitted on every invocation; module cannot suppress.
- **CM-7 (least functionality).** Each module's permitted topic glob is policy-engine-gated; promotion to a new version is an approval event with the same workflow as any other release decision.
- **SR-3 / SR-11 (supply chain).** Wasmtime pinned by version + digest in CI; partner module distribution rooted at signed CA chain; no platform-mediated module access.

## Trigger conditions for revisit

- Wasmtime declares a sandbox-affecting CVE that pinning-and-update cannot mitigate within an acceptable window. Revisit engine choice.
- WASI 0.2 Component Model partner toolchains reach broad maturity (e.g., Rust + TinyGo + Python + JS all produce WASI 0.2 components). Migrate ABI from custom to Component Model.
- A second category of external-author code emerges that needs *more* than byte-in / verdict-out (e.g., partner-supplied subscriber that needs to call back into the fabric). The narrow ABI is insufficient; revisit with a richer host-import set, possibly via WASI 0.2 capabilities.
- Operational evidence shows fuel-pricing assumptions are wrong (modules routinely hit wall-clock guard before fuel limit, or vice versa). Tune defaults.
- Partner adoption stalls because the toolchain bar is too high. Revisit reference-module language coverage.
- Block 28 ships and the first partner-conditioning module is in production for ≥ 90 days without sandbox incident → flip ADR Status `Proposed → Accepted`.

## Glossary pointer

Acronyms and project-specific terms used in this ADR are defined once in `GLOSSARY.md`.
