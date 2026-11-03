package onboardstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrGitHubNotEditable is returned by SetProposalGitHub when the proposal is
// not in a state where a post-mint GitHub change applies: before the mint the
// review-stage field edit owns the value, mid-mint the runner holds the row,
// and an archived, offboarded, rejected or failed person has no live access
// for a username to extend.
var ErrGitHubNotEditable = errors.New("proposal is not onboarded or enrolled")

// SetProposalGitHub writes identity.github on an onboarded or enrolled
// proposal's manifest and returns the value it replaced. An empty login
// removes the key.
//
// The manifest is otherwise frozen once minted (UpdateProposalManifest), and
// this does not loosen that: the GitHub login is not an authorization decision
// and nothing minted from the manifest carries it. It is the one input the
// repo-collaborator grant reads that a person can legitimately supply late,
// and the grant reads it from here, so a username added after onboarding has
// to land here for the grant to see it (docs/design/onboarding-github-repo-grants.md).
//
// The caller validates the login shape; this only stores it.
func (s *Store) SetProposalGitHub(ctx context.Context, id, login string) (string, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("set proposal github: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var row struct {
		Status   string `db:"status"`
		Manifest string `db:"manifest_json"`
	}
	err = tx.GetContext(ctx, &row, `SELECT status, manifest_json FROM onboard_proposals WHERE id = $1 FOR UPDATE`, id)
	if errors.Is(err, sqlErrNoRows()) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("set proposal github: %w", err)
	}
	if row.Status != ProposalOnboarded && row.Status != ProposalEnrolled {
		return "", ErrGitHubNotEditable
	}

	// UseNumber so the rest of the manifest (ttl_days, usage_limit, quotas)
	// round-trips byte-for-byte as integers.
	dec := json.NewDecoder(strings.NewReader(row.Manifest))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return "", fmt.Errorf("set proposal github: manifest is not JSON: %w", err)
	}
	ident, _ := m["identity"].(map[string]any)
	if ident == nil {
		ident = map[string]any{}
		m["identity"] = ident
	}
	before, _ := ident["github"].(string)
	login = strings.TrimSpace(login)
	if login == "" {
		delete(ident, "github")
	} else {
		ident["github"] = login
	}
	out, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("set proposal github: %w", err)
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE onboard_proposals SET manifest_json = $1 WHERE id = $2 AND status = $3`,
		string(out), id, row.Status)
	if err != nil {
		return "", fmt.Errorf("set proposal github: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("set proposal github: count affected rows: %w", err)
	}
	if n != 1 {
		return "", ErrGitHubNotEditable
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("set proposal github: %w", err)
	}
	return before, nil
}
