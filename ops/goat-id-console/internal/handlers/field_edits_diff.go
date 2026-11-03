package handlers

import (
	"sort"
	"strings"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// diffFieldEdits applies fields to the manifest m in place and returns the
// fields that changed. It writes nothing to the store; applyFieldEdits
// persists m and audits the diff.
func (s *Server) diffFieldEdits(m map[string]any, fields map[string]string) ([]fieldEdit, *fieldEditError) {
	// Deterministic order so the audit diff and the response read the same way
	// on every call.
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)

	edits := make([]fieldEdit, 0, len(names))
	for _, name := range names {
		val := strings.TrimSpace(fields[name])
		// `stored` is what actually landed in the manifest, which is NOT always
		// what was typed (nation upper-cases, a github login loses its @). The
		// diff and the audit record `stored`, because an audit row that disagrees
		// with the document it describes is worse than no audit row.
		var before, stored string
		var ferr *fieldEditError

		switch name {
		case FieldAffiliationID:
			before, stored, ferr = s.setAffiliationOrg(m, val)
		case FieldAffiliationNat:
			before, stored, ferr = setNation(m, val)
		case FieldOrgDisplayName:
			before, stored, ferr = setDisplayName(m, val)
		case FieldIdentityGitHub:
			before, stored, ferr = setGitHub(m, val)
		}
		if ferr != nil {
			return nil, ferr
		}
		if before == stored {
			continue // no-op: keep it out of the diff rather than logging a non-change
		}
		edits = append(edits, fieldEdit{Field: name, Before: before, After: stored})
	}
	return edits, nil
}

// setDisplayName sets the organisation display name. The schema requires a
// name (minLength 1), so a blank has no meaning here: it is never a clear,
// only a document the approve gate refuses.
func setDisplayName(m map[string]any, val string) (string, string, *fieldEditError) {
	before := orgDisplayName(m)
	if val == "" {
		return before, "", badRequest("%s cannot be blank — the manifest schema requires an organisation display name; type the name, or leave the box as it was", FieldOrgDisplayName)
	}
	setOrgDisplayName(m, val)
	return before, val, nil
}

// precheckDraftEdit runs fields against the manifest draft p would complete
// to, before completeDraft persists it and writes its audit row. The editor
// prefills a draft's boxes from that manifest, so Apply with nothing changed
// is a natural click; without this check it completed the draft and then
// answered "nothing to change", and the admin read a refusal as "nothing
// happened". An edit the Apply would refuse is caught here too. A draft that
// cannot assemble passes through: completeDraft names the gap.
func (s *Server) precheckDraftEdit(p *onboardstore.Proposal, fields map[string]string) *fieldEditError {
	a, err := s.assembleDraft(p, loadIntakeHints)
	if err != nil || a.outcome != draftCompleted {
		return nil //nolint:nilerr // completeDraft re-runs the assembly and answers this failure with its own status
	}
	edits, ferr := s.diffFieldEdits(a.manifest, fields)
	if ferr != nil {
		return ferr
	}
	if len(edits) == 0 {
		return nothingToChange()
	}
	return nil
}

func nothingToChange() *fieldEditError {
	return badRequest("every given value already matches the proposal — nothing to change")
}
