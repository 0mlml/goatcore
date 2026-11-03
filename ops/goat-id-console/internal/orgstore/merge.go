// Alias-absorb merge (ADR 1112, design doc §8): a duplicate org id becomes a
// permanent alias of the winner; every consumer resolves through aliases so
// nothing keyed to the loser id ever breaks. Deprecate-never-delete: the
// loser row is statused `absorbed`, never removed. Un-merge is the reversal.
//
// The governance-union acknowledgment (design §8 fail-closed guard) is
// enforced at the API/CLI layer — the caller fetches and DISPLAYS both orgs'
// authority sets (govern nominations, grants-as-grantee, shared pools live
// in the portal registry, not here) and asserts acknowledgment; this store
// refuses a merge without that assertion flag threaded through.
package orgstore

import (
	"context"
	"fmt"
)

// MergeOrg absorbs loser into winner:
//   - loser's open parent edge closes (its history survives);
//   - loser's CHILDREN re-point to winner via new effective-dated edges
//     (the old edges close — as-of-date queries still answer the past);
//   - loser's aliases re-point to winner;
//   - loser's id becomes an `absorbed-id` alias of winner;
//   - loser's row is statused `absorbed`.
//
// Both names may be ids or aliases. Refuses self-merge, an already-absorbed
// loser, and (fail-closed) a missing acknowledgment.
func (s *Store) MergeOrg(loserName, winnerName, actor, justification string, acknowledgedGovernanceUnion bool) error {
	if !acknowledgedGovernanceUnion {
		return fmt.Errorf("%w: merge requires explicit acknowledgment of the combined governance/grant authority set (fetch both orgs' authority sets and acknowledge)", ErrValidation)
	}
	loser, err := s.Resolve(loserName)
	if err != nil {
		return err
	}
	winner, err := s.Resolve(winnerName)
	if err != nil {
		return err
	}
	if loser == winner {
		return fmt.Errorf("%w: %s already resolves to %s — nothing to merge", ErrValidation, loserName, winner)
	}
	var loserStatus string
	if err := s.db.Get(&loserStatus, `SELECT status FROM orgs WHERE id = $1`, loser); err != nil {
		return err
	}
	if loserStatus == "absorbed" {
		return fmt.Errorf("%w: %s is already absorbed", ErrConflict, loser)
	}
	// Refuse a merge that would change what an org IS. `kind` is a claim about
	// the nature of the organisation -- organic is a US gov/mil unit, vendor is
	// a commercial company (identity-grammar.md) -- so absorbing a vendor into
	// an organic org asserts that a company is a military unit, and the loser id
	// survives forever as an alias of that claim. Fail closed: an operator who
	// genuinely means it re-kinds the loser first, which is reversible, then
	// merges. Nation is guarded for the same reason, after normalisation so that
	// US and USA are not treated as a mismatch.
	//
	// This is the rail that was missing when the USAREUR-AF fragments were
	// reviewed: nine single-member rows sat under one family with `kind`
	// disagreeing, and nothing in the merge path would have objected.
	var lk, wk, ln, wn string
	if err := s.db.Get(&lk, `SELECT COALESCE(kind,'') FROM orgs WHERE id = $1`, loser); err != nil {
		return err
	}
	if err := s.db.Get(&wk, `SELECT COALESCE(kind,'') FROM orgs WHERE id = $1`, winner); err != nil {
		return err
	}
	if lk != "" && wk != "" && lk != wk {
		return fmt.Errorf("%w: refusing to merge %s (kind %s) into %s (kind %s) — merging changes what the org IS and the loser id becomes a permanent alias of that claim; re-kind the loser deliberately first, then merge", ErrValidation, loser, lk, winner, wk)
	}
	if err := s.db.Get(&ln, `SELECT COALESCE(nation,'') FROM orgs WHERE id = $1`, loser); err != nil {
		return err
	}
	if err := s.db.Get(&wn, `SELECT COALESCE(nation,'') FROM orgs WHERE id = $1`, winner); err != nil {
		return err
	}
	if ln != "" && wn != "" && normalizeNation(ln) != normalizeNation(wn) {
		return fmt.Errorf("%w: refusing to merge %s (nation %s) into %s (nation %s) — set the loser's nation deliberately first if this is right", ErrValidation, loser, ln, winner, wn)
	}
	now := s.Now().Unix()
	tx, err := s.db.Beginx()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Close the loser's own open parent edge.
	if _, err := tx.ExecContext(context.Background(), `UPDATE org_parent_edges SET valid_to = $1 WHERE org_id = $2 AND valid_to IS NULL`, now, loser); err != nil {
		return err
	}
	// Re-point the loser's children to the winner (close + reopen so history
	// survives). A child that IS the winner would self-parent — skip it; the
	// operator re-homes that one explicitly.
	var children []string
	if err := tx.Select(&children, `SELECT org_id FROM org_parent_edges WHERE parent_id = $1 AND valid_to IS NULL`, loser); err != nil {
		return err
	}
	for _, child := range children {
		if child == winner {
			continue
		}
		if _, err := tx.ExecContext(context.Background(), `UPDATE org_parent_edges SET valid_to = $1 WHERE org_id = $2 AND valid_to IS NULL`, now, child); err != nil {
			return err
		}
		if _, err := tx.ExecContext(context.Background(), `INSERT INTO org_parent_edges (org_id, parent_id, valid_from, valid_to, recorded_at, actor, justification)
			VALUES ($1, $2, $3, NULL, $4, $5, $6)`, child, winner, now, now, actor, "re-pointed by merge of "+loser+" into "+winner); err != nil {
			return err
		}
	}
	// Re-point the loser's aliases; add the loser id itself as absorbed-id.
	if _, err := tx.ExecContext(context.Background(), `UPDATE org_aliases SET org_id = $1 WHERE org_id = $2`, winner, loser); err != nil {
		return err
	}
	if _, err := tx.ExecContext(context.Background(), `INSERT INTO org_aliases (alias, org_id, kind, recorded_at, actor) VALUES ($1, $2, 'absorbed-id', $3, $4)`,
		loser, winner, now, actor); err != nil {
		return err
	}
	if _, err := tx.ExecContext(context.Background(), `UPDATE orgs SET status = 'absorbed' WHERE id = $1`, loser); err != nil {
		return err
	}
	return tx.Commit()
}

// UnmergeOrg reverses a merge: the absorbed-id alias row is removed and the
// org row reactivates. Parent edges are NOT auto-reverted — the merge's edge
// moves are effective-dated history, and silently rewriting the tree on
// un-merge would fabricate a past; the operator re-attaches deliberately.
// Aliases that belonged to the loser before the merge stay with the winner
// for the same reason (each is re-pointable individually).
func (s *Store) UnmergeOrg(loserName, actor string) error {
	// The loser id is currently an alias — resolve it literally, not through
	// the alias table.
	var status string
	err := s.db.Get(&status, `SELECT status FROM orgs WHERE id = $1`, loserName)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrNotFound, loserName)
	}
	if status != "absorbed" {
		return fmt.Errorf("%w: %s is not absorbed (status %s)", ErrValidation, loserName, status)
	}
	tx, err := s.db.Beginx()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(context.Background(), `DELETE FROM org_aliases WHERE alias = $1 AND kind = 'absorbed-id'`, loserName); err != nil {
		return err
	}
	if _, err := tx.ExecContext(context.Background(), `UPDATE orgs SET status = 'active' WHERE id = $1`, loserName); err != nil {
		return err
	}
	_ = actor // the API layer audits with the actor; the row mutation is the reversal itself
	return tx.Commit()
}
