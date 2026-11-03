package orgstore

import (
	"fmt"
	"testing"
)

// ADR 1171's org axis. `composition` decides what `nation` MEANS on the row —
// a sovereign's code for `national`, a body marker for `multinational` — and it
// is what will make a `constituent` membership refusable on a national org.

func TestCreateOrgValidatesComposition(t *testing.T) {
	s := open(t)
	for i, tc := range []struct {
		in      string
		wantErr bool
	}{
		{"national", false},
		{"multinational", false},
		{"NATIONAL", false}, // folded, like every other closed vocabulary here
		{"  multinational ", false},
		{"", false},       // not yet declared is a real state, not an error
		{"federal", true}, // a plausible-sounding value must not slip through
		{"nation", true},
	} {
		id := fmt.Sprintf("org-comp-%d", i) // distinct ids: the vocabulary, not id collisions, is under test
		_, err := s.CreateOrg(Org{ID: id, Composition: tc.in, Provenance: "operator-verified"})
		if tc.wantErr && err == nil {
			t.Errorf("CreateOrg(composition=%q) was accepted; a closed vocabulary must refuse it", tc.in)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("CreateOrg(composition=%q): %v", tc.in, err)
		}
	}
}

func TestCompositionRoundTripsAndFolds(t *testing.T) {
	s := open(t)
	got, err := s.CreateOrg(Org{ID: "org79x", Composition: "MultiNational", Nation: "NATO", Provenance: "operator-verified"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Composition != "multinational" {
		t.Errorf("stored composition %q, want the folded form", got.Composition)
	}
	back, err := s.GetOrg("org79x")
	if err != nil {
		t.Fatal(err)
	}
	if back.Composition != "multinational" {
		t.Errorf("read back %q — the column must survive a round trip", back.Composition)
	}
}

func TestUpdateOrgSetsAndClearsComposition(t *testing.T) {
	s := open(t)
	// An org registered before this axis existed carries "" and must be able to
	// declare it without DB surgery — the reason the update verb exists at all.
	if _, err := s.CreateOrg(Org{ID: "usareur-af", Nation: "USA", Provenance: "operator-verified"}); err != nil {
		t.Fatal(err)
	}
	c := "national"
	got, err := s.UpdateOrg("usareur-af", OrgUpdate{Composition: &c})
	if err != nil {
		t.Fatalf("UpdateOrg: %v", err)
	}
	if got.Composition != "national" {
		t.Errorf("composition = %q, want national", got.Composition)
	}
	// Clearing back to "we do not know yet" must be allowed: refusing it would
	// make a wrong guess permanent. This is deliberately unlike Kind and Nation,
	// which refuse an empty replacement.
	empty := ""
	got, err = s.UpdateOrg("usareur-af", OrgUpdate{Composition: &empty})
	if err != nil {
		t.Fatalf("clearing composition: %v", err)
	}
	if got.Composition != "" {
		t.Errorf("composition = %q, want it cleared", got.Composition)
	}
	// A bad value is refused on update as firmly as on create.
	bad := "sovereign"
	if _, err := s.UpdateOrg("usareur-af", OrgUpdate{Composition: &bad}); err == nil {
		t.Error("UpdateOrg accepted an unknown composition")
	}
}
