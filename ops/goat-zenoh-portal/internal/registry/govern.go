package registry

// govern.go is the governance-admin nomination store (ADR 1102 decision 1, the
// U9 surface). An org starts in BOOTSTRAP mode — every registry member of O
// carries data.govern.org:O — and is NARROWED the moment it has at least one
// active nomination row: from then on only nominated admins govern. Rows are
// deprecate-never-delete (removal end-dates + flips status), and removing the
// last active admin of an org is refused — narrowing is monotonic; an org never
// silently falls open back to everyone-governs. Principal-key discipline
// (ADR 1051/1069): the nominee is keyed on the identity UUID, never email.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

// Govern-admin lifecycle status (closed set).
const (
	GovernAdminActive  = "active"
	GovernAdminRemoved = "removed"
)

// ErrLastGovernAdmin signals a refused removal of an org's only active admin.
var ErrLastGovernAdmin = errors.New("cannot remove the last governance admin of a narrowed org — nominate a replacement first")

// ErrGovernAdminExists signals a duplicate active nomination (same org + identity).
var ErrGovernAdminExists = errors.New("this identity is already an active governance admin of the org")

// GovernAdmin is one nomination row.
type GovernAdmin struct {
	ID            string  `db:"id"            json:"id"`
	Org           string  `db:"org"           json:"org"`
	IdentityID    string  `db:"identity_id"   json:"identity_id"`
	NominatedBy   string  `db:"nominated_by"  json:"nominated_by"`
	Justification string  `db:"justification" json:"justification"`
	Status        string  `db:"status"        json:"status"`
	CreatedAt     int64   `db:"created_at"    json:"created_at"`
	RemovedAt     *int64  `db:"removed_at"    json:"removed_at,omitempty"`
	RemovedBy     *string `db:"removed_by"    json:"removed_by,omitempty"`
}

func newGovernAdminID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "gadm-" + hex.EncodeToString(b[:]), nil
}

// NominateGovernAdmin inserts an active nomination row. Refuses a duplicate
// active nomination for the same (org, identity). Assigns and returns g.ID.
func (r *Registry) NominateGovernAdmin(ctx context.Context, g *GovernAdmin) error {
	g.Org = strings.TrimSpace(g.Org)
	g.IdentityID = strings.TrimSpace(g.IdentityID)
	if g.Org == "" || g.IdentityID == "" {
		return fmt.Errorf("govern admin: org and identity_id are required")
	}
	var n int
	if err := r.db.GetContext(ctx, &n,
		`SELECT COUNT(*) FROM govern_admins WHERE org = $1 AND identity_id = $2 AND status = $3`,
		g.Org, g.IdentityID, GovernAdminActive); err != nil {
		return err
	}
	if n > 0 {
		return ErrGovernAdminExists
	}
	id, err := newGovernAdminID()
	if err != nil {
		return fmt.Errorf("mint govern-admin id: %w", err)
	}
	g.ID = id
	if g.CreatedAt == 0 {
		g.CreatedAt = nowUnix()
	}
	g.Status = GovernAdminActive
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO govern_admins (id, org, identity_id, nominated_by, justification, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		g.ID, g.Org, g.IdentityID, g.NominatedBy, g.Justification, g.Status, g.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert govern admin: %w", err)
	}
	return nil
}

// GetGovernAdmin returns one nomination row by id, or ErrNotFound.
func (r *Registry) GetGovernAdmin(ctx context.Context, id string) (*GovernAdmin, error) {
	var g GovernAdmin
	err := r.reader().GetContext(ctx, &g, `SELECT * FROM govern_admins WHERE id = $1`, id)
	if errors.Is(err, sqlErrNoRows()) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// ListGovernAdmins returns nomination rows, newest first, optionally narrowed
// to one org and/or active-only.
func (r *Registry) ListGovernAdmins(ctx context.Context, org string, onlyActive bool) ([]GovernAdmin, error) {
	q := `SELECT * FROM govern_admins`
	var where []string
	var args []any
	if strings.TrimSpace(org) != "" {
		where = append(where, "org = ?")
		args = append(args, strings.TrimSpace(org))
	}
	if onlyActive {
		where = append(where, "status = ?")
		args = append(args, GovernAdminActive)
	}
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY created_at DESC, id DESC"
	q = sqlx.Rebind(sqlx.DOLLAR, q)
	out := []GovernAdmin{}
	if err := r.reader().SelectContext(ctx, &out, q, args...); err != nil {
		return nil, fmt.Errorf("list govern admins: %w", err)
	}
	return out, nil
}

// RemoveGovernAdmin end-dates a nomination (status=removed + stamps). Refuses
// removing the org's LAST active admin (ErrLastGovernAdmin — narrowing is
// monotonic). ErrNotFound on an unknown id; removing an already-removed row is
// a no-op that succeeds.
func (r *Registry) RemoveGovernAdmin(ctx context.Context, id, removedBy string) error {
	g, err := r.GetGovernAdmin(ctx, id)
	if err != nil {
		return err
	}
	if g.Status != GovernAdminActive {
		return nil
	}
	var n int
	if err := r.db.GetContext(ctx, &n,
		`SELECT COUNT(*) FROM govern_admins WHERE org = $1 AND status = $2`,
		g.Org, GovernAdminActive); err != nil {
		return err
	}
	if n <= 1 {
		return ErrLastGovernAdmin
	}
	_, err = r.db.ExecContext(ctx,
		`UPDATE govern_admins SET status = $1, removed_at = $2, removed_by = $3 WHERE id = $4 AND status = $5`,
		GovernAdminRemoved, nowUnix(), removedBy, id, GovernAdminActive)
	return err
}

// GovernNarrowedOrgs returns the set of orgs holding at least one ACTIVE
// nomination — the orgs where the bootstrap member-of⇒govern rule no longer
// applies and only nominated admins carry data.govern.org:<O>.
func (r *Registry) GovernNarrowedOrgs(ctx context.Context) (map[string]bool, error) {
	var orgs []string
	if err := r.reader().SelectContext(ctx, &orgs,
		`SELECT DISTINCT org FROM govern_admins WHERE status = $1`, GovernAdminActive); err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(orgs))
	for _, o := range orgs {
		out[o] = true
	}
	return out, nil
}

// GovernAdminOrgs returns the orgs an identity is an ACTIVE nominated admin of.
func (r *Registry) GovernAdminOrgs(ctx context.Context, identityID string) (map[string]bool, error) {
	var orgs []string
	if err := r.reader().SelectContext(ctx, &orgs,
		`SELECT DISTINCT org FROM govern_admins WHERE identity_id = $1 AND status = $2`,
		strings.TrimSpace(identityID), GovernAdminActive); err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(orgs))
	for _, o := range orgs {
		out[o] = true
	}
	return out, nil
}

// GovernMetaGet returns the value of a govern_meta key and whether it was set.
// Used by the one-time cutover migration to check its own idempotency marker.
func (r *Registry) GovernMetaGet(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := r.reader().GetContext(ctx, &v, `SELECT value FROM govern_meta WHERE key = $1`, key)
	if errors.Is(err, sqlErrNoRows()) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// GovernMetaSet upserts a govern_meta key. The cutover migration writes its
// completion marker here so it never re-runs on a later boot.
func (r *Registry) GovernMetaSet(ctx context.Context, key, value string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO govern_meta (key, value, updated_at) VALUES ($1, $2, $3)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		strings.TrimSpace(key), value, nowUnix())
	return err
}
