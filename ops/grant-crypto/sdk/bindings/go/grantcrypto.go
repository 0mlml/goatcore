package grantcrypto

/*
#cgo CFLAGS: -I${SRCDIR}/../../capi/include
#cgo darwin LDFLAGS: ${SRCDIR}/../../target/debug/libgrant_crypto_capi.a
#cgo linux LDFLAGS: ${SRCDIR}/../../target/debug/libgrant_crypto_capi.a -lm -ldl -lpthread
#include <grant_crypto.h>
#include <stdlib.h>
*/
import "C"

import (
	"math"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"
)

// checkNoNUL rejects a string argument containing a NUL byte. The C ABI reads these as
// NUL-terminated (via CStr), so an interior NUL would silently truncate the argument —
// reject it client-side before the FFI call. Returns ErrEmbeddedNUL (a Malformed-class
// error) so callers matching on ErrMalformed still catch it.
func checkNoNUL(s string) error {
	if strings.IndexByte(s, 0) >= 0 {
		return ErrEmbeddedNUL
	}
	return nil
}

// ABIVersion returns the core's monotone C ABI version. The core and this binding
// ship in lockstep from one tag (SONAME-pinned); there is no cross-version promise.
func ABIVersion() uint32 { return uint32(C.gc_abi_version()) }

// ---- leak accounting (a finalizer may REPORT a leak, never dispose — the design
// mandates explicit Close as the only sanctioned release path) ----

var leakCount int64

// LeakLogger, if set, is called from a finalizer when a handle is garbage-collected
// without Close having been called. It must not call back into this package. It is a
// diagnostic only: the finalizer never disposes the leaked handle (doing so would make
// release nondeterministic and could race a concurrent user), so the core's zeroize
// runs only at process exit for a leaked handle.
var LeakLogger func(kind string)

// Leaks returns the number of handles finalized without an explicit Close since the
// process started. A nonzero value is a bug in the caller, not the library.
func Leaks() int64 { return atomic.LoadInt64(&leakCount) }

func reportLeak(kind string) {
	atomic.AddInt64(&leakCount, 1)
	if f := LeakLogger; f != nil {
		f(kind)
	}
}

// ---- handle: the shared opaque-u64 lifecycle, fail-closed ----

// handle wraps a core registry id. Close is idempotent-harmless (a second Close, or
// any use after Close, yields ErrDisposed); the core never recycles ids, so a closed
// handle is permanently invalid and can never alias a live object.
type handle struct {
	mu      sync.Mutex
	id      C.uint64_t
	closed  bool
	kind    string
	dispose func(C.uint64_t) C.int32_t
}

func newHandle(id C.uint64_t, kind string, dispose func(C.uint64_t) C.int32_t, owner any) *handle {
	h := &handle{id: id, kind: kind, dispose: dispose}
	runtime.SetFinalizer(owner, func(any) {
		h.mu.Lock()
		leaked := !h.closed
		h.mu.Unlock()
		if leaked {
			reportLeak(kind)
		}
	})
	return h
}

// value returns the live id for a call, or ErrDisposed if the handle is closed. The
// caller must hold no assumption about the id beyond the immediate call.
func (h *handle) value() (C.uint64_t, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return 0, ErrDisposed
	}
	return h.id, nil
}

// close disposes the underlying core object exactly once. A second call is a harmless
// ErrDisposed.
func (h *handle) close(owner any) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return ErrDisposed
	}
	code := h.dispose(h.id)
	h.closed = true
	runtime.SetFinalizer(owner, nil)
	return errFromCode(code)
}

// ---- cgo argument helpers ----

var zeroByte byte

// bytePtr returns a *C.uint8_t for a byte slice, using a stable dummy for an empty
// slice so the ABI's non-null check passes on a legitimately empty payload. The
// pointer is valid only for the duration of the enclosing cgo call; callers pair it
// with runtime.KeepAlive on the slice.
func bytePtr(b []byte) *C.uint8_t {
	if len(b) == 0 {
		return (*C.uint8_t)(unsafe.Pointer(&zeroByte))
	}
	return (*C.uint8_t)(unsafe.Pointer(&b[0]))
}

// bufBytes copies a gc_buf handle's contents into a fresh Go slice and immediately
// disposes the buffer (zeroizing the core copy). Payload bytes (plaintext, envelope,
// receipt JSON) are allowed to cross the boundary; we never leave a core buffer alive.
// A length beyond the C int range (2 GiB) is refused rather than truncated by the cast.
func bufBytes(h C.uint64_t) ([]byte, error) {
	defer C.gc_buf_dispose(h)
	p := C.gc_buf_ptr(h)
	n := C.gc_buf_len(h)
	if p == nil || n == 0 {
		return []byte{}, nil
	}
	if uint64(n) > uint64(math.MaxInt32) {
		return nil, ErrBufferTooLarge
	}
	return C.GoBytes(unsafe.Pointer(p), C.int(n)), nil
}

// ---- Identity (a member's id + private key material; unwrap-capable) ----

// Identity is a member's id plus its private key material, held as an opaque handle
// inside the core — the raw scalar never enters Go memory beyond the NewIdentity call
// that hands it across. Unwrap-capable (admit, acquire) and rotatable. Call Close when
// done to zeroize the scalar.
type Identity struct{ h *handle }

// NewIdentity constructs a member identity from its id and a raw 32-byte P-256 private
// scalar (the enrollment-credential form). The scalar is validated in the core;
// ErrMalformed (or ErrBadScalar for a wrong length) if it is not a valid key. The
// caller may zeroize its own copy of scalar32 after this returns.
func NewIdentity(memberID string, scalar32 []byte) (*Identity, error) {
	if len(scalar32) != 32 {
		return nil, ErrBadScalar
	}
	if err := checkNoNUL(memberID); err != nil {
		return nil, err
	}
	cID := C.CString(memberID)
	defer C.free(unsafe.Pointer(cID))
	var out C.uint64_t
	code := C.gc_identity_new(cID, bytePtr(scalar32), &out)
	runtime.KeepAlive(scalar32)
	if err := errFromCode(code); err != nil {
		return nil, err
	}
	id := &Identity{}
	id.h = newHandle(out, "Identity", func(h C.uint64_t) C.int32_t { return C.gc_identity_dispose(h) }, id)
	return id, nil
}

// Close disposes the identity, zeroizing its scalar. Idempotent-harmless: a second
// call returns ErrDisposed.
func (id *Identity) Close() error { return id.h.close(id) }

// Rotate re-keys this identity's leaf to newScalar32 in place: on success the handle
// now holds the new scalar and the old one is zeroized. keepAccess=false is the
// Destroy default (drop the old key, re-wrap nothing — full forward secrecy);
// keepAccess=true re-wraps the member's still-live DEKs from bundlesJSON (a JSON array
// of bundles) to the new key locally, reducing forward secrecy for those generations.
// Returns the rotation receipt JSON (a structured, self-dating fact); under keepAccess
// the re-wrapped entries ride under a "rewrapped" key of the same object.
func (id *Identity) Rotate(newScalar32 []byte, keepAccess bool, bundlesJSON []byte) (receiptJSON []byte, err error) {
	if len(newScalar32) != 32 {
		return nil, ErrBadScalar
	}
	hid, err := id.h.value()
	if err != nil {
		return nil, err
	}
	var out C.uint64_t
	code := C.gc_rotate(
		hid,
		bytePtr(newScalar32),
		C.bool(keepAccess),
		bytePtr(bundlesJSON),
		C.uintptr_t(len(bundlesJSON)),
		&out,
	)
	runtime.KeepAlive(newScalar32)
	runtime.KeepAlive(bundlesJSON)
	if err := errFromCode(code); err != nil {
		return nil, err
	}
	return bufBytes(out)
}

// ---- KeyStore (the consumer: admit slots, open messages) ----

// KeyStore holds the DEKs a member has unwrapped, keyed by (release-set, generation).
// Call Close to zeroize every held DEK.
type KeyStore struct{ h *handle }

// NewKeyStore creates an empty keystore.
func NewKeyStore() (*KeyStore, error) {
	var out C.uint64_t
	code := C.gc_keystore_new(&out)
	if err := errFromCode(code); err != nil {
		return nil, err
	}
	ks := &KeyStore{}
	ks.h = newHandle(out, "KeyStore", func(h C.uint64_t) C.int32_t { return C.gc_keystore_dispose(h) }, ks)
	return ks, nil
}

// Close disposes the keystore, zeroizing all held DEKs.
func (ks *KeyStore) Close() error { return ks.h.close(ks) }

// Admit unwraps this identity's slot in a (previously verified) bundle and retains the
// DEK for the bundle's (rs, gen). hadSlot reports whether the member had a slot: a
// normal false for an outsider (nothing retained, not an error). An error is returned
// only for a malformed slot/bundle or a disposed handle. Verify the bundle first with
// VerifyBundle.
func (ks *KeyStore) Admit(id *Identity, bundleJSON []byte) (hadSlot bool, err error) {
	ksid, err := ks.h.value()
	if err != nil {
		return false, err
	}
	idid, err := id.h.value()
	if err != nil {
		return false, err
	}
	var out C.bool
	code := C.gc_keystore_admit(
		ksid,
		idid,
		bytePtr(bundleJSON),
		C.uintptr_t(len(bundleJSON)),
		&out,
	)
	runtime.KeepAlive(bundleJSON)
	if err := errFromCode(code); err != nil {
		return false, err
	}
	return bool(out), nil
}

// Open opens a data message received on keyexpr (its in-band JSON body) with the held
// DEK for the message's (rs, gen). keyexpr is bound as AEAD additional data — opening
// under the wrong key-expression fails with ErrDecrypt. ErrNoSlot if no DEK is held for
// the message's (rs, gen).
func (ks *KeyStore) Open(keyexpr string, envelopeJSON []byte) ([]byte, error) {
	if err := checkNoNUL(keyexpr); err != nil {
		return nil, err
	}
	ksid, err := ks.h.value()
	if err != nil {
		return nil, err
	}
	cKE := C.CString(keyexpr)
	defer C.free(unsafe.Pointer(cKE))
	var out C.uint64_t
	code := C.gc_keystore_open(
		ksid,
		cKE,
		bytePtr(envelopeJSON),
		C.uintptr_t(len(envelopeJSON)),
		&out,
	)
	runtime.KeepAlive(envelopeJSON)
	if err := errFromCode(code); err != nil {
		return nil, err
	}
	return bufBytes(out)
}

// ---- StreamKey (the producer: seal messages under an acquired key) ----

// StreamKey is a producer's seal-capable key, unwrapped from its own bundle slot at the
// highest generation of a release set. It owns the DEK, the seal counter, and the
// message-count cap; Close zeroizes the DEK.
type StreamKey struct{ h *handle }

// AcquireStreamKey unwraps the producer's stream key from a JSON array of bundles:
// the highest generation of rs in which id has a slot. ErrNoSlot if the producer is not
// in any visible audience.
func AcquireStreamKey(bundlesJSON []byte, rs string, id *Identity) (*StreamKey, error) {
	if err := checkNoNUL(rs); err != nil {
		return nil, err
	}
	idid, err := id.h.value()
	if err != nil {
		return nil, err
	}
	cRS := C.CString(rs)
	defer C.free(unsafe.Pointer(cRS))
	var out C.uint64_t
	code := C.gc_streamkey_acquire(
		bytePtr(bundlesJSON),
		C.uintptr_t(len(bundlesJSON)),
		cRS,
		idid,
		&out,
	)
	runtime.KeepAlive(bundlesJSON)
	if err := errFromCode(code); err != nil {
		return nil, err
	}
	sk := &StreamKey{}
	sk.h = newHandle(out, "StreamKey", func(h C.uint64_t) C.int32_t { return C.gc_streamkey_dispose(h) }, sk)
	return sk, nil
}

// Close disposes the stream key, zeroizing the DEK.
func (sk *StreamKey) Close() error { return sk.h.close(sk) }

// Seal seals plaintext for publication on keyexpr under this stream key, returning the
// serialized Envelope (the in-band wire body). keyexpr is bound as AEAD additional
// data; the core supplies a fresh random nonce and stamps (suite, rs, gen). ErrRotationRequired
// once the message-count cap is reached — seal refuses until the identity rotates.
func (sk *StreamKey) Seal(keyexpr string, plaintext []byte) ([]byte, error) {
	if err := checkNoNUL(keyexpr); err != nil {
		return nil, err
	}
	skid, err := sk.h.value()
	if err != nil {
		return nil, err
	}
	cKE := C.CString(keyexpr)
	defer C.free(unsafe.Pointer(cKE))
	var out C.uint64_t
	code := C.gc_streamkey_seal(
		skid,
		cKE,
		bytePtr(plaintext),
		C.uintptr_t(len(plaintext)),
		&out,
	)
	runtime.KeepAlive(plaintext)
	if err := errFromCode(code); err != nil {
		return nil, err
	}
	return bufBytes(out)
}

// Remaining reports how many messages may still be sealed under this key before
// rotation is required (the structured approach-to-cap fact — surface it well before
// the refusal).
func (sk *StreamKey) Remaining() (uint64, error) {
	skid, err := sk.h.value()
	if err != nil {
		return 0, err
	}
	var out C.uint64_t
	code := C.gc_streamkey_remaining(skid, &out)
	if err := errFromCode(code); err != nil {
		return 0, err
	}
	return uint64(out), nil
}

// SetCap tightens the seal cap. A caller may only tighten, never loosen: a cap above
// the core default is refused with ErrMalformed. No-op-safe on the current value.
func (sk *StreamKey) SetCap(cap uint64) error {
	skid, err := sk.h.value()
	if err != nil {
		return err
	}
	return errFromCode(C.gc_streamkey_set_cap(skid, C.uint64_t(cap)))
}

// ---- VerifyBundle (authenticate a bundle; no handle) ----

// VerifyBundle authenticates a bundle's signature against the key-service verifying key
// (SPKI DER). nil on success; ErrBadSignature or ErrMalformed otherwise. Consumers MUST
// verify before Admit.
func VerifyBundle(bundleJSON, vkSPKIDER []byte) error {
	code := C.gc_verify_bundle(
		bytePtr(bundleJSON),
		C.uintptr_t(len(bundleJSON)),
		bytePtr(vkSPKIDER),
		C.uintptr_t(len(vkSPKIDER)),
	)
	runtime.KeepAlive(bundleJSON)
	runtime.KeepAlive(vkSPKIDER)
	return errFromCode(code)
}
