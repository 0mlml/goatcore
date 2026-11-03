# grant-crypto — human crypto review brief

**Status:** requested.

## Why this review, why now

`ops/grant-crypto` (package `envelope`) is the crypto core for **grant-keyed payload encryption** — the mechanism that will enforce per-stream *read* control on the machine plane (see `docs/design/grant-keyed-encryption.md`, ADR 1094). It is CI-validated (15 unit tests + a live-Zenoh transport proof, `go vet` + strict `golangci-lint` clean).

This review is the gate before **E1** wires it to enforce.

## What to review

Small, self-contained, standard-library-only Go (~400 LOC of logic + tests):

| File | Primitive | Lines |
|---|---|---|
| [`envelope/envelope.go`](envelope/envelope.go) | payload `Seal`/`Open` (AES-256-GCM, AEAD-bound context) | ~140 |
| [`envelope/dek.go`](envelope/dek.go) | `WrapDEK`/`UnwrapDEK` (ECDH→HKDF→AES-GCM), `RecipientFromCert` | ~135 |
| [`envelope/bundle.go`](envelope/bundle.go) | `KeyBundle` / `NewBundle` / topic helper | ~65 |
| [`envelope/envelope_test.go`](envelope/envelope_test.go) | the 15 tests (adversarial cases) — review as *evidence*, and for gaps | ~ |
| [`cmd/gce`](cmd/gce/) + [`transport-proof/`](transport-proof/) | the proof harness (not security-critical itself; confirms the above compose over the wire) | — |

## What the crypto must guarantee (and what it explicitly does not)

**Goals (in scope for this review):**
1. **Confidentiality to an audience.** Only a member holding a wrapped slot in `keys/<rs>/<gen>` can recover the DEK and decrypt the stream. A non-member (no slot, or a revoked slot) cannot.
2. **Generation is the revocation lever.** A key from generation *N* must not decrypt a message labelled generation *N+1* (or vice-versa) — the AEAD binding is what makes "revoke = rotate to a new generation" real.
3. **No cross-audience / cross-topic reuse.** A ciphertext sealed for one release-set (or, when set, one key-expr) must not open under another's key.
4. **Fail-closed.** A partial or malformed bundle must never silently drop a member or hand back plaintext.

**Owner decisions** — resolved after this brief's first draft; the checklist below reflects them. Full text in `grant-keyed-encryption.md` §7 (6–9):
- **Bundle authenticity is REQUIRED** — ACL (write on `keys/**` restricted to the key-service) **plus** a key-service **signature** on the bundle, verified before unwrap.
- **PQC not in the first suite, but a migration path is REQUIRED** — crypto-agility (suite ID in the AEAD-bound format) now; hybrid ML-KEM wrap later.
- **Nonce:** random nonces + a **message-count rotation cap** (not counter nonces).
- **Language: OPEN, leaning Rust** — decide at E1 kickoff (§7 item 9). If the core moves to Rust, some items below change form (zeroization becomes available; the format spec + test vectors become the durable artifact).

**Non-goals (do NOT re-litigate here; noted so they aren't mistaken for flaws):**
- **Topic granularity, not field/row.** Machine-plane read control reaches release-set granularity and no finer, by design (`grant-keyed-encryption.md` §0).
- **Not post-quantum *in the first suite*** — but a **migration path is now required** (decision 7). Classical ECDH P-256 / AES-GCM ships first; the review should confirm the format carries an AEAD-bound **suite identifier** so a hybrid ML-KEM wrap can be added by generation without a flag-day.
- **Memory zeroization** — Go's GC makes reliable key-zeroing impractical; DEKs/shared-secrets may linger on the heap. **Tied to the language decision (9):** accepted if the core stays Go; *resolvable* if it moves to Rust (`zeroize`). Flag if disqualifying for the threat model under Go.
- **Flat O(N) wrapping.** One wrap per member; the key-tree (MLS/TreeKEM) upgrade is deferred to E4 past low-thousands (§5).

## The constructions, as built (verify these against the code)

- **Payload envelope** — `AES-256-GCM(DEK, nonce, plaintext, AAD)` where `AAD = len(releaseSet)‖releaseSet ‖ uint64(gen) ‖ additional`. `additional` is the Zenoh key-expr when supplied (topic-binding). Nonce = 12 random bytes per `Seal`. `Open` returns one opaque error for any failure.
- **DEK wrap** — ephemeral-static ECDH: a fresh ephemeral key on the recipient's curve agrees with the recipient's static SVID key; `KEK = HKDF-SHA256(sharedSecret, salt=nil, info="grant-crypto/dek-wrap/v1", 32B)`; `AES-256-GCM(KEK, nonce, DEK, AAD = ephemeralPub ‖ recipientPub)`. Recipient key is **EC/P-256 only** (`RecipientFromCert` rejects RSA/non-EC).
- **Bundle** — `map[memberId]WrappedDEK` on `keys/<rs>/<gen>`, built in sorted-id order; **any wrap failure aborts the whole bundle** (fail-closed).

## Review checklist — the specific questions

**AEAD / envelope**
- [ ] Is the `AAD` encoding unambiguous? (release-set is length-prefixed; confirm `("ab","c")` can't collide with `("a","bc")`.)
- [ ] **Nonce strategy under volume — DECIDED: random + rotation cap (§7.8).** 96-bit random nonces are reused across every message under one `(rs, gen)` DEK; safe to ~2³² messages (birthday bound, NIST SP 800-38D). The decision is a **message-count rotation cap**, not counter nonces (a shared release-set DEK makes per-publisher counter partitioning fragile). **Verify:** E1 wires a cumulative-message-count rotation trigger with margin, and confirm the realistic per-stream throughput sets a safe cap. Confirm the AAD/nonce handling is correct for the random scheme.
- [ ] Generation + release-set are authenticated (in AAD) — confirm a re-labelled generation fails `Open`. (Test covers it; confirm the binding, not just the test.)

**DEK wrap (ECDH/HKDF/GCM)**
- [ ] ECIES-style construction sound? Ephemeral-static ECDH, both public keys bound as AAD (anti-KCI / anti-swap). Is binding *both* pubkeys sufficient, or is a transcript/context string also wanted?
- [ ] **HKDF salt = nil** (→ zero salt). Acceptable for a high-entropy ECDH secret with a versioned `info`? Or should a salt / additional context be introduced?
- [ ] Point validation: `ecdh.PublicKey`/`NewPublicKey` on-curve checks relied on — confirm no invalid-curve exposure.
- [ ] **Bundle authenticity — DECIDED + BUILT (§7.6).** `SignBundle`/`VerifyBundle` (sign.go): the key-service signs a canonical, length-prefixed transcript over `(domain, suite, rs, gen, signer, sorted members incl. each wrapped slot)` with **ECDSA-P256/SHA-256**; members verify before unwrapping; `ErrBadSignature` on any failure; live-proven over Zenoh (member verifies, wrong-signer denied). **Review the construction:** transcript completeness (dropped/added member, tampered slot, tampered signer/gen all covered), domain separation from the payload AEAD, and the signer→trusted-key binding. Crypto-agility: v1 = ECDSA; a future ML-DSA signature lands under the `v2` suite. Full spec in [`WIRE-FORMAT.md`](WIRE-FORMAT.md).

**Bundle / API**
- [ ] Fail-closed on partial wrap confirmed; no path returns a partial bundle.
- [ ] Errors are opaque (no padding/format oracle) — `ErrDecrypt`/`ErrUnwrap` reveal only pass/fail.
- [ ] RNG is `crypto/rand` everywhere (DEK, nonces, ephemeral keys) — confirm no `math/rand`.

**Cross-cutting**
- [ ] Memory zeroization absence acceptable for the threat model? (see non-goals)
- [ ] Any test gap: missing negative case, missing malleability check, missing curve/format edge?

## How to run it

```bash
cd ops/grant-crypto
go test -race -count=1 ./...           # 15 unit tests + the CLI round-trip
./transport-proof/run.sh               # the live-Zenoh end-to-end proof (pulls eclipse-zenoh==1.9.0)
```

## Sign-off

Please record the outcome here (or in a linked note) so E1 has a clear gate:

- **Reviewer:** ______  **Date:** ______
- **Verdict:** ☐ sound as-is ☐ sound with the fixes below ☐ needs rework
- **Findings / required changes before E1:** ______
- **Design questions answered (nonce cap, bundle signing):** ______

## References

- `docs/design/grant-keyed-encryption.md` — the mechanism (§1 envelope, §3 key service, §4 rotation/revocation, §5 scale, §7 open decisions, §8 build plan).
- ADR 1094 (grant-keyed payload encryption) · ADR 1076 (machine-plane parity) · ADR 0412 (SPIFFE/SPIRE — the SVID keys wrapped to).
- `docs/project/data-access-program.md` — where this sits in the program (Phase E, the gate before E1).
