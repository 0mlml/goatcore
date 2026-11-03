// Package store holds the invite-token store. v1 is in-memory behind
// the InviteStore interface; v2 will swap in a Postgres-backed impl.
package store

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

var (
	ErrInviteNotFound = errors.New("invite not found")
	ErrInviteUsed     = errors.New("invite already used")
	ErrInviteRevoked  = errors.New("invite has been revoked")
	ErrInviteExpired  = errors.New("invite has expired")
)

type Invite struct {
	Token        string     `json:"token"`
	IdPKey       string     `json:"idp_key"`
	PresetGroups []string   `json:"preset_groups"`
	TTLAt        time.Time  `json:"ttl_at"`
	CreatedBy    string     `json:"created_by"`
	CreatedAt    time.Time  `json:"created_at"`
	UsedAt       *time.Time `json:"used_at,omitempty"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
	UsedByEmail  string     `json:"used_by_email,omitempty"`
	// Email is the address the admin BOUND the invite to at mint, or "" for
	// an invite whose redeemer types their own. Only a bound address is
	// imported verified on accept: the accept route is
	// unauthenticated and its link is shown on screen, so a typed address
	// proves nothing about the mailbox.
	Email string `json:"email,omitempty"`
}

// Status returns "active", "used", "revoked", or "expired" for UI rendering.
func (i Invite) Status() string {
	if i.RevokedAt != nil {
		return "revoked"
	}
	if i.UsedAt != nil {
		return "used"
	}
	if time.Now().After(i.TTLAt) {
		return "expired"
	}
	return "active"
}

type InviteStore interface {
	Create(idpKey, createdBy, email string, presetGroups []string, ttl time.Duration) (*Invite, error)
	Get(token string) (*Invite, error)
	List() ([]Invite, error)
	MarkUsed(token, byEmail string) error
	Revoke(token, byEmail string) error
}

type Memory struct {
	mu      sync.RWMutex
	invites map[string]*Invite
}

func NewMemory() *Memory {
	return &Memory{invites: map[string]*Invite{}}
}

func (m *Memory) Create(idpKey, createdBy, email string, presetGroups []string, ttl time.Duration) (*Invite, error) {
	tok, err := mintToken()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	inv := &Invite{
		Token:        tok,
		IdPKey:       idpKey,
		PresetGroups: presetGroups,
		TTLAt:        now.Add(ttl),
		CreatedBy:    createdBy,
		CreatedAt:    now,
		Email:        email,
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.invites[tok] = inv
	return inv, nil
}

func (m *Memory) Get(token string) (*Invite, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	inv, ok := m.invites[token]
	if !ok {
		return nil, ErrInviteNotFound
	}
	// Return a copy so callers can't mutate the stored row.
	cp := *inv
	return &cp, nil
}

// GetForAccept returns the invite with all not-acceptable conditions converted to errors.
// Callers use this on the accept path to fail fast before any Kratos work.
func (m *Memory) GetForAccept(token string) (*Invite, error) {
	inv, err := m.Get(token)
	if err != nil {
		return nil, err
	}
	if inv.RevokedAt != nil {
		return nil, ErrInviteRevoked
	}
	if inv.UsedAt != nil {
		return nil, ErrInviteUsed
	}
	if time.Now().After(inv.TTLAt) {
		return nil, ErrInviteExpired
	}
	return inv, nil
}

func (m *Memory) List() ([]Invite, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Invite, 0, len(m.invites))
	for _, inv := range m.invites {
		out = append(out, *inv)
	}
	return out, nil
}

func (m *Memory) MarkUsed(token, byEmail string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	inv, ok := m.invites[token]
	if !ok {
		return ErrInviteNotFound
	}
	if inv.RevokedAt != nil {
		return ErrInviteRevoked
	}
	if inv.UsedAt != nil {
		return ErrInviteUsed
	}
	if time.Now().After(inv.TTLAt) {
		return ErrInviteExpired
	}
	now := time.Now().UTC()
	inv.UsedAt = &now
	inv.UsedByEmail = byEmail
	return nil
}

func (m *Memory) Revoke(token, byEmail string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	inv, ok := m.invites[token]
	if !ok {
		return ErrInviteNotFound
	}
	if inv.UsedAt != nil {
		return ErrInviteUsed
	}
	if inv.RevokedAt != nil {
		// Idempotent — already revoked is fine.
		return nil
	}
	now := time.Now().UTC()
	inv.RevokedAt = &now
	return nil
}

// mintToken returns a 32-byte URL-safe random token. base64url with
// no padding gives 43 chars — short enough to fit in a URL but with
// 256 bits of entropy.
func mintToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
