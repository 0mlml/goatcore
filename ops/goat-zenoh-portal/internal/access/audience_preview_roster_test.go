package access

import (
	"testing"
	"time"
)

// TestPreviewOverTheCommittedRoster runs the real rule over the real committed
// roster projection, for every org that has member anchors.
//
// It asserts the ONE invariant that must hold whatever the registry contains:
// a preview may never claim to be evaluable while having decided nothing. That
// is the property a producer's judgement rests on — an evaluable zero means the
// restriction is holding, a non-evaluable zero means the question was not
// answerable — and it must not depend on how much of the registry is filled in
// on any given day.
//
// It logs the live distribution rather than asserting counts, because those
// numbers move every time an org or a manifest declares a nation, and a test
// that pinned them would go red on a registry backfill (the exact class of
// brittleness F-479 was).
func TestPreviewOverTheCommittedRoster(t *testing.T) {
	dr, err := LoadDataRoster("../../../../docs/operators/roster.json")
	if err != nil {
		t.Skipf("committed roster not readable from here: %v", err)
	}
	fw, ferr := LoadCoalitionFrameworks("../../../../ops/identity/coalition-frameworks.yaml")
	if ferr != nil {
		t.Logf("coalition frameworks not loaded (%v); multinational orgs will refuse", ferr)
	}
	asOf := time.Now().UTC()

	var orgs, evaluable, notEvaluable int
	var total, admitted, excluded, orgUndeclared, holderUndeclared, noFramework int

	for _, org := range dr.OrgsWithPersonMembers() {
		anchors := dr.ProducersUnderOrg(org)
		if len(anchors) == 0 {
			continue
		}
		orgs++
		ins := make([]PreviewInput, 0, len(anchors))
		for _, a := range anchors {
			ok, why, _ := dr.ClassifyOrgMaterial(a, org, fw, asOf)
			held, _ := dr.ProducerNationality(a)
			ins = append(ins, PreviewInput{Anchor: a, Org: org, Admitted: ok, Refusal: why, Nationality: held})
		}
		p := BuildAudiencePreview(ins)

		// THE INVARIANT. Evaluable means the rule reached a verdict on somebody:
		// it admitted a holder, or it refused one on facts both sides declared.
		// Claiming evaluable without either would tell a producer that a zero is
		// a restriction when it is an unanswered question.
		if p.Evaluable && p.Holders == 0 && p.Refused.Excluded == 0 {
			t.Fatalf("org %q: preview claims evaluable having decided nothing (%+v)", org, p.Refused)
		}
		// And the converse: a preview that admitted somebody is evaluable by
		// construction.
		if p.Holders > 0 && !p.Evaluable {
			t.Fatalf("org %q: %d holders admitted but preview reports not evaluable", org, p.Holders)
		}
		// No breakdown cell may ever sit below the floor while named.
		for _, c := range append(append([]AudienceCell{}, p.ByNationality...), p.ByOrg...) {
			if c.Value != "" && c.Holders < KAnonymityFloor {
				t.Fatalf("org %q: named cell %q holds %d, below the k-floor of %d",
					org, c.Value, c.Holders, KAnonymityFloor)
			}
		}

		if p.Evaluable {
			evaluable++
		} else {
			notEvaluable++
		}
		total += len(anchors)
		admitted += p.Holders
		excluded += p.Refused.Excluded
		orgUndeclared += p.Refused.OrgNationUndeclared
		holderUndeclared += p.Refused.HolderNationalityUndeclared
		noFramework += p.Refused.FrameworkUnavailable
	}

	t.Logf("orgs with member anchors: %d — evaluable %d, NOT evaluable %d", orgs, evaluable, notEvaluable)
	t.Logf("anchor decisions: %d total | admitted %d | excluded %d | org-nation undeclared %d | holder-nationality undeclared %d | no framework %d",
		total, admitted, excluded, orgUndeclared, holderUndeclared, noFramework)
}
