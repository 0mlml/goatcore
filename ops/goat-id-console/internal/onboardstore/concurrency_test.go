package onboardstore

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

// On SQLite one open connection serialized every read-then-write path. On
// Postgres the pool runs them in parallel, so each path now holds its own lock
// (ADR 1165 step A). These tests race N callers at each path and require the
// outcome one connection gave: exactly one winner.

const racers = 12

func race(t *testing.T, f func() error) (wins int, errs []error) {
	t.Helper()
	var mu sync.Mutex
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := f()
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				wins++
			} else {
				errs = append(errs, err)
			}
		}()
	}
	close(start)
	wg.Wait()
	return wins, errs
}

func TestConcurrentEnqueueActionQueuesOne(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	p := &Proposal{Sponsor: "s", Env: "zagros", Handle: "pat", Email: "pat@example.test", Status: ProposalOnboarded, ManifestJSON: "{}"}
	if err := s.CreateProposal(ctx, p); err != nil {
		t.Fatal(err)
	}
	wins, errs := race(t, func() error {
		_, err := s.EnqueueAction(ctx, p.ID, ActionReissueHandset, "ops@example.test")
		return err
	})
	if wins != 1 {
		t.Fatalf("%d enqueues succeeded, want 1 (errors: %v)", wins, errs)
	}
	for _, err := range errs {
		if !errors.Is(err, ErrActionPending) {
			t.Fatalf("loser got %v, want ErrActionPending", err)
		}
	}
}

func TestConcurrentEnqueueHoldActionQueuesOne(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	entered := time.Date(2027, 1, 15, 8, 0, 0, 0, time.UTC)
	params, err := json.Marshal(map[string]string{"peer_id": "peer-1", "hold_entered_at": entered.Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	wins, errs := race(t, func() error {
		_, err := s.EnqueueHoldAction(ctx, "zagros", "peer-1", entered, "pat", "ops@example.test", string(params))
		return err
	})
	if wins != 1 {
		t.Fatalf("%d hold enqueues succeeded, want 1 (errors: %v)", wins, errs)
	}
	for _, err := range errs {
		if !errors.Is(err, ErrActionPending) {
			t.Fatalf("loser got %v, want ErrActionPending", err)
		}
	}
}

func TestConcurrentClaimActionHasOneWinner(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	p := &Proposal{Sponsor: "s", Env: "zagros", Handle: "pat", Email: "pat@example.test", Status: ProposalOnboarded, ManifestJSON: "{}"}
	if err := s.CreateProposal(ctx, p); err != nil {
		t.Fatal(err)
	}
	a, err := s.EnqueueAction(ctx, p.ID, ActionReissueDevice, "ops@example.test")
	if err != nil {
		t.Fatal(err)
	}
	wins, errs := race(t, func() error { return s.ClaimAction(ctx, a.ID, "runner") })
	if wins != 1 {
		t.Fatalf("%d claims succeeded, want 1 (errors: %v)", wins, errs)
	}
	for _, err := range errs {
		if !errors.Is(err, ErrActionNotQueued) {
			t.Fatalf("loser got %v, want ErrActionNotQueued", err)
		}
	}
}

func TestConcurrentConsumeInviteMintsOneProposal(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	inv := &Invite{Sponsor: "jane-admin", Env: "zagros", ExpiresAt: time.Now().Add(time.Hour).Unix()}
	tok, err := s.MintInvite(ctx, inv)
	if err != nil {
		t.Fatal(err)
	}
	wins, errs := race(t, func() error {
		_, err := s.ConsumeInvite(ctx, tok, func(_ *Invite, _ string) (*Proposal, error) {
			return &Proposal{Handle: "pat", Email: "pat@example.test", ManifestJSON: "{}"}, nil
		})
		return err
	})
	if wins != 1 {
		t.Fatalf("%d submits succeeded, want 1 (errors: %v)", wins, errs)
	}
	for _, err := range errs {
		if !errors.Is(err, ErrInviteConsumed) {
			t.Fatalf("loser got %v, want ErrInviteConsumed", err)
		}
	}
	ps, err := s.ListProposals(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 {
		t.Fatalf("%d proposals written, want 1", len(ps))
	}
}

func TestConcurrentCollectOperatorKeyDeliversOnce(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	k, err := s.RequestOperatorKey(ctx, &OperatorKey{Handle: "pat", Email: "pat@example.test", Env: "zagros", Reason: "portal admin"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApproveOperatorKey(ctx, k.ID, "boss@example.test", nil); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var secrets []string
	wins, errs := race(t, func() error {
		sec, err := s.CollectOperatorKey(ctx, k.ID, "pat")
		if err == nil {
			mu.Lock()
			secrets = append(secrets, sec)
			mu.Unlock()
		}
		return err
	})
	if wins != 1 || len(secrets) != 1 || secrets[0] == "" {
		t.Fatalf("%d collections succeeded, want exactly 1 with the secret (errors: %v)", wins, errs)
	}
	for _, err := range errs {
		if !errors.Is(err, ErrOperatorKeyAlreadyDelivered) {
			t.Fatalf("loser got %v, want ErrOperatorKeyAlreadyDelivered", err)
		}
	}
}
