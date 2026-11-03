package main

import (
	"crypto/rand"
	"fmt"

	"golang.org/x/crypto/curve25519"
)

// generateWGKeypair returns a (pubkey, privkey) Curve25519 keypair
// suitable for WireGuard (including wg-cp0). The private key is clamped
// per RFC 7748 §5: low 3 bits cleared, high bit cleared, second-high bit
// set. WireGuard expects clamped scalars; not clamping yields a privkey
// that produces incorrect handshake material on some peers.
//
// Both halves are 32 bytes. Caller is responsible for zeroing the privkey
// when done with it.
func generateWGKeypair() (pub, priv []byte, err error) {
	priv = make([]byte, curve25519.ScalarSize)
	if _, err := rand.Read(priv); err != nil {
		return nil, nil, fmt.Errorf("rand read: %w", err)
	}
	clampPrivateKey(priv)

	pub, err = curve25519.X25519(priv, curve25519.Basepoint)
	if err != nil {
		return nil, nil, fmt.Errorf("curve25519 derive pub: %w", err)
	}
	return pub, priv, nil
}

// clampPrivateKey applies the RFC 7748 §5 X25519 clamping to a 32-byte
// scalar in place. WireGuard's wg(8) does this internally on `wg genkey`;
// we replicate it here so generated keys interop with peers that compare
// derived pubkeys (e.g. wg-cp0-bundle-apply's sanity check).
func clampPrivateKey(k []byte) {
	k[0] &= 248
	k[31] &= 127
	k[31] |= 64
}
