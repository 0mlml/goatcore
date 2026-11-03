// Package envelope is the grant-keyed-encryption crypto core (data-access M2,
// phase E0). It provides two primitives:
//
//   - the payload ENVELOPE: AES-256-GCM sealing of a stream payload, bound to a
//     (release-set, generation) pair so a consumer holding the wrong generation's
//     key fails to decrypt cleanly rather than reading stale plaintext; and
//   - DEK WRAPPING (dek.go): sealing the 256-bit data key to one recipient's EC
//     public key (the SPIFFE SVID leaf), so only that cert-process can recover it.
//
// The release-set + generation are bound as the AEAD's additional authenticated
// data, which is what makes the generation tag load-bearing: re-labeling a
// gen-N ciphertext as gen-N+1, or opening it with the wrong release-set's key,
// fails authentication. This package is transport-agnostic; the carrier
// (a Zenoh attachment, per grant-keyed-encryption.md §7) lives above it.
//
// Standard library only — no third-party crypto.
package envelope

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
)

// KeyLen is the DEK / AES-256 key length in bytes.
const KeyLen = 32

// nonceLen is the AES-GCM nonce length (96-bit, the standard/efficient size).
const nonceLen = 12

// Suite identifies the cryptographic suite an Envelope / WrappedDEK / KeyBundle
// was produced with. It rides the wire format and is bound into the AEAD's
// additional authenticated data, so a suite cannot be silently downgraded and a
// consumer that does not implement a named suite fails closed
// (ErrUnsupportedSuite) rather than misinterpreting bytes. This is the
// crypto-agility seam: a future post-quantum suite (a hybrid ML-KEM wrap) is
// added as a new identifier and old/new coexist across generations. See
// WIRE-FORMAT.md.
type Suite string

const (
	// SuiteV1ECDHP256 is the classical suite: AES-256-GCM payloads; the DEK wrapped
	// by ephemeral-static ECDH on P-256 → HKDF-SHA256 → AES-256-GCM. The only suite
	// implemented today.
	SuiteV1ECDHP256 Suite = "v1-ecdh-p256"
	// SuiteV2X25519MLKEM768 is the reserved post-quantum suite (a hybrid X25519 +
	// ML-KEM-768 wrap). The identifier is fixed here so the format is stable; the
	// suite is NOT yet implemented.
	SuiteV2X25519MLKEM768 Suite = "v2-x25519-mlkem768"
)

// supported reports whether this build implements the suite.
func (s Suite) supported() bool { return s == SuiteV1ECDHP256 }

// Generation numbers a release-set's DEK. It increments on every key change
// (rotation / revoke). Bound into every envelope so cross-generation reuse is
// caught by the AEAD, not by convention.
type Generation uint64

// DEK is a 256-bit data-encryption key. One live DEK per (release-set, generation).
type DEK [KeyLen]byte

// NewDEK returns a fresh random DEK from the system CSPRNG.
func NewDEK() (DEK, error) {
	var k DEK
	if _, err := rand.Read(k[:]); err != nil {
		return DEK{}, fmt.Errorf("envelope: generating DEK: %w", err)
	}
	return k, nil
}

// Envelope is a sealed payload plus the routing/keying context a consumer needs
// to pick the right DEK. Everything here is safe to transmit in the clear; the
// confidentiality is in Ciphertext, the integrity in the GCM tag it carries, and
// the ReleaseSet/Generation are authenticated (tampering with them breaks Open).
type Envelope struct {
	Suite      Suite      `json:"suite"`
	ReleaseSet string     `json:"rs"`
	Generation Generation `json:"gen"`
	Nonce      []byte     `json:"nonce"`
	Ciphertext []byte     `json:"ct"` // AES-256-GCM output: ciphertext || tag
}

var (
	// ErrBadKey is returned when a DEK is the wrong length.
	ErrBadKey = errors.New("envelope: DEK must be 32 bytes")
	// ErrDecrypt is returned when authentication fails — a wrong DEK, a wrong
	// generation/release-set (they are authenticated), or tampering. It is
	// deliberately opaque: the caller learns only that this key cannot open this
	// envelope, never why.
	ErrDecrypt = errors.New("envelope: cannot decrypt (wrong key, wrong generation, or tampered)")
	// ErrUnsupportedSuite is returned when an artifact names a suite this build
	// does not implement. It is distinct from ErrDecrypt because it is a capability
	// signal (upgrade needed), not a key/authentication failure, and reveals
	// nothing about keys — so returning it before attempting decryption is safe.
	ErrUnsupportedSuite = errors.New("envelope: unsupported cryptographic suite")
)

// aad binds the keying context into the AEAD. Length-prefixing suite and
// release-set makes the encoding unambiguous, so no two distinct (suite,
// release-set) pairs can collide into the same authenticated data. Binding the
// suite is what makes a downgrade to a different suite fail authentication.
func aad(suite Suite, releaseSet string, gen Generation, additional []byte) []byte {
	su := []byte(suite)
	rs := []byte(releaseSet)
	b := make([]byte, 0, 8+len(su)+8+len(rs)+8+len(additional))
	var lp [8]byte
	binary.BigEndian.PutUint64(lp[:], uint64(len(su)))
	b = append(b, lp[:]...)
	b = append(b, su...)
	binary.BigEndian.PutUint64(lp[:], uint64(len(rs)))
	b = append(b, lp[:]...)
	b = append(b, rs...)
	binary.BigEndian.PutUint64(lp[:], uint64(gen))
	b = append(b, lp[:]...)
	b = append(b, additional...)
	return b
}

func gcm(dek DEK) (cipher.AEAD, error) {
	block, err := aes.NewCipher(dek[:])
	if err != nil {
		return nil, ErrBadKey
	}
	return cipher.NewGCM(block)
}

// Seal encrypts plaintext under dek and binds it to (releaseSet, generation).
// additional is optional extra AAD (e.g. the Zenoh key expression) that a
// consumer must supply identically to Open — pass nil if unused. A fresh random
// nonce is generated per call.
func Seal(dek DEK, releaseSet string, gen Generation, plaintext, additional []byte) (Envelope, error) {
	aead, err := gcm(dek)
	if err != nil {
		return Envelope{}, err
	}
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return Envelope{}, fmt.Errorf("envelope: generating nonce: %w", err)
	}
	suite := SuiteV1ECDHP256
	ct := aead.Seal(nil, nonce, plaintext, aad(suite, releaseSet, gen, additional))
	return Envelope{
		Suite:      suite,
		ReleaseSet: releaseSet,
		Generation: gen,
		Nonce:      nonce,
		Ciphertext: ct,
	}, nil
}

// Open authenticates and decrypts env under dek. It returns ErrDecrypt if dek is
// wrong, if the caller-supplied additional AAD differs from Seal, or if the
// envelope's ReleaseSet/Generation/Nonce/Ciphertext were altered — the
// generation and release-set are authenticated, so this is how a stale-key
// consumer is stopped rather than served old plaintext.
func Open(dek DEK, env Envelope, additional []byte) ([]byte, error) {
	if !env.Suite.supported() {
		return nil, ErrUnsupportedSuite
	}
	aead, err := gcm(dek)
	if err != nil {
		return nil, err
	}
	if len(env.Nonce) != nonceLen {
		return nil, ErrDecrypt
	}
	pt, err := aead.Open(nil, env.Nonce, env.Ciphertext, aad(env.Suite, env.ReleaseSet, env.Generation, additional))
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}
