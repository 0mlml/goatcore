package onboardstore

import (
	"context"
	"testing"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/pgtest"
)

// The stale-invite archive lane. The operator ask was "a category for revoked
// or expired invites, archived after they are stale for 7 days or more", and
// the two halves of that sentence are what these tests pin: only dead links
// move, and only after the window.

func mustMint(t *testing.T, s *Store, inv *Invite) string {
	t.Helper()
	if _, err := s.MintInvite(context.Background(), inv); err != nil {
		t.Fatalf("mint: %v", err)
	}
	return inv.ID
}

func statusOf(t *testing.T, s *Store, id string) string {
	t.Helper()
	got, err := s.GetInvite(context.Background(), id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return got.Status
}

// The load-bearing test: an invite that died YESTERDAY is still an operator's
// live concern and must stay on the working list. Archiving it early is the
// failure that would make the archive untrustworthy — somebody revokes a link,
// looks for it the next morning, and it is gone.
func TestArchiveStaleInvites_onlyPastTheWindow(t *testing.T) {
	s, err := Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()
	now := time.Now()

	// Expired 8 days ago — past the window.
	oldExpired := mustMint(t, s, &Invite{Env: "zagros", ExpiresAt: now.Add(-8 * 24 * time.Hour).Unix()})
	// Expired 1 day ago — dead, but still this week's business.
	freshExpired := mustMint(t, s, &Invite{Env: "zagros", ExpiresAt: now.Add(-24 * time.Hour).Unix()})
	// Open and well inside its TTL.
	live := mustMint(t, s, &Invite{Env: "zagros", ExpiresAt: now.Add(72 * time.Hour).Unix()})

	moved, err := s.ArchiveStaleInvites(ctx, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(moved) != 1 || moved[0].ID != oldExpired {
		t.Fatalf("want only %s archived, got %+v", oldExpired, moved)
	}
	if got := statusOf(t, s, oldExpired); got != InviteArchived {
		t.Fatalf("old expired: want %q, got %q", InviteArchived, got)
	}
	if got := statusOf(t, s, freshExpired); got == InviteArchived {
		t.Fatalf("invite dead only 1 day was archived — the window is not being honoured")
	}
	if got := statusOf(t, s, live); got != InviteInvited {
		t.Fatalf("live invite: want %q, got %q", InviteInvited, got)
	}
	if moved[0].ArchivedAt == nil || *moved[0].ArchivedAt == 0 {
		t.Fatalf("archived row carries no archived_at")
	}
}

// A revoked invite's staleness runs from the REVOCATION, not from the TTL it
// would otherwise have had. Without this a 30-day link revoked on day one
// clutters the working list for the rest of the month.
func TestArchiveStaleInvites_revokedDatesFromRevocation(t *testing.T) {
	s, err := Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()

	// TTL is far in the future; the revocation is what kills it.
	id := mustMint(t, s, &Invite{Env: "zagros", ExpiresAt: time.Now().Add(30 * 24 * time.Hour).Unix()})
	if err := s.RevokeInvite(ctx, id); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	// Just revoked: not stale yet.
	moved, err := s.ArchiveStaleInvites(ctx, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(moved) != 0 {
		t.Fatalf("a just-revoked invite was archived immediately: %+v", moved)
	}

	// Backdate the revocation past the window; the TTL is untouched and still
	// in the future, so only revoked_at can be driving this.
	if _, err := s.db.ExecContext(ctx,
		`UPDATE onboard_invites SET revoked_at = $1 WHERE id = $2`,
		time.Now().Add(-8*24*time.Hour).Unix(), id); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	moved, err = s.ArchiveStaleInvites(ctx, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("sweep 2: %v", err)
	}
	if len(moved) != 1 || moved[0].ID != id {
		t.Fatalf("want %s archived once its revocation aged out, got %+v", id, moved)
	}
}

// A row revoked before revoked_at existed has no revocation timestamp. It must
// still be sweepable — falling back to the TTL, which is LATER than the truth,
// so a legacy row lingers rather than disappearing early.
func TestArchiveStaleInvites_legacyRevokedFallsBackToTTL(t *testing.T) {
	s, err := Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()

	id := mustMint(t, s, &Invite{Env: "zagros", ExpiresAt: time.Now().Add(-30 * 24 * time.Hour).Unix()})
	if _, err := s.db.ExecContext(ctx,
		`UPDATE onboard_invites SET status = $1, revoked_at = NULL WHERE id = $2`,
		InviteRevoked, id); err != nil {
		t.Fatalf("stage legacy row: %v", err)
	}

	moved, err := s.ArchiveStaleInvites(ctx, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(moved) != 1 || moved[0].ID != id {
		t.Fatalf("legacy revoked row was not archived: %+v", moved)
	}
}

// A submitted invite became a proposal and has its own lifecycle; the sweep
// must never touch it, however old it is.
func TestArchiveStaleInvites_leavesSubmittedAlone(t *testing.T) {
	s, err := Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()

	tok, err := func() (string, error) {
		inv := &Invite{Env: "zagros", ExpiresAt: time.Now().Add(time.Hour).Unix()}
		return s.MintInvite(ctx, inv)
	}()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	inv, err := s.GetInviteByToken(ctx, tok)
	if err != nil {
		t.Fatalf("get by token: %v", err)
	}
	if _, err := s.ConsumeInvite(ctx, tok, func(i *Invite, pid string) (*Proposal, error) {
		return &Proposal{ID: pid, Handle: "someone", Email: "someone@example.test", Env: "zagros"}, nil
	}); err != nil {
		t.Fatalf("consume: %v", err)
	}
	// Age it well past the window.
	if _, err := s.db.ExecContext(ctx,
		`UPDATE onboard_invites SET expires_at = $1 WHERE id = $2`,
		time.Now().Add(-90*24*time.Hour).Unix(), inv.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	moved, err := s.ArchiveStaleInvites(ctx, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(moved) != 0 {
		t.Fatalf("sweep touched a submitted invite: %+v", moved)
	}
	if got := statusOf(t, s, inv.ID); got != InviteSubmitted {
		t.Fatalf("submitted invite: want %q, got %q", InviteSubmitted, got)
	}
}

// Running the sweep twice must be a no-op the second time — it runs on a timer.
func TestArchiveStaleInvites_idempotent(t *testing.T) {
	s, err := Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()
	mustMint(t, s, &Invite{Env: "zagros", ExpiresAt: time.Now().Add(-8 * 24 * time.Hour).Unix()})

	first, err := s.ArchiveStaleInvites(ctx, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("sweep 1: %v", err)
	}
	second, err := s.ArchiveStaleInvites(ctx, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("sweep 2: %v", err)
	}
	if len(first) != 1 || len(second) != 0 {
		t.Fatalf("want 1 then 0, got %d then %d", len(first), len(second))
	}
}

// An archived invite is exactly as dead as it was before: the token still fails
// closed. Archiving is a display move, and this is the test that says so.
func TestArchivedInviteStillFailsClosed(t *testing.T) {
	s, err := Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()

	inv := &Invite{Env: "zagros", ExpiresAt: time.Now().Add(-8 * 24 * time.Hour).Unix()}
	tok, err := s.MintInvite(ctx, inv)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if _, err := s.ArchiveStaleInvites(ctx, 7*24*time.Hour); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if _, err := s.ConsumeInvite(ctx, tok, func(*Invite, string) (*Proposal, error) {
		return &Proposal{}, nil
	}); err == nil {
		t.Fatalf("an archived invite was consumable — archiving must not resurrect a link")
	}
}

// A zero window must not mean "archive everything dead right now".
func TestArchiveStaleInvites_zeroWindowUsesDefault(t *testing.T) {
	s, err := Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()
	id := mustMint(t, s, &Invite{Env: "zagros", ExpiresAt: time.Now().Add(-24 * time.Hour).Unix()})

	moved, err := s.ArchiveStaleInvites(ctx, 0)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(moved) != 0 {
		t.Fatalf("zero window archived a 1-day-dead invite: %+v", moved)
	}
	// Reads as expired (GetInvite resolves TTL on read, by design) — the point
	// is that it is NOT archived.
	if got := statusOf(t, s, id); got == InviteArchived {
		t.Fatalf("zero window archived a 1-day-dead invite")
	}
}
