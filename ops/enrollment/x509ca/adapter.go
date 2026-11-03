// Package x509ca issues X.509 certificates signed by the offline CA root key.
//
// This package is the X.509 complement to the offline-enrollment ECDSA P-256
// bundle-signing CA. The same root key (held by ca.Signer) signs both:
//
//   - Bundles: ECDSA P-256 signatures (ASN.1 DER) over CBOR-canonical bytes
//     (handled by the bundle package).
//   - X.509 certs: ECDSA P-256 signatures over X.509 TBSCertificate bytes
//     produced by crypto/x509.CreateCertificate (handled here).
//
// Architecture: ADR 0407 (single-tier internal X.509 CA, shares ADR 0406
// root). Design: docs/design/internal-tls-ca.md.
//
// Block 79: hard cutover from Ed25519 to ECDSA P-256. Native
// trust-store import works via the standard OS dialog across macOS / iOS /
// Android / Windows / Linux post-rotation.
//
// First consumer: vega mgmt-API TLS termination at https://198.18.0.1:443
// behind the Block 22 wg-cp0 silent control plane.
package x509ca

import (
	"crypto"
	"errors"
	"fmt"
	"io"

	"github.com/usareur-af/goatcore/ops/enrollment/ca"
)

// cryptoSignerAdapter wraps a ca.Signer to implement crypto.Signer, the
// interface that crypto/x509.CreateCertificate expects.
//
// crypto/x509.CreateCertificate hashes the TBS bytes per RFC 5280 (SHA-256
// for ECDSA P-256) and invokes Sign(rand, digest, opts) on the wrapped
// signer with opts.HashFunc() == crypto.SHA256. We forward the digest to
// ca.Signer.SignDigest to avoid double-hashing.
type cryptoSignerAdapter struct {
	inner ca.Signer
}

// adapt returns a crypto.Signer view of inner.
func adapt(inner ca.Signer) crypto.Signer {
	return &cryptoSignerAdapter{inner: inner}
}

// Public returns the CA root's ECDSA P-256 public key as a crypto.PublicKey.
// crypto/x509.CreateCertificate uses this to derive the SignatureAlgorithm
// (ECDSA P-256 → SignatureAlgorithm = ECDSAWithSHA256).
func (a *cryptoSignerAdapter) Public() crypto.PublicKey {
	return a.inner.PublicKey()
}

// Sign produces an ECDSA P-256 signature over the pre-computed digest.
// crypto/x509.CreateCertificate hashes TBS bytes itself; opts.HashFunc()
// reports the digest algorithm. We assert SHA-256 (the only digest used
// for ECDSA P-256 in our trust model) and forward to SignDigest.
func (a *cryptoSignerAdapter) Sign(_ io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	if a.inner == nil {
		return nil, errors.New("x509ca: nil ca signer")
	}
	if opts == nil || opts.HashFunc() != crypto.SHA256 {
		return nil, fmt.Errorf("x509ca: unsupported hash %v; want SHA-256", opts)
	}
	return a.inner.SignDigest(digest)
}
