package onboardstore

import (
	"context"
	"testing"
)

// Archive is cold storage for an onboarding that never happened, and restore is
// the single admin action that undoes it. The pair exists so the ADR 1136 purge
// lane stops being irreversible — deleting the record discarded the two human
// gates, which made tidying the queue cost a full re-onboarding per person and
// so guaranteed nobody ever tidied it.
//
// These tests fence the two properties that make it safe to drive from a sweep:
// archive touches ONLY `onboarded`, and restore touches ONLY `archived`.
func TestArchiveAndRestoreProposal(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)

	mk := func(id, status string) {
		t.Helper()
		p := &Proposal{ID: id, Sponsor: "s", Env: "zagros", Handle: id, Email: id + "@x", Status: status, ManifestJSON: validRestoreManifest}
		if err := st.CreateProposal(ctx, p); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
		if p.Status != status {
			if err := st.DecideProposal(ctx, id, status, "t", ""); err != nil {
				t.Fatalf("set status %s: %v", status, err)
			}
		}
	}

	// Happy path: onboarded → archived, carrying the reason.
	mk("prop-cold", ProposalOnboarded)
	ok, err := st.ArchiveProposal(ctx, "prop-cold", "key expired unredeemed")
	if err != nil || !ok {
		t.Fatalf("archive of an onboarded row: ok=%v err=%v", ok, err)
	}
	got, _ := st.GetProposal(ctx, "prop-cold")
	if got.Status != ProposalArchived {
		t.Errorf("status = %q, want %q", got.Status, ProposalArchived)
	}
	if got.Note != "key expired unredeemed" {
		t.Errorf("note = %q — the reason must survive, it is the only record of WHY", got.Note)
	}

	// Idempotent: a second sweep pass must not re-report or re-act.
	if ok, _ := st.ArchiveProposal(ctx, "prop-cold", "again"); ok {
		t.Error("second archive reported true — a repeated sweep tick must be a no-op")
	}

	// Restore returns it to `approved`, the state the runner claims from, so a
	// fresh key is minted and the welcome package re-sent with NO gate repeated.
	ok, err = st.RestoreProposal(ctx, "prop-cold", " admin@x ")
	if err != nil || !ok {
		t.Fatalf("restore of an archived row: ok=%v err=%v", ok, err)
	}
	got, _ = st.GetProposal(ctx, "prop-cold")
	if got.Status != ProposalApproved {
		t.Fatalf("restored status = %q, want %q — anything earlier would re-run the human gates",
			got.Status, ProposalApproved)
	}
	// The restore mark is what tells this row from a fresh approval sitting
	// beside it in the queue: who did it, trimmed, and when.
	if got.RestoredBy != "admin@x" || got.RestoredAt == 0 {
		t.Errorf("restore mark = (%q, %d), want (\"admin@x\", non-zero)", got.RestoredBy, got.RestoredAt)
	}
	// The stale credential must not survive the round trip: leaving the old key
	// on the row would let a surface show a revoked key as if it were current.
	if got.SetupKey != "" || got.BundlePath != "" {
		t.Errorf("restore left stale credentials (key=%q bundle=%q)", got.SetupKey, got.BundlePath)
	}
	// And the provenance that justifies skipping the gates must still be there.
	if got.Sponsor == "" {
		t.Error("restore dropped the sponsor — that provenance is the whole reason to archive rather than delete")
	}

	if ok, _ := st.RestoreProposal(ctx, "prop-cold", "admin@x"); ok {
		t.Error("second restore reported true — it must match no row")
	}
}

// Archive must refuse every state that is not `onboarded`. `enrolled` is the
// one that matters most: that person DID join, and a dormancy sweep that
// archived them would cold-store somebody who is actively on the mesh.
func TestArchiveProposal_refusesEveryOtherState(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)

	for _, status := range []string{
		ProposalEnrolled,        // joined — archiving them is the dangerous case
		ProposalRejected,        // has its own terminal meaning
		ProposalFailed,          // retryable; archiving hides a broken mint
		ProposalOnboarding,      // mint in flight
		ProposalApproved,        // queued for the runner
		ProposalProposed,        // awaiting the sponsor gate
		ProposalSponsorApproved, // awaiting the admin gate
	} {
		id := "prop-" + status
		p := &Proposal{ID: id, Sponsor: "s", Env: "zagros", Handle: id, Email: id + "@x", Status: status, ManifestJSON: "{}"}
		if err := st.CreateProposal(ctx, p); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
		if p.Status != status {
			if err := st.DecideProposal(ctx, id, status, "t", ""); err != nil {
				t.Fatalf("set %s: %v", status, err)
			}
		}
		ok, err := st.ArchiveProposal(ctx, id, "sweep")
		if err != nil {
			t.Fatalf("archive %s: %v", status, err)
		}
		if ok {
			t.Errorf("archive of %q reported true — only an onboarded row may be archived", status)
		}
		if got, _ := st.GetProposal(ctx, id); got.Status != status {
			t.Errorf("archive of %q changed status to %q — it must not", status, got.Status)
		}
	}
}

// An archived person still holds the email. Restore is the way back, not a
// fresh submission — otherwise one person ends up with two proposal rows and
// the collision-safety guard that keys on email is defeated.
func TestArchivedProposalStillBlocksADuplicateSubmission(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)

	p := &Proposal{ID: "prop-dup", Sponsor: "s", Env: "zagros", Handle: "dup",
		Email: "dup@x", Status: ProposalOnboarded, ManifestJSON: "{}"}
	if err := st.CreateProposal(ctx, p); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.DecideProposal(ctx, "prop-dup", ProposalOnboarded, "t", ""); err != nil {
		t.Fatalf("set onboarded: %v", err)
	}
	if _, err := st.ArchiveProposal(ctx, "prop-dup", "unredeemed"); err != nil {
		t.Fatalf("archive: %v", err)
	}

	got, found, err := st.ActiveProposalForEmail(ctx, "dup@x")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if !found {
		t.Fatal("an archived row must still match the duplicate-email guard; " +
			"treating it as terminal-negative would let a second row exist for one person")
	}
	if got.Status != ProposalArchived {
		t.Errorf("matched row status = %q, want %q", got.Status, ProposalArchived)
	}
}
