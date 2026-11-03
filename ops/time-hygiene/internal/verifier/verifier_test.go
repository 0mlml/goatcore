package verifier

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func mustKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return pub, priv
}

func TestVerifyAccepts(t *testing.T) {
	pub, priv := mustKey(t)
	msg := []byte("ntp-attestation: vega-a stratum=1 rms=0.0001")
	sig := ed25519.Sign(priv, msg)

	if err := Verify(pub, msg, sig); err != nil {
		t.Errorf("Verify(valid sig): %v", err)
	}
}

func TestVerifyRejectsTamperedMsg(t *testing.T) {
	pub, priv := mustKey(t)
	msg := []byte("ntp-attestation: vega-a stratum=1")
	sig := ed25519.Sign(priv, msg)

	tampered := []byte("ntp-attestation: vega-a stratum=2")
	if err := Verify(pub, tampered, sig); err == nil {
		t.Error("Verify(tampered msg): expected error, got nil")
	}
}

func TestVerifyRejectsTamperedSig(t *testing.T) {
	pub, priv := mustKey(t)
	msg := []byte("payload")
	sig := ed25519.Sign(priv, msg)
	sig[0] ^= 0x01

	if err := Verify(pub, msg, sig); err == nil {
		t.Error("Verify(tampered sig): expected error, got nil")
	}
}

func TestVerifyRejectsWrongKey(t *testing.T) {
	_, priv := mustKey(t)
	other, _ := mustKey(t)
	msg := []byte("payload")
	sig := ed25519.Sign(priv, msg)

	if err := Verify(other, msg, sig); err == nil {
		t.Error("Verify(wrong key): expected error, got nil")
	}
}

func TestVerifyRejectsBadKeyLength(t *testing.T) {
	cases := [][]byte{
		nil,
		make([]byte, 0),
		make([]byte, 16),
		make([]byte, 31),
		make([]byte, 33),
		make([]byte, 64),
	}
	msg := []byte("x")
	sig := make([]byte, ed25519.SignatureSize)
	for _, k := range cases {
		if err := Verify(k, msg, sig); err == nil {
			t.Errorf("Verify(len=%d key): expected error, got nil", len(k))
		}
	}
}
