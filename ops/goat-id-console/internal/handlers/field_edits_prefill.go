package handlers

import (
	"encoding/json"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// editableManifest is the document the review editor prefills from: the
// stored manifest, or on an incomplete draft the manifest completion would
// write.
//
// The form posts every box it renders and an empty value clears the field,
// so the promise "leave a box untouched to leave the field alone" holds only
// while every box starts prefilled from the document Apply will write
// (docs/design/onboarding-review-field-edits.md). A draft holds no manifest,
// only the intake record, and prefilling from that record's shape rendered
// every box empty. The first Apply on a draft then completed it and cleared
// the display name the completion had just written, and the row failed the
// approve gate's schema check with no field named (zagros).
//
// Nothing is persisted here. A draft that cannot assemble prefills nothing,
// and Apply on it answers 409 before any write. A row that lacks the complete
// flag but already holds a manifest is read as-is, the same dual read
// applyFieldEdits makes.
func (s *Server) editableManifest(p onboardstore.Proposal, hints intakeHintsSource) map[string]any {
	if !p.Complete {
		a, err := s.assembleDraft(&p, hints)
		switch {
		case err != nil, a.outcome == draftBlocked:
			return nil
		case a.outcome == draftCompleted:
			return a.manifest
		}
		// draftNoRecord: the stored document is the manifest.
	}
	m, err := unmarshalManifest(p)
	if err != nil {
		return nil
	}
	return m
}

// unmarshalManifest decodes p's stored manifest into the map the field
// editor reads and writes. The prefill and the Apply path both decode
// through here, so the two cannot drift apart.
func unmarshalManifest(p onboardstore.Proposal) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(p.ManifestJSON), &m); err != nil {
		return nil, err
	}
	return m, nil
}
