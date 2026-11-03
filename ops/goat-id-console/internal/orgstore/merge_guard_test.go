package orgstore

import (
	"errors"
	"strings"
	"testing"
)

// A merge rewrites what an org IS and leaves the loser id as a permanent alias
// of that claim, so a kind or nation mismatch must fail closed rather than be
// absorbed silently. The USAREUR-AF family is the live case this guards: nine
// single-member rows under one name with `kind` disagreeing — organic (a US
// gov/mil unit) against vendor (a commercial company) — and nothing in the
// merge path objected.
func TestMergeRefusesKindAndNationMismatch(t *testing.T) {
	s := open(t)

	if _, err := s.CreateOrg(Org{ID: "unit-organic", Kind: "organic", Nation: "USA", CreatedBy: "test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateOrg(Org{ID: "company-vendor", Kind: "vendor", Nation: "USA", CreatedBy: "test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateOrg(Org{ID: "unit-other-nation", Kind: "organic", Nation: "SVK", CreatedBy: "test"}); err != nil {
		t.Fatal(err)
	}

	// kind mismatch: a commercial company must not be absorbed into a mil unit.
	err := s.MergeOrg("company-vendor", "unit-organic", "op", "cleanup", true)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("kind mismatch: want ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "kind") {
		t.Errorf("refusal must name kind as the reason: %v", err)
	}

	// nation mismatch, genuinely different countries.
	if err := s.MergeOrg("unit-other-nation", "unit-organic", "op", "cleanup", true); !errors.Is(err, ErrValidation) {
		t.Fatalf("nation mismatch: want ErrValidation, got %v", err)
	}
}

// US and USA are the same nation, so the guard must NOT fire on them. Refusing
// that merge would block the exact repair the guard exists to make safe — the
// roster carries `demo-org` as USA and `demo-org-odt` as US today.
func TestMergeAllowsEitherNationAbbreviation(t *testing.T) {
	s := open(t)

	if _, err := s.CreateOrg(Org{ID: "demo-org", Kind: "organic", Nation: "USA", CreatedBy: "test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateOrg(Org{ID: "demoorg", Kind: "organic", Nation: "US", CreatedBy: "test"}); err != nil {
		t.Fatal(err)
	}

	if err := s.MergeOrg("demoorg", "demo-org", "op", "missing hyphens", true); err != nil {
		t.Fatalf("US into USA must be allowed after normalisation, got %v", err)
	}

	got, err := s.Resolve("demoorg")
	if err != nil {
		t.Fatal(err)
	}
	if got != "demo-org" {
		t.Errorf("loser id must resolve to the winner: got %q", got)
	}
}

// An org with no kind or nation recorded must not be blocked — the guard fires
// only when both sides carry a value and they genuinely disagree.
func TestMergeGuardIgnoresUnsetFields(t *testing.T) {
	s := open(t)

	if _, err := s.CreateOrg(Org{ID: "known", Kind: "organic", Nation: "USA", CreatedBy: "test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateOrg(Org{ID: "bare", CreatedBy: "test"}); err != nil {
		t.Fatal(err)
	}

	if err := s.MergeOrg("bare", "known", "op", "no facts recorded", true); err != nil {
		t.Fatalf("unset kind/nation must not block a merge, got %v", err)
	}
}
