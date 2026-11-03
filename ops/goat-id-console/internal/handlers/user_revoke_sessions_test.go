package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/kratos"
)

type revokeSessionsRoundTripper struct {
	getStatus   int
	revokeCalls int
}

func (rt *revokeSessionsRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case req.Method == http.MethodGet && strings.HasPrefix(req.URL.Path, "/admin/identities/"):
		// Target fetch for the revoke path.
		w.WriteHeader(rt.getStatus)
	case req.Method == http.MethodDelete && strings.HasSuffix(req.URL.Path, "/sessions"):
		// RevokeIdentitySessions.
		rt.revokeCalls++
		w.WriteHeader(http.StatusNoContent)
	case req.Method == http.MethodGet && strings.HasPrefix(req.URL.Path, "/admin/sessions"):
		// Step-up posture read — return an empty list so any incidental
		// callers see "no sessions".
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
	return w.Result(), nil
}

// fakeKratosRevokeSessionsTargetError serves the minimal Kratos admin
// surface the revoke-sessions handler calls: a GET on the identity and a
// DELETE on its sessions. getStatus controls the identity GET response; the
// DELETE path increments a counter so tests can assert that a failed target
// fetch never proceeds to revoke sessions.
func fakeKratosRevokeSessionsTargetError(t *testing.T, getStatus int) (*kratos.Client, func() int) {
	t.Helper()
	rt := &revokeSessionsRoundTripper{getStatus: getStatus}
	cli := kratos.NewClient("https://kratos.example.test")
	cli.HTTP = &http.Client{Transport: rt}
	return cli, func() int { return rt.revokeCalls }
}

// postRevokeSessions drives POST /users/{id}/revoke-sessions with the
// supplied CSRF token. When withCSRF is false, the handler should 400.
func postRevokeSessions(t *testing.T, h http.Handler, csrf string, withCSRF bool) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{}
	if withCSRF {
		form.Set("csrf", csrf)
	}
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/users/id-1/revoke-sessions?idp=tenant", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Forwarded-Groups", "PLATFORM-ADMIN")
	r.Header.Set("X-Forwarded-Email", "admin@example.org")
	r.Header.Set("X-Forwarded-Preferred-Username", "actor-1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// A nonexistent identity is a 404, and the handler must not proceed to
// revoke sessions — the step-up gate (and the mutation) are never reached.
func TestUserRevokeSessions_TargetNotFound404(t *testing.T) {
	kc, revokeCalls := fakeKratosRevokeSessionsTargetError(t, http.StatusNotFound)
	s := newUsersAPITestServer(t, kc)
	w := postRevokeSessions(t, s.Routes(), s.csrfNonce, true)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if got := revokeCalls(); got != 0 {
		t.Fatalf("revoke sessions must not be called on unknown identity; got %d calls", got)
	}
}

// A backend error on the target fetch is a 502, and the handler must not
// proceed to revoke sessions so the step-up gate cannot be skipped.
func TestUserRevokeSessions_TargetFetchFails502(t *testing.T) {
	kc, revokeCalls := fakeKratosRevokeSessionsTargetError(t, http.StatusInternalServerError)
	s := newUsersAPITestServer(t, kc)
	w := postRevokeSessions(t, s.Routes(), s.csrfNonce, true)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
	}
	if got := revokeCalls(); got != 0 {
		t.Fatalf("revoke sessions must not be called when the target fetch fails; got %d calls", got)
	}
}
