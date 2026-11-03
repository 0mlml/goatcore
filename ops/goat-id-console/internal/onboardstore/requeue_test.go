package onboardstore

import (
	"context"
	"testing"
	"time"
)

// RequeueStaleClaims recovers a row a dead runner stranded (goat#977). These
// tests pin the two halves: only a claim that has actually gone stale moves,
// and it moves back to the exact queue state the runner polls.

func backdateClaim(t *testing.T, s *Store, id string, claimedAt int64) {
	t.Helper()
	if _, err := s.db.ExecContext(context.Background(),
		`UPDATE onboard_proposals SET claimed_at = $1 WHERE id = $2`, claimedAt, id); err != nil {
		t.Fatalf("backdate claimed_at for %s: %v", id, err)
	}
}

func TestRequeueStaleClaims_OnboardingPastDeadlineGoesBackToApproved(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)

	p := &Proposal{ID: "prop-stale-on", Sponsor: "s", Env: "zagros", Handle: "h", Email: "h@x",
		Status: ProposalApproved, ManifestJSON: "{}"}
	if err := st.CreateProposal(ctx, p); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.ClaimProposal(ctx, p.ID); err != nil {
		t.Fatalf("claim: %v", err)
	}
	backdateClaim(t, st, p.ID, time.Now().Add(-20*time.Minute).Unix())

	moved, err := st.RequeueStaleClaims(ctx, 10*time.Minute)
	if err != nil {
		t.Fatalf("RequeueStaleClaims: %v", err)
	}
	if len(moved) != 1 || moved[0].ID != p.ID {
		t.Fatalf("moved = %+v, want exactly %q", moved, p.ID)
	}

	got, err := st.GetProposal(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetProposal: %v", err)
	}
	if got.Status != ProposalApproved {
		t.Fatalf("status = %q, want %q (back on the queue the runner polls)", got.Status, ProposalApproved)
	}
	if got.ClaimedAt != 0 {
		t.Errorf("claimed_at = %d, want 0 (no longer claimed)", got.ClaimedAt)
	}
	if got.Note == "" {
		t.Errorf("note is empty, want a record of why this was re-queued")
	}
}

func TestRequeueStaleClaims_OffboardingPastDeadlineGoesBackToOffboardRequested(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)

	p := &Proposal{ID: "prop-stale-off", Sponsor: "s", Env: "zagros", Handle: "h", Email: "h@x",
		Status: ProposalOffboardRequested, ManifestJSON: "{}"}
	if err := st.CreateProposal(ctx, p); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.ClaimOffboard(ctx, p.ID); err != nil {
		t.Fatalf("claim offboard: %v", err)
	}
	backdateClaim(t, st, p.ID, time.Now().Add(-20*time.Minute).Unix())

	moved, err := st.RequeueStaleClaims(ctx, 10*time.Minute)
	if err != nil {
		t.Fatalf("RequeueStaleClaims: %v", err)
	}
	if len(moved) != 1 || moved[0].ID != p.ID {
		t.Fatalf("moved = %+v, want exactly %q", moved, p.ID)
	}

	got, err := st.GetProposal(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetProposal: %v", err)
	}
	if got.Status != ProposalOffboardRequested {
		t.Fatalf("status = %q, want %q (back on the offboard queue)", got.Status, ProposalOffboardRequested)
	}
	if got.ClaimedAt != 0 {
		t.Errorf("claimed_at = %d, want 0 (no longer claimed)", got.ClaimedAt)
	}
}

// A claim seconds old is a mint/revoke in flight, not a stranded row; the
// sweep must leave it alone, or it double-runs every healthy claim.
func TestRequeueStaleClaims_FreshClaimIsLeftAlone(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)

	p := &Proposal{ID: "prop-fresh", Sponsor: "s", Env: "zagros", Handle: "h", Email: "h@x",
		Status: ProposalApproved, ManifestJSON: "{}"}
	if err := st.CreateProposal(ctx, p); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.ClaimProposal(ctx, p.ID); err != nil {
		t.Fatalf("claim: %v", err)
	}

	moved, err := st.RequeueStaleClaims(ctx, 10*time.Minute)
	if err != nil {
		t.Fatalf("RequeueStaleClaims: %v", err)
	}
	if len(moved) != 0 {
		t.Fatalf("moved = %+v, want none — this claim is seconds old", moved)
	}
	got, err := st.GetProposal(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetProposal: %v", err)
	}
	if got.Status != ProposalOnboarding {
		t.Fatalf("status = %q, want unchanged %q", got.Status, ProposalOnboarding)
	}
}

// A row claimed before this column existed has claimed_at 0. It must fall
// back to created_at and still recover, not stay stranded (the goat#977 bug).
func TestRequeueStaleClaims_ZeroClaimedAtFallsBackToCreatedAt(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)

	old := time.Now().Add(-20 * time.Minute).Unix()
	p := &Proposal{ID: "prop-legacy", Sponsor: "s", Env: "zagros", Handle: "h", Email: "h@x",
		Status: ProposalOnboarding, ManifestJSON: "{}", CreatedAt: old}
	if err := st.CreateProposal(ctx, p); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// claimed_at left at its default 0, as a pre-migration row would be.
	moved, err := st.RequeueStaleClaims(ctx, 10*time.Minute)
	if err != nil {
		t.Fatalf("RequeueStaleClaims: %v", err)
	}
	if len(moved) != 1 || moved[0].ID != p.ID {
		t.Fatalf("moved = %+v, want exactly %q", moved, p.ID)
	}
	got, err := st.GetProposal(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetProposal: %v", err)
	}
	if got.Status != ProposalApproved {
		t.Fatalf("status = %q, want %q", got.Status, ProposalApproved)
	}
}

// A row younger than the deadline with a zero claimed_at must NOT be treated
// as ancient — the created_at fallback must still respect the window.
func TestRequeueStaleClaims_ZeroClaimedAtButRecentCreatedAtIsLeftAlone(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)

	p := &Proposal{ID: "prop-legacy-fresh", Sponsor: "s", Env: "zagros", Handle: "h", Email: "h@x",
		Status: ProposalOnboarding, ManifestJSON: "{}", CreatedAt: time.Now().Unix()}
	if err := st.CreateProposal(ctx, p); err != nil {
		t.Fatalf("seed: %v", err)
	}

	moved, err := st.RequeueStaleClaims(ctx, 10*time.Minute)
	if err != nil {
		t.Fatalf("RequeueStaleClaims: %v", err)
	}
	if len(moved) != 0 {
		t.Fatalf("moved = %+v, want none — created_at is recent", moved)
	}
}
