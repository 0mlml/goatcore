# grant-crypto wire format

The normative, language-neutral description of what travels on the wire, so independent implementations (the Go reference here, a future Rust implementation at the consuming peers, others) interoperate and cannot silently drift. See `docs/design/grant-keyed-encryption.md` for the mechanism and [`CRYPTO-REVIEW.md`](CRYPTO-REVIEW.md) for the review scope.

## Suites (crypto-agility)

Every structure carries a `suite` identifier, **bound into the AEAD additional-authenticated-data (AAD)** of the structure it labels, so a suite cannot be silently downgraded and an implementation that does not recognise a suite fails closed rather than misparsing.

| Suite id | Status | Payload AEAD | DEK wrap |
|---|---|---|---|
| `v1-ecdh-p256` | **implemented** | AES-256-GCM | ephemeral-static ECDH P-256 → HKDF-SHA256 → AES-256-GCM |
| `v2-x25519-mlkem768` | **provisional (Rust, inert)** | AES-256-GCM | hybrid X25519 + ML-KEM-768 → KDF → AES-256-GCM |

**v2 hybrid wrap ([`sdk/core/src/pqc.rs`](sdk/core/src/pqc.rs)).** The DEK wrap runs *both* a classical X25519 ephemeral-static exchange and an ML-KEM-768 encapsulation, combining the two shared secrets so confidentiality holds if either KEM is unbroken (harvest-now-decrypt-later defence). The v2 wrapped entry carries `{ epk_x25519 (32B), mlkem_ct (1088B), nonce, wct }` and needs a **hybrid recipient key** (X25519 point + ML-KEM encapsulation key) — a PKI extension over today's EC SVIDs. **Combiner — X-Wing** (external review; implemented). The v2 wrapped entry carries `{ epk_x25519 (32B), mlkem_ct (1088B), nonce, wct }` and the KEK is derived by X-Wing (`draft-connolly-cfrg-xwing-kem`): `xwing_ss = SHA3-256(label ‖ ss_M ‖ ss_X ‖ ct_X ‖ pk_X)` (label = the 6 bytes of `\.//^\`), then `KEK = HKDF-SHA256(xwing_ss, info = "grant-crypto/dek-wrap/v2")`. Implemented directly over `ml-kem` + `x25519-dalek` (the `x-wing` crate needs rand_core 0.10, which conflicts with the aes-gcm/p256 stack on rand_core 0.6; construction is byte-identical, migrate to the crate later). Inert; round-trip-tested (`tests/pqc_roundtrip.rs`).

An unknown or absent suite is a hard, fail-closed error (`ErrUnsupportedSuite`) — distinct from a decryption failure, and returned before any key operation, so it leaks nothing about keys.

## Structures (JSON; byte fields are base64-std)

### Envelope — a sealed stream message
```
{ "suite": "v1-ecdh-p256", "rs": "<release-set>", "gen": <uint64>,
  "nonce": <12 bytes>, "ct": <AES-256-GCM ciphertext || 16-byte tag> }
```
`AAD = len8(suite)‖suite ‖ len8(rs)‖rs ‖ be64(gen) ‖ additional`, where `additional` is the data topic (key expression) when the caller binds it, else empty. `len8` is an 8-byte big-endian length prefix; `be64` is 8-byte big-endian. Length-prefixing makes the concatenation unambiguous.

### WrappedDEK — the DEK sealed to one member
```
{ "suite": "v1-ecdh-p256", "epk": <ephemeral public point, uncompressed>,
  "nonce": <12 bytes>, "wct": <AES-256-GCM(KEK) of the 32-byte DEK || tag> }
```
`KEK = HKDF-SHA256(secret = ECDH(eph, recipient), salt = "", info = "grant-crypto/dek-wrap/v1", L = 32)`.
`AAD = len8(suite)‖suite ‖ epk ‖ recipientPublicPoint` — binding the suite and **both** public keys, so a wrap cannot be re-presented for a different recipient or downgraded.

### KeyBundle — published on `keys/<release-set>/<generation>`
```
{ "suite": "v1-ecdh-p256", "rs": "<release-set>", "gen": <uint64>,
  "members": { "<memberId>": WrappedDEK, ... },
  "signer": "<key-service identity>", "sig": <signature>,
  "signer_svid": "<key-service SVID, PEM>"  // OPTIONAL; NOT in the transcript
}
```
Built fail-closed: any per-member wrap failure aborts the whole bundle (never a partial bundle).

**Authentication (decision §7).** The key-service signs the bundle; a member MUST verify before trusting any wrapped key. For `v1`, the signature is **ECDSA-P256 over SHA-256** of the canonical transcript (a post-quantum suite moves this to ML-DSA):
```
transcript =
  len8("grant-crypto/bundle-sig/v1") ‖ "grant-crypto/bundle-sig/v1" ‖
  len8(suite)‖suite ‖ len8(rs)‖rs ‖ be64(gen) ‖ len8(signer)‖signer ‖
  be64(N) ‖  for each memberId in ascending order:
             len8(id)‖id ‖ len8(wrap.suite)‖wrap.suite ‖
             len8(wrap.epk)‖wrap.epk ‖ len8(wrap.nonce)‖wrap.nonce ‖ len8(wrap.wct)‖wrap.wct
```
`sig` itself is excluded from the transcript it covers. `signer` binds the claimed identity, so a signature cannot be moved to another signer; the verifier maps `signer` to a trusted public key. `signer_svid` (decision §7 rev 2) is an OPTIONAL, **self-describing** carrier of that key: the key-service's own SVID (PEM), so a member validates it against the SPIRE root + expected key-service SPIFFE ID and obtains the verifying key without a side channel — and an old cached bundle stays verifiable after the key-service rotates its identity. It is deliberately **excluded from the transcript**, so its presence or absence never changes `sig` or the conformance vectors; a member MAY instead be handed the verifying key out of band. Verification failure, an absent signature, or the wrong key is `ErrBadSignature` — the member treats the bundle as untrusted and unwraps nothing.

## Conformance

An implementation is conformant if, for a fixed suite and fixed key material, it (a) produces envelopes/wraps/bundles another implementation opens, and (b) opens those another implementation produced, with identical accept/reject decisions on the negative cases (wrong key, wrong generation, wrong release-set, wrong topic, tampered bytes, unknown suite, bad signature).

The **canonical decrypt-direction test vectors** live in [`envelope/testdata/vectors/`](envelope/testdata/vectors/) (schema: the [`conformance`](conformance/) package): committed fixed keys + produced artifacts + accept/reject cases. The **consumer order** every implementation must follow is `verify → slot → unwrap → open`:

1. if a trust identity is supplied, verify the bundle signature — reject on failure;
2. find the member's slot — reject if absent (not in the audience);
3. unwrap the DEK with the member key — reject on failure;
4. open the envelope with the case key-expression — reject on failure.

Both implementations validate against these vectors: the Go reference (`go test ./envelope -run Conformance`) and the **Rust production reference** ([`sdk/core/`](sdk/core/), `cargo test`) — the Rust read side is byte-compatible and passes all cases. Regenerate only when the format changes: `go run ./cmd/vectorgen ./envelope/testdata/vectors`.
