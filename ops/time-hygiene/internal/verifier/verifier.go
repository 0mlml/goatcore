// Package verifier provides Ed25519 attestation verification for cross-site
// peer attestation (Block 12D). The trust anchor for each peer is its
// ntp_attestation_pubkey from the offline-enrollment bundle.
package verifier

import (
	"crypto/ed25519"
	"errors"
)

// Verify checks sig over msg using the given Ed25519 public key.
// pubKey must be 32 bytes (raw Ed25519 public key, as stored in the bundle field).
func Verify(pubKey, msg, sig []byte) error {
	if len(pubKey) != ed25519.PublicKeySize {
		return errors.New("invalid public key length: expected 32 bytes")
	}
	if !ed25519.Verify(ed25519.PublicKey(pubKey), msg, sig) {
		return errors.New("signature verification failed")
	}
	return nil
}
