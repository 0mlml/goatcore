package orgstore

import (
	"testing"
)

func TestNormalizeName(t *testing.T) {
	cases := map[string]string{
		"Forțele Aeriene Române": "fortele aeriene romane",
		"Ruritanian Air Force":   "ruritanian air force",
		"ruritanian-air-force":   "ruritanian air force",
		"  org124x  ":               "org124x",
		"Acme Control-Systems!!": "acme control systems",
		"":                       "",
	}
	for in, want := range cases {
		if got := NormalizeName(in); got != want {
			t.Errorf("NormalizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSuggestBandsAndPolicy(t *testing.T) {
	s := open(t)
	if _, err := s.CreateOrg(Org{ID: "ruritanian-air-force", DisplayName: "Ruritanian Air Force", Kind: "partner", Nation: "SVN"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateOrg(Org{ID: "ruritanian-land-component", DisplayName: "Ruritanian Land Component", Kind: "partner", Nation: "SVN"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateOrg(Org{ID: "usareur-af", Kind: "organic", Nation: "USA"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAlias("org124x", "ruritanian-air-force", "abbreviation", "op"); err != nil {
		t.Fatal(err)
	}

	// Exact by normalized id (dashes fold to spaces).
	cs, err := s.Suggest("Ruritanian Air Force", "", 5)
	if err != nil || len(cs) == 0 || cs[0].ID != "ruritanian-air-force" || cs[0].Band != BandExact {
		t.Fatalf("exact by name: %+v err %v", cs, err)
	}
	// Exact through an accreted alias — the recall-improves-per-onboard loop.
	cs, _ = s.Suggest("org124x", "", 5)
	if len(cs) == 0 || cs[0].ID != "ruritanian-air-force" || cs[0].Band != BandExact || cs[0].Via != "alias:org124x" {
		t.Fatalf("exact by alias: %+v", cs)
	}
	// Diacritic variant lands high-band at least (translation, not variant
	// spelling, so exact is not required — but it must be offered).
	cs, _ = s.Suggest("Fortele Aeriene Romane", "SVN", 5)
	found := false
	for _, c := range cs {
		if c.ID == "ruritanian-air-force" {
			found = true
		}
	}
	if !found {
		t.Fatalf("translated name did not surface the org: %+v", cs)
	}
	// Nation scoping: a SVN query excludes the USA org, keeps nation-less orgs.
	cs, _ = s.Suggest("usareur af", "SVN", 5)
	for _, c := range cs {
		if c.ID == "usareur-af" {
			t.Fatalf("nation scope leak: %+v", cs)
		}
	}
	// Negative pair: demoted out of exact/high, still listed, flagged.
	if err := s.RecordNegativePair("Ruritanian Air Force", "ruritanian-air-force", "op"); err != nil {
		t.Fatal(err)
	}
	cs, _ = s.Suggest("Ruritanian Air Force", "", 5)
	var demotedSeen bool
	for _, c := range cs {
		if c.ID == "ruritanian-air-force" {
			demotedSeen = true
			if c.Band != BandLow || !c.Demoted {
				t.Fatalf("negative pair not demoted: %+v", c)
			}
		}
	}
	if !demotedSeen {
		t.Fatalf("negative pair HID the candidate (must demote, never hide): %+v", cs)
	}
	// Idempotent re-record.
	if err := s.RecordNegativePair("Ruritanian Air Force", "ruritanian-air-force", "op"); err != nil {
		t.Fatalf("re-record: %v", err)
	}
}
