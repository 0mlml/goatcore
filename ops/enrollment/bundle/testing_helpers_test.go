package bundle

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"testing"
)

// testCAKey returns an ECDSA P-256 keypair + a signing function suitable
// for bundle.EnrollmentBundle.Sign and CRLEntry.Sign in tests. Block 79
// hard cutover: replaces the previous ed25519-shaped test fixture pattern.
func testCAKey(t *testing.T) (*ecdsa.PublicKey, func([]byte) ([]byte, error)) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ecdsa P-256: %v", err)
	}
	sign := func(msg []byte) ([]byte, error) {
		digest := sha256.Sum256(msg)
		return ecdsa.SignASN1(rand.Reader, priv, digest[:])
	}
	return &priv.PublicKey, sign
}
