package registry

// grants.go is the ADR 1075 producer data-access grant store: enumerable
// effective-dated "grantee may VIEW producer" relations, deprecate-never-delete
// (G6 — revoke end-dates + flips status; nothing is ever DELETEd, so the
// before-the-fact audit-by-enumeration keeps its history). The row shape is the
// SoT law (ops/permissions/data-access-roles.yaml `grants.row_shape`). The
// data-roles resolver reads these rows live and joins them to a principal — no
// parallel policy store anywhere (SoT no_third_shim).
//
// PRINCIPAL-KEY DISCIPLINE (ADR 1051/1069): a principal grantee is keyed on the
// identity UUID, an org grantee on the org id; email never appears here.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

// Grantee kinds (the SoT `org:<id> | principal:<identity-uuid>` split).
const (
	GranteeOrg       = "org"
	GranteePrincipal = "principal"
)

// Grant lifecycle status (closed set).
const (
	GrantActive  = "active"
	GrantRevoked = "revoked"
)

// Grant action (ADR 1105). A view grant is the ADR 1075 default (read a
// producer's namespace); a write grant is a co-publish delegation — the
// grantee may `put` under a registered MULTI-PUBLISHER TOPIC, whose key-base
// (`multipub/<pool>/<topic>`, see multipub_topics.go) is stored in `producer`.
// A write grant NEVER targets a single-owner `<uuid>/**` root (the handler's
// guardrail enforces that; the store stays producer-agnostic). The closed set:
// legacy rows backfill to view, and validateGrant refuses any other value —
// fail-closed (an unknown action never reads as write).
const (
	GrantActionView  = "view"
	GrantActionWrite = "write"
)

// ErrGrantInvalid signals a structurally invalid grant (bad grantee kind, empty
// id/producer, or an effective window that ends before it starts).
var ErrGrantInvalid = errors.New("invalid grant row")

// Grant is one grant row. GranteeKind+GranteeID are the SoT grantee
// (`org:<id>` | `principal:<uuid>`), split for join-without-parse. EffectiveTo
// is nil for an open-ended grant. Status flips to revoked (never deleted) on
// revoke, which also stamps RevokedAt/RevokedBy.
type Grant struct {
	ID            string  `db:"id"             json:"id"`
	GranteeKind   string  `db:"grantee_kind"   json:"grantee_kind"`
	GranteeID     string  `db:"grantee_id"     json:"grantee_id"`
	Producer      string  `db:"producer"       json:"producer"`
	Action        string  `db:"action"         json:"action"`
	GrantedBy     string  `db:"granted_by"     json:"granted_by"`
	Justification string  `db:"justification"  json:"justification"`
	EffectiveFrom int64   `db:"effective_from" json:"effective_from"`
	EffectiveTo   *int64  `db:"effective_to"   json:"effective_to,omitempty"`
	Status        string  `db:"status"         json:"status"`
	CreatedAt     int64   `db:"created_at"     json:"created_at"`
	RevokedAt     *int64  `db:"revoked_at"     json:"revoked_at,omitempty"`
	RevokedBy     *string `db:"revoked_by"    json:"revoked_by,omitempty"`
}

// InEffectAt reports whether the grant confers access at unix time t: active
// status, started (from <= t), and not yet ended (to nil or t < to). A revoked
// grant is never in effect regardless of window (the mesh-first instant cut).
func (g Grant) InEffectAt(t int64) bool {
	if g.Status != GrantActive {
		return false
	}
	if g.EffectiveFrom > t {
		return false
	}
	if g.EffectiveTo != nil && t >= *g.EffectiveTo {
		return false
	}
	return true
}

// newGrantID returns an opaque random id ("grant-<16-hex>").
func newGrantID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "grant-" + hex.EncodeToString(b[:]), nil
}

// validateGrant checks the structural invariants before a write. Grantee kind
// is the closed set; grantee id + producer are required; the effective window
// must be well-ordered. It does NOT validate that the producer exists (a grant
// may be minted preemptively for a producer the caller names — existence is the
// handler's live check against the registry).
func validateGrant(g *Grant) error {
	if g.GranteeKind != GranteeOrg && g.GranteeKind != GranteePrincipal {
		return fmt.Errorf("%w: grantee_kind %q must be %q or %q", ErrGrantInvalid, g.GranteeKind, GranteeOrg, GranteePrincipal)
	}
	if strings.TrimSpace(g.GranteeID) == "" {
		return fmt.Errorf("%w: grantee_id is required", ErrGrantInvalid)
	}
	if strings.TrimSpace(g.Producer) == "" {
		return fmt.Errorf("%w: producer is required", ErrGrantInvalid)
	}
	if g.EffectiveFrom == 0 {
		return fmt.Errorf("%w: effective_from is required", ErrGrantInvalid)
	}
	if g.EffectiveTo != nil && *g.EffectiveTo <= g.EffectiveFrom {
		return fmt.Errorf("%w: effective_to must be after effective_from", ErrGrantInvalid)
	}
	// ADR 1105: action is the closed set; anything else fails closed (never
	// silently reads as a write). A write (co-publish) grant is only meaningful
	// to a principal grantee — the co-publisher is an identity/anchor, not an org
	// — and its `producer` must be a well-formed MULTI-PUBLISHER TOPIC key-base
	// (`multipub/<pool>/<topic>`), never a single-owner `<uuid>/**` root. The
	// handler additionally checks the multi-publisher topic is REGISTERED and
	// org-scoped; this store-level check is the structural floor (defense in
	// depth).
	if g.Action != GrantActionView && g.Action != GrantActionWrite {
		return fmt.Errorf("%w: action %q must be %q or %q", ErrGrantInvalid, g.Action, GrantActionView, GrantActionWrite)
	}
	if g.Action == GrantActionWrite {
		if g.GranteeKind != GranteePrincipal {
			return fmt.Errorf("%w: a write (co-publish) grant requires a %q grantee, not %q", ErrGrantInvalid, GranteePrincipal, g.GranteeKind)
		}
		if !IsMultipubTopicKeyBase(g.Producer) {
			return fmt.Errorf("%w: a write (co-publish) grant target must be a multipub-topic key-base (multipub/<pool>/<topic>), got %q — single-owner roots are inviolable", ErrGrantInvalid, g.Producer)
		}
	}
	return nil
}

// CreateGrant validates and inserts a new active grant row. GrantedBy is the
// custodian actor. CreatedAt + EffectiveFrom default to now when unset;
// Status defaults to active. Assigns and returns g.ID.
func (r *Registry) CreateGrant(ctx context.Context, g *Grant) error {
	g.GranteeKind = strings.ToLower(strings.TrimSpace(g.GranteeKind))
	g.GranteeID = strings.TrimSpace(g.GranteeID)
	g.Producer = strings.TrimSpace(g.Producer)
	g.Action = strings.ToLower(strings.TrimSpace(g.Action))
	if g.CreatedAt == 0 {
		g.CreatedAt = nowUnix()
	}
	if g.EffectiveFrom == 0 {
		g.EffectiveFrom = g.CreatedAt
	}
	if g.Status == "" {
		g.Status = GrantActive
	}
	if g.Action == "" {
		g.Action = GrantActionView
	}
	if err := validateGrant(g); err != nil {
		return err
	}
	id, err := newGrantID()
	if err != nil {
		return fmt.Errorf("mint grant id: %w", err)
	}
	g.ID = id
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO grants (id, grantee_kind, grantee_id, producer, action, granted_by, justification, effective_from, effective_to, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		g.ID, g.GranteeKind, g.GranteeID, g.Producer, g.Action, g.GrantedBy, g.Justification, g.EffectiveFrom, g.EffectiveTo, g.Status, g.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert grant: %w", err)
	}
	return nil
}

// GetGrant returns one grant by id, or ErrNotFound.
func (r *Registry) GetGrant(ctx context.Context, id string) (*Grant, error) {
	var g Grant
	err := r.reader().GetContext(ctx, &g, `SELECT * FROM grants WHERE id = $1`, id)
	if errors.Is(err, sqlErrNoRows()) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// ListGrants returns grant rows, newest first, optionally narrowed. An empty
// producer / granteeID does not filter on that field; onlyActive limits to
// status=active rows (still subject to the caller's own in-effect window check).
func (r *Registry) ListGrants(ctx context.Context, producer, granteeKind, granteeID string, onlyActive bool) ([]Grant, error) {
	q := `SELECT * FROM grants`
	var where []string
	var args []any
	if strings.TrimSpace(producer) != "" {
		where = append(where, "producer = ?")
		args = append(args, strings.TrimSpace(producer))
	}
	if strings.TrimSpace(granteeKind) != "" {
		where = append(where, "grantee_kind = ?")
		args = append(args, strings.ToLower(strings.TrimSpace(granteeKind)))
	}
	if strings.TrimSpace(granteeID) != "" {
		where = append(where, "grantee_id = ?")
		args = append(args, strings.TrimSpace(granteeID))
	}
	if onlyActive {
		where = append(where, "status = ?")
		args = append(args, GrantActive)
	}
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY created_at DESC, id DESC"
	q = sqlx.Rebind(sqlx.DOLLAR, q)
	out := []Grant{}
	if err := r.reader().SelectContext(ctx, &out, q, args...); err != nil {
		return nil, fmt.Errorf("list grants: %w", err)
	}
	return out, nil
}

// ActiveGrants returns every grant with status=active (still subject to the
// caller's own effective-window check via Grant.InEffectAt). The resolver reads
// this and joins in-effect rows to the principal — the "who can see X" read.
func (r *Registry) ActiveGrants(ctx context.Context) ([]Grant, error) {
	return r.ListGrants(ctx, "", "", "", true)
}

// ProducersWithViewGrants returns the distinct producers that carry at least
// one view grant in any status — the set of release-sets grant-keyd must sweep
// in grant-row mode (WS4 auto-sweep-list: a `goat access grant` alone puts the
// producer in scope, no operator config edit). Revoked and expired grants keep
// the producer listed: revoking the last grant has to reach grant-keyd as an
// audience that shrank to the producer, so it rotates to a generation the
// revoked reader is not in. Dropping the producer instead leaves its newest
// generation current, and the producer keeps sealing to the revoked reader.
// Effective-window filtering stays the resolver's job; this is the
// enumeration of candidates.
func (r *Registry) ProducersWithViewGrants(ctx context.Context) ([]string, error) {
	var producers []string
	// VIEW grants only — a co-publish (write) grant's `producer` is a
	// multipub-topic key-base, not an encryption-audience producer, so it must
	// never enter the grant-keyd sweep (ADR 1105 write grants are ACL-enforced,
	// not key-wrapped).
	err := r.reader().SelectContext(ctx, &producers,
		`SELECT DISTINCT producer FROM grants WHERE action = 'view' ORDER BY producer`)
	if err != nil {
		return nil, err
	}
	return producers, nil
}

// IsWrite reports whether this is a co-publish (write) grant (ADR 1105).
func (g Grant) IsWrite() bool { return g.Action == GrantActionWrite }

// CoPublishKeyBase returns the multipub-topic key-expr base a write grant
// authorizes the grantee to publish under — the `producer` field itself holds
// the registered `multipub/<pool>/<topic>` key-base. The ACL render appends the
// `/**`, `volatile/`, and `/@adv/**` forms. Empty for a non-write grant.
func (g Grant) CoPublishKeyBase() string {
	if !g.IsWrite() {
		return ""
	}
	return g.Producer
}

// ActiveWriteGrants returns every active co-publish (write) grant, newest
// first — the enumeration the resolver joins to a co-publisher and the
// group_var export materializes into zenoh_copublish_grants. Effective-window
// filtering stays the caller's job via Grant.InEffectAt.
func (r *Registry) ActiveWriteGrants(ctx context.Context) ([]Grant, error) {
	out := []Grant{}
	err := r.reader().SelectContext(ctx, &out,
		`SELECT * FROM grants WHERE status = 'active' AND action = 'write' ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list active write grants: %w", err)
	}
	return out, nil
}

// RevokeGrant end-dates a grant: flips status to revoked and stamps
// revoked_at/revoked_by. Deprecate-never-delete (G6) — the row stays for the
// audit history. Idempotent (revoking an already-revoked grant is a no-op that
// still succeeds); ErrNotFound on an unknown id. Revocation is the instant cut:
// an in-effect window no longer matters once status is revoked.
func (r *Registry) RevokeGrant(ctx context.Context, id, revokedBy string) error {
	now := nowUnix()
	res, err := r.db.ExecContext(ctx,
		`UPDATE grants SET status = $1, revoked_at = COALESCE(revoked_at, $2), revoked_by = COALESCE(revoked_by, $3) WHERE id = $4`,
		GrantRevoked, now, revokedBy, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
