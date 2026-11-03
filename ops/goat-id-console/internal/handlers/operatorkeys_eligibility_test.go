package handlers

// Who may ASK for an operator key.
//
// The queue is two-person, and the request door is limited too: if it were open
// to anyone with a console session with an optional reason field, the only thing
// standing between a developer and a credential that administers the whole
// goatnet would be the approving admin noticing.
//
// Eligibility is the matrix question adminGate already asks (ADR 1073), asked
// about the REQUESTER: a login that could not administer this console has no
// business holding the credential that administers every plane behind it.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// seedOnboardedPerson gives an email the onboarding record every real console
// user has, so the handle lookup behind /operator-key resolves.
func seedOnboardedPerson(t *testing.T, s *Server, handle, email string) {
	t.Helper()
	p := &onboardstore.Proposal{
		Sponsor: "seed@example.org", Env: "zagros", Handle: handle, Email: email,
		Status: onboardstore.ProposalOnboarded, ManifestJSON: "{}",
	}
	if err := s.Onboard.CreateProposal(context.Background(), p); err != nil {
		t.Fatalf("seed proposal for %s: %v", email, err)
	}
}

// A developer — signed in, onboarded, no admin group — is refused at the door,
// and nothing is recorded for an admin to approve later.
func TestOperatorKeyRequest_refusesNonAdmin(t *testing.T) {
	s := newOnboardTestServer(t)
	s.Cfg.DevBypassAuth = false
	s.csrfNonce = "nonce-123"
	h := s.Routes()

	w := browserForm(t, h, "dev@example.org", "DEVELOPER,OBSERVER", "/operator-key",
		url.Values{"csrf": {"nonce-123"}, "reason": {"logging in to the fabric portal, etc"}})

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", w.Code, w.Body.String())
	}
	// The refusal has to name the credential they actually want, or they come
	// back with the same request through an admin.
	if !strings.Contains(w.Body.String(), "vp_") {
		t.Errorf("refusal does not point at the vp_ key; body=%s", w.Body.String())
	}
	rows, err := s.Onboard.ListOperatorKeys(context.Background(), "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, r := range rows {
		if r.Email == "dev@example.org" {
			t.Fatalf("a refused request was still recorded: %+v", r)
		}
	}
}

// An admin with no reason is refused too. The reason is the only thing the
// deciding admin has to judge, and "" cannot be told from a request that should
// be declined.
func TestOperatorKeyRequest_requiresReason(t *testing.T) {
	s := newOnboardTestServer(t)
	s.Cfg.DevBypassAuth = false
	s.csrfNonce = "nonce-123"
	seedOnboardedPerson(t, s, "admin-x", "admin@example.org")
	h := s.Routes()

	w := browserForm(t, h, "admin@example.org", "IAM-ADMIN", "/operator-key",
		url.Values{"csrf": {"nonce-123"}, "reason": {"   "}})

	if w.Code == http.StatusForbidden {
		t.Fatalf("an eligible admin was refused outright: %s", w.Body.String())
	}
	rows, err := s.Onboard.ListOperatorKeys(context.Background(), "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, r := range rows {
		if r.Email == "admin@example.org" {
			t.Fatalf("a reasonless request was recorded: %+v", r)
		}
	}
}

// The same admin, with a reason, gets through — the gate refuses a class of
// request, not the feature.
func TestOperatorKeyRequest_adminWithReasonRecorded(t *testing.T) {
	s := newOnboardTestServer(t)
	s.Cfg.DevBypassAuth = false
	s.csrfNonce = "nonce-123"
	seedOnboardedPerson(t, s, "admin-x", "admin@example.org")
	h := s.Routes()

	w := browserForm(t, h, "admin@example.org", "IAM-ADMIN", "/operator-key",
		url.Values{"csrf": {"nonce-123"}, "reason": {"minting slots for the region C cohort"}})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	rows, err := s.Onboard.ListOperatorKeys(context.Background(), "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found bool
	for _, r := range rows {
		if r.Email == "admin@example.org" {
			found = true
			if r.Reason != "minting slots for the region C cohort" {
				t.Errorf("reason = %q, want it stored verbatim", r.Reason)
			}
		}
	}
	if !found {
		t.Fatalf("an eligible admin's request was not recorded; rows=%+v", rows)
	}
}

// The self-service page shows a developer no form at all. A form that will be
// refused on submit is an invitation to ask an admin to work around it.
func TestOperatorKeyPage_noFormForNonAdmin(t *testing.T) {
	s := newOnboardTestServer(t)
	s.Cfg.DevBypassAuth = false
	h := s.Routes()

	r := httptest.NewRequestWithContext(context.Background(), "GET", "/operator-key", nil)
	r.Header.Set("X-Forwarded-Email", "dev@example.org")
	r.Header.Set("X-Forwarded-Groups", "DEVELOPER")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "Request an operator key") {
		t.Errorf("request form rendered for a non-admin; body=%s", w.Body.String())
	}
}
