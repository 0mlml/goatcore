package envelope

import (
	"errors"
	"testing"
)

// TestSuiteStamped confirms every artifact carries the classical suite id, so a
// consumer can dispatch on it and (once a second suite exists) old and new coexist.
func TestSuiteStamped(t *testing.T) {
	dek := mustDEK(t)
	env, err := Seal(dek, "rs", 1, []byte("hello"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if env.Suite != SuiteV1ECDHP256 {
		t.Fatalf("envelope suite = %q, want %q", env.Suite, SuiteV1ECDHP256)
	}

	rk := echKey(t)
	w, err := WrapDEK(dek, rk.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	if w.Suite != SuiteV1ECDHP256 {
		t.Fatalf("wrap suite = %q, want %q", w.Suite, SuiteV1ECDHP256)
	}

	b, err := NewBundle("rs", 1, dek, map[string]RecipientKey{"m": {Pub: rk.PublicKey()}})
	if err != nil {
		t.Fatal(err)
	}
	if b.Suite != SuiteV1ECDHP256 {
		t.Fatalf("bundle suite = %q, want %q", b.Suite, SuiteV1ECDHP256)
	}
}

// TestOpenRejectsUnsupportedSuite proves the envelope fails closed on a suite this
// build does not implement — a reserved future suite or an empty/absent one — and
// that it is reported as a capability error, not a decryption failure.
func TestOpenRejectsUnsupportedSuite(t *testing.T) {
	dek := mustDEK(t)
	env, err := Seal(dek, "rs", 1, []byte("hello"), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []Suite{SuiteV2X25519MLKEM768, "", "v9-made-up"} {
		e := env
		e.Suite = s
		if _, err := Open(dek, e, nil); !errors.Is(err, ErrUnsupportedSuite) {
			t.Fatalf("Open with suite %q: want ErrUnsupportedSuite, got %v", s, err)
		}
	}
}

// TestUnwrapRejectsUnsupportedSuite is the same fail-closed guard on the wrap.
func TestUnwrapRejectsUnsupportedSuite(t *testing.T) {
	dek := mustDEK(t)
	rk := echKey(t)
	w, err := WrapDEK(dek, rk.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	w.Suite = SuiteV2X25519MLKEM768
	if _, err := UnwrapDEK(w, rk); !errors.Is(err, ErrUnsupportedSuite) {
		t.Fatalf("Unwrap with reserved suite: want ErrUnsupportedSuite, got %v", err)
	}
}

// Note: the suite is bound into the AEAD's additional authenticated data (see
// aad / wrapAAD), so once a *second* supported suite exists, relabelling a
// ciphertext from one supported suite to another will fail authentication with
// ErrDecrypt. That direct binding test is added with the second suite; today only
// the fail-closed guard above is reachable, since exactly one suite is supported.
