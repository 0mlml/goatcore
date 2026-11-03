package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/kratos"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

func TestBrowserOffboard_EligibleStates(t *testing.T) {
	cases := []struct {
		name  string
		prior string
		note  string // archived → offboarded is one-way and refuses a blank note
		setup func(t *testing.T, s *Server, pid string)
	}{
		{
			name:  "enrolled to offboarded",
			prior: onboardstore.ProposalEnrolled,
			setup: func(t *testing.T, s *Server, pid string) {
				if _, err := s.Onboard.MarkEnrolled(context.Background(), pid); err != nil {
					t.Fatalf("mark enrolled: %v", err)
				}
			},
		},
		{
			name:  "archived to offboarded",
			prior: onboardstore.ProposalArchived,
			note:  "left the unit",
			setup: func(t *testing.T, s *Server, pid string) {
				if _, err := s.Onboard.ArchiveProposal(context.Background(), pid, "dormant"); err != nil {
					t.Fatalf("archive: %v", err)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newOnboardTestServer(t)
			h := s.Routes()
			email := "pat-" + strings.ReplaceAll(tc.name, " ", "-") + "@partner.mil"
			pid := onboardedProposalInState(t, s, h, email, onboardstore.ProposalOnboarded)

			// Move the proposal into the eligible state.
			tc.setup(t, s, pid)

			// Fake Kratos reporting inactive so the browser precheck passes.
			s.Kratos = map[string]*kratos.Client{"tenant": fakeKratosIdentityList(t, email, "inactive")}

			var rec *httptest.ResponseRecorder
			if tc.note != "" {
				rec = postBrowserOffboardNote(t, h, s, pid, tc.note)
			} else {
				rec = postBrowserOffboard(t, h, s, pid)
			}
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("want 303 Redirect, got %d %s", rec.Code, rec.Body.String())
			}

			if got := statusOf(t, s, pid); got != onboardstore.ProposalOffboarded {
				t.Fatalf("want offboarded, got %q", got)
			}

			// Audit row written: the console must emit the
			// "onboard_offboard" action with the final status AND the
			// state the row was offboarded FROM — with three source
			// states, "from enrolled credential-holder" vs "from
			// never-joined archived" is the distinction audit keeps.
			found := false
			for _, e := range s.Audit.Recent(50) {
				if e.Action == "onboard_offboard" {
					if status, ok := e.After["status"]; ok && status == onboardstore.ProposalOffboarded {
						if prior, ok := e.Before["status"]; !ok || prior != tc.prior {
							t.Fatalf("audit Before status = %v, want %q", prior, tc.prior)
						}
						found = true
						break
					}
				}
			}
			if !found {
				t.Fatalf("audit row for onboard_offboard with offboarded status not found")
			}

			// Duplicate-email guard: after offboard, this email must no longer
			// be reported as active, so a fresh submission would pass the guard.
			if _, ok, err := s.Onboard.ActiveProposalForEmail(context.Background(), email); err != nil {
				t.Fatalf("ActiveProposalForEmail: %v", err)
			} else if ok {
				t.Fatalf("offboarded proposal must not be active for %q", email)
			}
		})
	}
}

func TestBrowserOffboard_IneligibleStatesRefused(t *testing.T) {
	s := newOnboardTestServer(t)
	h := s.Routes()

	cases := []struct {
		name   string
		status string
	}{
		{"proposed", onboardstore.ProposalProposed},
		{"sponsor_approved", onboardstore.ProposalSponsorApproved},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tok := mintTokenViaStore(t, s)
			email := "pat-" + tc.name + "@partner.mil"
			pid := submit(t, h, tok, map[string]any{"name": "Pat Start", "email": email, "organization": "Offboard Org"})

			if tc.status == onboardstore.ProposalSponsorApproved {
				if err := s.Onboard.DecideProposal(context.Background(), pid, tc.status, "sponsor@x", ""); err != nil {
					t.Fatalf("decide %s: %v", tc.status, err)
				}
			}

			// Fake Kratos reporting inactive so it does not get rejected by
			// the login-plane precheck first; this test exercises the state guard.
			s.Kratos = map[string]*kratos.Client{"tenant": fakeKratosIdentityList(t, email, "inactive")}

			rec := postBrowserOffboard(t, h, s, pid)
			loc := rec.Header().Get("Location")
			if rec.Code != http.StatusSeeOther || !strings.Contains(loc, "flash=offboard_state") {
				t.Fatalf("want 303 Redirect with offboard_state flash, got %d %q", rec.Code, loc)
			}

			if got := statusOf(t, s, pid); got != tc.status {
				t.Fatalf("refused offboard must not move row, want %q got %q", tc.status, got)
			}
		})
	}
}
