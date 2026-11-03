package envelope

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"testing"
)

func ecdsaKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa key: %v", err)
	}
	return k
}

func signedBundle(t *testing.T, signer string, priv *ecdsa.PrivateKey) KeyBundle {
	t.Helper()
	dek := mustDEK(t)
	rk := echKey(t)
	b, err := NewBundle("coalition-air", 7, dek, map[string]RecipientKey{
		"member-a": {Pub: rk.PublicKey()},
		"member-b": {Pub: echKey(t).PublicKey()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := SignBundle(&b, signer, priv); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestBundleSignVerify(t *testing.T) {
	ks := ecdsaKey(t)
	b := signedBundle(t, "key-service", ks)
	if err := VerifyBundle(b, &ks.PublicKey); err != nil {
		t.Fatalf("verify with correct key: %v", err)
	}
}

func TestBundleVerifyRejectsWrongSigner(t *testing.T) {
	b := signedBundle(t, "key-service", ecdsaKey(t))
	other := ecdsaKey(t) // a different key-service key
	if err := VerifyBundle(b, &other.PublicKey); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("verify with wrong key: want ErrBadSignature, got %v", err)
	}
}

func TestBundleVerifyRejectsUnsigned(t *testing.T) {
	dek := mustDEK(t)
	b, err := NewBundle("rs", 1, dek, map[string]RecipientKey{"m": {Pub: echKey(t).PublicKey()}})
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyBundle(b, &ecdsaKey(t).PublicKey); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("verify unsigned bundle: want ErrBadSignature, got %v", err)
	}
}

func TestBundleVerifyRejectsTampering(t *testing.T) {
	ks := ecdsaKey(t)

	// Tampered signer field.
	b := signedBundle(t, "key-service", ks)
	b.Signer = "impostor"
	if err := VerifyBundle(b, &ks.PublicKey); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("tampered signer: want ErrBadSignature, got %v", err)
	}

	// Tampered generation.
	b = signedBundle(t, "key-service", ks)
	b.Generation = 8
	if err := VerifyBundle(b, &ks.PublicKey); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("tampered generation: want ErrBadSignature, got %v", err)
	}

	// Tampered wrapped-key bytes for a member.
	b = signedBundle(t, "key-service", ks)
	w := b.Members["member-a"]
	if len(w.Ciphertext) > 0 {
		w.Ciphertext[0] ^= 0xff
	}
	b.Members["member-a"] = w
	if err := VerifyBundle(b, &ks.PublicKey); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("tampered member wrap: want ErrBadSignature, got %v", err)
	}

	// Dropped member (audience narrowed after signing).
	b = signedBundle(t, "key-service", ks)
	delete(b.Members, "member-b")
	if err := VerifyBundle(b, &ks.PublicKey); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("dropped member: want ErrBadSignature, got %v", err)
	}
}
