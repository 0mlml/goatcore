package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/kanidmdoor"
)

const doorMember = "member@example.test"

// fakeDoor is a Kanidm-shaped door holding one account for doorMember.
func fakeDoor(t *testing.T, handle string, hasCred bool) *kanidmdoor.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-kanidm-auth-session-id", "sid")
		_, _ = w.Write([]byte(`{"state":{"success":"bearer"}}`))
	})
	mux.HandleFunc("/v1/person", func(w http.ResponseWriter, r *http.Request) {
		attrs := map[string][]string{"name": {handle}, "mail": {doorMember}}
		if hasCred {
			attrs["primary_credential"] = []string{"set"}
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"attrs": attrs}})
	})
	mux.HandleFunc("/v1/person/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"token":"aB3dE-fG7hI-jK1lM-nO5pQ","expiry_time":4102444800}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return kanidmdoor.New(srv.URL, "pw")
}

func setupCodeServer(t *testing.T, handle string, hasCred bool) *Server {
	t.Helper()
	s := newOnboardTestServer(t)
	s.Cfg.DevBypassAuth = false
	s.CollabDoor = fakeDoor(t, handle, hasCred)
	s.PublicDoor = kanidmdoor.New("", "") // unconfigured on this goatnet
	return s
}

func mintPost(t *testing.T, h http.Handler, s *Server, email, plane string, withCSRF bool) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{}
	if withCSRF {
		form.Set("csrf", s.csrfNonce)
	}
	form.Set("plane", plane)
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/setup-code", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if email != "" {
		r.Header.Set("X-Forwarded-Email", email)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestSetupCode_LandingOffersOnlyConfiguredDoors(t *testing.T) {
	s := setupCodeServer(t, "member-h", false)
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/setup-code", nil)
	r.Header.Set("X-Forwarded-Email", doorMember)
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /setup-code = %d, want 200; %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "collaboration space") {
		t.Error("configured collab door is not offered")
	}
	if strings.Contains(body, "relay health") {
		t.Error("an UNCONFIGURED door is offered — that renders a button that cannot work")
	}
}

// The reason the page exists: a person who already has a credential and forgot
// it must be able to reset it themselves. Refusing them is the gap.
func TestSetupCode_ExistingCredentialGetsAResetNotARefusal(t *testing.T) {
	s := setupCodeServer(t, "member-h", true)
	w := mintPost(t, s.Routes(), s, doorMember, "collab", true)
	if w.Code != http.StatusOK {
		t.Fatalf("mint = %d, want 200; %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "aB3dE-fG7hI-jK1lM-nO5pQ") {
		t.Error("the code is not on the page")
	}
	if !strings.Contains(body, "resets your password") {
		t.Error("an account that already has a credential must be told this is a RESET")
	}
}

// The code goes on the page. It must never be wrapped into a URL, because that
// is spent by mail scanners and jams the reset form.
func TestSetupCode_PageNeverRendersATokenisedURL(t *testing.T) {
	s := setupCodeServer(t, "member-h", false)
	body := mintPost(t, s.Routes(), s, doorMember, "collab", true).Body.String()
	if strings.Contains(body, "reset?token=") {
		t.Fatal("page renders a tokenised reset URL")
	}
	if !strings.Contains(body, "/ui/reset") {
		t.Error("page does not give the bare reset address")
	}
	if !strings.Contains(body, "Do not put the") {
		t.Error("page does not warn against pasting the code into the address bar")
	}
}

// The account is resolved from the SESSION email. A form field naming somebody
// else's account would be an account-takeover path with a UI on it.
func TestSetupCode_AccountComesFromTheSessionNotTheForm(t *testing.T) {
	s := setupCodeServer(t, "member-h", false)
	form := url.Values{}
	form.Set("csrf", s.csrfNonce)
	form.Set("plane", "collab")
	form.Set("handle", "someone-else")
	form.Set("email", "victim@example.test")
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/setup-code", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Forwarded-Email", doorMember)
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)

	body := w.Body.String()
	if strings.Contains(body, "someone-else") || strings.Contains(body, "victim@example.test") {
		t.Fatal("a form-supplied handle or address reached the mint")
	}
	if !strings.Contains(body, "member-h") {
		t.Error("the session's own account was not the one used")
	}
}

func TestSetupCode_NoAccountSaysSoWithoutOfferingAMint(t *testing.T) {
	s := setupCodeServer(t, "member-h", false)
	w := mintPost(t, s.Routes(), s, "stranger@example.test", "collab", true)
	body := w.Body.String()
	if strings.Contains(body, "aB3dE-fG7hI-jK1lM-nO5pQ") {
		t.Fatal("minted a code for an address with no account on the door")
	}
	if !strings.Contains(body, "no collaboration space account") {
		t.Errorf("refusal is not the actionable one; got %q", body)
	}
}

func TestSetupCode_AnonymousAndCSRFlessPostsRefused(t *testing.T) {
	s := setupCodeServer(t, "member-h", false)
	if w := mintPost(t, s.Routes(), s, "", "collab", true); w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous POST = %d, want 401", w.Code)
	}
	if w := mintPost(t, s.Routes(), s, doorMember, "collab", false); w.Code != http.StatusForbidden {
		t.Errorf("CSRF-less POST = %d, want 403", w.Code)
	}
}

func TestSetupCode_UnknownPlaneIsRefused(t *testing.T) {
	s := setupCodeServer(t, "member-h", false)
	w := mintPost(t, s.Routes(), s, doorMember, "public", true) // configured empty
	if strings.Contains(w.Body.String(), "aB3dE-fG7hI-jK1lM-nO5pQ") {
		t.Fatal("minted against a door that is not configured")
	}
}
