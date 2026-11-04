package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

func inviteRecipientRequest(t *testing.T, s *Server, door, email, id string) *httptest.ResponseRecorder {
	t.Helper()
	if door == "api" {
		return machineReq(t, s.Routes(), "runner-key", http.MethodPost, "/api/onboard/invite",
			`{"env":"zagros","email":"`+email+`"}`)
	}
	form := url.Values{"csrf": {s.csrfNonce}, "env": {"zagros"}, "candidate_email": {email},
		"invite_id": {id}, "intake_url": {"https://intake.example.test/onboard.html?t=synthetic"}}
	path := "/onboarding/invite"
	if door != "mint" {
		path += "/email/" + door
	}
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)
	return w
}

func TestInviteRecipientRefusesExistingProposal(t *testing.T) {
	for _, status := range []string{onboardstore.ProposalOnboarded, onboardstore.ProposalArchived, onboardstore.ProposalProposed} {
		for _, door := range []string{"mint", "api", "send", "eml"} {
			t.Run(status+"/"+door, func(t *testing.T) {
				s := newOnboardTestServer(t)
				messages := captureMail(t, s)
				// The invite predates the proposal, exercising the delivery-time
				// recheck rather than relying only on the mint-time check.
				id := seedInvite(t, s, "admin@example.test", "", time.Now().Add(time.Hour).Unix(), "")
				if err := s.Onboard.AddressInvite(context.Background(), id, "existing@example.org"); err != nil {
					t.Fatal(err)
				}
				seedProposal(t, s, "existing", "admin@example.test", "browse", status)
				w := inviteRecipientRequest(t, s, door, "EXISTING@EXAMPLE.ORG", id)
				if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "existing") || !strings.Contains(w.Body.String(), status) {
					t.Fatalf("got %d %s; want conflict naming existing proposal and stage", w.Code, w.Body.String())
				}
				if len(messages) != 0 {
					t.Fatal("conflicting recipient received mail")
				}
				rows := listInvites(t, s.Routes())
				if len(rows) != 1 || rows[0].SentAt != nil || rows[0].SentVia != "" {
					t.Fatalf("refusal changed invite/delivery state: %+v", rows)
				}
				for _, ev := range noticeEvents(s, "onboard_email_invite") {
					if ev.Result == "ok" {
						t.Fatal("refusal recorded successful delivery")
					}
				}
			})
		}
	}
}

func TestInviteRecipientChangedAddressIsRechecked(t *testing.T) {
	for _, door := range []string{"send", "eml"} {
		t.Run(door, func(t *testing.T) {
			s := newOnboardTestServer(t)
			messages := captureMail(t, s)
			id := seedInvite(t, s, "admin@example.test", "", time.Now().Add(time.Hour).Unix(), "")
			if err := s.Onboard.AddressInvite(context.Background(), id, "fresh@example.test"); err != nil {
				t.Fatal(err)
			}
			seedProposal(t, s, "existing", "admin@example.test", "browse", onboardstore.ProposalOnboarded)
			w := inviteRecipientRequest(t, s, door, "existing@example.org", id)
			stored, err := s.Onboard.GetInvite(context.Background(), id)
			if err != nil || w.Code != http.StatusConflict || len(messages) != 0 || stored.CandidateEmail != "fresh@example.test" || stored.SentAt != nil {
				t.Fatalf("changed recipient bypassed refusal: status=%d stored=%+v err=%v", w.Code, stored, err)
			}
		})
	}
}

func TestInviteRecipientFreshAndTerminalControls(t *testing.T) {
	for _, status := range []string{"", onboardstore.ProposalRejected, onboardstore.ProposalFailed, onboardstore.ProposalOffboarded} {
		for _, door := range []string{"mint", "api", "send", "eml"} {
			t.Run(status+"/"+door, func(t *testing.T) {
				s := newOnboardTestServer(t)
				messages := captureMail(t, s)
				id := seedInvite(t, s, "admin@example.test", "", time.Now().Add(time.Hour).Unix(), "")
				if status != "" {
					seedProposal(t, s, "existing", "admin@example.test", "browse", status)
				}
				w := inviteRecipientRequest(t, s, door, "existing@example.org", id)
				want := http.StatusOK
				if door == "api" {
					want = http.StatusCreated
				}
				if w.Code != want {
					t.Fatalf("got %d %s; want %d", w.Code, w.Body.String(), want)
				}
				if door == "eml" {
					if !strings.Contains(w.Body.String(), "one-time link") || len(messages) != 0 {
						t.Fatal("email export must contain invite without sending it")
					}
				} else if len(messages) != 1 {
					t.Fatalf("want one delivered invite, got %d", len(messages))
				}
			})
		}
	}
}

func TestInviteRecipientLookupFailureStopsOperation(t *testing.T) {
	for _, door := range []string{"mint", "api", "send", "eml"} {
		t.Run(door, func(t *testing.T) {
			s := newOnboardTestServer(t)
			messages := captureMail(t, s)
			if err := s.Onboard.Close(); err != nil {
				t.Fatal(err)
			}
			w := inviteRecipientRequest(t, s, door, "candidate@example.test", "synthetic")
			if w.Code != http.StatusServiceUnavailable || len(messages) != 0 {
				t.Fatalf("got %d %s and %d messages; want unavailable without delivery", w.Code, w.Body.String(), len(messages))
			}
		})
	}
}

func TestInviteRecipientAddressFreeMint(t *testing.T) {
	for _, door := range []string{"mint", "api"} {
		t.Run(door, func(t *testing.T) {
			s := newOnboardTestServer(t)
			messages := captureMail(t, s)
			w := inviteRecipientRequest(t, s, door, "", "")
			if w.Code >= 300 || len(messages) != 0 || len(listInvites(t, s.Routes())) != 1 {
				t.Fatalf("address-free mint failed: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
