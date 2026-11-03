package onboardstore

import (
	"context"
	"testing"
)

// The CLI mint path had no way to record what it created, so 104 identities on
// ZAGROS existed in the roster and in no lifecycle lane. These tests fence
// the two properties that make registering safe to call from a mint and from a
// backfill over the same population.
func TestRegisterMintedProposal(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)

	by := "operator@x"
	p := &Proposal{Sponsor: by, Env: "zagros", Handle: "cli-person",
		Email: "cli@x", DecidedBy: &by}
	created, existing, err := st.RegisterMintedProposal(ctx, p)
	if err != nil || !created || existing != nil {
		t.Fatalf("first register: created=%v existing=%v err=%v", created, existing, err)
	}

	got, err := st.GetProposal(ctx, p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// It lands at `onboarded`, NOT `proposed`: the gates decide whether to mint,
	// and this records a mint that already happened. A `proposed` row would put a
	// live credential into a review queue for a decision nobody is waiting on.
	if got.Status != ProposalOnboarded {
		t.Errorf("status = %q, want %q", got.Status, ProposalOnboarded)
	}
	// Retroactive means "provenance predates the three-stage system". This row
	// HAS provenance — the operator who ran the mint — so claiming otherwise
	// would discard the one attribution it carries.
	if got.Retroactive {
		t.Error("retroactive must not be set: this row knows who minted it")
	}
	if got.DecidedBy == nil || *got.DecidedBy != by {
		t.Errorf("decided_by = %v, want %q", got.DecidedBy, by)
	}

	// Idempotent by email: a re-run of the mint, or of the backfill over the
	// same roster, must not produce a second row for one person.
	dup := &Proposal{Sponsor: by, Env: "zagros", Handle: "cli-person-again", Email: "cli@x"}
	created, existing, err = st.RegisterMintedProposal(ctx, dup)
	if err != nil {
		t.Fatalf("second register: %v", err)
	}
	if created {
		t.Error("second register created a row — one person must not acquire two")
	}
	if existing == nil || existing.ID != p.ID {
		t.Errorf("second register did not return the existing row: %v", existing)
	}
}

// Registering beside a terminal row must not resurrect somebody by the side
// door. An archived person comes back through Restore, deliberately; a rejected
// one does not come back at all by re-running a mint.
func TestRegisterMintedProposal_refusesBesideATerminalRow(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)

	for _, status := range []string{ProposalRejected, ProposalArchived, ProposalOffboarded} {
		email := status + "@x"
		seed := &Proposal{Sponsor: "s", Env: "zagros", Handle: "h-" + status,
			Email: email, Status: ProposalOnboarded, ManifestJSON: "{}"}
		if err := st.CreateProposal(ctx, seed); err != nil {
			t.Fatalf("seed %s: %v", status, err)
		}
		if _, err := st.db.ExecContext(ctx,
			`UPDATE onboard_proposals SET status = $1 WHERE id = $2`, status, seed.ID); err != nil {
			t.Fatalf("set %s: %v", status, err)
		}
		created, existing, err := st.RegisterMintedProposal(ctx,
			&Proposal{Sponsor: "s", Env: "zagros", Handle: "h2-" + status, Email: email})
		if err != nil {
			t.Fatalf("register beside %s: %v", status, err)
		}
		if created {
			t.Errorf("registered a NEW row beside a %q row — that resurrects somebody "+
				"without the deliberate path (Restore) that exists for it", status)
		}
		if existing == nil || existing.Status != status {
			t.Errorf("beside %q: returned %v", status, existing)
		}
	}
}

// A row with no handle or no env cannot be joined to a peer, a manifest or a
// roster entry, so it is worse than absent — it looks like coverage.
func TestRegisterMintedProposal_requiresHandleAndEnv(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	for _, p := range []*Proposal{
		{Env: "zagros", Email: "a@x"},
		{Handle: "h", Email: "b@x"},
	} {
		if _, _, err := st.RegisterMintedProposal(ctx, p); err == nil {
			t.Errorf("register accepted an unjoinable row: %+v", p)
		}
	}
}

// A roster-sourced row carries NO email, so the email-keyed guard is skipped
// entirely — and without a fallback the same person could be registered twice.
// Found in the ZAGROS dry run: 110 of 245 roster handles appear more than once.
func TestRegisterMintedProposal_dedupesByHandleWhenEmailIsEmpty(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)

	first := &Proposal{Sponsor: "backfill", Env: "zagros", Handle: "no-email-person"}
	created, _, err := st.RegisterMintedProposal(ctx, first)
	if err != nil || !created {
		t.Fatalf("first: created=%v err=%v", created, err)
	}

	again := &Proposal{Sponsor: "backfill", Env: "zagros", Handle: "no-email-person"}
	created, existing, err := st.RegisterMintedProposal(ctx, again)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if created {
		t.Error("registered a SECOND row for one handle with no email — the roster " +
			"lists people more than once, so this would duplicate at scale")
	}
	if existing == nil || existing.ID != first.ID {
		t.Errorf("second call did not return the first row: %v", existing)
	}

	// Same handle, DIFFERENT goatnet, is a different person-on-a-network and
	// must still be allowed: the lifecycle lanes join on handle+env, not handle.
	other := &Proposal{Sponsor: "backfill", Env: "zagros-prod", Handle: "no-email-person"}
	created, _, err = st.RegisterMintedProposal(ctx, other)
	if err != nil || !created {
		t.Errorf("same handle on another goatnet must register: created=%v err=%v", created, err)
	}
}
