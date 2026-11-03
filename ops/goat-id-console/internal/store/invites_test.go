package store

import (
	"errors"
	"testing"
	"time"
)

func TestCreateAndGet(t *testing.T) {
	m := NewMemory()
	inv, err := m.Create("tenant", "admin@example.com", "", []string{"VENDOR"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Token == "" {
		t.Fatal("empty token")
	}
	got, err := m.Get(inv.Token)
	if err != nil {
		t.Fatal(err)
	}
	if got.CreatedBy != "admin@example.com" {
		t.Errorf("CreatedBy = %q", got.CreatedBy)
	}
	if got.IdPKey != "tenant" {
		t.Errorf("IdPKey = %q", got.IdPKey)
	}
	if got.Status() != "active" {
		t.Errorf("Status = %q, want active", got.Status())
	}
}

func TestMarkUsedTwiceRefuses(t *testing.T) {
	m := NewMemory()
	inv, _ := m.Create("tenant", "a@b", "", []string{"VENDOR"}, time.Hour)
	if err := m.MarkUsed(inv.Token, "new@user"); err != nil {
		t.Fatal(err)
	}
	err := m.MarkUsed(inv.Token, "again@user")
	if !errors.Is(err, ErrInviteUsed) {
		t.Errorf("second MarkUsed = %v, want ErrInviteUsed", err)
	}
}

func TestRevokeBlocksAccept(t *testing.T) {
	m := NewMemory()
	inv, _ := m.Create("tenant", "a@b", "", []string{"VENDOR"}, time.Hour)
	if err := m.Revoke(inv.Token, "a@b"); err != nil {
		t.Fatal(err)
	}
	_, err := m.GetForAccept(inv.Token)
	if !errors.Is(err, ErrInviteRevoked) {
		t.Errorf("GetForAccept after revoke = %v, want ErrInviteRevoked", err)
	}
}

func TestExpiredBlocksAccept(t *testing.T) {
	m := NewMemory()
	// 1-nanosecond TTL; sleep past it.
	inv, _ := m.Create("tenant", "a@b", "", []string{"VENDOR"}, time.Nanosecond)
	time.Sleep(5 * time.Millisecond)
	_, err := m.GetForAccept(inv.Token)
	if !errors.Is(err, ErrInviteExpired) {
		t.Errorf("GetForAccept after TTL = %v, want ErrInviteExpired", err)
	}
}

func TestTokenEntropy(t *testing.T) {
	m := NewMemory()
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		inv, err := m.Create("tenant", "a@b", "", []string{"VENDOR"}, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if seen[inv.Token] {
			t.Fatalf("duplicate token in 100 mints: %s", inv.Token)
		}
		seen[inv.Token] = true
		if len(inv.Token) < 40 {
			t.Errorf("token len %d, want >= 40 (base64url-32-bytes)", len(inv.Token))
		}
	}
}

// A bound address survives the round trip; an unbound invite carries none.
func TestCreateBindsTheEmail(t *testing.T) {
	m := NewMemory()
	bound, err := m.Create("tenant", "admin@example.com", "alice@example.org", []string{"VENDOR"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.GetForAccept(bound.Token)
	if err != nil {
		t.Fatal(err)
	}
	if got.Email != "alice@example.org" {
		t.Errorf("Email = %q, want the bound address", got.Email)
	}
	free, _ := m.Create("tenant", "admin@example.com", "", []string{"VENDOR"}, time.Hour)
	if free.Email != "" {
		t.Errorf("unbound invite carries Email %q", free.Email)
	}
}
