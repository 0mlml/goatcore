// Package identity handles peer identity tokenization and encryption.
//
// Peer IPs and names are sensitive — they reveal mesh topology and can be
// used to deduce real-world positioning. This package:
//   - Tokenizes peer identifiers with HMAC-SHA256 (deterministic, keyed)
//   - Encrypts identity records with AES-256-GCM (data at rest protection)
//
// If the database file is exfiltrated without the master key, an attacker
// sees only opaque tokens and encrypted blobs — no peer names, IPs, or topology.
package identity

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// Manager handles peer identity tokenization and encryption.
type Manager struct {
	tokenKey [32]byte
	gcm      cipher.AEAD
}

// NewManager creates an identity manager from a master key (min 16 bytes).
// Derives separate sub-keys for HMAC tokenization and AES encryption.
func NewManager(masterKey []byte) (*Manager, error) {
	if len(masterKey) < 16 {
		return nil, fmt.Errorf("master key must be at least 16 bytes, got %d", len(masterKey))
	}
	m := &Manager{}
	m.tokenKey = sha256.Sum256(append([]byte("snitch-hmac:"), masterKey...))
	encryptKey := sha256.Sum256(append([]byte("snitch-aes:"), masterKey...))

	block, err := aes.NewCipher(encryptKey[:])
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	m.gcm, err = cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	return m, nil
}

// LoadOrGenerateKey reads a 32-byte key from path, or generates and saves one.
func LoadOrGenerateKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil && len(data) >= 32 {
		return data[:32], nil
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	if err := os.WriteFile(path, key, 0600); err != nil {
		return nil, fmt.Errorf("save key to %s: %w", path, err)
	}
	return key, nil
}

// Tokenize returns a stable 16-char hex token for a peer identifier (IP or name).
// Same input + same key = same token. Cannot be reversed without the key.
func (m *Manager) Tokenize(peerID string) string {
	h := hmac.New(sha256.New, m.tokenKey[:])
	h.Write([]byte(peerID))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// Encrypt encrypts plaintext with AES-256-GCM. Returns nonce||ciphertext.
func (m *Manager) Encrypt(plaintext string) ([]byte, error) {
	nonce := make([]byte, m.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return m.gcm.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// Decrypt decrypts ciphertext produced by Encrypt.
func (m *Manager) Decrypt(ciphertext []byte) (string, error) {
	ns := m.gcm.NonceSize()
	if len(ciphertext) < ns {
		return "", fmt.Errorf("ciphertext too short")
	}
	plain, err := m.gcm.Open(nil, ciphertext[:ns], ciphertext[ns:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
