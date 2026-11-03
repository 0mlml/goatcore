package onboardstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

const (
	ReactivationOpen     = "open"
	ReactivationDeclined = "declined"
	ReactivationClosed   = "closed"
)

// ReactivationRequest is a member's request to review one exact dormancy hold.
// It is an intake record only: this package has no mesh mutation method.
type ReactivationRequest struct {
	ID             string `db:"id"`
	PeerID         string `db:"peer_id"`
	PeerName       string `db:"peer_name"`
	Handle         string `db:"handle"`
	HoldEnteredAt  int64  `db:"hold_entered_at"`
	RequestedAt    int64  `db:"requested_at"`
	Status         string `db:"status"`
	DecidedBy      string `db:"decided_by"`
	DecidedAt      int64  `db:"decided_at"`
	DecisionReason string `db:"decision_reason"`
}

// RequestReactivation records at most one request for an exact hold. created
// is false when that hold was already requested, so reloads and double-clicks
// are harmless.
func (s *Store) RequestReactivation(ctx context.Context, peerID, peerName, handle string, enteredAt time.Time) (req ReactivationRequest, created bool, err error) {
	var b [8]byte
	if _, err = rand.Read(b[:]); err != nil {
		return req, false, fmt.Errorf("request id: %w", err)
	}
	req = ReactivationRequest{
		ID:            "react-" + hex.EncodeToString(b[:]),
		PeerID:        peerID,
		PeerName:      peerName,
		Handle:        handle,
		HoldEnteredAt: enteredAt.UTC().Unix(),
		RequestedAt:   time.Now().UTC().Unix(),
		Status:        ReactivationOpen,
	}
	res, err := s.db.NamedExecContext(ctx, `INSERT INTO probation_reactivation_requests
		(id, peer_id, peer_name, handle, hold_entered_at, requested_at, status)
		VALUES (:id, :peer_id, :peer_name, :handle, :hold_entered_at, :requested_at, :status)
		ON CONFLICT(peer_id, hold_entered_at) DO UPDATE SET
			peer_name=excluded.peer_name, handle=excluded.handle,
			requested_at=excluded.requested_at, status='open',
			decided_by='', decided_at=0, decision_reason=''
		WHERE probation_reactivation_requests.status='declined'`, req)
	if err != nil {
		return ReactivationRequest{}, false, fmt.Errorf("insert reactivation request: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return ReactivationRequest{}, false, fmt.Errorf("reactivation rows affected: %w", err)
	}
	created = n == 1
	// An upsert that reopens a declined row preserves its original primary key;
	// always reload so callers receive the durable id rather than the unused
	// random candidate generated for the INSERT arm.
	if err := s.db.GetContext(ctx, &req, `SELECT id, peer_id, peer_name, handle, hold_entered_at, requested_at, status,
		decided_by, decided_at, decision_reason
		FROM probation_reactivation_requests WHERE peer_id = $1 AND hold_entered_at = $2`, peerID, enteredAt.UTC().Unix()); err != nil {
		return ReactivationRequest{}, false, fmt.Errorf("load reactivation request: %w", err)
	}
	return req, created, nil
}

// HasOpenReactivationRequest reports whether the exact hold already has a
// request awaiting review.
func (s *Store) HasOpenReactivationRequest(ctx context.Context, peerID string, enteredAt time.Time) (bool, error) {
	var exists bool
	if err := s.db.GetContext(ctx, &exists, `SELECT EXISTS(
		SELECT 1 FROM probation_reactivation_requests
		WHERE peer_id = $1 AND hold_entered_at = $2 AND status = 'open'
	)`, peerID, enteredAt.UTC().Unix()); err != nil {
		return false, fmt.Errorf("check open reactivation request: %w", err)
	}
	return exists, nil
}

// ListOpenReactivationRequests returns the dedicated admin queue, oldest ask
// first. There is intentionally no approve/release method in this increment.
func (s *Store) ListOpenReactivationRequests(ctx context.Context) ([]ReactivationRequest, error) {
	var out []ReactivationRequest
	if err := s.db.SelectContext(ctx, &out, `SELECT id, peer_id, peer_name, handle, hold_entered_at, requested_at, status,
		decided_by, decided_at, decision_reason
		FROM probation_reactivation_requests WHERE status = $1 ORDER BY requested_at ASC, id ASC`, ReactivationOpen); err != nil {
		return nil, fmt.Errorf("list reactivation requests: %w", err)
	}
	return out, nil
}

// GetReactivationRequest loads a request by opaque id.
func (s *Store) GetReactivationRequest(ctx context.Context, id string) (ReactivationRequest, error) {
	var out ReactivationRequest
	if err := s.db.GetContext(ctx, &out, `SELECT id, peer_id, peer_name, handle, hold_entered_at, requested_at, status,
		decided_by, decided_at, decision_reason FROM probation_reactivation_requests WHERE id = $1`, id); err != nil {
		if errors.Is(err, sqlErrNoRows()) {
			return out, ErrNotFound
		}
		return out, fmt.Errorf("get reactivation request: %w", err)
	}
	return out, nil
}

// DeclineReactivationRequest changes only an open request row. It deliberately
// has no mesh dependency or side effect.
func (s *Store) DeclineReactivationRequest(ctx context.Context, id, actor, reason string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE probation_reactivation_requests
		SET status=$1, decided_by=$2, decided_at=$3, decision_reason=$4
		WHERE id=$5 AND status=$6`, ReactivationDeclined, actor, time.Now().UTC().Unix(), reason, id, ReactivationOpen)
	if err != nil {
		return false, fmt.Errorf("decline reactivation request: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("decline reactivation rows affected: %w", err)
	}
	return n == 1, nil
}

// CloseReactivationRequest closes the request for one exact hold. It is used
// only after the release is verified, or while reading the queue when the hold
// is provably gone.
func (s *Store) CloseReactivationRequest(ctx context.Context, peerID string, enteredAt time.Time, actor string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE probation_reactivation_requests
		SET status=$1, decided_by=$2, decided_at=$3
		WHERE peer_id=$4 AND hold_entered_at=$5 AND status=$6`, ReactivationClosed, actor,
		time.Now().UTC().Unix(), peerID, enteredAt.UTC().Unix(), ReactivationOpen)
	if err != nil {
		return false, fmt.Errorf("close reactivation request: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("close reactivation rows affected: %w", err)
	}
	return n == 1, nil
}
