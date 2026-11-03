package onboardstore

import (
	"context"
	"testing"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/pgtest"
)

// ListInvites must not report a link that has already died as still open. The
// web page resolves TTL in its view layer and the store does too, so
// `goat admin invites ls` and /onboarding agree about the same row; the
// reassuring wrong answer is the direction that hides a problem.
func TestListInvites_expiredByTTLIsNotReportedOpen(t *testing.T) {
	s, err := Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	ctx := context.Background()

	dead := &Invite{Env: "zagros", ExpiresAt: time.Now().Add(-time.Minute).Unix()}
	if _, err := s.MintInvite(ctx, dead); err != nil {
		t.Fatalf("mint dead: %v", err)
	}
	live := &Invite{Env: "zagros", ExpiresAt: time.Now().Add(72 * time.Hour).Unix()}
	if _, err := s.MintInvite(ctx, live); err != nil {
		t.Fatalf("mint live: %v", err)
	}

	open, err := s.ListInvites(ctx, InviteInvited)
	if err != nil {
		t.Fatalf("list invited: %v", err)
	}
	for _, inv := range open {
		if inv.ID == dead.ID {
			t.Fatal("an invite past its TTL must not be listed as open")
		}
	}
	if len(open) != 1 || open[0].ID != live.ID {
		t.Fatalf("expected only the live invite, got %d rows", len(open))
	}

	// ...and it must be findable where it now belongs, not vanish entirely.
	expired, err := s.ListInvites(ctx, InviteExpired)
	if err != nil {
		t.Fatalf("list expired: %v", err)
	}
	if len(expired) != 1 || expired[0].ID != dead.ID {
		t.Fatalf("expired filter must surface the dead invite, got %d rows", len(expired))
	}
}
