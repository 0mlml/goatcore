package coalition

import (
	"strings"
	"testing"
	"time"
)

func TestReconcile(t *testing.T) {
	member := Resolution{Standing: StandingMember, Nation: "FIN", Framework: "nato",
		Basis: "FIN has been a nato member since 2023-04-04"}
	partner := Resolution{Standing: StandingPartner, Nation: "UKR", Framework: "nato",
		Basis: "UKR is not a nato member — partner standing"}

	t.Run("no seeded value is the healthy state", func(t *testing.T) {
		if c := Reconcile("", member); c != nil {
			t.Errorf("expected no conflict, got %v", c)
		}
	})
	t.Run("whitespace-only seed is treated as absent", func(t *testing.T) {
		if c := Reconcile("   ", member); c != nil {
			t.Errorf("expected no conflict, got %v", c)
		}
	})
	t.Run("agreeing seed is not a conflict", func(t *testing.T) {
		if c := Reconcile("member", member); c != nil {
			t.Errorf("expected no conflict, got %v", c)
		}
	})
	t.Run("case and spacing do not manufacture a conflict", func(t *testing.T) {
		if c := Reconcile("  MEMBER ", member); c != nil {
			t.Errorf("expected no conflict, got %v", c)
		}
	})

	// The case this exists for: Finland's manifest could honestly have said
	// partner in 2022, and would still say it today.
	t.Run("stale seed conflicts", func(t *testing.T) {
		c := Reconcile("partner", member)
		if c == nil {
			t.Fatal("expected a conflict")
		}
		if c.Seeded != "partner" || c.Derived.Standing != StandingMember {
			t.Errorf("conflict = %+v, want seeded=partner derived=member", *c)
		}
		if !strings.Contains(c.Error(), "has been a nato member since") {
			t.Errorf("conflict message drops the resolution Basis: %s", c.Error())
		}
	})
	t.Run("over-claimed membership conflicts", func(t *testing.T) {
		if c := Reconcile("member", partner); c == nil {
			t.Fatal("expected a conflict for an over-claimed membership")
		}
	})
	// A value outside the closed vocabulary is also worth showing an operator.
	t.Run("unrecognized seed conflicts rather than being ignored", func(t *testing.T) {
		if c := Reconcile("ally", member); c == nil {
			t.Fatal("expected a conflict for a value outside the grammar")
		}
	})
}

func TestOrganic(t *testing.T) {
	cases := []struct {
		name              string
		nation, sovereign string
		wantOrganic       bool
		wantKnown         bool
	}{
		{"same nation is organic", "USA", "USA", true, true},
		{"case and spacing normalise", " est ", "EST", true, true},
		{"different nation is foreign", "EST", "USA", false, true},
		{"relative: the same org is organic on its own sovereign's goatnet", "EST", "EST", true, true},
		{"no operating sovereign is UNKNOWN, not foreign", "EST", "", false, false},
		{"no nation is UNKNOWN, not foreign", "", "USA", false, false},
		{"neither known", "", "", false, false},
		{"an alliance body is nobody's own", "NATO", "USA", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			organic, known := Organic(c.nation, c.sovereign)
			if organic != c.wantOrganic || known != c.wantKnown {
				t.Errorf("Organic(%q, %q) = (%v, %v), want (%v, %v)",
					c.nation, c.sovereign, organic, known, c.wantOrganic, c.wantKnown)
			}
		})
	}
}

func TestDeriveKind(t *testing.T) {
	cases := []struct {
		name         string
		sector       string
		organic      bool
		organicKnown bool
		wantKind     string
		wantOK       bool
	}{
		{"commercial is vendor whatever the nation", "commercial", false, true, "vendor", true},
		{"a state-owned company is still vendor", "commercial", true, true, "vendor", true},
		{"civil-society is civil", "civil-society", false, true, "civil", true},
		{"legacy ngo collapses to civil", "ngo", false, true, "civil", true},
		{"legacy media collapses to civil", "media", false, true, "civil", true},
		{"legacy academic collapses to civil", "academic", false, true, "civil", true},
		{"state + organic", "state", true, true, "organic", true},
		{"state + foreign", "state", false, true, "partner", true},
		{"legacy military + organic", "military", true, true, "organic", true},
		{"legacy law-enforcement + foreign", "law-enforcement", false, true, "partner", true},
		{"legacy intelligence + foreign", "intelligence", false, true, "partner", true},
		{"legacy civil-government + organic", "civil-government", true, true, "organic", true},
		{"state with unknown organic cannot decide", "state", false, false, "", false},
		{"empty sector cannot decide", "", false, true, "", false},
		{"garbage sector cannot decide", "banana", true, true, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kind, ok := DeriveKind(c.sector, c.organic, c.organicKnown)
			if kind != c.wantKind || ok != c.wantOK {
				t.Errorf("DeriveKind(%q, %v, %v) = (%q, %v), want (%q, %v)",
					c.sector, c.organic, c.organicKnown, kind, ok, c.wantKind, c.wantOK)
			}
		})
	}
}

func TestGovBranch(t *testing.T) {
	cases := []struct {
		legacy                 string
		wantSector, wantBranch string
		wantOK                 bool
	}{
		{"military", "state", "military", true},
		{"law-enforcement", "state", "law-enforcement", true},
		{"intelligence", "state", "intelligence", true},
		{"civil-government", "state", "civil", true},
		// Target values need no collapsing, and neither do non-gov legacy ones.
		{"state", "", "", false},
		{"commercial", "", "", false},
		{"civil-society", "", "", false},
		{"ngo", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.legacy, func(t *testing.T) {
			s, b, ok := GovBranch(c.legacy)
			if s != c.wantSector || b != c.wantBranch || ok != c.wantOK {
				t.Errorf("GovBranch(%q) = (%q, %q, %v), want (%q, %q, %v)",
					c.legacy, s, b, ok, c.wantSector, c.wantBranch, c.wantOK)
			}
		})
	}
}

// The legacy sector values must derive the SAME kind the manifest already
// carries, or the migration window is not safe: an org that reads `organic`
// today would start reading `partner` tomorrow with no data change.
func TestLegacySectorsDeriveTheKindTheyAlreadyCarry(t *testing.T) {
	for _, legacy := range []string{"military", "law-enforcement", "intelligence", "civil-government"} {
		sector, branch, ok := GovBranch(legacy)
		if !ok || sector != "state" || branch == "" {
			t.Fatalf("GovBranch(%q) did not collapse to a state branch", legacy)
		}
		// Collapsed and uncollapsed must agree, for both organic answers.
		for _, organic := range []bool{true, false} {
			viaLegacy, ok1 := DeriveKind(legacy, organic, true)
			viaTarget, ok2 := DeriveKind(sector, organic, true)
			if !ok1 || !ok2 || viaLegacy != viaTarget {
				t.Errorf("kind disagrees for %q organic=%v: legacy=%q target=%q",
					legacy, organic, viaLegacy, viaTarget)
			}
		}
	}
}

// ADR 1151's worked example, asserted end to end. Under the old single-field
// model a foreign commercial vendor had NO correct value: `vendor` dropped the
// foreignness, `partner` dropped the commercialness.
func TestEstonianDefenceContractorHasACompleteAnswer(t *testing.T) {
	reg := canonicalRegistry(t)
	asOf := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)

	const nation, sovereign, sector = "EST", "USA", "commercial"

	organic, known := Organic(nation, sovereign)
	if !known {
		t.Fatal("organic must be knowable when both nations are declared")
	}
	if organic {
		t.Error("an Estonian org on a US-operated goatnet is not organic")
	}
	if got := reg.Resolve(nation, "", asOf); got.Standing != StandingMember {
		t.Errorf("alliance_standing = %q, want member — region E is in the framework", got.Standing)
	}
	if kind, ok := DeriveKind(sector, organic, known); !ok || kind != "vendor" {
		t.Errorf("derived kind = (%q, %v), want (vendor, true)", kind, ok)
	}
}

// The defect this fold exists to prevent: an operator types `US` on a
// USA-operated network and is told the organisation is FOREIGN and NOT a member.
// Both were false, and both came from comparing raw strings.
func TestAlphaTwoCodesResolveToTheSameNation(t *testing.T) {
	for _, c := range []struct{ typed, sovereign string }{
		{"US", "USA"}, {"USA", "US"}, {"us", "USA"}, {" US ", "USA"}, {"US", "US"},
	} {
		organic, known := Organic(c.typed, c.sovereign)
		if !known || !organic {
			t.Errorf("Organic(%q, %q) = (%v, %v) — alpha-2 and alpha-3 name one country",
				c.typed, c.sovereign, organic, known)
		}
	}
	// And the fold must not make unrelated countries equal.
	if organic, _ := Organic("ISL", "USA"); organic {
		t.Error("ISL must not read as USA")
	}
	// Multinational markers are not countries and must pass through untouched.
	if organic, known := Organic("NATO", "USA"); !known || organic {
		t.Errorf("NATO is nobody's sovereign: got (%v, %v)", organic, known)
	}
}

// Standing must fold too: `US` is a NATO member because USA is.
func TestStandingResolvesAlphaTwo(t *testing.T) {
	reg := canonicalRegistry(t)
	asOf := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	for _, code := range []string{"US", "USA", "us"} {
		res := reg.Resolve(code, "", asOf)
		if res.Standing != StandingMember {
			t.Errorf("Resolve(%q) = %q, want member — %s", code, res.Standing, res.Basis)
		}
		// The basis must name the country and the alliance in full, so a
		// two-letter input is visibly matched to the right thing.
		if !strings.Contains(res.Basis, "United States (USA)") {
			t.Errorf("basis should name the resolved nation in full, got %q", res.Basis)
		}
		if !strings.Contains(res.Basis, "NATO") || strings.Contains(res.Basis, "nato") {
			t.Errorf("the alliance acronym must be upper-cased and unexpanded, got %q", res.Basis)
		}
	}
}

// NationDisplay is what the form echoes back, so it must resolve the code the
// operator typed rather than repeating it.
func TestNationDisplay(t *testing.T) {
	reg := canonicalRegistry(t)
	for _, c := range []struct{ in, want string }{
		{"US", "United States (USA)"},
		{"USA", "United States (USA)"},
		{"LT", "Lithuania (LTU)"}, // oss-allow: public ISO or alliance table
		{"ZZZ", "ZZZ"},            // unknown code renders as itself, never blank
		{"", ""},
	} {
		if got := NationDisplay(reg, c.in); got != c.want {
			t.Errorf("NationDisplay(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// A nil registry must not panic and must still normalise.
	if got := NationDisplay(nil, "US"); got != "USA" {
		t.Errorf("NationDisplay(nil, US) = %q, want USA", got)
	}
}
