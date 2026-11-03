package ca

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"path/filepath"
	"testing"

	"filippo.io/age"
)

// newX25519Pair generates a fresh age X25519 identity+recipient for tests.
func newX25519Pair(t *testing.T) (*age.X25519Identity, age.Recipient) {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate x25519 identity: %v", err)
	}
	return id, id.Recipient()
}

func TestGenerateAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ca.key.age")

	id, rcpt := newX25519Pair(t)

	pub, err := GenerateAndWrite(path, []age.Recipient{rcpt})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if pub == nil {
		t.Fatal("public key is nil")
	}
	if pub.Curve.Params().Name != "P-256" {
		t.Fatalf("public key curve: got %s want P-256", pub.Curve.Params().Name)
	}

	signer, err := NewAgeSigner("dev-goat-ca-test", path, []age.Identity{id})
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	t.Cleanup(func() {
		if err := signer.Close(); err != nil {
			t.Errorf("close signer: %v", err)
		}
	})

	if signer.ID() != "dev-goat-ca-test" {
		t.Errorf("ID: got %q", signer.ID())
	}
	// Public key after reload must match what GenerateAndWrite returned.
	loaded := signer.PublicKey()
	if loaded.X.Cmp(pub.X) != 0 || loaded.Y.Cmp(pub.Y) != 0 {
		t.Fatalf("public key mismatch after reload")
	}

	// Round-trip sign/verify.
	msg := []byte("the offline ca will sign this")
	sig, err := signer.Sign(msg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	digest := sha256.Sum256(msg)
	if !ecdsa.VerifyASN1(signer.PublicKey(), digest[:], sig) {
		t.Error("verify failed against returned public key")
	}
}

func TestGenerateAndWriteRejectsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ca.key.age")

	_, rcpt := newX25519Pair(t)
	if _, err := GenerateAndWrite(path, []age.Recipient{rcpt}); err != nil {
		t.Fatalf("first generate: %v", err)
	}
	// Second call must fail — rotation ceremony goes elsewhere, we don't
	// silently clobber.
	if _, err := GenerateAndWrite(path, []age.Recipient{rcpt}); err == nil {
		t.Error("second GenerateAndWrite should have failed on existing file")
	}
}

func TestNewAgeSignerWrongIdentity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ca.key.age")

	_, rcpt := newX25519Pair(t)
	if _, err := GenerateAndWrite(path, []age.Recipient{rcpt}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	otherID, _ := newX25519Pair(t)
	if _, err := NewAgeSigner("dev", path, []age.Identity{otherID}); err == nil {
		t.Error("decrypt should fail with a different identity")
	}
}

func TestAgeSignerCloseIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ca.key.age")

	id, rcpt := newX25519Pair(t)
	if _, err := GenerateAndWrite(path, []age.Recipient{rcpt}); err != nil {
		t.Fatalf("generate: %v", err)
	}
	signer, err := NewAgeSigner("dev", path, []age.Identity{id})
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	if err := signer.Close(); err != nil {
		t.Errorf("first close: %v", err)
	}
	if err := signer.Close(); err != nil {
		t.Errorf("second close: %v", err)
	}
	// Sign after close must error, not panic.
	if _, err := signer.Sign([]byte("x")); err == nil {
		t.Error("sign after close should return error")
	}
}

func TestNewAgeSignerMissingIdentities(t *testing.T) {
	if _, err := NewAgeSigner("dev", "/nonexistent", nil); err == nil {
		t.Error("NewAgeSigner with no identities should fail fast")
	}
}
