package handlers

import (
	"testing"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// The invite detail pane answers "why is this still here" by showing when a
// link died and, when the sweep is armed, when it will archive. inviteToView
// derives those times from the store row so the UI and the sweep share one
// definition of "dead since when".
func TestInviteToView_ArchiveTimes_ExpiredAndRevoked(t *testing.T) {
	archiveAfter := 7 * 24 * time.Hour
	now := time.Now().UTC()

	// Expired row (stored as InviteExpired): staleness anchors at the TTL.
	expires := now.Add(-48 * time.Hour)
	expired := onboardstore.Invite{
		ID:        "inv-expired",
		CreatedAt: expires.Add(-72 * time.Hour).Unix(),
		ExpiresAt: expires.Unix(),
		Status:    onboardstore.InviteExpired,
	}
	ve := inviteToView(expired, archiveAfter)
	if ve.State != "expired" {
		t.Fatalf("expired invite state = %q, want expired", ve.State)
	}
	wantDead := expires.Format(time.RFC3339)
	if ve.DeadAt != wantDead {
		t.Fatalf("expired.DeadAt = %q, want %q", ve.DeadAt, wantDead)
	}
	wantArchivesAt := expires.Add(archiveAfter).UTC().Format(time.RFC3339)
	if ve.ArchivesAt != wantArchivesAt {
		t.Fatalf("expired.ArchivesAt = %q, want %q", ve.ArchivesAt, wantArchivesAt)
	}
	if ve.ArchivesIn == "" {
		t.Fatalf("expired.ArchivesIn is empty; want a humanized window remaining")
	}

	// Revoked row: staleness anchors at the revocation, not the TTL.
	revokedAt := now.Add(-24 * time.Hour)
	revokedAtUnix := revokedAt.Unix()
	revoked := onboardstore.Invite{
		ID:        "inv-revoked",
		CreatedAt: now.Add(-96 * time.Hour).Unix(),
		ExpiresAt: now.Add(30 * 24 * time.Hour).Unix(), // far in the future
		Status:    onboardstore.InviteRevoked,
		RevokedAt: &revokedAtUnix,
	}
	vr := inviteToView(revoked, archiveAfter)
	if vr.State != "revoked" {
		t.Fatalf("revoked invite state = %q, want revoked", vr.State)
	}
	wantDeadR := revokedAt.Format(time.RFC3339)
	if vr.DeadAt != wantDeadR {
		t.Fatalf("revoked.DeadAt = %q, want %q (revocation time)", vr.DeadAt, wantDeadR)
	}
	wantArchivesAtR := revokedAt.Add(archiveAfter).UTC().Format(time.RFC3339)
	if vr.ArchivesAt != wantArchivesAtR {
		t.Fatalf("revoked.ArchivesAt = %q, want %q", vr.ArchivesAt, wantArchivesAtR)
	}
	if vr.ArchivesIn == "" {
		t.Fatalf("revoked.ArchivesIn is empty; want a humanized window remaining")
	}

	// Legacy revoked row: RevokedAt nil with a future TTL. Staleness (and
	// archiving) anchor at the TTL, but the view does not claim a revocation
	// date in the future.
	legacyExpires := now.Add(20 * 24 * time.Hour)
	legacyRevoked := onboardstore.Invite{
		ID:        "inv-legacy-revoked",
		CreatedAt: legacyExpires.Add(-96 * time.Hour).Unix(),
		ExpiresAt: legacyExpires.Unix(),
		Status:    onboardstore.InviteRevoked,
	}
	vl := inviteToView(legacyRevoked, archiveAfter)
	if vl.State != "revoked" {
		t.Fatalf("legacy revoked state = %q, want revoked", vl.State)
	}
	if vl.DeadAt != "" || vl.DeadAtShort != "" || vl.DeadAgo != "" {
		t.Fatalf("legacy revoked must not carry a death moment; got DeadAt=%q DeadAtShort=%q DeadAgo=%q", vl.DeadAt, vl.DeadAtShort, vl.DeadAgo)
	}
	wantArchivesAtLegacy := legacyExpires.Add(archiveAfter).UTC().Format(time.RFC3339)
	if vl.ArchivesAt != wantArchivesAtLegacy {
		t.Fatalf("legacy revoked ArchivesAt = %q, want %q", vl.ArchivesAt, wantArchivesAtLegacy)
	}
	if vl.ArchivesIn == "" {
		t.Fatalf("legacy revoked ArchivesIn is empty; want a humanized window remaining")
	}
}

// An invite still stored as "invited" but past its TTL is DISPLAY-expired and
// archives off the TTL. When the sweep is disabled (archiveAfter = 0), the
// view still records when it died but omits any archive time.
func TestInviteToView_InvitedPastTTL_SweepOff(t *testing.T) {
	now := time.Now().UTC()
	expires := now.Add(-2 * time.Hour)
	inv := onboardstore.Invite{
		ID:        "inv-stale-open",
		CreatedAt: expires.Add(-72 * time.Hour).Unix(),
		ExpiresAt: expires.Unix(),
		Status:    onboardstore.InviteInvited,
	}
	v := inviteToView(inv, 0)
	if v.State != "expired" {
		t.Fatalf("stale invited state = %q, want expired", v.State)
	}
	wantDead := expires.Format(time.RFC3339)
	if v.DeadAt != wantDead {
		t.Fatalf("stale invited DeadAt = %q, want %q", v.DeadAt, wantDead)
	}
	if v.ArchivesAt != "" || v.ArchivesIn != "" {
		t.Fatalf("sweep-off view must not set ArchivesAt/ArchivesIn; got %q / %q", v.ArchivesAt, v.ArchivesIn)
	}
}

// Archived rows report when they were archived and how long ago, but no future
// archive time.
func TestInviteToView_ArchivedCarriesArchivedAtOnly(t *testing.T) {
	archivedAt := time.Now().UTC().Add(-24 * time.Hour)
	archivedAtUnix := archivedAt.Unix()
	inv := onboardstore.Invite{
		ID:         "inv-archived",
		CreatedAt:  archivedAt.Add(-96 * time.Hour).Unix(),
		ExpiresAt:  archivedAt.Add(-72 * time.Hour).Unix(),
		Status:     onboardstore.InviteArchived,
		ArchivedAt: &archivedAtUnix,
	}
	v := inviteToView(inv, 7*24*time.Hour)
	if v.State != "archived" {
		t.Fatalf("archived state = %q, want archived", v.State)
	}
	wantArchived := archivedAt.Format(time.RFC3339)
	if v.ArchivedAt != wantArchived {
		t.Fatalf("ArchivedAt = %q, want %q", v.ArchivedAt, wantArchived)
	}
	if v.ArchivedAgo == "" {
		t.Fatalf("ArchivedAgo is empty; want a humanized age")
	}
	if v.DeadAt != "" || v.ArchivesAt != "" || v.ArchivesIn != "" {
		t.Fatalf("archived view must not carry future archive fields; got DeadAt=%q ArchivesAt=%q ArchivesIn=%q", v.DeadAt, v.ArchivesAt, v.ArchivesIn)
	}
}

func TestHumanArchiveWindow(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{0, ""},
		{-1 * time.Hour, ""},
		{24 * time.Hour, "1 day"},
		{7 * 24 * time.Hour, "7 days"},
		{time.Hour, "1 hour"},
		{12 * time.Hour, "12 hours"},
		{24*time.Hour + time.Hour, "25 hours"},
		{30 * time.Minute, "30m"},                // fallback: twoUnit, not d.String()
		{90 * time.Minute, "1h 30m"},             // fallback: two units, space-separated
		{25*time.Hour + 30*time.Minute, "1d 1h"}, // fallback past a day truncates to two units like the rest of the page
	} {
		if got := humanArchiveWindow(tc.d); got != tc.want {
			t.Errorf("humanArchiveWindow(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
