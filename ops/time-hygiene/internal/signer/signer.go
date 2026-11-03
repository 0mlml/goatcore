// Package signer provides Ed25519 attestation signing for ntp-health-agent
// heartbeats (Block 12D). The signing key is the node's attestation private key
// carried in the offline-enrollment bundle's ntp_attestation_pubkey field.
package signer

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
)

// Signer holds an Ed25519 private key and signs canonical JSON payloads.
type Signer struct {
	priv ed25519.PrivateKey
}

// Load reads an Ed25519 private key from a PEM file. Two encodings are
// accepted, because two producers exist in the fleet:
//   - a raw 32-byte seed (what Generate below writes), and
//   - PKCS#8 (what `openssl genpkey -algorithm Ed25519` writes — the form the
//     ntp-health-agent ansible role generates on each host).
// Accepting both is load-bearing: with `--attestation-key` pointed at the
// role-generated PKCS#8 key, a seed-only Load would fail and the agent (which
// treats an explicit-but-unloadable key as fatal) would crash-loop on boot.
func Load(path string) (*Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read attestation key %s: %w", path, err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block in %s", path)
	}
	// Raw 32-byte seed (Generate's format).
	if len(block.Bytes) == ed25519.SeedSize {
		return &Signer{priv: ed25519.NewKeyFromSeed(block.Bytes)}, nil
	}
	// PKCS#8 (openssl's format).
	if key, perr := x509.ParsePKCS8PrivateKey(block.Bytes); perr == nil {
		ed, ok := key.(ed25519.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("PKCS#8 key in %s is %T, not ed25519.PrivateKey", path, key)
		}
		return &Signer{priv: ed}, nil
	}
	return nil, fmt.Errorf("unsupported key format in %s (expected raw-seed or PKCS#8 Ed25519 PEM)", path)
}

// Generate creates a new Ed25519 keypair, writes the private key to path,
// and returns the Signer along with the 32-byte public key.
func Generate(path string) (*Signer, []byte, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate key: %w", err)
	}
	block := &pem.Block{Type: "ED25519 PRIVATE KEY", Bytes: priv.Seed()}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0600); err != nil {
		return nil, nil, fmt.Errorf("write key to %s: %w", path, err)
	}
	return &Signer{priv: priv}, pub, nil
}

// Sign returns an Ed25519 signature over msg.
func (s *Signer) Sign(msg []byte) []byte {
	return ed25519.Sign(s.priv, msg)
}

// PublicKey returns the 32-byte Ed25519 public key.
func (s *Signer) PublicKey() []byte {
	return s.priv.Public().(ed25519.PublicKey)
}
