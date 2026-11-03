// Package ca defines the signing-backend interface for the offline CA and
// provides implementations for the age-encrypted file (dev) and YubiKey PIV
// (production, stub) backends.
//
// Callers use the Signer interface to remain independent of which backend is
// active. The bundle package uses it through a `func([]byte) ([]byte, error)`
// adapter (see Signer.SignFunc) so the cryptographic details stay local.
//
// Block 79: hard cutover from Ed25519 to ECDSA P-256. macOS
// Keychain Services rejects Ed25519 root cert imports; ECDSA P-256
// roots import cleanly via the standard OS dialog across macOS / iOS /
// Android / Windows / Linux trust stores. All offline-CA-signed artifacts
// (CBOR bundles, CRL entries, X.509 root + intermediate + leaves) sign and
// verify under ECDSA P-256 post-rotation. No backward compatibility for
// Ed25519-signed artifacts — the ceremony re-issues every artifact under
// the new root.
//
// API shape: Sign(msg) hashes msg with SHA-256 internally then ECDSA-signs
// (the convenience entry-point for bundle / CRL canonical bytes).
// SignDigest(digest) signs an already-computed SHA-256 digest (used by the
// crypto.Signer adapter that crypto/x509.CreateCertificate consumes).
package ca

import "crypto/ecdsa"

// Signer is the minimal interface a CA backend must implement.
//
// Implementations:
//   - AgeSigner: private key lives in an age-encrypted file (dev fallback
//     while YubiKey hardware is being procured).
//   - YubikeySigner: private key lives on a YubiKey PIV slot with
//     touch-policy=always (production).
type Signer interface {
	// Sign hashes msg with SHA-256 then produces an ECDSA P-256 signature
	// over the digest. The signature is the ASN.1 DER ECDSA-Sig-Value
	// envelope per ecdsa.SignASN1; typical length is 70-72 bytes
	// (variable). Used by bundle.EnrollmentBundle.Sign + CRLEntry.Sign
	// over canonical CBOR bytes.
	Sign(msg []byte) ([]byte, error)

	// SignDigest signs an already-computed 32-byte SHA-256 digest. Used
	// by the crypto.Signer adapter that crypto/x509.CreateCertificate
	// consumes — that path pre-hashes the TBS bytes per RFC 5280 and
	// invokes Sign(rand, digest, opts) on the wrapped signer. Returning
	// an ASN.1 DER signature matches what x509 expects for ECDSA.
	SignDigest(digest []byte) ([]byte, error)

	// PublicKey returns the CA's ECDSA P-256 public key.
	PublicKey() *ecdsa.PublicKey

	// ID returns the CA identifier carried in the ca_id field of bundles
	// and CRL entries. Dev-mode IDs have a "dev-" prefix.
	ID() string

	// Close releases any resources (e.g., zeroes a cached private key or
	// disconnects from the YubiKey). Must be safe to call multiple times.
	Close() error
}

// SignFunc adapts a Signer into a bare `func([]byte) ([]byte, error)` used by
// bundle.EnrollmentBundle.Sign and bundle.CRLEntry.Sign.
func SignFunc(s Signer) func([]byte) ([]byte, error) {
	return func(msg []byte) ([]byte, error) { return s.Sign(msg) }
}
