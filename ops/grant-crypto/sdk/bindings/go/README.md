# grant-crypto — Go binding

A thin, hand-written Go binding over the grant-crypto C ABI ([ADR 1106](../../../../../docs/adr/1106-grant-crypto-sdk-and-endpoint-forward-secrecy.md)). Same library as the Rust core and the Python/TS bindings; same [conformance vectors](../../../envelope/testdata/vectors/). The frozen public surface is [`docs/design/grant-crypto-sdk.md`](../../../../../docs/design/grant-crypto-sdk.md) — five nouns, six verbs, rendered idiomatically and nothing added. The full godoc narrative (what crosses the boundary, the leak-reporting finalizer, the error taxonomy) is in [`doc.go`](doc.go).

**Endpoint forward secrecy, by construction:** the platform holds no keys; only members decrypt, in-process; the producer is not a key authority (`Seal` takes no recipients). Private scalars and DEKs live and die inside the Rust core — **they never enter Go memory**. Only wire bytes (bundle/envelope/receipt JSON) and plaintext cross the boundary.

Own Go module (`github.com/usareur-af/goatcore/ops/grant-crypto/sdk/bindings/go`), zero Go dependencies, cgo over the static lib — so the SDK subtree stays self-contained.

## Prerequisites

Build the C ABI static library once (the binding links it statically via cgo):

```sh
cd ops/grant-crypto/sdk && cargo build -p grant-crypto-capi
```

Then build/test this package normally (`CGO_ENABLED=1`, the default):

```sh
cd bindings/go && go test ./...
```

The cgo directives resolve `libgrant_crypto_capi.a` under `../../target/debug` relative to the package; a release build overrides the `LDFLAGS` path to the shipped, checksum-pinned native. On Linux the core also needs `-lm -ldl -lpthread` (wired in the cgo directives); macOS needs nothing beyond libSystem. Go **1.22+**.

## Quickstart

The one obvious path — a producer seals, a consumer verifies, admits its slot, and opens — then the member rotates its leaf:

```go
import gc "github.com/usareur-af/goatcore/ops/grant-crypto/sdk/bindings/go"

id, _ := gc.NewIdentity("org-a/member", memberScalar) // raw 32-byte P-256 scalar
defer id.Close()

// Producer: acquire a seal-capable key from your own bundle slot, then seal.
sk, _ := gc.AcquireStreamKey(bundlesJSON, "org-a", id) // highest-gen slot for this id
defer sk.Close()
envelope, _ := sk.Seal("data/org-a/feed", []byte("feed snapshot"))

// Consumer: verify the bundle, admit your slot, open the message.
if err := gc.VerifyBundle(bundleJSON, keyServiceVKDER); err != nil {
    return err // bad signature — do not admit
}
ks, _ := gc.NewKeyStore()
defer ks.Close()
if _, err := ks.Admit(id, bundleJSON); err != nil {
    return err
}
plaintext, _ := ks.Open("data/org-a/feed", envelope)

// Rotate: destroy-by-default forward secrecy (re-wraps nothing).
receiptJSON, _ := id.Rotate(nextScalar, false, bundlesJSON)
```

`bundleJSON` / `bundlesJSON` / `envelope` are `[]byte` of the wire JSON; `Identity` scalars are raw 32-byte P-256 scalars from the member's enrollment credential.

A complete, **CI-tested** runnable version of this flow — `identity → acquire → seal → verify → admit → open → rotate`, asserted end to end — is [`example_test.go`](example_test.go). It runs under `go test` (the `// Output:` block is checked), so it can never drift from the API:

```sh
cd bindings/go && go test -run Example ./...
```

## Errors

Errors are exported sentinels mapped 1:1 from the C ABI status codes; compare with `errors.Is`, never on message text (see [`errors.go`](errors.go)):

| Sentinel | Code | Meaning |
|---|---|---|
| `ErrUnsupportedSuite` | -1 | Suite not implemented — a **capability signal**, pre-key |
| `ErrDecrypt` | -2 | AEAD authentication failed (wrong key/gen/rs/topic, tamper) — never an oracle |
| `ErrUnwrap` | -3 | The wrapped DEK could not be recovered |
| `ErrBadSignature` | -4 | Bundle signature did not verify |
| `ErrNoSlot` | -5 | No slot / no held key (not in the audience) — a normal outcome |
| `ErrMalformed` | -6 | Malformed field (bad base64/length/key/JSON) |
| `ErrRotationRequired` | -7 | Seal cap reached — rotate to continue (write-side, never on the wire) |
| `ErrDisposed` | -101 | Handle already closed, never issued, or wrong type |

The **capability-vs-reject** distinction is load-bearing: `ErrUnsupportedSuite` signals "I don't know this suite" before any key operation; `ErrNoSlot` ("not in the audience") is a normal outcome, not a failure; the read-side rejects (`ErrDecrypt`, `ErrUnwrap`, `ErrBadSignature`) are advisory, never a decrypt oracle. `ErrBadScalar` (a wrong-length scalar, caught in Go before the ABI) wraps `ErrMalformed`.

## Security notes

- **Explicit `Close` is the only sanctioned release path.** `defer Close()` every `Identity`, `KeyStore`, and `StreamKey`; `Close` is what zeroizes the secret material (best-effort, ceiling in the design doc). Any use after `Close`, or a double `Close`, returns `ErrDisposed` and never touches freed state.
- **The finalizer never disposes.** A handle garbage-collected without `Close` is only **reported** (the `LeakLogger` hook fires and `Leaks()` increments) — the finalizer never disposes for you (ADR 1106 lifecycle contract), so a leaked handle's secret is zeroized only at process exit. Always `defer Close`.
- **Keys never enter Go memory.** Scalars and DEKs stay in the core behind opaque handles. The one exception is the raw 32-byte scalar you hand to `NewIdentity`/`Rotate`; the core copies and validates it, and you may zeroize your copy as soon as the call returns. **Plaintext and envelope `[]byte` are yours to manage** — the binding does not zeroize what it returns; overwrite them when done if they are sensitive.
- **Forward-secrecy defaults.** `Rotate(new, keepAccess=false, ...)` is the `Destroy` default: drop the old key, re-wrap nothing — full forward secrecy. `keepAccess=true` re-wraps your still-live DEKs to the new key locally and **reduces** forward secrecy for those generations; the returned receipt records exactly what changed (a structured, self-dating JSON fact, never time-relative prose).
- **Seal cap.** Each stream key enforces a cumulative message-count cap (default 2²⁴) below the GCM nonce-collision bound. `Remaining()` is the structured approach-to-cap fact; at the cap, `Seal` refuses with `ErrRotationRequired`. `SetCap` may only **tighten** (a value above the default returns `ErrMalformed`).

## Conformance

`go test ./...` runs the shared decrypt-direction vectors (the cross-language merge gate) through `verify → admit → open`, asserting byte-exact accept/reject parity and typed sentinels, plus write-side roundtrip, cap, rotate, and handle-lifecycle suites. One ABI nuance: the keystore indexes held DEKs by `(rs, gen)`, so the *wrong-generation* vector rejects as `ErrNoSlot` (no DEK held for that generation) rather than `ErrDecrypt` — both are rejects, so accept/reject parity holds and the advisory `reason` field is respected as advisory.
