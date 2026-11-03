package bundle

import (
	"bytes"
	"crypto/rand"
	"testing"
	"time"
)

func fixtureCRLEntry(t *testing.T) *CRLEntry {
	t.Helper()
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatalf("gen nonce: %v", err)
	}
	return &CRLEntry{
		Nonce:     nonce,
		CAID:      "dev-goat-ca-2026-04",
		RevokedAt: time.Date(2026, 4, 20, 15, 30, 0, 0, time.UTC),
		Reason:    ReasonCompromised,
	}
}

func TestCRLEntryRoundtrip(t *testing.T) {
	e := fixtureCRLEntry(t)
	data, err := e.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := UnmarshalCRLEntry(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !bytes.Equal(got.Nonce, e.Nonce) {
		t.Errorf("Nonce: got %x want %x", got.Nonce, e.Nonce)
	}
	if got.Reason != e.Reason {
		t.Errorf("Reason: got %q want %q", got.Reason, e.Reason)
	}
	if !got.RevokedAt.Equal(e.RevokedAt) {
		t.Errorf("RevokedAt: got %s want %s", got.RevokedAt, e.RevokedAt)
	}
}

func TestCRLEntrySignVerify(t *testing.T) {
	e := fixtureCRLEntry(t)
	caPub, sign := testCAKey(t)

	if err := e.Sign(sign); err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := e.Verify(caPub); err != nil {
		t.Errorf("verify should succeed: %v", err)
	}
}

func TestCRLEntryVerifyTamper(t *testing.T) {
	e := fixtureCRLEntry(t)
	caPub, sign := testCAKey(t)
	if err := e.Sign(sign); err != nil {
		t.Fatalf("sign: %v", err)
	}
	// Tamper: change reason from compromised to routine decommission.
	e.Reason = ReasonDecommissioned
	if err := e.Verify(caPub); err == nil {
		t.Error("verify should fail after reason tamper")
	}
}
