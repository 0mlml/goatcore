# WASM conditioning sandbox — design (companion to ADR 0513)

| Field | Value |
|---|---|
| Status | Draft (Phase 3 commitment, first WASM service) |
| ADR anchor | [`../adr/0513-wasm-conditioning-sandbox.md`](../adr/0513-wasm-conditioning-sandbox.md) |
| Companions | [`mission-partner-sharing.md`](mission-partner-sharing.md), `zenoh-data-fabric.md`, [`publisher-trust-model.md`](publisher-trust-model.md) |
| Implementation tracker | `../project/implementation-plan.md` Block 28 |

## Why this exists

Phase 3 introduces the first situation in goat where **externally-authored code needs to run inside a trusted host process**: per-partner conditioning logic on `release/<partner>/...` (drop / redact / transform). ADR [`0501`](../adr/0501-rust-interceptors.md) keeps Rust as the extension language for *our* interceptors. ADR [`0513`](../adr/0513-wasm-conditioning-sandbox.md) carves out WASM as the **only** mechanism for loading external-author code into a host we own — the partner-conditioning module is the first instance.

This design doc covers the sandbox shape, ABI, identity / signing flow, audit integration, and the partner-conditioning reference module that ships as Block 28's exit criterion.

## Scope reminder

WASM here is **not** an alternative to Rust interceptors, Compose / Quadlet for services, or Firecracker for whole-process isolation. It is the right tool for one narrow thing — function calls we don't trust, inside services we do. See ADR 0513 §1 for the carve-out boundary.

## Architecture

```
                           ┌─ Zenoh router (Rust) ──────────────────────────┐
publish → release-bridge → │  release-bridge interceptor (Rust, ours)       │
                           │     │                                          │
                           │     ├── load partner module (Wasmtime)         │
                           │     │      [signed .wasm; per-partner;         │
                           │     │       narrow ABI; fuel + memory caps]    │
                           │     │                                          │
                           │     ├── invoke condition(input_bytes) → verdict│
                           │     │                                          │
                           │     ├── Allow:  forward (possibly transformed) │
                           │     ├── Deny:   drop, audit-log                │
                           │     │                                          │
                           │     └── audit emit (fuel/wall/peak/digest)     │
                           └────────────────────────────────────────────────┘
                                              │
                                              ▼
                              audit-integrity tier (Block 17D)
                              wide-event store (Block 27)
```

The release-bridge is a Rust Zenoh interceptor (per ADR 0501). The Wasmtime runtime is embedded in the bridge as a Rust crate. Each `release/<partner>/<topic>/...` publish is conditioned by the partner's currently-active module before it leaves our boundary.

## ABI (v1 — narrow custom)

### Module exports

```wit
// Provisional. Spelled out in concrete WIT only when v2 (Component Model)
// migration lands. v1 expresses the same shape over Wasmtime linear memory.

condition:
  input  = (input_ptr: i32, input_len: i32, scratch_ptr: i32, scratch_cap: i32)
  output = i64    // packed: (verdict_code << 32) | output_len

verdict_code:
  0 = Allow                  // forward unchanged; output_len ignored
  1 = Deny                   // drop; output_len = 0
  2 = AllowWithModification  // forward output_ptr[0..output_len]
  3..  = reserved (host treats unknown as Deny, audit-flag)
```

The host writes the candidate message into the module's linear memory at `input_ptr`, allocates `scratch_cap` bytes at `scratch_ptr` for the module to write its output, and calls `condition`. The module returns its verdict + (for verdict 2) the length of bytes it wrote into scratch.

### Module imports (host-provided)

```
host_log(level: i32, msg_ptr: i32, msg_len: i32) -> ()
  // Forwarded into the audit pipeline tagged {partner, module, version, level}.
  // Levels: 0=trace 1=debug 2=info 3=warn 4=error.
  // No stdout, no stderr, no syslog — only the audit channel.

host_now_unix_ms() -> i64
  // Monotonic-coarse Unix epoch in milliseconds.
  // Host clamps granularity to ≥ 1ms to prevent fine-grained timing oracles.
```

**That is the entire host-import surface.** No `wasi:filesystem`, no `wasi:sockets`, no `wasi:cli`, no `wasi:random` (modules that need randomness must accept it as input bytes — partner conditioning is deterministic by design).

### Why narrow + custom in v1

The WASI 0.2 Component Model is the right long-term shape but the partner-side toolchain ecosystem (Rust + TinyGo + AssemblyScript + others) is not uniformly ready to produce conformant components. A narrow custom ABI on Wasmtime linear memory is supportable in any toolchain that targets `wasm32-unknown-unknown`. Migration path to Component Model is a host-side change once partner toolchains converge — modules built against v1 keep working until end-of-life is announced.

## Resource limits (defaults; tunable via policy engine)

| Limit | Default | Rationale |
|---|---|---|
| Fuel | 1,000,000 units | Per-message budget. Empirically ~1ms of busy CPU on common module shapes. |
| Memory cap | 8 MiB linear | Generous for byte-shuffle conditioning; rejects modules requesting more. |
| Wall-clock | 10 ms per invocation | Defense-in-depth against fuel-pricing surprises; epoch deadline checked by host. |
| Instance lifetime | Per call | Fresh instance every invocation. Module-level static data loaded once at module-load; no per-call carry-over. |
| Module size | 4 MiB | Operational ceiling; larger modules require explicit waiver. |

Policy-engine entries can override defaults per partner / per topic; overrides are policy decisions like any other.

## Identity, signing, distribution

### Signing

Partner signs each `.wasm` artifact with their offline-CA-issued module-signing key. Same primitives as offline enrollment (ADR [`0308`](../adr/0308-offline-enrollment-cbor-bundles.md)) and Block 22H peer-key attestation — keys live on partner's YubiKey-PIV-equivalent custody, signature lineage roots at the partner's CA, our side validates the chain.

For partners who federated through the Bring-Your-Own-OIDC pattern (ADR [`0821`](../adr/0821-mission-partner-sharing-pattern.md)) but do not yet have offline-CA infrastructure, the bootstrap path is **partner-attested via OIDC + module-digest pinned in policy-engine record**. Trust is then upgraded to offline-CA-rooted as their stack matures. The interim-pinning path is documented as such and audit-flagged so it cannot drift into a long-term posture.

### Distribution

Per-partner directory layout on each bridge:

```
/var/lib/wasm-modules/<partner-slug>/
  ├── <module-name>/
  │   ├── manifest.json          # host-API version, fuel/memory caps, permitted topic glob, sig digest
  │   ├── 1.0.0.wasm
  │   ├── 1.0.0.wasm.sig
  │   ├── 1.0.1.wasm
  │   └── 1.0.1.wasm.sig
  └── <other-module>/
      └── …
```

Distribution is push-on-policy-event:

1. Partner publishes a new module version (out-of-band: signed artifact + manifest delivered through the Partner Data Sharing Charter's Appendix B technical channel — typically a partner-staged HTTPS endpoint or a signed-bundle drop).
2. We fetch the artifact + sig + manifest, verify signature against the partner's CA chain, validate manifest against ADR-0513-permitted shape.
3. Policy-engine record proposes the version as a candidate for the affected `release/<partner>/<topic>` glob.
4. Two-of-N approval (ADR [`0805`](../adr/0805-policy-engine-active-passive.md)) promotes the version. Audit log captures the decision.
5. Bridge-side hot-reload — Wasmtime instance pool drains, new version loaded; in-flight messages complete on the prior version.

### Loading

At every load (cold start, hot reload, recovery from crash):

- Verify signature against partner CA chain — reject on mismatch / expiry / revocation.
- Validate manifest declares the same `host-api-version` the bridge supports.
- Validate `topic-glob` matches the policy-engine record for this partner / module.
- Compile to native via Wasmtime; cache compiled artifact keyed by `(module-digest, wasmtime-version)` to avoid recompilation on every reload.

There is **no in-memory signature pin** — every cold start re-verifies against the live CA chain so revocation propagates without bridge restart loops.

## Audit-by-construction

Every `condition()` invocation emits one audit record (per ADR [`0508`](../adr/0508-audit-logger-interceptor.md), persisted into the integrity tier of [`0811`](../adr/0811-audit-log-integrity.md), correlated via [`0822`](../adr/0822-wide-event-observability-otlp-openobserve.md)):

```jsonl
{
  "ts":              "<rfc3339>",
  "correlation_id":  "<id>",
  "partner_id":      "<slug>",
  "module_name":     "<name>",
  "module_version":  "<semver>",
  "module_digest":   "blake3:<hex>",
  "topic":           "release/<partner>/<topic>/...",
  "input_digest":    "blake3:<hex>",
  "output_digest":   "blake3:<hex>"   // empty on Deny
  "verdict":         "Allow|Deny|AllowWithModification|Reserved",
  "fuel_used":       <int>,
  "wall_clock_ms":   <int>,
  "memory_peak_b":   <int>,
  "host_log":        [{"level":"info","msg":"..."}, ...]   // module-emitted lines, host-stamped
}
```

The host emits the record. The module cannot suppress it. `host_log()` lines emitted by the module attach to the same record.

## Reference partner-conditioning module — first WASM service

Block 28 exit criterion is **one partner-conditioning module shipped end-to-end**. The reference module skeleton (ours, published as an integration example partners may copy):

### Rust skeleton (`ops/wasm-modules/reference/conditioner-rs/`)

```rust
#![no_std]

#[no_mangle]
pub extern "C" fn condition(
    input_ptr: *const u8, input_len: usize,
    scratch_ptr: *mut u8, scratch_cap: usize,
) -> i64 {
    let input = unsafe { core::slice::from_raw_parts(input_ptr, input_len) };
    let scratch = unsafe { core::slice::from_raw_parts_mut(scratch_ptr, scratch_cap) };

    // Example: track-fusion conditioning for Partner X
    //   - drop tracks with confidence < 0.6
    //   - round position to 100m grid
    //   - strip source_sensor_id

    match decode_track(input) {
        Ok(t) if t.confidence < 0.6 => pack(VERDICT_DENY, 0),
        Ok(mut t) => {
            t.position = round_grid(t.position, 100.0);
            t.source_sensor_id = None;
            let n = encode_track(&t, scratch);
            pack(VERDICT_ALLOW_MOD, n)
        }
        Err(_) => pack(VERDICT_DENY, 0),
    }
}

fn pack(v: i64, n: usize) -> i64 { (v << 32) | (n as i64) }
const VERDICT_ALLOW: i64    = 0;
const VERDICT_DENY: i64     = 1;
const VERDICT_ALLOW_MOD: i64 = 2;
```

Build: `cargo build --release --target wasm32-unknown-unknown` → `.wasm`. Strip + sign with offline-CA-issued key.

### TinyGo equivalent (`ops/wasm-modules/reference/conditioner-go/`)

Same shape, exported as a Go package targeting `GOOS=js GOARCH=wasm` (TinyGo). Provided as a starting point for partners on the Go track.

Both reference modules are deliberately tiny — the point is the *contract*, not the *cleverness*.

## Failure modes & containment

| Failure | Containment |
|---|---|
| Module hits fuel limit | Wasmtime traps; host treats as Deny + audit-flag `fuel_exhausted=true`. Message dropped, audit record emitted. |
| Module hits memory cap | Wasmtime traps; host treats as Deny + audit-flag. |
| Module exceeds wall-clock | Host epoch interrupt; treats as Deny + audit-flag. |
| Module returns malformed verdict (≥ 3) | Host treats as Deny + audit-flag `verdict_reserved=true`. |
| Module signature invalid at load | Module never instantiates; bridge falls back to default-deny; loud alert. |
| Wasmtime sandbox CVE | Pin + advisory-monitor on dependency-bump CI; emergency procedure: revoke module loads of affected version range until host is patched. |
| Partner ships a deliberately-malicious module | Bounded by sandbox + per-call instance + per-message blast radius. Forensics via audit + module-digest record. Revocation is a policy-engine event. |

## Operational shape

- **Bridge-side metric set** — fuel-used percentile, wall-clock percentile, verdict distribution per partner / module / topic. Prometheus per ADR [`0815`](../adr/0815-observability-stack.md).
- **isitup tile** — per-partner conditioning-module health (loaded/loading/error/revoked).
- **Alert rules** — `module_load_signature_invalid`, `module_fuel_exhaustion_rate_high`, `module_revocation_pending`.
- **Runbook** at `../operations/wasm-module-runbook.md` (Block 28F deliverable) — load, hot-reload, revoke, debug-replay.

## Integration surface

| Path | Purpose |
|---|---|
| `zenoh-plugins/release-bridge/` | Rust Zenoh interceptor; embeds Wasmtime; per-message `condition()` invocation |
| `ops/wasm-modules/reference/conditioner-rs/` | Reference Rust partner module |
| `ops/wasm-modules/reference/conditioner-go/` | Reference TinyGo partner module |
| `ops/policy-engine/schema/wasm_module.sql` | Spock-replicated module-version + activation-state table |
| `ops/ansible/roles/release-bridge-wasm/` | Bridge-side role: install Wasmtime, configure module dirs, key chain |
| `docs/operations/wasm-module-runbook.md` | Operator runbook |
| `docs/operations/partner-wasm-onboarding.md` | Partner integrator guide (Charter Appendix B reference) |

## Test plan

- **Unit (host).** Wasmtime fuel-exhaustion, memory-cap-trap, wall-clock-trap, malformed-verdict path, missing-signature path. All produce Deny + audit-flag.
- **Unit (module).** Reference-module verdict tables for known inputs, including malformed input → Deny.
- **Integration.** End-to-end on integdev: signed module promoted via policy engine, message published on `release/<partner-test>/track/...`, conditioned, audit record verified against integrity tier, hot-reload to a v2, in-flight messages complete on v1.
- **Soak (smoke tier per ADR 1008).** 5-minute load-gen at 100 msgs/sec under reference module; assert audit completeness + bounded fuel distribution.
- **Soak (validation tier).** 2-hour run with deliberate-fault module variants (infinite-loop module, oversize-output module, malformed-verdict module). Assert containment.
- **Adversarial.** Tabletop: partner ships a module that tries to read off-bounds linear memory, write to `host_log` at high frequency, return verdicts that don't match its declared topic glob. Each must be caught by host or audit.
- **Pin-regression.** Wasmtime version bump triggers smoke-tier soak rerun (per ADR [`1008`](../adr/1008-soak-testing-discipline.md)).

## Out of scope

- WASI 0.2 Component Model migration — separate effort, gated on partner-toolchain maturity.
- WASM for in-browser compute — different use case (UI), separate track if it ever lands.
- WASM as Zenoh interceptor language for our own code — explicitly rejected (ADR [`0501`](../adr/0501-rust-interceptors.md) stands).
- WASM for full services — wrong tool; use containers / Firecracker.
- Module-to-module composition / pipelines — v1 is one module per `release/<partner>/<topic>` glob. Pipelines deferred until a real need surfaces.
- Cross-classification (CDS) conditioning — separate ADR [`0504`](../adr/0504-cds-conditioning.md). The pattern composes (CDS frontend can use this sandbox), but the operational and accreditation envelopes differ.

## References

- ADR [`0513`](../adr/0513-wasm-conditioning-sandbox.md) — anchor decision
- ADR [`0501`](../adr/0501-rust-interceptors.md) — Rust interceptors (still stands for our own code)
- ADR [`0508`](../adr/0508-audit-logger-interceptor.md) — audit-logger pattern
- ADR [`0511`](../adr/0511-publisher-trust-model.md) — publisher trust model
- ADR [`0805`](../adr/0805-policy-engine-active-passive.md) — policy-engine approval flow
- ADR [`0811`](../adr/0811-audit-log-integrity.md) — audit-integrity tier
- ADR [`0821`](../adr/0821-mission-partner-sharing-pattern.md) — mission partner sharing
- ADR [`0822`](../adr/0822-wide-event-observability-otlp-openobserve.md) — wide-event correlation
- ADR [`1008`](../adr/1008-soak-testing-discipline.md) — soak-testing discipline (pin-regression)
- [`mission-partner-sharing.md`](mission-partner-sharing.md) — the boundary at which the first WASM service runs
- `zenoh-data-fabric.md` — interceptor architecture
- `../project/implementation-plan.md` Block 28
