package grantcrypto_test

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"errors"
	"math"
	"runtime"
	"testing"
	"time"

	gc "github.com/usareur-af/goatcore/ops/grant-crypto/sdk/bindings/go"
)

// bundlesArray wraps one committed bundle as the JSON array the acquire/rotate verbs
// take. Uses the vectors' signed material rather than reimplementing the key service.
func bundlesArray(t *testing.T, v *vectors, name string) []byte {
	t.Helper()
	b, err := json.Marshal([]json.RawMessage{v.Artifacts[name].Bundle})
	if err != nil {
		t.Fatalf("marshal bundles array: %v", err)
	}
	return b
}

// freshScalar returns a valid 32-byte P-256 private scalar to rotate into.
func freshScalar(t *testing.T) []byte {
	t.Helper()
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate scalar: %v", err)
	}
	return k.Bytes()
}

func TestABIVersion(t *testing.T) {
	if got := gc.ABIVersion(); got != 1 {
		t.Fatalf("ABIVersion = %d, want 1", got)
	}
}

// TestWriteSideRoundtrip seals through the binding and opens the result through the
// binding: the write path is nondeterministic (fresh nonce per seal) so it cannot be a
// committed vector; this proves the produced envelope is self-consistent end to end.
func TestWriteSideRoundtrip(t *testing.T) {
	v := loadVectors(t)
	id, err := gc.NewIdentity("member", memberScalar(t, v, "member"))
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	defer id.Close()

	sk, err := gc.AcquireStreamKey(bundlesArray(t, v, "base"), "coalition-air", id)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer sk.Close()

	const keyexpr = "data/coalition-air/cop"
	plaintext := []byte("hello from the Go binding")
	env, err := sk.Seal(keyexpr, plaintext)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	ks, err := gc.NewKeyStore()
	if err != nil {
		t.Fatalf("new keystore: %v", err)
	}
	defer ks.Close()
	hadSlot, err := ks.Admit(id, v.Artifacts["base"].Bundle)
	if err != nil || !hadSlot {
		t.Fatalf("admit: hadSlot=%v err=%v", hadSlot, err)
	}
	got, err := ks.Open(keyexpr, env)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("roundtrip plaintext = %q, want %q", got, plaintext)
	}

	// Eight seals under the same key must each carry a distinct nonce (a fresh random
	// 96-bit nonce per seal — the nonce-safety property the seal cap exists to protect).
	nonces := make(map[string]struct{})
	for i := 0; i < 8; i++ {
		wire, err := sk.Seal(keyexpr, plaintext)
		if err != nil {
			t.Fatalf("seal %d: %v", i, err)
		}
		var parsed struct {
			Nonce string `json:"nonce"`
		}
		if err := json.Unmarshal(wire, &parsed); err != nil {
			t.Fatalf("parse envelope %d: %v", i, err)
		}
		if parsed.Nonce == "" {
			t.Fatalf("envelope %d carries no nonce", i)
		}
		nonces[parsed.Nonce] = struct{}{}
	}
	if len(nonces) != 8 {
		t.Fatalf("expected 8 distinct nonces across 8 seals, got %d", len(nonces))
	}
}

// TestWrongTopicFailsAEAD proves the key-expression is AEAD-bound: opening a sealed
// message under a different topic is ErrDecrypt, not a silent success.
func TestWrongTopicFailsAEAD(t *testing.T) {
	v := loadVectors(t)
	id, err := gc.NewIdentity("member", memberScalar(t, v, "member"))
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	defer id.Close()
	sk, err := gc.AcquireStreamKey(bundlesArray(t, v, "base"), "coalition-air", id)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer sk.Close()
	env, err := sk.Seal("data/coalition-air/cop", []byte("x"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	ks, err := gc.NewKeyStore()
	if err != nil {
		t.Fatalf("new keystore: %v", err)
	}
	defer ks.Close()
	if _, err := ks.Admit(id, v.Artifacts["base"].Bundle); err != nil {
		t.Fatalf("admit: %v", err)
	}
	if _, err := ks.Open("data/coalition-air/OTHER", env); !errors.Is(err, gc.ErrDecrypt) {
		t.Fatalf("wrong-topic open = %v, want ErrDecrypt", err)
	}
}

// TestSealCap: tighten-only, remaining counts down, refusal at the cap.
func TestSealCap(t *testing.T) {
	v := loadVectors(t)
	id, err := gc.NewIdentity("member", memberScalar(t, v, "member"))
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	defer id.Close()
	sk, err := gc.AcquireStreamKey(bundlesArray(t, v, "base"), "coalition-air", id)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer sk.Close()

	if err := sk.SetCap(math.MaxUint64); !errors.Is(err, gc.ErrMalformed) {
		t.Fatalf("loosening cap = %v, want ErrMalformed", err)
	}
	// The seal budget is process-cumulative per (rs, gen) and the bindings share the one
	// committed generation, so other tests in this process may already have sealed under
	// it. Tighten relative to what remains now to leave exactly two seals.
	const defaultSealCap = 1 << 24
	rem0, err := sk.Remaining()
	if err != nil {
		t.Fatalf("remaining: %v", err)
	}
	if err := sk.SetCap(defaultSealCap - rem0 + 2); err != nil {
		t.Fatalf("tighten cap: %v", err)
	}
	if rem, _ := sk.Remaining(); rem != 2 {
		t.Fatalf("remaining = %d, want 2", rem)
	}
	for i := 0; i < 2; i++ {
		if _, err := sk.Seal("data/coalition-air/cop", []byte("m")); err != nil {
			t.Fatalf("seal %d: %v", i, err)
		}
	}
	if rem, _ := sk.Remaining(); rem != 0 {
		t.Fatalf("remaining after 2 seals = %d, want 0", rem)
	}
	if _, err := sk.Seal("data/coalition-air/cop", []byte("m")); !errors.Is(err, gc.ErrRotationRequired) {
		t.Fatalf("seal at cap = %v, want ErrRotationRequired", err)
	}
}

// TestRotateDestroy: the default drops the old key and re-wraps nothing; the receipt is
// a structured, self-dating fact.
func TestRotateDestroy(t *testing.T) {
	v := loadVectors(t)
	id, err := gc.NewIdentity("member", memberScalar(t, v, "member"))
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	defer id.Close()

	receipt, err := id.Rotate(freshScalar(t), false, bundlesArray(t, v, "base"))
	if err != nil {
		t.Fatalf("rotate destroy: %v", err)
	}
	var r map[string]any
	if err := json.Unmarshal(receipt, &r); err != nil {
		t.Fatalf("parse receipt: %v", err)
	}
	if r["member_id"] != "member" {
		t.Fatalf("member_id = %v, want member", r["member_id"])
	}
	if r["policy"] != "destroy" {
		t.Fatalf("policy = %v, want destroy", r["policy"])
	}
	if r["now_unreadable_generations"].(float64) != 1 {
		t.Fatalf("now_unreadable_generations = %v, want 1", r["now_unreadable_generations"])
	}
	if r["rewrapped_generations"].(float64) != 0 {
		t.Fatalf("rewrapped_generations = %v, want 0", r["rewrapped_generations"])
	}
	if _, ok := r["rewrapped"]; ok {
		t.Fatal("destroy receipt must carry no rewrapped entries")
	}
	if ts, _ := r["timestamp_rfc3339"].(string); len(ts) == 0 || ts[len(ts)-1] != 'Z' {
		t.Fatalf("timestamp_rfc3339 = %q, want a UTC ...Z instant", ts)
	}

	// The handle now holds the NEW scalar: it can no longer unwrap the old slot.
	ks, err := gc.NewKeyStore()
	if err != nil {
		t.Fatalf("new keystore: %v", err)
	}
	defer ks.Close()
	if _, err := ks.Admit(id, v.Artifacts["base"].Bundle); !errors.Is(err, gc.ErrUnwrap) {
		t.Fatalf("post-rotate admit = %v, want ErrUnwrap", err)
	}
}

// TestRotateKeepAccess: the opt-out re-wraps the member's still-live DEKs to the new
// key; the receipt carries the rewrapped entries.
func TestRotateKeepAccess(t *testing.T) {
	v := loadVectors(t)
	id, err := gc.NewIdentity("member", memberScalar(t, v, "member"))
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	defer id.Close()

	receipt, err := id.Rotate(freshScalar(t), true, bundlesArray(t, v, "base"))
	if err != nil {
		t.Fatalf("rotate keep-access: %v", err)
	}
	var r map[string]any
	if err := json.Unmarshal(receipt, &r); err != nil {
		t.Fatalf("parse receipt: %v", err)
	}
	if r["policy"] != "keep-access" {
		t.Fatalf("policy = %v, want keep-access", r["policy"])
	}
	if r["now_unreadable_generations"].(float64) != 1 {
		t.Fatalf("now_unreadable_generations = %v, want 1", r["now_unreadable_generations"])
	}
	if r["rewrapped_generations"].(float64) != 1 {
		t.Fatalf("rewrapped_generations = %v, want 1", r["rewrapped_generations"])
	}
	rw, ok := r["rewrapped"].([]any)
	if !ok || len(rw) != 1 {
		t.Fatalf("rewrapped = %v, want a 1-element array", r["rewrapped"])
	}
	entry := rw[0].(map[string]any)
	if entry["rs"] != "coalition-air" {
		t.Fatalf("rewrapped[0].rs = %v, want coalition-air", entry["rs"])
	}
	if entry["gen"].(float64) != 7 {
		t.Fatalf("rewrapped[0].gen = %v, want 7", entry["gen"])
	}
}

// TestLifecycleDisposed: use-after-Close and double-Close both fail closed.
func TestLifecycleDisposed(t *testing.T) {
	v := loadVectors(t)

	// Identity: Close, then use it, then Close again.
	id, err := gc.NewIdentity("member", memberScalar(t, v, "member"))
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	if err := id.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := id.Close(); !errors.Is(err, gc.ErrDisposed) {
		t.Fatalf("double Close = %v, want ErrDisposed", err)
	}
	if _, err := gc.AcquireStreamKey(bundlesArray(t, v, "base"), "coalition-air", id); !errors.Is(err, gc.ErrDisposed) {
		t.Fatalf("acquire with closed identity = %v, want ErrDisposed", err)
	}

	// KeyStore: use after Close.
	ks, err := gc.NewKeyStore()
	if err != nil {
		t.Fatalf("new keystore: %v", err)
	}
	if err := ks.Close(); err != nil {
		t.Fatalf("keystore Close: %v", err)
	}
	if _, err := ks.Open("data/coalition-air/cop", []byte(`{}`)); !errors.Is(err, gc.ErrDisposed) {
		t.Fatalf("open on closed keystore = %v, want ErrDisposed", err)
	}

	// StreamKey: use after Close.
	live, err := gc.NewIdentity("member", memberScalar(t, v, "member"))
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	defer live.Close()
	sk, err := gc.AcquireStreamKey(bundlesArray(t, v, "base"), "coalition-air", live)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := sk.Close(); err != nil {
		t.Fatalf("streamkey Close: %v", err)
	}
	if _, err := sk.Remaining(); !errors.Is(err, gc.ErrDisposed) {
		t.Fatalf("remaining on closed streamkey = %v, want ErrDisposed", err)
	}
}

// TestEmbeddedNULRejected: a string argument containing a NUL byte is rejected
// client-side (Malformed class) before the FFI call — the C ABI reads these
// NUL-terminated, so an interior NUL would silently truncate.
func TestEmbeddedNULRejected(t *testing.T) {
	v := loadVectors(t)
	scalar := memberScalar(t, v, "member")

	if _, err := gc.NewIdentity("member\x00evil", scalar); !errors.Is(err, gc.ErrMalformed) {
		t.Fatalf("NUL in member id = %v, want ErrMalformed", err)
	}

	id, err := gc.NewIdentity("member", scalar)
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	defer id.Close()

	if _, err := gc.AcquireStreamKey(bundlesArray(t, v, "base"), "coalition\x00air", id); !errors.Is(err, gc.ErrMalformed) {
		t.Fatalf("NUL in rs = %v, want ErrMalformed", err)
	}

	sk, err := gc.AcquireStreamKey(bundlesArray(t, v, "base"), "coalition-air", id)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer sk.Close()
	if _, err := sk.Seal("data/x\x00y", []byte("m")); !errors.Is(err, gc.ErrMalformed) {
		t.Fatalf("NUL in seal keyexpr = %v, want ErrMalformed", err)
	}

	ks, err := gc.NewKeyStore()
	if err != nil {
		t.Fatalf("new keystore: %v", err)
	}
	defer ks.Close()
	if _, err := ks.Open("data/x\x00y", []byte("{}")); !errors.Is(err, gc.ErrMalformed) {
		t.Fatalf("NUL in open keyexpr = %v, want ErrMalformed", err)
	}
}

// TestBadScalarLength rejects a wrong-length scalar in Go, before the ABI.
func TestBadScalarLength(t *testing.T) {
	if _, err := gc.NewIdentity("member", make([]byte, 31)); !errors.Is(err, gc.ErrMalformed) {
		t.Fatalf("31-byte scalar = %v, want ErrMalformed (via ErrBadScalar)", err)
	}
	if !errors.Is(gc.ErrBadScalar, gc.ErrMalformed) {
		t.Fatal("ErrBadScalar must wrap ErrMalformed")
	}
}

// TestFinalizerReportsLeak: a handle collected without Close is REPORTED (never
// disposed) — the leak-accounting hook fires. This intentionally leaks one core handle.
func TestFinalizerReportsLeak(t *testing.T) {
	v := loadVectors(t)
	fired := make(chan string, 1)
	gc.LeakLogger = func(kind string) {
		select {
		case fired <- kind:
		default:
		}
	}
	t.Cleanup(func() { gc.LeakLogger = nil })

	before := gc.Leaks()
	func() {
		// Deliberately no Close: dropped at the end of this scope.
		id, err := gc.NewIdentity("member", memberScalar(t, v, "member"))
		if err != nil {
			t.Fatalf("new identity: %v", err)
		}
		_ = id
	}()

	deadline := time.After(5 * time.Second)
	for {
		runtime.GC()
		select {
		case kind := <-fired:
			if kind != "Identity" {
				t.Fatalf("leak kind = %q, want Identity", kind)
			}
			if gc.Leaks() <= before {
				t.Fatalf("Leaks() = %d, want > %d", gc.Leaks(), before)
			}
			return
		case <-deadline:
			t.Fatal("finalizer did not report the leak within 5s")
		case <-time.After(20 * time.Millisecond):
			// retry GC
		}
	}
}
