package handlers

import (
	"strings"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/orgstore"
)

// nationSeedField is the hidden form field the review card posts beside a
// seeded Nation box. It names the registered org the box was filled from, so
// the server can tell an untouched seed from a nation the admin typed.
const nationSeedField = "affiliation.nation.seed"

// seedNation decides what the review card's correct-a-field Nation box renders.
//
// The manifest's own value always wins: it is what alliance standing and repo
// grants read, and a registered org that disagrees with it has to stay visible
// rather than be reconciled. When the manifest carries none and the typed org
// resolves, the box takes the REGISTERED org's nation. That is a registry fact,
// so ADR 1171 holds: nothing here comes from the candidate's own answer, which
// is the inheritance Decision 6 removed. fromOrg names the org the value came
// from and is empty when the manifest supplied it, or nothing did.
//
// The registry does not shape-check what it stores (`goat admin org register`
// passes the nation through untouched), while setNation refuses anything that
// is not a 2–4 letter code. A registered value that would fail Apply is not
// seeded: the box stays empty and the divergence hint beside it still shows
// the registry's value, so the reviewer sees it without every later Apply
// on the proposal failing on a box they never touched.
func seedNation(manifestNation string, resolved []orgstore.OrgView) (nation, fromOrg string) {
	if manifestNation != "" || len(resolved) == 0 {
		return manifestNation, ""
	}
	if !validNationCode(resolved[0].Nation) {
		return "", ""
	}
	return resolved[0].Nation, resolved[0].ID
}

// seedNationFromRegisteredOrg applies seedNation to an editable review card. It
// runs after fillEditable and orgChainForProposal, because it reads both. Until
// #1122 the box rendered empty for every typed org: intake asks only for the
// person's nationality, so the reviewer read the registered nation off the hint
// and typed it back in by hand.
func (v *proposalView) seedNationFromRegisteredOrg() {
	if !v.Editable {
		return
	}
	v.OrgNation, v.OrgNationFromOrg = seedNation(v.OrgNation, v.OrgResolved)
}

// reconcileSeededNation runs on the browser form's Apply, before the edits are
// applied. seedOrg is the hidden nationSeedField the card posted: the org the
// Nation box was seeded from, or empty when the box held the manifest's value.
//
// The posted nation is an untouched seed when it still equals that org's
// registered nation. An untouched seed is written, because that is the point
// of seeding, but it is written HONESTLY: the returned source names the org
// so the audit row records a registry-sourced value rather than an admin's
// correction. When the admin also changed the org in the same Apply, the
// seed belongs to the org they just moved away from; edits apply in field
// order, so without this the OLD org's nation would land on the manifest
// beside the NEW org and stand there. The seed is re-read from the org the
// admin chose, or dropped from the Apply when that org has no usable nation,
// so the next render seeds it afresh. A nation that differs from the seed
// was typed by the admin and is left exactly as typed.
func (s *Server) reconcileSeededNation(fields map[string]string, seedOrg string) (source string) {
	seedOrg = strings.TrimSpace(seedOrg)
	posted, hasNation := fields[FieldAffiliationNat]
	if seedOrg == "" || !hasNation || s.Orgs == nil {
		return ""
	}
	seeded, err := s.Orgs.GetOrg(seedOrg)
	if err != nil || strings.TrimSpace(posted) != seeded.Nation {
		return "" // the admin typed it, or the seed's org is gone: their value stands
	}
	if typed, ok := fields[FieldAffiliationID]; ok {
		canonical, rerr := s.Orgs.Resolve(strings.TrimSpace(typed))
		if rerr == nil && canonical != seeded.ID {
			next, gerr := s.Orgs.GetOrg(canonical)
			if gerr == nil && validNationCode(next.Nation) {
				fields[FieldAffiliationNat] = next.Nation
				return next.ID
			}
			delete(fields, FieldAffiliationNat)
			return ""
		}
	}
	return seeded.ID
}
