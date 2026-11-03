package handlers

// /docs/onboarding is an ungated redirect to the guide. A second, hand-maintained
// copy of the guide's admin material drifts, and the welcome mail sends every
// newly-onboarded person there as "start here", so a gate would answer an
// ordinary developer, observer or partner with a 403.
//
// These tests hold both halves: the gate is gone, and the target is the guide
// rather than the retired page.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// getDocsOnboarding issues an unauthenticated GET through Routes() — no
// forwarded email, no groups, which is exactly the caller the old gate refused.
func getDocsOnboarding(t *testing.T, s *Server) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/docs/onboarding", nil)
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)
	return w
}

// The gate is the regression that mattered: a reader with no admin groups and
// no sponsorship must not be refused.
func TestDocsOnboarding_RedirectsAndIsNotGated(t *testing.T) {
	s := newOnboardTestServer(t)
	s.Cfg.DevBypassAuth = false

	w := getDocsOnboarding(t, s)

	if w.Code == http.StatusForbidden || w.Code == http.StatusBadGateway {
		t.Fatalf("GET /docs/onboarding = %d for an ungrouped reader — the sponsorship gate is back; %s",
			w.Code, w.Body.String())
	}
	if w.Code != http.StatusFound {
		t.Fatalf("GET /docs/onboarding = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "/public/onboarding-guide") {
		t.Errorf("redirect target %q does not reach the guide", loc)
	}
	if !strings.Contains(loc, "topic=identity-admin") {
		t.Errorf("redirect target %q does not land on the admin topic", loc)
	}
}

// With a guide host configured the redirect must leave the console entirely —
// that host is the unauthenticated one, which is the whole point.
func TestDocsOnboarding_PrefersTheGuideHost(t *testing.T) {
	s := newOnboardTestServer(t)
	s.Cfg.DevBypassAuth = false
	s.Cfg.NotifyPortalURL = "https://portal.zagros.netbird.zagros.example"

	loc := getDocsOnboarding(t, s).Header().Get("Location")

	if !strings.HasPrefix(loc, "https://guide.") {
		t.Errorf("redirect target %q is not the guide host", loc)
	}
}

// No template may survive for this route: a stale copy that still renders is
// the failure mode this change exists to end.
func TestDocsOnboarding_TemplateIsGone(t *testing.T) {
	s := newOnboardTestServer(t)
	if s.Templates.Lookup("docs_onboarding") != nil {
		t.Error("docs_onboarding template still loaded — the retired page can still render")
	}
}
