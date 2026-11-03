package onboardstore

import (
	"context"
	"testing"
	"time"
)

func TestHasOpenReactivationRequest(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	enteredAt := time.Date(2026, time.October, 10, 12, 0, 0, 0, time.UTC)

	if got, err := s.HasOpenReactivationRequest(ctx, "peer-1", enteredAt); err != nil || got {
		t.Fatalf("before request: got=%v err=%v, want false", got, err)
	}
	if _, _, err := s.RequestReactivation(ctx, "peer-1", "peer-one", "one", enteredAt); err != nil {
		t.Fatalf("request reactivation: %v", err)
	}
	if got, err := s.HasOpenReactivationRequest(ctx, "peer-1", enteredAt); err != nil || !got {
		t.Fatalf("open request: got=%v err=%v, want true", got, err)
	}
	if got, err := s.HasOpenReactivationRequest(ctx, "peer-1", enteredAt.Add(time.Second)); err != nil || got {
		t.Fatalf("different hold: got=%v err=%v, want false", got, err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE probation_reactivation_requests SET status = 'closed'`); err != nil {
		t.Fatalf("close request: %v", err)
	}
	if got, err := s.HasOpenReactivationRequest(ctx, "peer-1", enteredAt); err != nil || got {
		t.Fatalf("closed request: got=%v err=%v, want false", got, err)
	}
}

func TestDeclinedReactivationRequestCanReopenButClosedCannot(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	enteredAt := time.Date(2026, time.October, 10, 12, 0, 0, 0, time.UTC)
	req, created, err := s.RequestReactivation(ctx, "peer-1", "peer-one", "one", enteredAt)
	if err != nil || !created {
		t.Fatalf("initial request: created=%v err=%v", created, err)
	}
	if ok, err := s.DeclineReactivationRequest(ctx, req.ID, "admin@example.invalid", "not yet"); err != nil || !ok {
		t.Fatalf("decline: ok=%v err=%v", ok, err)
	}
	got, err := s.GetReactivationRequest(ctx, req.ID)
	if err != nil || got.Status != ReactivationDeclined || got.DecidedBy != "admin@example.invalid" || got.DecisionReason != "not yet" || got.DecidedAt == 0 {
		t.Fatalf("declined row = %+v err=%v", got, err)
	}
	reopened, created, err := s.RequestReactivation(ctx, "peer-1", "peer-renamed", "one", enteredAt)
	if err != nil || !created || reopened.ID != req.ID || reopened.Status != ReactivationOpen || reopened.DecidedBy != "" || reopened.DecidedAt != 0 || reopened.DecisionReason != "" {
		t.Fatalf("reopened row = %+v created=%v err=%v", reopened, created, err)
	}
	if ok, err := s.CloseReactivationRequest(ctx, "peer-1", enteredAt, "release-runner"); err != nil || !ok {
		t.Fatalf("close: ok=%v err=%v", ok, err)
	}
	closed, created, err := s.RequestReactivation(ctx, "peer-1", "peer-one", "one", enteredAt)
	if err != nil || created || closed.Status != ReactivationClosed {
		t.Fatalf("closed request reopened: row=%+v created=%v err=%v", closed, created, err)
	}
}
