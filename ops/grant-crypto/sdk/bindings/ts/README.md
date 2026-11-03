# grant-crypto — TypeScript/Node binding

A thin, hand-written TypeScript binding over the grant-crypto C ABI ([ADR 1106](../../../../../docs/adr/1106-grant-crypto-sdk-and-endpoint-forward-secrecy.md)). Same library as the Rust core and the Go/Python bindings; same [conformance vectors](../../../envelope/testdata/vectors/). The frozen public surface is [`docs/design/grant-crypto-sdk.md`](../../../../../docs/design/grant-crypto-sdk.md) — five nouns, six verbs, rendered idiomatically and nothing added.

**Endpoint forward secrecy, by construction:** the platform holds no keys; only members decrypt, in-process; the producer is not a key authority (`seal` takes no recipients). Private keys and DEKs live and die inside the Rust core — **they never enter JS memory**. Only wire bytes (bundle/envelope JSON) and plaintext cross the boundary.

## Prerequisites

Build the C ABI shared library once (the binding loads it at runtime):

```sh
cd ops/grant-crypto/sdk && cargo build -p grant-crypto-capi
```

This produces `target/debug/libgrant_crypto_capi.{dylib,so,dll}`. The binding finds it automatically via a relative dev-path search, or set `GRANT_CRYPTO_CAPI_LIB` to its full path.

Then, in this directory:

```sh
npm install      # installs koffi (runtime) + typescript/@types/node (dev)
npm run build    # tsc -> dist/
npm test         # node --test over the conformance + roundtrip + lifecycle suites
```

Node **20+**.

## 5-minute quickstart

```ts
import { Identity, KeyStore, StreamKey, verifyBundle } from "grant-crypto";

const enc = new TextEncoder();

// --- Producer: acquire a stream key from your visible bundles and seal ---
const id = Identity.create("example-set/prod", myScalar32);   // raw 32-byte P-256 scalar
const sk = StreamKey.acquire(bundlesJson, "example-set", id); // highest-gen slot for this id
const envelope = sk.seal("data/example-set/status", enc.encode("status snapshot"));
// publish `envelope` (bytes) on the "data/example-set/status" key-expression

// --- Consumer: verify, admit, open ---
verifyBundle(bundleJson, keyServiceSpkiDer);   // throws BadSignatureError on a bad bundle
const ks = KeyStore.create();
const hadSlot = ks.admit(id, bundleJson);      // false for an outsider is normal, not an error
const plaintext = ks.open("data/example-set/status", envelope);

// --- Always dispose (keys are zeroized on close) ---
sk.close(); ks.close(); id.close();
```

`bundleJson` / `bundlesJson` / `envelope` are `Uint8Array` of the wire JSON; `Identity` scalars are raw 32-byte P-256 scalars from the member's enrollment credential.

### `using` (explicit resource management)

Every handle implements `Symbol.dispose`, so `using` closes it at scope exit:

```ts
using id = Identity.create("m", scalar32);
using ks = KeyStore.create();
// ...id and ks are closed automatically when the block ends
```

## Errors

Every C ABI status maps 1:1 to a typed subclass of `GrantCryptoError` (with `.code`, the numeric C code). Branch with `instanceof`, never on message text:

| Error | Code | Meaning |
|---|---|---|
| `UnsupportedSuiteError` | -1 | Suite not implemented — a **capability signal**, pre-key |
| `DecryptError` | -2 | AEAD authentication failed (wrong key/gen/rs/topic, tamper) — never an oracle |
| `UnwrapError` | -3 | The wrapped DEK could not be recovered |
| `BadSignatureError` | -4 | Bundle signature did not verify |
| `NoSlotError` | -5 | No slot / no held key for this `(rs, gen)` |
| `MalformedError` | -6 | Malformed field (bad base64/length/key/JSON) |
| `RotationRequiredError` | -7 | Seal cap reached — rotate to continue (write-side, never on the wire) |
| `DisposedHandleError` | -101 | Handle already closed, never issued, or wrong type |

The **capability-vs-reject** distinction is preserved: `UnsupportedSuiteError` signals "I don't know this suite" before any key operation, distinct from the authenticated-failure rejects.

## Security notes

- **Explicit disposal.** Call `close()` (or use `using`) on every `Identity`, `KeyStore`, and `StreamKey`. `close()` zeroizes the underlying secret (best-effort) inside the core. A leaked handle that gets garbage-collected only emits a **warning** — the finalizer never disposes for you (ADR 1106 lifecycle contract).
- **Fail-closed handles.** Any use after `close()` throws `DisposedHandleError`; double-close throws the same (harmless).
- **Keys never enter JS memory.** Scalars and DEKs stay in the core behind opaque handles. **Plaintext `Uint8Array`s are yours to manage** — the binding does not zeroize what it returns to you; overwrite them when done if they are sensitive.
- **Forward secrecy defaults.** `rotate` defaults to `Destroy` (drop the old key, re-wrap nothing — full forward secrecy). `KeepAccess` re-wraps your still-live DEKs to the new key locally and **reduces** forward secrecy for those generations; the returned receipt records exactly what changed.
- **Seal cap.** Each stream key enforces a cumulative message-count cap (default 2²⁴) below the GCM nonce-collision bound. `remaining()` is the structured approach-to-cap fact; at the cap, `seal` refuses with `RotationRequiredError`. You may `setCap()` **down** (tighten), never up.

## Dependency pin

`koffi` is pinned to the **exact** version `2.16.3`. koffi was LGPL through 2.3.8 and relicensed to **MIT at 2.3.9**; we require a version at or after that boundary so this binding carries no copyleft obligation. `2.16.3` is the last release of the mature 2.x line (3.x is a ground-up rewrite with a different API), making it the conservative, MIT, battle-tested choice. `typescript` and `@types/node` are dev-only and exact-pinned.

## Conformance

`npm test` runs the shared decrypt-direction vectors (the cross-language merge gate) through `verify → admit → open`, asserting byte-exact accept/reject parity and typed errors, plus write-side roundtrip, cap, rotate, and lifecycle coverage. A note on one ABI nuance: the keystore indexes held DEKs by `(rs, gen)`, so the *wrong-generation* vector rejects as `NoSlotError` (no DEK held for that generation) rather than `DecryptError` — both are rejects, so accept/reject parity holds and the advisory `reason` field is respected as advisory.
