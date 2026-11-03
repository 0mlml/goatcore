# grant-crypto SDK

The canonical, cross-platform grant-keyed-encryption library ([ADR 1106](../../../docs/adr/1106-grant-crypto-sdk-and-endpoint-forward-secrecy.md)): one Rust core behind a hand-authored, versioned C ABI, from which the thin Go / Python / TS bindings are built. It makes the forward-secrecy hygiene a default — the platform holds no keys, only members decrypt in-process, and rotation destroys the retired key with no re-wrap. The frozen v1 public surface (the five nouns, six verbs, the error taxonomy, and the C-ABI contract) is [`docs/design/grant-crypto-sdk.md`](../../../docs/design/grant-crypto-sdk.md); where code and that doc conflict, the doc wins until amended.

## Layout

- `core/` — the `grant-crypto` crate: the RustCrypto-backed crypto reference (envelope seal/open, DEK wrap/unwrap, bundle sign/verify, the `service` key-distribution state machine). Dependency-light and conformance-vector gated.
- `capi/` — the `grant-crypto-capi` crate: the versioned, opaque-handle C ABI (`cdylib` + `staticlib` + `rlib`) and the committed, cbindgen-generated header at `capi/include/grant_crypto.h`. This is the single audited FFI seam the bindings link against.
- `bindings/go/` — the `grantcrypto` Go module (cgo over the static lib; zero Go deps; own module so the subtree stays self-contained).
- `bindings/python/` — the `grant-crypto` Python package (stdlib-only `ctypes`; zero pip deps; context-manager dispose).
- `bindings/ts/` — the `grant-crypto` Node/TypeScript package (koffi 2.16.3, MIT — pinned ≥ 2.3.9 for the post-LGPL license line; explicit `close()` + `Symbol.dispose`).

Every binding runs the **same shared conformance vectors through the binding itself** as its CI merge gate (`.github/workflows/grant-crypto.yml` jobs `binding-go` / `binding-python` / `binding-ts`), plus write-side roundtrip, cap, rotate, and handle-lifecycle suites. Each has its own quickstart README.

## Quickstarts

Every language walks the same one obvious path — `identity → acquire → seal → verify → admit → open → rotate` — and each ships a CI-tested runnable example so the quickstart can't rot:

| Language | Quickstart | Runnable example |
|---|---|---|
| Go | [`bindings/go/README.md`](bindings/go/README.md) | [`example_test.go`](bindings/go/example_test.go) — `go test -run Example ./...` |
| Python | [`bindings/python/README.md`](bindings/python/README.md) | [`examples/quickstart.py`](bindings/python/examples/quickstart.py) — `python3 examples/quickstart.py` |
| TypeScript | [`bindings/ts/README.md`](bindings/ts/README.md) | [`examples/quickstart.ts`](bindings/ts/examples/quickstart.ts) — `npm run example` |

**Rust (the core crate directly, no C ABI)** — the same datapath + rotate, in ~30 seconds. Parse the signed bundle off the fabric, `acquire` your producer key and `seal` under a `Context` (the required typed topic binding), `admit`/`open` as the consumer, then `rotate` your leaf:

```rust
use grant_crypto::datapath::{acquire_stream_key, Context, KeyStore};
use grant_crypto::rotate::{rotate, Identity, RotationPolicy};
use grant_crypto::{secret_from_scalar, verify_bundle, verifying_key_from_spki, KeyBundle};

let bundle: KeyBundle = serde_json::from_slice(bundle_json)?;
let bundles = std::slice::from_ref(&bundle);
let member = secret_from_scalar(member_scalar)?; // raw 32-byte P-256 scalar

// Producer: acquire a seal-capable key from your own slot, then seal.
let mut sk = acquire_stream_key("example-set", bundles, "member", &member)?;
let env = sk.seal(Context("data/example-set/status"), b"status snapshot")?;

// Consumer: verify, admit, open.
verify_bundle(&bundle, &verifying_key_from_spki(vk_spki_der)?)?;
let mut ks = KeyStore::new();
ks.admit(&bundle, "member", &member)?;
let plaintext = ks.open(Context("data/example-set/status"), &env)?;

// Rotate: destroy-by-default forward secrecy (re-wraps nothing).
let id = Identity::from_scalar("member", member_scalar)?;
let (id, receipt, _rewrapped) = rotate(id, next_secret, RotationPolicy::Destroy, bundles);
```

`seal` refuses with `Error::RotationRequired` at the cumulative message-count cap (`DEFAULT_SEAL_CAP = 2²⁴`); held DEKs zeroize on drop.

## Phase-1 surface (ADR 1106)

The misuse-resistant verbs the bindings expose, all suite-agnostic (no algorithm/nonce/RNG at any call site):

- **`datapath`** — the producer/consumer path: `acquire_stream_key` → `StreamKey::seal(Context, plaintext)`; `KeyStore::admit` → `KeyStore::open(Context, envelope)`. `Context(&str)` is the **required typed AAD binding** (the data topic) — there is no context-free overload. `StreamKey` enforces a **cumulative message-count cap** (`DEFAULT_SEAL_CAP = 2²⁴`, tighten-only via `with_cap`/`set_cap`, `remaining()` surfaces the approach) and refuses with `Error::RotationRequired` at the cap — the nonce-safety default. Held DEKs zeroize on drop (best-effort; ceiling in the design doc).
- **`rotate`** — member-leaf forward-secrecy hygiene: `rotate(Identity, new_secret, RotationPolicy, &[KeyBundle]) -> (Identity, RotationReceipt, Vec<ReWrappedDek>)`. `Destroy` (default) drops the retired key and re-wraps nothing; `KeepAccess` locally re-wraps still-live DEKs to the new key (labeled as reducing forward secrecy). The `RotationReceipt` is a structured, self-dating fact (counts + RFC3339 timestamp), never time-relative prose.
- **C ABI additions:** `gc_streamkey_set_cap` / `gc_streamkey_remaining` (cap surface), `gc_rotate` (in-place scalar swap; receipt + `rewrapped` entries as one JSON buffer). New status code `GC_ERR_ROTATION_REQUIRED = -7`.

## Split-readiness rule (ADR 1106 open-item 3)

This subtree is a self-contained Cargo workspace so the eventual split to a `usareur-af` sibling repo is a directory move, not a refactor. Two constraints keep it so:

- **No cargo path-dependency points outside `sdk/`.** The `grant-keyd` service (`../keyd`) and other goat integrations consume the core from *outside*; nothing inside `sdk/` knows about Zenoh, the portal, SPIRE, or ZAGROS.
- **The shared conformance vectors** are read from `../envelope/testdata/vectors` (the Go reference produces them). They are the cross-language drift guard and **move *with* the SDK** at split time — as does the normative wire spec `../WIRE-FORMAT.md` (the SDK's frozen wire contract).

## Tests

```bash
# core (roundtrip, pqc_roundtrip, conformance vectors) + capi (ABI roundtrip)
cd ops/grant-crypto/sdk && cargo test

# just the C-ABI cdylib artifact
cargo build -p grant-crypto-capi
```

A bare `cargo test` here builds only `core` + `capi` — `grant-keyd` is outside the workspace, so its ~380-crate zenoh tree never enters this build.

**Toolchain floors:** Rust ≥ 1.88 (`rust-version` in both crates; the patched RustCrypto dependencies require it), Go ≥ 1.22, Python ≥ 3.8, Node ≥ 20. The declared Rust minimum is checked separately in CI.

Rust integrations use P-256 0.14 public key types. Upgrade a direct P-256 dependency alongside the SDK; wire bytes and C ABI version 1 are unchanged.
