package onboardstore

import (
	"context"
	"fmt"
	"strings"
)

// CompleteProposal turns a minimal-burden draft into a complete proposal: the
// intake record in manifest_json is replaced by the assembled manifest, the
// draft flag clears, the handle follows the assembled identity.handle, and the
// note loses its draft marker. Review-stage rows only (proposed /
// sponsor_approved), like UpdateProposalManifest, and only while the row is
// still a draft: a complete row is never rewritten through this door.
func (s *Store) CompleteProposal(ctx context.Context, id, handle, manifestJSON, note string) error {
	if strings.TrimSpace(manifestJSON) == "" {
		return fmt.Errorf("complete proposal: empty manifest")
	}
	if strings.TrimSpace(handle) == "" {
		return fmt.Errorf("complete proposal: empty handle")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE onboard_proposals SET manifest_json = $1, handle = $2, note = $3, complete = 1
		 WHERE id = $4 AND complete = 0 AND status IN ($5, $6)`,
		manifestJSON, handle, note, id, ProposalProposed, ProposalSponsorApproved)
	if err != nil {
		return fmt.Errorf("complete proposal: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("complete proposal: count affected rows: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateDraftNote rewrites the note on an INCOMPLETE draft without touching
// its completion flag or lifecycle status. Review-stage rows only (proposed /
// sponsor_approved), like CompleteProposal, and only while the row is still
// marked incomplete.
func (s *Store) UpdateDraftNote(ctx context.Context, id, note string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE onboard_proposals SET note = $1
		   WHERE id = $2 AND complete = 0 AND status IN ($3, $4)`,
		note, id, ProposalProposed, ProposalSponsorApproved)
	if err != nil {
		return fmt.Errorf("update draft note: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update draft note: count affected rows: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
