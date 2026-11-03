package envelope

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
)

// bundleSigDomain domain-separates the bundle signature from any other signature
// a key-service identity might produce. Bump the suffix if the transcript changes.
const bundleSigDomain = "grant-crypto/bundle-sig/v1"

// ErrBadSignature is returned when a KeyBundle's signature does not verify under
// the provided key — a forged or tampered bundle, the wrong signer key, or a
// bundle that was never signed. It is the authenticity gate a member checks
// before it trusts any wrapped key in the bundle (decision §7): even though the
// transport ACL restricts who may publish key topics, the signature is the
// cryptographic guarantee that survives ACL misconfiguration and federated
// multi-writer deployments.
var ErrBadSignature = errors.New("envelope: key-bundle signature does not verify")

// bundleSigningTranscript is the canonical, language-neutral byte string the
// bundle signature covers: a domain tag, the suite, release-set, generation, and
// signer identity, then every member entry in ascending id order. Every
// variable-length field is 8-byte big-endian length-prefixed, so independent
// implementations produce byte-identical transcripts and cannot drift. The Sig
// field is not part of the transcript (it is the output).
func bundleSigningTranscript(b KeyBundle) []byte {
	var buf []byte
	put := func(p []byte) {
		var lp [8]byte
		binary.BigEndian.PutUint64(lp[:], uint64(len(p)))
		buf = append(buf, lp[:]...)
		buf = append(buf, p...)
	}
	putU64 := func(v uint64) {
		var lp [8]byte
		binary.BigEndian.PutUint64(lp[:], v)
		buf = append(buf, lp[:]...)
	}
	put([]byte(bundleSigDomain))
	put([]byte(b.Suite))
	put([]byte(b.ReleaseSet))
	putU64(uint64(b.Generation))
	put([]byte(b.Signer))
	ids := make([]string, 0, len(b.Members))
	for id := range b.Members {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	putU64(uint64(len(ids)))
	for _, id := range ids {
		w := b.Members[id]
		put([]byte(id))
		put([]byte(w.Suite))
		put(w.EphemeralPub)
		put(w.Nonce)
		put(w.Ciphertext)
	}
	return buf
}

// SignBundle signs b in place with the key-service's ECDSA private key, recording
// signer as the bundle's claimed signer identity (bound into the transcript, so a
// signature cannot be moved to a different claimed signer). The signer id is what
// a verifier maps to a trusted public key out of band — in production, the
// key-service's SVID. v1 signatures are ECDSA-P256 over SHA-256; the post-quantum
// path (decision §7/§8) moves this to ML-DSA under a new suite.
func SignBundle(b *KeyBundle, signer string, priv *ecdsa.PrivateKey) error {
	if b == nil {
		return errors.New("envelope: nil bundle")
	}
	if priv == nil {
		return errors.New("envelope: nil signing key")
	}
	b.Signer = signer
	b.Sig = nil
	h := sha256.Sum256(bundleSigningTranscript(*b))
	sig, err := ecdsa.SignASN1(rand.Reader, priv, h[:])
	if err != nil {
		return fmt.Errorf("envelope: signing bundle: %w", err)
	}
	b.Sig = sig
	return nil
}

// VerifyBundle checks b's signature against pub — the trusted public key the
// caller has bound to b.Signer (the key-service identity). It returns
// ErrBadSignature on any failure. A member MUST call this and get nil before it
// trusts or unwraps anything in the bundle.
func VerifyBundle(b KeyBundle, pub *ecdsa.PublicKey) error {
	if pub == nil || len(b.Sig) == 0 {
		return ErrBadSignature
	}
	sig := b.Sig
	b.Sig = nil // the signature is not part of the transcript it covers
	h := sha256.Sum256(bundleSigningTranscript(b))
	if !ecdsa.VerifyASN1(pub, h[:], sig) {
		return ErrBadSignature
	}
	return nil
}

// SignerPubFromCert extracts the ECDSA public key from an x509 leaf — the
// key-service's SVID — for VerifyBundle. A non-EC leaf is rejected.
func SignerPubFromCert(cert *x509.Certificate) (*ecdsa.PublicKey, error) {
	if cert == nil {
		return nil, errors.New("envelope: nil certificate")
	}
	pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("envelope: signer key is %T, want *ecdsa.PublicKey", cert.PublicKey)
	}
	return pub, nil
}
