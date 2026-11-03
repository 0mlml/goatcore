package access

import "testing"

// The COP carries onboarding stage so "who is here" and "who never finished
// arriving" are one question rather than two surfaces. It is display
// state: keyed off the write anchor, it must never become an access input, and
// an anchor with no progress row must come back empty rather than defaulted.
func TestRoster_carriesOnboardingStageKeyedOnAnchor(t *testing.T) {
	src := Sources{
		Vendors: []VendorIn{
			{Prefix: "with-stage", DisplayName: "With Stage", WriteAnchor: "with-stage"},
			{Prefix: "no-stage", DisplayName: "No Stage", WriteAnchor: "no-stage"},
		},
		OnboardingStage: map[string]string{"with-stage": "mesh-enrolled"},
	}
	g := Assemble(src)
	rows := g.Roster().Rows

	byID := map[string]RosterRow{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	if got := byID["with-stage"].OnboardingStage; got != "mesh-enrolled" {
		t.Errorf("stage should ride through verbatim; got %q", got)
	}
	// No progress row = empty, so the consumer can render "unknown". A default
	// here would assert a negative the registry never recorded.
	if got := byID["no-stage"].OnboardingStage; got != "" {
		t.Errorf("anchor with no progress row must be empty, got %q", got)
	}
	// Stage must not have created or altered any grant.
	if byID["with-stage"].Writes != byID["no-stage"].Writes {
		t.Errorf("onboarding stage changed write grants: %d vs %d",
			byID["with-stage"].Writes, byID["no-stage"].Writes)
	}
}

// A PERSON row carries no write anchor of its own — the anchor sits on the org
// spine it resolves to. Reading the person's own (empty) WriteAnchor made every
// person row render "unknown" on the live ZAGROS roster, which is exactly the
// population an admin searches by: they type an email, not a 32-hex anchor.
func TestRoster_personInheritsOrgOnboardingStage(t *testing.T) {
	src := Sources{
		Vendors: []VendorIn{
			{Prefix: "anchor1", DisplayName: "Org One", WriteAnchor: "anchor1"},
		},
		Identities: []IdentityIn{
			{ID: "kratos-uuid-1", Email: "someone@example.test", Name: "Someone", Vendor: "anchor1"},
		},
		OnboardingStage: map[string]string{"anchor1": "mesh-enrolled"},
	}
	g := Assemble(src)

	var person, org *RosterRow
	for i := range g.Roster().Rows {
		r := g.Roster().Rows[i]
		switch r.Kind {
		case KindPerson:
			rr := r
			person = &rr
		case KindVendor:
			rr := r
			org = &rr
		}
	}
	if org == nil || person == nil {
		t.Fatalf("expected both an org and a person row; org=%v person=%v", org, person)
	}
	if org.OnboardingStage != "mesh-enrolled" {
		t.Errorf("org row stage = %q, want mesh-enrolled", org.OnboardingStage)
	}
	if person.OnboardingStage != "mesh-enrolled" {
		t.Errorf("person row must inherit its org's stage, got %q", person.OnboardingStage)
	}
}
