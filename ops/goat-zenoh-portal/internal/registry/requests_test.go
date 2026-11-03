package registry

import (
	"context"
	"errors"
	"testing"
)

func TestRequestCreateAndDefaults(t *testing.T) {
	reg := testRegistry(t)
	ctx := context.Background()
	req := &DataAccessRequest{RequesterID: "uuid-alice", RequesterLabel: "alice@example.com", Producer: "acme-sensor", Justification: "need it"}
	if err := reg.CreateRequest(ctx, req); err != nil {
		t.Fatalf("create request: %v", err)
	}
	if req.ID == "" || req.Status != RequestPending {
		t.Fatalf("defaults wrong: %+v", req)
	}
	if req.GranteeKind != GranteePrincipal || req.GranteeID != "uuid-alice" {
		t.Fatalf("self-grantee defaults wrong: %+v", req)
	}
}

func TestRequestDecideSingleShot(t *testing.T) {
	reg := testRegistry(t)
	ctx := context.Background()
	req := &DataAccessRequest{RequesterID: "uuid-bob", Producer: "acme-sensor"}
	if err := reg.CreateRequest(ctx, req); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := reg.DecideRequest(ctx, req.ID, RequestApproved, "operator", "grant-abc", "ok"); err != nil {
		t.Fatalf("first decide: %v", err)
	}
	got, _ := reg.GetRequest(ctx, req.ID)
	if got.Status != RequestApproved || got.GrantID == nil || *got.GrantID != "grant-abc" {
		t.Fatalf("approval not recorded: %+v", got)
	}
	// Second decision on a non-pending request fails closed (no double-mint).
	if err := reg.DecideRequest(ctx, req.ID, RequestDeclined, "operator", "", ""); !errors.Is(err, ErrRequestNotPending) {
		t.Fatalf("expected ErrRequestNotPending, got %v", err)
	}
	// Unknown id.
	if err := reg.DecideRequest(ctx, "dar-nope", RequestApproved, "op", "g", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListRequestsFilters(t *testing.T) {
	reg := testRegistry(t)
	ctx := context.Background()
	_ = reg.CreateRequest(ctx, &DataAccessRequest{RequesterID: "a", Producer: "p1"})
	_ = reg.CreateRequest(ctx, &DataAccessRequest{RequesterID: "b", Producer: "p2"})
	p1, _ := reg.ListRequests(ctx, "p1", "")
	if len(p1) != 1 {
		t.Fatalf("expected 1 request for p1, got %d", len(p1))
	}
	pending, _ := reg.ListRequests(ctx, "", RequestPending)
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(pending))
	}
}
