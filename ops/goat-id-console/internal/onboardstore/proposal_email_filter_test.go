package onboardstore

import (
	"context"
	"testing"
)

func TestListProposalsFilteredByEmail(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	for _, row := range []struct{ id, email, status string }{
		{"first", "Person+tag@example.test", ProposalArchived},
		{"second", "PERSON+TAG@example.test", ProposalProposed},
		{"substring", "other-Person+tag@example.test", ProposalProposed},
		{"unicode", "Pérson@example.test", ProposalProposed},
	} {
		p := &Proposal{ID: row.id, Sponsor: "sponsor", Env: "test", Handle: row.id,
			Email: row.email, ManifestJSON: "{}"}
		if err := s.CreateProposal(ctx, p); err != nil {
			t.Fatal(err)
		}
		if err := s.SetProposalStatus(ctx, p.ID, row.status); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, status, email string
		want                int
	}{
		{"all states", "", "person+tag@EXAMPLE.test", 2},
		{"status intersection", ProposalArchived, "person+tag@example.test", 1},
		{"exact match", "", "person", 0},
		{"SQL value", "", "' OR 1=1 --", 0},
		{"literal wildcard", "", "%@example.test", 0},
		{"ASCII fold", "", "PÉRSON@example.test", 0},
		{"no filter", "", "", 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := s.ListProposalsFiltered(ctx, tc.status, tc.email)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != tc.want {
				t.Fatalf("got %d rows, want %d: %+v", len(rows), tc.want, rows)
			}
		})
	}
	rows, err := s.ListProposalsFiltered(ctx, "", "person+tag@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].ID != "second" || rows[1].ID != "first" {
		t.Fatalf("ordering differs from newest-first list: %+v", rows)
	}
}
