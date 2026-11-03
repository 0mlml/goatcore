package registry

// requests.go is the ADR 1075 discover->request->approve store. A request is a
// viewer's inert ask to read a producer's restricted namespace; the producer
// custodian (or operator) approves, which mints a grant row and marks the
// request approved. Preemptive grants (ADR 1075 §12) never touch this table —
// the grant verb has no pending-request precondition — so a request row is
// provenance for the request-driven path only.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

// Data-access request lifecycle status (closed set).
const (
	RequestPending  = "pending"
	RequestApproved = "approved"
	RequestDeclined = "declined"
)

// ErrRequestNotPending signals an approve/decline on a request that is no longer
// pending (already decided) — fail closed rather than double-mint a grant.
var ErrRequestNotPending = errors.New("data-access request is not pending")

// DataAccessRequest is one row of the request table. RequesterLabel is
// display-only (email/handle) and never a join key; RequesterID + GranteeID are
// identity UUIDs (or an org id for an org-scoped ask). GrantID is set to the
// minted grant when the request is approved.
type DataAccessRequest struct {
	ID             string  `db:"id"              json:"id"`
	RequesterID    string  `db:"requester_id"    json:"requester_id"`
	RequesterLabel string  `db:"requester_label" json:"requester_label,omitempty"`
	Producer       string  `db:"producer"        json:"producer"`
	GranteeKind    string  `db:"grantee_kind"    json:"grantee_kind"`
	GranteeID      string  `db:"grantee_id"      json:"grantee_id"`
	Justification  string  `db:"justification"   json:"justification,omitempty"`
	Status         string  `db:"status"          json:"status"`
	CreatedAt      int64   `db:"created_at"      json:"created_at"`
	DecidedBy      *string `db:"decided_by"     json:"decided_by,omitempty"`
	DecidedAt      *int64  `db:"decided_at"      json:"decided_at,omitempty"`
	GrantID        *string `db:"grant_id"       json:"grant_id,omitempty"`
	Note           string  `db:"note"            json:"note,omitempty"`
}

// newRequestID returns an opaque random id ("dar-<16-hex>").
func newRequestID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "dar-" + hex.EncodeToString(b[:]), nil
}

// CreateRequest inserts a new pending request. RequesterID + Producer are
// required; GranteeKind defaults to principal and GranteeID to the requester
// (a viewer asking for themselves). Assigns and returns req.ID.
func (r *Registry) CreateRequest(ctx context.Context, req *DataAccessRequest) error {
	req.RequesterID = strings.TrimSpace(req.RequesterID)
	req.Producer = strings.TrimSpace(req.Producer)
	if req.RequesterID == "" {
		return fmt.Errorf("requester_id is required")
	}
	if req.Producer == "" {
		return fmt.Errorf("producer is required")
	}
	if req.GranteeKind == "" {
		req.GranteeKind = GranteePrincipal
	}
	req.GranteeKind = strings.ToLower(strings.TrimSpace(req.GranteeKind))
	if req.GranteeKind != GranteeOrg && req.GranteeKind != GranteePrincipal {
		return fmt.Errorf("grantee_kind %q must be %q or %q", req.GranteeKind, GranteeOrg, GranteePrincipal)
	}
	if strings.TrimSpace(req.GranteeID) == "" {
		req.GranteeID = req.RequesterID
	}
	if req.Status == "" {
		req.Status = RequestPending
	}
	if req.CreatedAt == 0 {
		req.CreatedAt = nowUnix()
	}
	id, err := newRequestID()
	if err != nil {
		return fmt.Errorf("mint request id: %w", err)
	}
	req.ID = id
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO data_access_requests (id, requester_id, requester_label, producer, grantee_kind, grantee_id, justification, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		req.ID, req.RequesterID, req.RequesterLabel, req.Producer, req.GranteeKind, req.GranteeID, req.Justification, req.Status, req.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert request: %w", err)
	}
	return nil
}

// GetRequest returns one request by id, or ErrNotFound.
func (r *Registry) GetRequest(ctx context.Context, id string) (*DataAccessRequest, error) {
	var req DataAccessRequest
	err := r.reader().GetContext(ctx, &req, `SELECT * FROM data_access_requests WHERE id = $1`, id)
	if errors.Is(err, sqlErrNoRows()) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// ListRequests returns request rows newest-first, optionally narrowed by
// producer and/or status. Empty filters do not narrow that field.
func (r *Registry) ListRequests(ctx context.Context, producer, status string) ([]DataAccessRequest, error) {
	q := `SELECT * FROM data_access_requests`
	var where []string
	var args []any
	if strings.TrimSpace(producer) != "" {
		where = append(where, "producer = ?")
		args = append(args, strings.TrimSpace(producer))
	}
	if strings.TrimSpace(status) != "" {
		where = append(where, "status = ?")
		args = append(args, strings.ToLower(strings.TrimSpace(status)))
	}
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY created_at DESC, id DESC"
	q = sqlx.Rebind(sqlx.DOLLAR, q)
	out := []DataAccessRequest{}
	if err := r.reader().SelectContext(ctx, &out, q, args...); err != nil {
		return nil, fmt.Errorf("list requests: %w", err)
	}
	return out, nil
}

// DecideRequest atomically transitions a PENDING request to approved|declined,
// stamping decided_by/decided_at (+ grant_id on approval). The guarded UPDATE
// (WHERE status='pending') makes the decision single-shot — a second approver
// racing the same request gets ErrRequestNotPending, so approval never
// double-mints a grant. grantID is "" for a decline.
func (r *Registry) DecideRequest(ctx context.Context, id, decision, decidedBy, grantID, note string) error {
	if decision != RequestApproved && decision != RequestDeclined {
		return fmt.Errorf("decision %q must be %q or %q", decision, RequestApproved, RequestDeclined)
	}
	now := nowUnix()
	var gid any
	if grantID != "" {
		gid = grantID
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE data_access_requests SET status = $1, decided_by = $2, decided_at = $3, grant_id = $4, note = $5 WHERE id = $6 AND status = $7`,
		decision, decidedBy, now, gid, note, id, RequestPending)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// Either the row is gone or it was not pending — disambiguate for the caller.
		if _, gerr := r.GetRequest(ctx, id); errors.Is(gerr, ErrNotFound) {
			return ErrNotFound
		}
		return ErrRequestNotPending
	}
	return nil
}
