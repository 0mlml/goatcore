package orgstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeName(t *testing.T) {
	cases := map[string]string{
		"Forțele Aeriene Române":  "fortele aeriene romane",
		"Ruritanian Air Force":      "ruritanian air force",
		"ruritanian-air-force":      "ruritanian air force",
		"  org124x  ":                "org124x",
		"Acme Control-Systems!!": "acme control systems",
		"":                        "",
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

// TestSuggestRealCatalogCorpus is the §13.2 corpus gate against the REAL
// committed org population (docs/operators/roster.json — sanitized ids only):
//   - known name-variant forms of real ids land in the exact/high band;
//   - the known-distinct near-identical pair (acme-control-systems vs
//     acmecontrolsystems) must NEVER be offered as an exact match for each
//     other's id — the never-auto-collapse floor.
func TestSuggestRealCatalogCorpus(t *testing.T) {
	rosterPath := filepath.Join("..", "..", "..", "..", "docs", "operators", "roster.json")
	raw, err := os.ReadFile(rosterPath)
	if err != nil {
		t.Skipf("roster.json not present at %s: %v", rosterPath, err)
	}
	var roster struct {
		Orgs []struct {
			ID     string  `json:"id"`
			Kind   *string `json:"kind"`
			Nation *string `json:"nation"`
		} `json:"orgs"`
	}
	if err := json.Unmarshal(raw, &roster); err != nil {
		t.Fatalf("parse roster: %v", err)
	}
	s := open(t)
	loaded := 0
	for _, o := range roster.Orgs {
		if !ValidID(o.ID) {
			continue // the "(internal / no vendor)" bucket etc. oss-allow: generic bucket label
		}
		org := Org{ID: o.ID}
		if o.Kind != nil {
			org.Kind = *o.Kind
		}
		if org.Kind != "" && !validKinds[org.Kind] {
			org.Kind = ""
		}
		if o.Nation != nil {
			org.Nation = *o.Nation
		}
		if _, err := s.CreateOrg(org); err != nil {
			t.Fatalf("load %s: %v", o.ID, err)
		}
		loaded++
	}
	if loaded < 100 {
		t.Fatalf("corpus too small (%d) — roster moved?", loaded)
	}
	t.Logf("corpus: %d real org ids", loaded)

	// Known variants of real ids must surface the org in exact/high band.
	variants := map[string]string{
		"USAREUR-AF":            "usareur-af",
		"demo org data":       "demo-org-data",
		"Acme Control Systems": "acme-control-systems",
	}
	for typed, want := range variants {
		cs, err := s.Suggest(typed, "", 5)
		if err != nil {
			t.Fatalf("suggest %q: %v", typed, err)
		}
		ok := false
		for _, c := range cs {
			if c.ID == want && (c.Band == BandExact || c.Band == BandHigh) {
				ok = true
			}
		}
		if !ok {
			t.Errorf("variant %q did not surface %s in exact/high: %+v", typed, want, cs)
		}
	}

	// The never-auto-collapse floor: each of the known-distinct pair, queried
	// by ITS OWN id, must rank itself exact — and the sibling must NOT be
	// exact (it may be high; a human decides).
	for self, sibling := range map[string]string{
		"acme-control-systems": "acmecontrolsystems",
		"acmecontrolsystems":   "acme-control-systems",
	} {
		cs, _ := s.Suggest(self, "", 5)
		if len(cs) == 0 || cs[0].ID != self || cs[0].Band != BandExact {
			t.Errorf("self-query %q: top = %+v (want itself, exact)", self, cs)
		}
		for _, c := range cs {
			if c.ID == sibling && c.Band == BandExact {
				t.Errorf("known-distinct pair: %q offered EXACT for %q — must never auto-collapse", sibling, self)
			}
		}
	}
}
