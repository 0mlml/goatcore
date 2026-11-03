package onboardstore

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestOffboardCancelsQueuedReissues(t *testing.T) {
	for _, kind := range []string{ActionReissueDevice, ActionReissueHandset} {
		for _, recorded := range []bool{false, true} {
			t.Run(kind+map[bool]string{false: "/request", true: "/record"}[recorded], func(t *testing.T) {
				s := openTest(t)
				ctx := context.Background()
				p := onboardedProposal(t, s)
				a, err := s.EnqueueAction(ctx, p.ID, kind, "fixture")
				if err != nil {
					t.Fatal(err)
				}
				if recorded {
					_, err = s.MarkOffboardedFrom(ctx, p.ID, "fixture", "")
				} else {
					_, err = s.RequestOffboard(ctx, p.ID, "fixture")
				}
				if err != nil {
					t.Fatal(err)
				}
				got, err := s.GetAction(ctx, a.ID)
				if err != nil {
					t.Fatal(err)
				}
				if got.Status != ActionFailed || got.Error == "" || got.FinishedAt == nil {
					t.Fatalf("not canceled: %+v", got)
				}
				if err := s.ClaimAction(ctx, a.ID, "runner"); !errors.Is(err, ErrActionNotQueued) {
					t.Fatalf("claim=%v", err)
				}
			})
		}
	}
}

func TestOffboardWaitsForRunningReissueAndRejectsLateCredential(t *testing.T) {
	for _, kind := range []string{ActionReissueDevice, ActionReissueHandset} {
		t.Run(kind, func(t *testing.T) {
			s := openTest(t)
			ctx := context.Background()
			p := onboardedProposal(t, s)
			a, err := s.EnqueueAction(ctx, p.ID, kind, "fixture")
			if err != nil {
				t.Fatal(err)
			}
			if err = s.ClaimAction(ctx, a.ID, "runner"); err != nil {
				t.Fatal(err)
			}
			if ok, err := s.MarkOffboardedFrom(ctx, p.ID, "fixture", ""); err != nil || ok {
				t.Fatalf("record crossed running mint: %v %v", ok, err)
			}
			if ok, err := s.RequestOffboard(ctx, p.ID, "fixture"); err != nil || !ok {
				t.Fatalf("request: %v %v", ok, err)
			}
			if err = s.ClaimOffboard(ctx, p.ID); !errors.Is(err, ErrReissueRunning) {
				t.Fatalf("cut crossed running mint: %v", err)
			}
			if _, err = s.FinishReissue(ctx, a.ID, ActionDone, `{}`, "", "SYNTHETIC-DEVICE", "SYNTHETIC-HANDSET"); !errors.Is(err, ErrActionNotOnboarded) {
				t.Fatalf("late success: %v", err)
			}
			got, err := s.GetProposal(ctx, p.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.SetupKey != "" || got.HandsetKey != "" || got.Status != ProposalOffboardRequested {
				t.Fatalf("late credential stored or parent changed")
			}
			if err = s.ClaimOffboard(ctx, p.ID); err != nil {
				t.Fatalf("finished mint still blocks cut: %v", err)
			}
			if _, err = s.FinishReissue(ctx, a.ID, ActionDone, `{}`, "", "LATER", ""); !errors.Is(err, ErrActionNotClaimed) {
				t.Fatalf("duplicate result: %v", err)
			}
		})
	}
}

func TestReissueEligibleResultCommitsBothRows(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	p := onboardedProposal(t, s)
	a, err := s.EnqueueAction(ctx, p.ID, ActionReissueDevice, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ClaimAction(ctx, a.ID, "runner"); err != nil {
		t.Fatal(err)
	}
	got, err := s.FinishReissue(ctx, a.ID, ActionDone, `{"setup_key_set":true}`, "", "SYNTHETIC", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.SetupKey != "SYNTHETIC" || got.Status != ProposalOnboarded {
		t.Fatal("eligible result missing")
	}
	stored, _ := s.GetAction(ctx, a.ID)
	if stored.Status != ActionDone {
		t.Fatal("action not terminal")
	}
}

func TestClaimAndOffboardRace(t *testing.T) {
	for i := 0; i < 20; i++ {
		s := openTest(t)
		ctx := context.Background()
		p := onboardedProposal(t, s)
		a, err := s.EnqueueAction(ctx, p.ID, ActionReissueHandset, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		var claimErr, offErr error
		go func() { defer wg.Done(); <-start; claimErr = s.ClaimAction(ctx, a.ID, "runner") }()
		go func() { defer wg.Done(); <-start; _, offErr = s.RequestOffboard(ctx, p.ID, "fixture") }()
		close(start)
		wg.Wait()
		if offErr != nil {
			t.Fatal(offErr)
		}
		if claimErr == nil {
			if err := s.ClaimOffboard(ctx, p.ID); err == nil {
				t.Fatal("offboard overtook claimed action")
			}
			if _, err := s.FinishReissue(ctx, a.ID, ActionDone, "{}", "", "", "LATE"); !errors.Is(err, ErrActionNotOnboarded) {
				t.Fatal(err)
			}
		} else if !errors.Is(claimErr, ErrActionNotQueued) {
			t.Fatal(claimErr)
		}
		if err := s.ClaimOffboard(ctx, p.ID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDeliveryReservationBlocksDuplicatesAndOffboard(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	p := onboardedProposal(t, s)
	a, err := s.EnqueueAction(ctx, p.ID, ActionReissueDevice, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimAction(ctx, a.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginReissueDelivery(ctx, a.ID, "other", ActionDone, "{}", "", "NEW", ""); !errors.Is(err, ErrActionNotClaimed) {
		t.Fatalf("foreign owner: %v", err)
	}
	if _, err := s.BeginReissueDelivery(ctx, a.ID, "owner", ActionDone, "{}", "", "NEW", ""); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{ActionDone, ActionFailed} {
		if _, err := s.BeginReissueDelivery(ctx, a.ID, "owner", status, "{}", "", "NEW", ""); !errors.Is(err, ErrActionNotClaimed) {
			t.Fatalf("duplicate %s: %v", status, err)
		}
	}
	if _, err := s.EnqueueAction(ctx, p.ID, ActionReissueDevice, "fixture"); !errors.Is(err, ErrActionPending) {
		t.Fatalf("enqueue during delivery: %v", err)
	}
	if ok, err := s.RequestOffboard(ctx, p.ID, "fixture"); err != nil || !ok {
		t.Fatalf("request: %v %v", ok, err)
	}
	if err := s.ClaimOffboard(ctx, p.ID); !errors.Is(err, ErrReissueRunning) {
		t.Fatalf("cut during delivery: %v", err)
	}
	if err := s.RecordActionDelivery(ctx, a.ID, `{"welcome_resend":"ok"}`); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimOffboard(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
}

func TestReissueCompletionRacesOffboardRequest(t *testing.T) {
	for i := 0; i < 20; i++ {
		s := openTest(t)
		ctx := context.Background()
		p := onboardedProposal(t, s)
		a, err := s.EnqueueAction(ctx, p.ID, ActionReissueDevice, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ClaimAction(ctx, a.ID, "owner"); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		result := make(chan error, 1)
		request := make(chan error, 1)
		go func() {
			<-start
			_, err := s.BeginReissueDelivery(ctx, a.ID, "owner", ActionDone, "{}", "", "NEW", "")
			result <- err
		}()
		go func() {
			<-start
			ok, err := s.RequestOffboard(ctx, p.ID, "fixture")
			if err == nil && !ok {
				err = errors.New("request lost")
			}
			request <- err
		}()
		close(start)
		if err := <-request; err != nil {
			t.Fatal(err)
		}
		err = <-result
		if err != nil && !errors.Is(err, ErrActionNotOnboarded) {
			t.Fatal(err)
		}
		pp, loadErr := s.GetProposal(ctx, p.ID)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if pp.Status != ProposalOffboardRequested {
			t.Fatalf("completion reopened parent: %s", pp.Status)
		}
		if errors.Is(err, ErrActionNotOnboarded) {
			if pp.SetupKey != "" {
				t.Fatal("refused key persisted")
			}
		} else {
			if pp.SetupKey != "NEW" {
				t.Fatal("accepted key missing")
			}
			if err := s.ClaimOffboard(ctx, p.ID); !errors.Is(err, ErrReissueRunning) {
				t.Fatalf("delivery barrier: %v", err)
			}
			if err := s.RecordActionDelivery(ctx, a.ID, `{}`); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.ClaimOffboard(ctx, p.ID); err != nil {
			t.Fatal(err)
		}
	}
}
