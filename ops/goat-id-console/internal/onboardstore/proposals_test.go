package onboardstore

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/pgtest"
)

// Retry is the recovery path for a mint that broke — and ONLY that. Without it,
// the sole way to move a failed proposal forward is a hand-written UPDATE against
// this SQLite file, the out-of-band surgery onboarding-rails.md forbids. These
// tests fence what it must never become: a way to re-decide, resurrect, or
// double-run.
func TestRetryProposal(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)

	mk := func(id, status string) {
		t.Helper()
		p := &Proposal{ID: id, Sponsor: "s", Env: "zagros-prod", Handle: id, Email: id + "@x", Status: status, ManifestJSON: validRestoreManifest}
		if err := st.CreateProposal(ctx, p); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
		if p.Status != status {
			if err := st.DecideProposal(ctx, id, status, "t", ""); err != nil {
				t.Fatalf("set status %s: %v", status, err)
			}
		}
	}

	// The happy path: failed → approved, so the runner claims it again.
	mk("prop-failed", ProposalFailed)
	if err := st.RetryProposal(ctx, "prop-failed"); err != nil {
		t.Fatalf("retry of a failed proposal must succeed: %v", err)
	}
	if got, _ := st.GetProposal(ctx, "prop-failed"); got.Status != ProposalApproved {
		t.Errorf("after retry status = %q, want %q", got.Status, ProposalApproved)
	}
	// Idempotent: the second call finds no failed row and says so rather than
	// silently re-queuing an already-queued mint.
	if err := st.RetryProposal(ctx, "prop-failed"); !errors.Is(err, ErrProposalNotFailed) {
		t.Errorf("second retry err = %v, want ErrProposalNotFailed", err)
	}

	// Every other state must refuse — a retry is not a decision.
	for _, status := range []string{
		ProposalRejected,        // must never resurrect a rejected candidate
		ProposalOnboarded,       // must never re-run a completed mint
		ProposalOnboarding,      // must never interrupt a mint in flight
		ProposalProposed,        // must never skip the sponsor gate
		ProposalSponsorApproved, // must never skip the admin gate
	} {
		id := "prop-" + status
		mk(id, status)
		if err := st.RetryProposal(ctx, id); !errors.Is(err, ErrProposalNotFailed) {
			t.Errorf("retry of %q err = %v, want ErrProposalNotFailed", status, err)
		}
		if got, _ := st.GetProposal(ctx, id); got.Status != status {
			t.Errorf("retry of %q changed status to %q — it must not", status, got.Status)
		}
	}

	// Unknown id is a 404 signal, distinct from a state refusal.
	if err := st.RetryProposal(ctx, "prop-ghost"); !errors.Is(err, ErrNotFound) {
		t.Errorf("retry of unknown id err = %v, want ErrNotFound", err)
	}
}

// MarkOffboardedFrom is the offboard-only guarded write: it must refuse to
// overwrite a row whose status is no longer in the offboardable set. An
// offboard racing a RestoreProposal must not stamp offboarded over approved.
func TestMarkOffboardedFrom_DoesNotOverwriteApproved(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)

	// Seed an approved proposal as RestoreProposal would leave it.
	p := &Proposal{
		ID:           "prop-restored",
		Sponsor:      "s",
		Env:          "zagros-prod",
		Handle:       "restored",
		Email:        "restored@example.test",
		Status:       ProposalApproved,
		ManifestJSON: "{}",
	}
	if err := st.CreateProposal(ctx, p); err != nil {
		t.Fatalf("CreateProposal(approved): %v", err)
	}

	ok, err := st.MarkOffboardedFrom(ctx, p.ID, "operator@example.test", "offboard after restore")
	if err != nil {
		t.Fatalf("MarkOffboardedFrom(approved) err = %v, want nil", err)
	}
	if ok {
		t.Fatalf("MarkOffboardedFrom(approved) ok = true, want false")
	}

	got, err := st.GetProposal(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetProposal(restored): %v", err)
	}
	if got.Status != ProposalApproved {
		t.Fatalf("MarkOffboardedFrom against approved row changed status to %q, want %q", got.Status, ProposalApproved)
	}
}

// An archived row's note carries the archive reason. A machine offboard with
// an empty note must preserve that reason; an explicit non-empty note must
// overwrite it.
func TestMarkOffboardedFrom_PreservesOrOverwritesNote(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)

	// Seed an onboarded proposal and archive it with a reason.
	p := &Proposal{
		ID:           "prop-archived",
		Sponsor:      "s",
		Env:          "zagros-prod",
		Handle:       "archived",
		Email:        "archived@example.test",
		Status:       ProposalOnboarded,
		ManifestJSON: "{}",
	}
	if err := st.CreateProposal(ctx, p); err != nil {
		t.Fatalf("CreateProposal(onboarded): %v", err)
	}
	ok, err := st.ArchiveProposal(ctx, p.ID, "key expired unredeemed")
	if err != nil || !ok {
		t.Fatalf("ArchiveProposal: ok=%v err=%v", ok, err)
	}
	archived, err := st.GetProposal(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetProposal(archived): %v", err)
	}
	if archived.Status != ProposalArchived || archived.Note != "key expired unredeemed" {
		t.Fatalf("archive left status=%q note=%q, want status=%q note=%q", archived.Status, archived.Note, ProposalArchived, "key expired unredeemed")
	}

	// Machine offboard with a blank note preserves the archive reason — and
	// "blank" includes whitespace-only, which is trimmed before the CASE so
	// it cannot blank out the recorded reason.
	ok, err = st.MarkOffboardedFrom(ctx, p.ID, "operator@example.test", "   ")
	if err != nil || !ok {
		t.Fatalf("MarkOffboardedFrom(blank-note): ok=%v err=%v", ok, err)
	}
	offboarded, err := st.GetProposal(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetProposal(offboarded): %v", err)
	}
	if offboarded.Status != ProposalOffboarded {
		t.Fatalf("status after empty-note offboard = %q, want %q", offboarded.Status, ProposalOffboarded)
	}
	if offboarded.Note != "key expired unredeemed" {
		t.Fatalf("note after empty-note offboard = %q, want archive reason preserved", offboarded.Note)
	}

	// A separate offboard with an explicit note overwrites the archive reason.
	p2 := &Proposal{
		ID:           "prop-archived-explicit",
		Sponsor:      "s",
		Env:          "zagros-prod",
		Handle:       "archived-explicit",
		Email:        "archived-explicit@example.test",
		Status:       ProposalOnboarded,
		ManifestJSON: "{}",
	}
	if err := st.CreateProposal(ctx, p2); err != nil {
		t.Fatalf("CreateProposal(onboarded-2): %v", err)
	}
	ok, err = st.ArchiveProposal(ctx, p2.ID, "key expired unredeemed")
	if err != nil || !ok {
		t.Fatalf("ArchiveProposal-2: ok=%v err=%v", ok, err)
	}
	ok, err = st.MarkOffboardedFrom(ctx, p2.ID, "operator@example.test", "left the org")
	if err != nil || !ok {
		t.Fatalf("MarkOffboardedFrom(explicit-note): ok=%v err=%v", ok, err)
	}
	offboarded2, err := st.GetProposal(ctx, p2.ID)
	if err != nil {
		t.Fatalf("GetProposal(offboarded2): %v", err)
	}
	if offboarded2.Note != "left the org" {
		t.Fatalf("note after explicit-note offboard = %q, want %q", offboarded2.Note, "left the org")
	}
}

// ActiveProposalForEmail must treat an explicitly offboarded proposal as a
// terminal-negative state: a cleanly offboarded row must NOT block a fresh
// proposal for the same email.
func TestActiveProposalForEmail_OffboardedIsNotActive(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	email := "user@example.test"

	// Seed an offboarded proposal; with only this row present, the lookup
	// should report "no active proposal".
	p1 := &Proposal{
		Sponsor:      "s",
		Env:          "zagros",
		Handle:       "u1",
		Email:        email,
		Status:       ProposalOffboarded,
		ManifestJSON: "{}",
	}
	if err := st.CreateProposal(ctx, p1); err != nil {
		t.Fatalf("CreateProposal(offboarded): %v", err)
	}
	if got, ok, err := st.ActiveProposalForEmail(ctx, email); err != nil {
		t.Fatalf("ActiveProposalForEmail(offboarded-only): %v", err)
	} else if ok {
		t.Fatalf("offboarded-only proposal must not be active, got %+v", got)
	}

	// A new proposed row for the same email becomes the active one.
	p2 := &Proposal{
		Sponsor:      "s",
		Env:          "zagros",
		Handle:       "u2",
		Email:        email,
		Status:       ProposalProposed,
		ManifestJSON: "{}",
	}
	if err := st.CreateProposal(ctx, p2); err != nil {
		t.Fatalf("CreateProposal(proposed): %v", err)
	}
	if got, ok, err := st.ActiveProposalForEmail(ctx, email); err != nil {
		t.Fatalf("ActiveProposalForEmail(after-new): %v", err)
	} else {
		if !ok {
			t.Fatal("expected an active proposal after creating a new proposed row")
		}
		if got.ID != p2.ID || got.Status != ProposalProposed {
			t.Fatalf("active proposal = %+v, want id=%q status=%q", got, p2.ID, ProposalProposed)
		}
	}
}

// org_source carries WHERE affiliation.id came from, and the review gate reads
// it to decide whether a membership still needs a human behind it (#1086).
// Two properties: the value survives a round trip through SELECT *, and it is
// writable only while the proposal is still at a review stage — the provenance
// of an approved manifest must not be rewritten after the decision.
func TestSetProposalOrgSource(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)

	p := &Proposal{ID: "prop-src", Sponsor: "s", Env: "zagros", Handle: "h", Email: "h@x",
		Status: ProposalProposed, ManifestJSON: "{}", OrgSource: OrgSourceTyped}
	if err := st.CreateProposal(ctx, p); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := st.GetProposal(ctx, "prop-src")
	if err != nil || got.OrgSource != OrgSourceTyped {
		t.Fatalf("org_source round trip = %q (err %v), want %q", got.OrgSource, err, OrgSourceTyped)
	}

	if err := st.SetProposalOrgSource(ctx, "prop-src", OrgSourceOperator); err != nil {
		t.Fatalf("set at a review stage: %v", err)
	}
	if got, _ := st.GetProposal(ctx, "prop-src"); got.OrgSource != OrgSourceOperator {
		t.Errorf("org_source = %q, want %q", got.OrgSource, OrgSourceOperator)
	}

	// Past the review stages the row is closed to this write.
	if err := st.DecideProposal(ctx, "prop-src", ProposalApproved, "admin@x", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := st.SetProposalOrgSource(ctx, "prop-src", OrgSourceTyped); !errors.Is(err, ErrNotFound) {
		t.Errorf("set on an approved proposal err = %v, want ErrNotFound", err)
	}
	if got, _ := st.GetProposal(ctx, "prop-src"); got.OrgSource != OrgSourceOperator {
		t.Errorf("org_source after the refused write = %q, want it unchanged", got.OrgSource)
	}
}

// Rows written before org_source existed get their provenance back from the
// invite that produced them: onboard_invites.prebound_json.org is the org the
// sponsor pinned, and onboard_invites.proposal_id links the two. Open runs the
// backfill, so the test leaves rows the way an older build did and reopens.
func TestOpenBackfillsOrgSourceFromTheInvite(t *testing.T) {
	ctx := context.Background()
	path := pgtest.DSN(t)
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	seed := []struct{ proposal, orgSource, prebound string }{
		{"prop-pinned", "", `{"tier":"developer","org":"demoforge"}`},
		{"prop-bare", "", `{"tier":"developer"}`},
		{"prop-decided", OrgSourceOperator, `{"org":"demoforge"}`},
		{"prop-malformed", "", `not json`},
		{"prop-api", "", ""}, // an API or batch intake: no invite behind it
	}
	for i, r := range seed {
		p := &Proposal{ID: r.proposal, Sponsor: "s", Env: "zagros", Handle: r.proposal, Email: r.proposal + "@x",
			Status: ProposalProposed, ManifestJSON: "{}", OrgSource: r.orgSource}
		if err := st.CreateProposal(ctx, p); err != nil {
			t.Fatalf("seed %s: %v", r.proposal, err)
		}
		if r.prebound == "" {
			continue
		}
		if _, err := st.db.ExecContext(ctx,
			`INSERT INTO onboard_invites (id, created_at, sponsor, token_hash, prebound_json, expires_at, status, proposal_id)
			 VALUES ($1, 1, 's', $2, $3, 1, $4, $5)`,
			fmt.Sprintf("inv-%d", i), fmt.Sprintf("hash-%d", i), r.prebound, InviteSubmitted, r.proposal); err != nil {
			t.Fatalf("seed invite for %s: %v", r.proposal, err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	st, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v (one malformed prebound_json must not keep the store from opening)", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for proposal, want := range map[string]string{
		"prop-pinned":    OrgSourceSponsor,
		"prop-bare":      OrgSourceTyped,
		"prop-decided":   OrgSourceOperator, // a recorded decision is never overwritten
		"prop-malformed": "",                // unrecoverable stays empty, which reads as a claim
		"prop-api":       "",
	} {
		got, err := st.GetProposal(ctx, proposal)
		if err != nil {
			t.Fatalf("get %s: %v", proposal, err)
		}
		if got.OrgSource != want {
			t.Errorf("%s: org_source = %q, want %q", proposal, got.OrgSource, want)
		}
	}
}

// Two active proposals for one email in the same second: the lookup and
// the list agree on which is newest, so a reader that walks ListProposals and
// keeps the first row per email matches the lookup. The tie breaks on the
// higher id (ADR 1165 step A: Postgres has no rowid, and no existing column
// records insert order inside one second), so the ids are fixed here and the
// FIRST insert carries the higher one.
func TestActiveProposalForEmail_SameSecondTieMatchesList(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	email := "tie@example.test"
	for _, r := range []struct{ id, h string }{{"prop-b", "first-insert"}, {"prop-a", "second-insert"}} {
		p := &Proposal{ID: r.id, Sponsor: "s", Env: "zagros", Handle: r.h, Email: email, Status: ProposalOnboarded, ManifestJSON: "{}", CreatedAt: 1_800_000_000}
		if err := st.CreateProposal(ctx, p); err != nil {
			t.Fatalf("CreateProposal(%s): %v", r.h, err)
		}
	}
	got, ok, err := st.ActiveProposalForEmail(ctx, email)
	if err != nil || !ok {
		t.Fatalf("ActiveProposalForEmail: ok=%v err=%v", ok, err)
	}
	if got.ID != "prop-b" {
		t.Fatalf("lookup picked %q, want prop-b (the higher id wins a same-second tie)", got.ID)
	}
	all, err := st.ListProposals(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range all {
		if p.Email == email {
			if p.Handle != got.Handle {
				t.Fatalf("ListProposals first row for the email is %q, lookup returned %q", p.Handle, got.Handle)
			}
			break
		}
	}
}

// ClaimProposal must stamp claimed_at, or the requeue sweep has no staleness
// anchor and the claim strands forever (goat#977).
func TestClaimProposal_StampsClaimedAt(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)

	p := &Proposal{ID: "prop-claim", Sponsor: "s", Env: "zagros", Handle: "h", Email: "h@x",
		Status: ProposalApproved, ManifestJSON: "{}"}
	if err := st.CreateProposal(ctx, p); err != nil {
		t.Fatalf("seed: %v", err)
	}

	before := time.Now().Unix()
	if err := st.ClaimProposal(ctx, "prop-claim"); err != nil {
		t.Fatalf("ClaimProposal: %v", err)
	}
	after := time.Now().Unix()

	got, err := st.GetProposal(ctx, "prop-claim")
	if err != nil {
		t.Fatalf("GetProposal: %v", err)
	}
	if got.Status != ProposalOnboarding {
		t.Fatalf("status = %q, want %q", got.Status, ProposalOnboarding)
	}
	if got.ClaimedAt < before || got.ClaimedAt > after {
		t.Fatalf("claimed_at = %d, want between %d and %d", got.ClaimedAt, before, after)
	}
}

// ClaimOffboard must stamp claimed_at too — the offboard leg strands the
// same way (goat#977).
func TestClaimOffboard_StampsClaimedAt(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)

	p := &Proposal{ID: "prop-offclaim", Sponsor: "s", Env: "zagros", Handle: "h", Email: "h@x",
		Status: ProposalOffboardRequested, ManifestJSON: "{}"}
	if err := st.CreateProposal(ctx, p); err != nil {
		t.Fatalf("seed: %v", err)
	}

	before := time.Now().Unix()
	if err := st.ClaimOffboard(ctx, "prop-offclaim"); err != nil {
		t.Fatalf("ClaimOffboard: %v", err)
	}
	after := time.Now().Unix()

	got, err := st.GetProposal(ctx, "prop-offclaim")
	if err != nil {
		t.Fatalf("GetProposal: %v", err)
	}
	if got.Status != ProposalOffboarding {
		t.Fatalf("status = %q, want %q", got.Status, ProposalOffboarding)
	}
	if got.ClaimedAt < before || got.ClaimedAt > after {
		t.Fatalf("claimed_at = %d, want between %d and %d", got.ClaimedAt, before, after)
	}
}
