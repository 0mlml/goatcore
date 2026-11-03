package access

import "testing"

// in builds a PreviewInput quickly.
func in(anchor, org string, admitted bool, why WrapRefusal, nat string) PreviewInput {
	return PreviewInput{Anchor: anchor, Org: org, Admitted: admitted, Refusal: why, Nationality: nat}
}

// nAdmitted returns n admitted holders of one nationality under one org.
func nAdmitted(n int, org, nat, prefix string) []PreviewInput {
	out := make([]PreviewInput, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, in(prefix+string(rune('a'+i)), org, true, WrapAdmitted, nat))
	}
	return out
}

// TestUndeclaredZeroIsNotAnExcludedZero is the central case. Both produce
// Holders == 0 and they mean opposite things; a producer who reads the second
// as the first concludes the restriction is tight when it is unevaluable.
func TestUndeclaredZeroIsNotAnExcludedZero(t *testing.T) {
	undeclared := BuildAudiencePreview([]PreviewInput{
		in("a1", "usareur-af", false, WrapHolderNationalityUndeclared, ""),
		in("a2", "usareur-af", false, WrapHolderNationalityUndeclared, ""),
	})
	if undeclared.Holders != 0 {
		t.Fatalf("holders = %d, want 0", undeclared.Holders)
	}
	if undeclared.Evaluable {
		t.Fatal("an all-undeclared audience must NOT report itself evaluable — that is the whole point")
	}
	if undeclared.Refused.HolderNationalityUndeclared != 2 {
		t.Fatalf("undeclared holders = %d, want 2", undeclared.Refused.HolderNationalityUndeclared)
	}
	if undeclared.Refused.Excluded != 0 {
		t.Fatalf("excluded = %d, want 0 — nobody was tested", undeclared.Refused.Excluded)
	}

	excluded := BuildAudiencePreview([]PreviewInput{
		in("a1", "usareur-af", false, WrapNationalityExcluded, "CAN"),
		in("a2", "usareur-af", false, WrapNationalityExcluded, "CAN"),
	})
	if excluded.Holders != 0 {
		t.Fatalf("holders = %d, want 0", excluded.Holders)
	}
	if !excluded.Evaluable {
		t.Fatal("an all-excluded audience IS evaluable — the rule was applied and refused everyone")
	}
	if excluded.Refused.Excluded != 2 {
		t.Fatalf("excluded = %d, want 2", excluded.Refused.Excluded)
	}
	// The two zeros must be distinguishable by a machine, not only by prose.
	if undeclared.Evaluable == excluded.Evaluable {
		t.Fatal("undeclared and excluded zeros are indistinguishable — the defect this surface exists to prevent")
	}
}

// TestOrgNationUndeclaredIsItsOwnBucket keeps "the org never declared" apart
// from "the holder never declared": they are fixed by different people.
func TestOrgNationUndeclaredIsItsOwnBucket(t *testing.T) {
	p := BuildAudiencePreview([]PreviewInput{
		in("a1", "nowhere", false, WrapOrgNationUndeclared, ""),
		in("a2", "nowhere", false, WrapOrgNationUndeclared, "USA"),
	})
	if p.Refused.OrgNationUndeclared != 2 {
		t.Fatalf("org-undeclared = %d, want 2", p.Refused.OrgNationUndeclared)
	}
	if p.Refused.HolderNationalityUndeclared != 0 {
		t.Fatalf("holder-undeclared = %d, want 0 — the org is what is missing", p.Refused.HolderNationalityUndeclared)
	}
	if p.Refused.Total != 2 {
		t.Fatalf("total = %d, want 2", p.Refused.Total)
	}
}

// TestSmallAudienceWithholdsItsBreakdown covers the re-identification case the
// brief names: "1 holder, CAN, usareur-af" is a name to that org's colleagues.
func TestSmallAudienceWithholdsItsBreakdown(t *testing.T) {
	p := BuildAudiencePreview(nAdmitted(1, "usareur-af", "CAN", "x"))
	if p.Holders != 1 {
		t.Fatalf("holders = %d, want 1 — the total is never suppressed", p.Holders)
	}
	for _, c := range p.ByNationality {
		if c.Value != "" {
			t.Fatalf("nationality %q leaked below the k-floor", c.Value)
		}
		if !c.Withheld {
			t.Fatal("small cell not marked withheld")
		}
	}
	for _, c := range p.ByOrg {
		if c.Value != "" {
			t.Fatalf("org %q leaked below the k-floor", c.Value)
		}
	}
}

// TestBreakdownShownOnceAboveTheFloor — the floor must not suppress everything
// forever, or the surface is useless at real sizes.
func TestBreakdownShownOnceAboveTheFloor(t *testing.T) {
	var ins []PreviewInput
	ins = append(ins, nAdmitted(KAnonymityFloor+1, "usareur-af", "USA", "u")...)
	p := BuildAudiencePreview(ins)
	if p.Holders != KAnonymityFloor+1 {
		t.Fatalf("holders = %d, want %d", p.Holders, KAnonymityFloor+1)
	}
	if len(p.ByNationality) != 1 || p.ByNationality[0].Value != "USA" {
		t.Fatalf("expected a visible USA cell, got %+v", p.ByNationality)
	}
	if p.ByNationality[0].Holders != KAnonymityFloor+1 {
		t.Fatalf("cell holders = %d", p.ByNationality[0].Holders)
	}
}

// TestSmallCellsFoldIntoOneWithheldRow — a big audience with a one-person
// nationality must not name that nationality.
func TestSmallCellsFoldIntoOneWithheldRow(t *testing.T) {
	var ins []PreviewInput
	ins = append(ins, nAdmitted(KAnonymityFloor+2, "big", "USA", "u")...)
	ins = append(ins, in("lone", "big", true, WrapAdmitted, "CAN"))
	p := BuildAudiencePreview(ins)

	var sawUSA, sawWithheld bool
	for _, c := range p.ByNationality {
		if c.Value == "CAN" {
			t.Fatal("a single-holder nationality was named — re-identification")
		}
		if c.Value == "USA" {
			sawUSA = true
		}
		if c.Withheld {
			sawWithheld = true
			if c.Holders != 1 || c.Cells != 1 {
				t.Fatalf("withheld row = %+v, want 1 holder in 1 cell", c)
			}
		}
	}
	if !sawUSA || !sawWithheld {
		t.Fatalf("want a visible USA cell and a withheld row, got %+v", p.ByNationality)
	}
	// The total still accounts for the withheld holder.
	if p.Holders != KAnonymityFloor+3 {
		t.Fatalf("holders = %d, want %d", p.Holders, KAnonymityFloor+3)
	}
}

// TestAnchorCountedOnceAcrossGrants — a holder reached by two grants receives
// one wrap slot, so it is one holder.
func TestAnchorCountedOnceAcrossGrants(t *testing.T) {
	p := BuildAudiencePreview([]PreviewInput{
		in("dup", "org-a", true, WrapAdmitted, "USA"),
		in("dup", "org-b", true, WrapAdmitted, "USA"),
	})
	if p.Holders != 1 {
		t.Fatalf("holders = %d, want 1 — one anchor is one wrap slot", p.Holders)
	}
}

// TestAdmittedAnywhereIsNotRefused — refused under one grant and admitted under
// another means the holder CAN decrypt, so it must not also be counted refused.
func TestAdmittedAnywhereIsNotRefused(t *testing.T) {
	p := BuildAudiencePreview([]PreviewInput{
		in("both", "org-a", false, WrapNationalityExcluded, "CAN"),
		in("both", "org-b", true, WrapAdmitted, "CAN"),
	})
	if p.Holders != 1 {
		t.Fatalf("holders = %d, want 1", p.Holders)
	}
	if p.Refused.Total != 0 {
		t.Fatalf("refused = %d, want 0 — the holder is in the audience", p.Refused.Total)
	}
}

// TestEmptyInputIsNotEvaluable — a producer with no org grants gets an honest
// "nothing to say", not a confident zero.
func TestEmptyInputIsNotEvaluable(t *testing.T) {
	p := BuildAudiencePreview(nil)
	if p.Holders != 0 || p.Refused.Total != 0 {
		t.Fatalf("want an empty preview, got %+v", p)
	}
	if p.Evaluable {
		t.Fatal("an empty preview must not claim to be evaluable")
	}
	if p.ByNationality == nil || p.ByOrg == nil {
		t.Fatal("breakdowns must serialize as [] not null")
	}
}
