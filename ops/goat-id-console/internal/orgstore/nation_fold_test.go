package orgstore

import (
	"fmt"
	"testing"
)

// A nation can be registered in alpha-2 (`US`) while the rest of the catalog
// carries alpha-3 (`USA`) for the same country. Neither spelling works reliably
// without folding: Suggest compares with EqualFold, which is case- but not
// alpha-2/alpha-3-aware, so the registered org would be EXCLUDED from a search
// scoped the other way and the reviewer walked toward creating a duplicate.

func TestCreateOrgFoldsNationOnWrite(t *testing.T) {
	s := open(t)
	for i, tc := range []struct{ in, want string }{
		{"US", "USA"}, {"usa", "USA"}, {" us ", "USA"},
		{"CA", "CAN"}, {"GB", "GBR"}, {"UK", "GBR"}, // oss-allow: public ISO table
		{"NATO", "NATO"}, // a marker is not a country and must pass through
		{"EU", "EU"},
		{"", ""}, // absent stays absent; never guessed
	} {
		id := fmt.Sprintf("org-%d", i) // distinct ids: the fold, not id collisions, is under test
		got, err := s.CreateOrg(Org{ID: id, Nation: tc.in, Provenance: "operator-verified"})
		if err != nil {
			t.Fatalf("CreateOrg(nation=%q): %v", tc.in, err)
		}
		if got.Nation != tc.want {
			t.Errorf("CreateOrg(nation=%q) stored %q, want %q", tc.in, got.Nation, tc.want)
		}
	}
}

func TestSuggestFindsAnOrgStoredWithTheOtherSpelling(t *testing.T) {
	s := open(t)
	if _, err := s.CreateOrg(Org{ID: "usareur-af", Nation: "US", Provenance: "operator-verified"}); err != nil {
		t.Fatal(err)
	}
	// Scoped with the alpha-3 spelling: the row was written `US` and must still
	// be found, which is the whole point — a reviewer who cannot see the
	// registered org registers a second one.
	for _, scope := range []string{"USA", "US", "usa"} {
		cs, err := s.Suggest("usareur-af", scope, 5)
		if err != nil {
			t.Fatalf("Suggest(nation=%q): %v", scope, err)
		}
		if len(cs) == 0 {
			t.Errorf("Suggest scoped %q found nothing — the org is registered and was hidden", scope)
		}
	}
	// A genuinely different nation must still exclude it.
	cs, err := s.Suggest("usareur-af", "CAN", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 0 {
		t.Errorf("Suggest scoped CAN returned %d candidate(s); a US org must not match a Canadian scope", len(cs))
	}
}

func TestUpdateOrgFoldsNation(t *testing.T) {
	s := open(t)
	if _, err := s.CreateOrg(Org{ID: "acme", Nation: "USA", Provenance: "operator-verified"}); err != nil {
		t.Fatal(err)
	}
	n := "us"
	got, err := s.UpdateOrg("acme", OrgUpdate{Nation: &n})
	if err != nil {
		t.Fatalf("UpdateOrg: %v", err)
	}
	if got.Nation != "USA" {
		t.Errorf("UpdateOrg(--nation us) stored %q, want USA — correcting a row by hand must not re-introduce the split", got.Nation)
	}
}
