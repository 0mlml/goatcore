package envelope

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
)

// dekWrapInfo domain-separates the HKDF for DEK wrapping from any other use of
// the same ECDH shared secret. Bump the version suffix if the construction changes.
const dekWrapInfo = "grant-crypto/dek-wrap/v1"

// WrappedDEK is a DEK sealed to one recipient EC public key by ephemeral-static
// ECDH: a per-wrap ephemeral key agrees with the recipient's static SVID key, the
// shared secret is HKDF-expanded to a key-encryption key, and the DEK is
// AES-256-GCM-sealed under it with both public keys bound as AAD. Only the holder
// of the recipient private key (the cert-process) can recover the DEK.
//
// This is the per-recipient entry the key-distribution service (E1) publishes on
// keys/<release-set>/<generation>; E0 wraps to a single hand-cut member.
type WrappedDEK struct {
	Suite        Suite  `json:"suite"` // the wrap suite; bound as AAD, fails closed if unknown
	EphemeralPub []byte `json:"epk"`   // recipient-curve public point (uncompressed)
	Nonce        []byte `json:"nonce"` // AES-GCM nonce for the wrap
	Ciphertext   []byte `json:"wct"`   // AES-256-GCM(KEK): wrapped DEK || tag
}

// ErrUnwrap is returned when a WrappedDEK cannot be opened with the given private
// key — the wrong recipient, a corrupt/forged wrap, or a curve mismatch.
var ErrUnwrap = errors.New("envelope: cannot unwrap DEK (wrong recipient key or corrupt wrap)")

// wrapAAD binds the suite and both public keys into the AEAD so a wrap made for
// recipient A cannot be re-presented as if made for recipient B, the ephemeral
// key can't be swapped, and the suite can't be downgraded. The suite is
// length-prefixed so it cannot run into the (fixed-length) public points.
func wrapAAD(suite Suite, ephemeralPub, recipientPub []byte) []byte {
	su := []byte(suite)
	b := make([]byte, 0, 8+len(su)+len(ephemeralPub)+len(recipientPub))
	var lp [8]byte
	binary.BigEndian.PutUint64(lp[:], uint64(len(su)))
	b = append(b, lp[:]...)
	b = append(b, su...)
	b = append(b, ephemeralPub...)
	b = append(b, recipientPub...)
	return b
}

func kek(shared []byte) (DEK, error) {
	k, err := hkdf.Key(sha256.New, shared, nil, dekWrapInfo, KeyLen)
	if err != nil {
		return DEK{}, err
	}
	var out DEK
	copy(out[:], k)
	return out, nil
}

// WrapDEK seals dek to recipient (an EC public key, e.g. from a SPIFFE SVID leaf).
func WrapDEK(dek DEK, recipient *ecdh.PublicKey) (WrappedDEK, error) {
	if recipient == nil {
		return WrappedDEK{}, errors.New("envelope: nil recipient key")
	}
	eph, err := recipient.Curve().GenerateKey(rand.Reader)
	if err != nil {
		return WrappedDEK{}, fmt.Errorf("envelope: ephemeral key: %w", err)
	}
	shared, err := eph.ECDH(recipient)
	if err != nil {
		return WrappedDEK{}, fmt.Errorf("envelope: ecdh: %w", err)
	}
	kk, err := kek(shared)
	if err != nil {
		return WrappedDEK{}, err
	}
	aead, err := gcm(kk)
	if err != nil {
		return WrappedDEK{}, err
	}
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return WrappedDEK{}, fmt.Errorf("envelope: nonce: %w", err)
	}
	suite := SuiteV1ECDHP256
	ephPub := eph.PublicKey().Bytes()
	ct := aead.Seal(nil, nonce, dek[:], wrapAAD(suite, ephPub, recipient.Bytes()))
	return WrappedDEK{Suite: suite, EphemeralPub: ephPub, Nonce: nonce, Ciphertext: ct}, nil
}

// UnwrapDEK recovers the DEK from w using the recipient's private key.
func UnwrapDEK(w WrappedDEK, recipient *ecdh.PrivateKey) (DEK, error) {
	if recipient == nil {
		return DEK{}, errors.New("envelope: nil recipient key")
	}
	if !w.Suite.supported() {
		return DEK{}, ErrUnsupportedSuite
	}
	if len(w.Nonce) != nonceLen {
		return DEK{}, ErrUnwrap
	}
	ephPub, err := recipient.Curve().NewPublicKey(w.EphemeralPub)
	if err != nil {
		return DEK{}, ErrUnwrap
	}
	shared, err := recipient.ECDH(ephPub)
	if err != nil {
		return DEK{}, ErrUnwrap
	}
	kk, err := kek(shared)
	if err != nil {
		return DEK{}, err
	}
	aead, err := gcm(kk)
	if err != nil {
		return DEK{}, err
	}
	pt, err := aead.Open(nil, w.Nonce, w.Ciphertext, wrapAAD(w.Suite, w.EphemeralPub, recipient.PublicKey().Bytes()))
	if err != nil || len(pt) != KeyLen {
		return DEK{}, ErrUnwrap
	}
	var dek DEK
	copy(dek[:], pt)
	return dek, nil
}

// RecipientFromCert extracts the ECDH public key from an x509 leaf — the SPIFFE
// SVID a member presents. SPIRE issues ECDSA (P-256) SVIDs; an RSA or non-EC leaf
// is rejected, since the wrap is EC-only by design.
func RecipientFromCert(cert *x509.Certificate) (*ecdh.PublicKey, error) {
	if cert == nil {
		return nil, errors.New("envelope: nil certificate")
	}
	pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("envelope: SVID public key is %T, want *ecdsa.PublicKey (EC/P-256)", cert.PublicKey)
	}
	ep, err := pub.ECDH()
	if err != nil {
		return nil, fmt.Errorf("envelope: converting SVID key to ECDH: %w", err)
	}
	return ep, nil
}
