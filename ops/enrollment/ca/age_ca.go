package ca

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"os"

	"filippo.io/age"
)

// AgeSigner loads an ECDSA P-256 private key from an age-encrypted file.
//
// Block 79 hard cutover: pre-rotation this type held an Ed25519 key. The new
// shape uses ECDSA P-256 for native trust-store import on macOS / iOS /
// Android / Windows / Linux per F-090 closure path C/D. The on-disk format
// is x509.MarshalECPrivateKey (SEC 1 EC private key DER bytes), age-encrypted
// to the operator recipient set.
//
// Dev fallback only. Not safe to ship bundles signed by an AgeSigner to a
// production site — any operator with the age recipient key plus repo access
// can mint bundles. Promote to YubikeySigner once hardware is received.
type AgeSigner struct {
	id   string
	priv *ecdsa.PrivateKey
}

// NewAgeSigner decrypts the private key at path using the given identities
// (the age private keys that can decrypt the file).
func NewAgeSigner(id, path string, identities []age.Identity) (*AgeSigner, error) {
	if len(identities) == 0 {
		return nil, errors.New("at least one age identity is required")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open ca key: %w", err)
	}
	defer f.Close()

	r, err := age.Decrypt(f, identities...)
	if err != nil {
		return nil, fmt.Errorf("age decrypt: %w", err)
	}

	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, r); err != nil {
		return nil, fmt.Errorf("read decrypted key: %w", err)
	}

	priv, err := x509.ParseECPrivateKey(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("parse EC private key: %w", err)
	}
	if priv.Curve != elliptic.P256() {
		return nil, fmt.Errorf("unexpected EC curve: got %s want P-256", priv.Curve.Params().Name)
	}
	return &AgeSigner{id: id, priv: priv}, nil
}

// GenerateAndWrite creates a new ECDSA P-256 keypair and writes the private
// key encrypted for the given recipients to path. The file is created with
// mode 0600 and the call fails if path already exists — CA rotation goes
// through a separate ceremony rather than silently clobbering the old key.
//
// Returns the public key for distribution.
func GenerateAndWrite(path string, recipients []age.Recipient) (*ecdsa.PublicKey, error) {
	if len(recipients) == 0 {
		return nil, errors.New("at least one age recipient is required")
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ecdsa p-256: %w", err)
	}

	der, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("marshal ec private key: %w", err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create ca key file: %w", err)
	}
	if err := writeEncryptedKey(f, der, recipients); err != nil {
		return nil, err
	}
	return &priv.PublicKey, nil
}

// writeEncryptedKey owns f and finishes both the encrypted stream and file
// before reporting success to the key-generation caller.
func writeEncryptedKey(f io.WriteCloser, der []byte, recipients []age.Recipient) (err error) {
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close ca key file: %w", closeErr))
		}
	}()

	w, err := age.Encrypt(f, recipients...)
	if err != nil {
		return fmt.Errorf("age encrypt: %w", err)
	}
	if _, err := w.Write(der); err != nil {
		return fmt.Errorf("write encrypted private key: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close age writer: %w", err)
	}
	return nil
}

// Sign implements Signer. Produces an ECDSA P-256 signature in ASN.1 DER
// form (ECDSA-Sig-Value) over SHA-256(msg).
func (s *AgeSigner) Sign(msg []byte) ([]byte, error) {
	digest := sha256.Sum256(msg)
	return s.SignDigest(digest[:])
}

// SignDigest implements Signer. Signs an already-hashed 32-byte digest.
// Used by the crypto.Signer adapter consumed by crypto/x509.CreateCertificate.
func (s *AgeSigner) SignDigest(digest []byte) ([]byte, error) {
	if s.priv == nil {
		return nil, errors.New("age signer closed")
	}
	if len(digest) != sha256.Size {
		return nil, fmt.Errorf("digest wrong size: got %d want %d", len(digest), sha256.Size)
	}
	return ecdsa.SignASN1(rand.Reader, s.priv, digest)
}

// PublicKey implements Signer.
func (s *AgeSigner) PublicKey() *ecdsa.PublicKey {
	if s.priv == nil {
		return nil
	}
	return &s.priv.PublicKey
}

// ID implements Signer.
func (s *AgeSigner) ID() string { return s.id }

// Close zeroes the in-memory private scalar. Safe to call multiple times.
// ecdsa.PrivateKey.D is a *big.Int; we set its bits to 0 by zero-allocating
// a new big.Int and assigning, then nil out the struct.
func (s *AgeSigner) Close() error {
	if s.priv != nil && s.priv.D != nil {
		s.priv.D.SetInt64(0)
	}
	s.priv = nil
	return nil
}
