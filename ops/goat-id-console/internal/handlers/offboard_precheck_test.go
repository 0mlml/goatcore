package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/kratos"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

type identityListRoundTripper struct {
	email string
	state string
}

func (rt identityListRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	w.Header().Set("Content-Type", "application/json")
	if req.Method == http.MethodGet && strings.HasPrefix(req.URL.Path, "/admin/identities") {
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id": "id-1", "state": rt.state,
			"traits": map[string]any{"email": rt.email},
		}})
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
	return w.Result(), nil
}

type unreachableRoundTripper struct{}

func (unreachableRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("login plane unreachable")
}

// fakeKratosIdentityList serves the admin identity list with one identity at
// the given email + state — the only read the offboard precheck makes.
func fakeKratosIdentityList(t *testing.T, email, state string) *kratos.Client {
	t.Helper()
	cli := kratos.NewClient("https://kratos.example.test")
	cli.HTTP = &http.Client{Transport: identityListRoundTripper{email: email, state: state}}
	return cli
}

func driveTo(t *testing.T, s *Server, pid, targetState string) {
	t.Helper()
	ctx := context.Background()
	for _, st := range []string{onboardstore.ProposalSponsorApproved, onboardstore.ProposalApproved} {
		if err := s.Onboard.DecideProposal(ctx, pid, st, "admin@x", ""); err != nil {
			t.Fatalf("decide %s: %v", st, err)
		}
	}
	if err := s.Onboard.ClaimProposal(ctx, pid); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := s.Onboard.SetProposalResult(ctx, pid, onboardstore.ProposalOnboarded, "", "", "", "", "", false); err != nil {
		t.Fatalf("onboarded: %v", err)
	}
	switch targetState {
	case onboardstore.ProposalEnrolled:
		if _, err := s.Onboard.MarkEnrolled(ctx, pid); err != nil {
			t.Fatalf("mark enrolled: %v", err)
		}
	case onboardstore.ProposalArchived:
		if _, err := s.Onboard.ArchiveProposal(ctx, pid, "dormant"); err != nil {
			t.Fatalf("archive: %v", err)
		}
	}
}

func onboardedProposalInState(t *testing.T, s *Server, h http.Handler, email, state string) string {
	t.Helper()
	tok := mintTokenViaStore(t, s)
	pid := submit(t, h, tok, map[string]any{"name": "Pat Liaison", "email": email, "organization": "Offboard Org"})
	driveTo(t, s, pid, state)
	return pid
}

func postBrowserOffboard(t *testing.T, h http.Handler, s *Server, pid string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{}
	form.Set("csrf", s.csrfNonce)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/onboarding/"+pid+"/offboard", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// precheckNote is the note the login-plane tests post for a row in `state`:
// an archived row must carry a reason before the precheck is reached at all
// (the reason check runs first, #1283), so these tests give it one; the
// reversible states post none, exactly as the button does.
func precheckNote(state string) string {
	if state == onboardstore.ProposalArchived {
		return "left the unit"
	}
	return ""
}

func statusOf(t *testing.T, s *Server, pid string) string {
	t.Helper()
	p, err := s.Onboard.GetProposal(context.Background(), pid)
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	return p.Status
}

// eligibleStates aliases the store's source of truth so these tests pin the
// EXPORTED set — a state added to (or dropped from) OffboardableStatuses
// changes what is exercised here without a second hand-kept copy drifting.
var eligibleStates = onboardstore.OffboardableStatuses

// The browser button asserts "access was already cut elsewhere". An ACTIVE
// login is positive evidence that it was not, and recording the mark would
// release the email-keyed duplicate guard while the person still holds
// credentials — so the assertion is refused and the row does not move.
func TestBrowserOffboardRefusedWhileLoginActive(t *testing.T) {
	for _, state := range eligibleStates {
		t.Run(state, func(t *testing.T) {
			s := newOnboardTestServer(t)
			h := s.Routes()
			email := "pat-" + state + "@partner.mil"
			pid := onboardedProposalInState(t, s, h, email, state)
			s.Kratos = map[string]*kratos.Client{"tenant": fakeKratosIdentityList(t, email, "active")}

			rec := postBrowserOffboardNote(t, h, s, pid, precheckNote(state))
			if loc := rec.Header().Get("Location"); !strings.Contains(loc, flashOffboardLive) {
				t.Fatalf("want a refusal flash %q, got %d %q", flashOffboardLive, rec.Code, loc)
			}
			if got := statusOf(t, s, pid); got != state {
				t.Fatalf("refused offboard must not move the row: got %q, want %q", got, state)
			}
		})
	}
}

// A real revoke leaves the identity inactive — then the assertion is true and
// the mark is recorded.
func TestBrowserOffboardRecordedWhenLoginInactive(t *testing.T) {
	for _, state := range eligibleStates {
		t.Run(state, func(t *testing.T) {
			s := newOnboardTestServer(t)
			h := s.Routes()
			email := "pat-" + state + "@partner.mil"
			pid := onboardedProposalInState(t, s, h, email, state)
			s.Kratos = map[string]*kratos.Client{"tenant": fakeKratosIdentityList(t, email, "inactive")}

			note := "" // archived → offboarded is one-way and refuses a blank note
			if state == onboardstore.ProposalArchived {
				note = "access cut elsewhere"
			}
			postBrowserOffboardNote(t, h, s, pid, note)
			if got := statusOf(t, s, pid); got != onboardstore.ProposalOffboarded {
				t.Fatalf("want offboarded after a verified cut, got %q", got)
			}
		})
	}
}

// An unreachable login plane means the claim cannot be verified. Fail closed:
// nothing is recorded, because the mark is what releases the duplicate guard.
func TestBrowserOffboardFailsClosedWhenLoginPlaneUnreachable(t *testing.T) {
	for _, state := range eligibleStates {
		t.Run(state, func(t *testing.T) {
			s := newOnboardTestServer(t)
			h := s.Routes()
			email := "pat-" + state + "@partner.mil"
			pid := onboardedProposalInState(t, s, h, email, state)
			s.Kratos = map[string]*kratos.Client{"tenant": {
				AdminURL: "https://kratos-unreachable.example.test",
				HTTP:     &http.Client{Transport: unreachableRoundTripper{}},
			}}

			rec := postBrowserOffboardNote(t, h, s, pid, precheckNote(state))
			if loc := rec.Header().Get("Location"); !strings.Contains(loc, flashOffboardUnverified) {
				t.Fatalf("want %q flash, got %d %q", flashOffboardUnverified, rec.Code, loc)
			}
			if got := statusOf(t, s, pid); got != state {
				t.Fatalf("unverifiable offboard must not move the row: got %q, want %q", got, state)
			}
		})
	}
}

// The MACHINE path is `goat admin onboard --revoke`'s R0 leg: it marks the row
// FIRST and performs the cut immediately after, in the same run, so the login
// is still active by design when it calls. Gating it would break the on-rails
// revoke — this pins that it stays ungated.
func TestMachineOffboardNotGatedByActiveLogin(t *testing.T) {
	for _, state := range eligibleStates {
		t.Run(state, func(t *testing.T) {
			s := newOnboardTestServer(t)
			h := s.Routes()
			email := "pat-" + state + "@partner.mil"
			pid := onboardedProposalInState(t, s, h, email, state)
			s.Kratos = map[string]*kratos.Client{"tenant": fakeKratosIdentityList(t, email, "active")}

			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/onboard/proposals/"+pid+"/offboard", strings.NewReader(`{"note":"cli revoke R0"}`))
			req.Header.Set("Authorization", "Bearer runner-key")
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("machine offboard must stay ungated: got %d %s", rec.Code, rec.Body.String())
			}
			if got := statusOf(t, s, pid); got != onboardstore.ProposalOffboarded {
				t.Fatalf("want offboarded via the machine rail, got %q", got)
			}
		})
	}
}

// A console with no login-plane client cannot verify the claim at all. That is
// a degraded deployment, not a goatnet without identities — refuse rather than
// record an unverifiable offboard.
func TestBrowserOffboardRefusedWhenNoLoginPlaneConfigured(t *testing.T) {
	for _, state := range eligibleStates {
		t.Run(state, func(t *testing.T) {
			s := newOnboardTestServer(t)
			h := s.Routes()
			email := "pat-" + state + "@partner.mil"
			pid := onboardedProposalInState(t, s, h, email, state)
			s.Kratos = nil

			rec := postBrowserOffboardNote(t, h, s, pid, precheckNote(state))
			if loc := rec.Header().Get("Location"); !strings.Contains(loc, flashOffboardUnverified) {
				t.Fatalf("want %q flash, got %d %q", flashOffboardUnverified, rec.Code, loc)
			}
			if got := statusOf(t, s, pid); got != state {
				t.Fatalf("row must not move: got %q, want %q", got, state)
			}
		})
	}
}
