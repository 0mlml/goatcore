package bundle

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/fxamacker/cbor/v2"
)

// Reason describes why a CRL entry was issued.
type Reason string

const (
	ReasonCompromised    Reason = "compromised"
	ReasonDecommissioned Reason = "decommissioned"
	ReasonReissued       Reason = "reissued"
	ReasonExpiredNoShow  Reason = "expired-no-show"
	ReasonCACompromise   Reason = "ca-compromise"
)

// CRLEntry revokes a bundle by its Nonce. Signed entries are self-authenticating
// so the CRL can be replicated by any mechanism (HTTP pull in Phase 2A, Spock
// in Phase 3) without trusting the replication path.
type CRLEntry struct {
	Nonce     []byte    `cbor:"nonce"`
	CAID      string    `cbor:"ca_id"`
	RevokedAt time.Time `cbor:"revoked_at"`
	Reason    Reason    `cbor:"reason"`
	Signature []byte    `cbor:"signature,omitempty"`
}

// Signable returns the canonical CBOR encoding without the Signature field.
func (e *CRLEntry) Signable() ([]byte, error) {
	em, err := canonicalEnc()
	if err != nil {
		return nil, fmt.Errorf("cbor canonical enc mode: %w", err)
	}
	unsigned := *e
	unsigned.Signature = nil
	return em.Marshal(&unsigned)
}

// Marshal returns the full canonical CBOR encoding including Signature.
func (e *CRLEntry) Marshal() ([]byte, error) {
	em, err := canonicalEnc()
	if err != nil {
		return nil, fmt.Errorf("cbor canonical enc mode: %w", err)
	}
	return em.Marshal(e)
}

// UnmarshalCRLEntry parses a CBOR-encoded CRL entry.
func UnmarshalCRLEntry(data []byte) (*CRLEntry, error) {
	var e CRLEntry
	if err := cbor.Unmarshal(data, &e); err != nil {
		return nil, fmt.Errorf("cbor unmarshal: %w", err)
	}
	return &e, nil
}

// Sign produces an ECDSA P-256 signature over the canonical payload
// (Block 79). Output is ASN.1 DER ECDSA-Sig-Value over SHA-256(payload).
func (e *CRLEntry) Sign(sign func([]byte) ([]byte, error)) error {
	payload, err := e.Signable()
	if err != nil {
		return fmt.Errorf("signable: %w", err)
	}
	sig, err := sign(payload)
	if err != nil {
		return fmt.Errorf("signer: %w", err)
	}
	if len(sig) == 0 || len(sig) > 256 {
		return fmt.Errorf("signature wrong size: got %d want 1-256 (ECDSA ASN.1 DER)", len(sig))
	}
	e.Signature = sig
	return nil
}

// Verify checks the entry's ECDSA P-256 signature against a trusted
// public key.
func (e *CRLEntry) Verify(pub *ecdsa.PublicKey) error {
	if pub == nil {
		return errors.New("public key is nil")
	}
	if pub.Curve.Params().Name != "P-256" {
		return fmt.Errorf("public key curve is %s, want P-256", pub.Curve.Params().Name)
	}
	if len(e.Signature) == 0 {
		return errors.New("signature is empty")
	}
	payload, err := e.Signable()
	if err != nil {
		return fmt.Errorf("signable: %w", err)
	}
	digest := sha256.Sum256(payload)
	if !ecdsa.VerifyASN1(pub, digest[:], e.Signature) {
		return errors.New("signature invalid")
	}
	return nil
}
