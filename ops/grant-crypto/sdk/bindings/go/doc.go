// Package grantcrypto is the idiomatic Go binding for the grant-crypto SDK — a thin,
// hand-written wrapper over the audited C ABI (ADR 1106). It renders the frozen
// five-noun / six-verb API (docs/design/grant-crypto-sdk.md) in Go, adding, removing,
// and reinterpreting nothing.
//
// grant-crypto is grant-keyed, endpoint-enforced envelope encryption: a key service
// mints a data-encryption key (DEK), wraps it to each authorized member, signs a
// bundle, and drops the DEK — it holds no keys and can never re-wrap. Producers seal
// under a stream key unwrapped from their own bundle slot; members open with the DEK
// they unwrap from theirs. Forward secrecy is the member's own key rotation.
//
// # Thirty-second example
//
//	// Producer: acquire a seal-capable key from the signed bundles, seal a message.
//	prod, _ := grantcrypto.NewIdentity("coalition-air/prod", prodScalar32)
//	defer prod.Close()
//	sk, _ := grantcrypto.AcquireStreamKey(bundlesJSON, "coalition-air", prod)
//	defer sk.Close()
//	envelope, _ := sk.Seal("data/coalition-air/cop", []byte("brigade COP snapshot"))
//
//	// Consumer: verify the bundle, admit your slot, open the message.
//	con, _ := grantcrypto.NewIdentity("coalition-air/con", conScalar32)
//	defer con.Close()
//	if err := grantcrypto.VerifyBundle(bundleJSON, keyServiceVKDER); err != nil {
//		return err // bad signature — do not admit
//	}
//	ks, _ := grantcrypto.NewKeyStore()
//	defer ks.Close()
//	if _, err := ks.Admit(con, bundleJSON); err != nil {
//		return err
//	}
//	plaintext, err := ks.Open("data/coalition-air/cop", envelope)
//
// # What crosses the boundary
//
// Only wire structures cross as bytes: bundle, envelope, and rotation-receipt JSON, and
// the plaintext payloads you seal and open. Key material never enters Go memory — an
// Identity, KeyStore, and StreamKey are opaque handles into the core, which holds the
// scalars and DEKs and zeroizes them on dispose. The one exception is the raw 32-byte
// private scalar you hand to NewIdentity and Rotate; the core copies and validates it,
// and you may zeroize your copy as soon as the call returns.
//
// # Lifecycle: explicit Close
//
// Every handle type (Identity, KeyStore, StreamKey) must be closed explicitly with
// Close — that is the only sanctioned release path, and it is what zeroizes the secret
// material it holds. Close is fail-closed and idempotent-harmless: a second Close, or
// any use after Close, returns ErrDisposed and never touches freed state (the core
// never recycles handle ids). A finalizer is installed only to REPORT a leak (a handle
// garbage-collected without Close) via the LeakLogger hook and the Leaks counter; it
// never disposes on your behalf, so relying on it leaves secrets un-zeroized until the
// process exits. Always defer Close.
//
// # Errors
//
// Errors are exported sentinels mapped 1:1 from the C ABI status codes and compared
// with errors.Is (see errors.go). The taxonomy preserves the design's capability-vs-reject
// distinction: ErrUnsupportedSuite is a pre-key capability signal, ErrRotationRequired is
// the write-side seal-cap refusal, ErrNoSlot ("not in the audience") is a normal outcome,
// and the read-side rejects (ErrDecrypt, ErrUnwrap, ErrBadSignature) are advisory — never
// a decrypt oracle.
//
// # Secure defaults: the seal cap and rotation
//
// A StreamKey enforces a cumulative message-count cap per key (default 2^24) with
// margin below the AEAD nonce-safety bound. Seal refuses with ErrRotationRequired once the cap is
// reached; Remaining surfaces the approach as a structured fact well before the refusal,
// and SetCap may only tighten it (never loosen). Rotate defaults to Destroy — it drops
// the retired key and re-wraps nothing, which is the forward secrecy; KeepAccess is an
// explicit opt-out that re-wraps the member's still-live DEKs to the new key locally and
// is labeled as reducing forward secrecy for those generations. Every rotation returns a
// structured, self-dating receipt (JSON), never time-relative prose.
//
// # Building
//
// The binding links the core statically via cgo, so no runtime shared-library path is
// needed. Build the C ABI static library first:
//
//	cd ops/grant-crypto/sdk && cargo build -p grant-crypto-capi
//
// then build/test this package normally (CGO_ENABLED=1, the default). The cgo directives
// resolve libgrant_crypto_capi.a under ../../target/debug relative to the package; a
// release/packaged build overrides the LDFLAGS path to the shipped, checksum-pinned
// native. On Linux the core additionally needs -lm -ldl -lpthread (wired in the cgo
// directives); macOS needs nothing beyond libSystem.
package grantcrypto
