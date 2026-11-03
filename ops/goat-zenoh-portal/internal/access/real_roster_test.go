package access

import (
	"testing"
	"time"
)

// Exercises the ratified rule against the REAL committed roster rather than a
// fixture, because every fixture I write agrees with me by construction.
func TestRealRoster_NationalityIntersection(t *testing.T) {
	dr, err := LoadDataRoster("../../../../docs/operators/roster.json")
	if err != nil {
		t.Skipf("roster not readable: %v", err)
	}
	asOf := time.Now().UTC()
	var admitted, refusedNat, refusedUndecl, refusedNoOrgNation int
	for org := range dr.orgProducers {
		for _, p := range dr.ProducersUnderOrg(org) {
			ok, why := dr.MayReceiveOrgMaterial(p, org, nil, asOf)
			switch {
			case ok:
				admitted++
			case why == "the organisation declares no owning nation, so no holder can be matched against it":
				refusedNoOrgNation++
			case why == "the holder declares no nationality — undeclared is not a match":
				refusedUndecl++
			default:
				refusedNat++
			}
		}
	}
	t.Logf("REAL ROSTER: admitted=%d refused(nationality differs)=%d refused(holder undeclared)=%d refused(org has no nation)=%d",
		admitted, refusedNat, refusedUndecl, refusedNoOrgNation)
	if admitted == 0 {
		t.Errorf("the intersection admits nobody on the real roster — the data did not reach it")
	}
}
