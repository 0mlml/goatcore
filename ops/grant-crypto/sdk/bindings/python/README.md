# grant-crypto — Python binding

Endpoint forward-secrecy for the goat data fabric ([ADR 1106](../../../../../docs/adr/1106-grant-crypto-sdk-and-endpoint-forward-secrecy.md)),
as an idiomatic Python package over the audited C ABI. Pure stdlib — `ctypes`,
zero pip dependencies. The frozen API ([`grant-crypto-sdk.md`](../../../../../docs/design/grant-crypto-sdk.md))
renders as five classes/functions; nothing is added, removed, or reinterpreted.

## Install / locate the native library

The package calls the `grant-crypto-capi` shared library. It is found, in order:

1. `$GRANT_CRYPTO_CAPI_LIB` — an explicit path to the library file **or** the
   directory containing it;
2. the Cargo dev build under `../../target/{debug,release}/` (i.e. the SDK
   workspace's `ops/grant-crypto/sdk/target/`);
3. the platform library bundled next to the package (a wheel-shipped native).

For local development, build it once from the SDK workspace:

```bash
cd ops/grant-crypto/sdk && cargo build -p grant-crypto-capi
```

If the library can't be found you get a clear `RuntimeError` naming every path
that was tried and the build command.

## Five-minute quickstart

The one obvious path — a consumer verifies a bundle, admits its slot, and opens a
message:

```python
import grant_crypto as gc

# `member_scalar` is your member's raw 32-byte P-256 enrollment scalar (bytes).
# `bundle_json` and `envelope_json` are wire bytes you received off the fabric.
# `key_service_spki_der` is the key service's verifying key (SPKI DER bytes).

with gc.Identity("example-set/con", member_scalar) as ident, gc.KeyStore() as ks:
    gc.verify_bundle(bundle_json, key_service_spki_der)   # authenticate first
    if ks.admit(ident, bundle_json):                      # unwrap our slot
        plaintext = ks.open("data/example-set/feed", envelope_json)
        print(plaintext)
```

A producer (a member of the release-set) acquires a stream key and seals:

```python
with gc.Identity("example-set/prod", producer_scalar) as ident:
    with gc.StreamKey.acquire(bundles_json, "example-set", ident) as sk:
        envelope_json = sk.seal("data/example-set/feed", b"example snapshot")
        # publish envelope_json on the topic; sk.remaining tells you seals left
```

Rotating a member leaf (forward-secrecy hygiene):

```python
with gc.Identity("example-set/con", member_scalar) as ident:
    receipt = ident.rotate(new_scalar)             # Destroy (default): full FS
    # receipt is a structured, self-dating dict:
    # {"policy": "destroy", "now_unreadable_generations": N,
    #  "rewrapped_generations": 0, "timestamp_rfc3339": "...Z"}
    # ident now holds the new key.
```

## API

| Symbol | Purpose |
| --- | --- |
| `Identity(member_id, scalar32)` | member id + private key; `.rotate(new_scalar32, keep_access=False, bundles_json=b"[]")` |
| `KeyStore()` | `.admit(identity, bundle_json) -> bool`, `.open(keyexpr, envelope_json) -> bytes` |
| `StreamKey.acquire(bundles_json, rs, identity)` | `.seal(keyexpr, plaintext) -> bytes`, `.remaining`, `.set_cap(n)` |
| `verify_bundle(bundle_json, vk_spki_der)` | raises on bad signature / malformed |

All wire arguments are **bytes** (bundle / envelope / receipt JSON, plaintext,
key material). `keyexpr`, `member_id`, and `rs` are `str`.

### Errors

One hierarchy, mapping the C status codes 1:1; `GrantCryptoError` is the base.
The capability-vs-reject distinction is preserved:

| Exception | Code | Meaning |
| --- | --- | --- |
| `UnsupportedSuiteError` | -1 | suite not implemented (capability signal, pre-key) |
| `DecryptError` | -2 | AEAD auth failed (wrong key/gen/rs/topic, tamper) |
| `UnwrapError` | -3 | wrapped DEK could not be recovered |
| `BadSignatureError` | -4 | bundle signature did not verify |
| `NoSlotError` | -5 | no slot / no held key (not in the audience) |
| `MalformedError` | -6 | bad base64/length/key/JSON, or cap request above default |
| `RotationRequiredError` | -7 | seal cap reached — rotate before sealing again |
| `DisposedHandleError` | -101 | use-after-close / wrong-type handle |
| `NullArgError` | -100 | required pointer was NULL (also `ValueError`) |
| `BadUtf8Error` | -102 | string arg not valid UTF-8 (also `ValueError`) |
| `InternalError` | -103 | internal invariant failed (also `RuntimeError`) |

## Security notes

- **Explicit disposal is the only sanctioned release path.** Use `with` or call
  `.close()` on every `Identity` / `KeyStore` / `StreamKey`. A handle's native
  material (private scalar, held DEKs, stream DEK, buffer contents) is zeroized on
  `.close()` (best-effort). Any use after close raises `DisposedHandleError`.
- **The finalizer never disposes.** If an object is garbage-collected without
  `.close()`, `__del__` emits a `ResourceWarning` about the leaked handle — it
  does **not** dispose (per the ABI's fail-closed lifecycle). Warning errors are
  suppressed so shutdown-time warning failures cannot escape the finalizer.
  A warnings-as-errors filter therefore does not make these leaks fatal.
  Test explicit disposal and capture `ResourceWarning` when testing leak reporting.
- **Secure defaults, not options.** `rotate` defaults to `Destroy` (drop the old
  key, re-wrap nothing — full forward secrecy); `keep_access=True` re-wraps your
  still-live DEKs to the new key locally and is labeled as *reducing* forward
  secrecy. The seal path enforces a per-key message-count cap (default 2²⁴) below
  the GCM nonce-birthday bound; `remaining` surfaces the approach as a structured
  fact, and at the cap `seal` refuses with `RotationRequiredError`. `set_cap` may
  only *tighten* (a request above the default raises `MalformedError`).

## What crosses the Python boundary

- **Never:** key material as a managed object. Identities, keystores, and stream
  keys are opaque native handles; DEKs and private scalars stay inside the native
  library. The scalar you pass to `Identity(...)` is handed straight through and
  not retained by the binding.
- **Yes:** wire bytes (bundle / envelope / receipt JSON) and **plaintext**.

### Honest zeroization note

Plaintext returned by `open()` and envelopes returned by `seal()` are ordinary
Python `bytes`. Python `bytes` are immutable and may be copied by the runtime;
**the binding cannot zeroize them for you.** Treat decrypted plaintext as
caller-managed sensitive data — minimize copies, drop references promptly. The
native side zeroizes its own copy when the `gc_buf` is disposed (which the binding
does immediately after copying out). Key material, by contrast, never enters
Python at all.

## Tests

Stdlib `unittest`, runnable from this directory:

```bash
python3 -m unittest discover -s tests -v
```

The conformance suite (`tests/test_conformance.py`) runs every committed
decrypt-direction vector (`../../../envelope/testdata/vectors/vectors.json`)
through `verify → admit → open` with byte-for-byte accept/reject parity — the same
gate the Rust core and the other bindings pass.
