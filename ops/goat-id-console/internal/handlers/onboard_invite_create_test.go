package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func postInviteCreate(t *testing.T, h http.Handler, csrf string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	form.Set("csrf", csrf)
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/onboarding/invite", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Forwarded-Groups", "PLATFORM-ADMIN")
	r.Header.Set("X-Forwarded-Email", "admin@example.org")
	r.Header.Set("X-Forwarded-Preferred-Username", "actor-1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestOnboardInviteCreate_InputValidation(t *testing.T) {
	s := newOnboardTestServer(t)
	s.Cfg.NotifyGoatnet = "" // Exercise form validation without a configured fallback.

	cases := []struct {
		name       string
		env        string
		quota      string
		ttl        string
		wantStatus int
		wantError  string
	}{
		{"valid inputs", "zagros", "5", "48", http.StatusOK, ""},
		{"missing env", "", "5", "48", http.StatusBadRequest, "env is required"},
		{"bad env", "ZAGROS", "5", "48", http.StatusBadRequest, "env must be a lowercase goatnet slug"},
		{"negative quota", "zagros", "-1", "48", http.StatusBadRequest, "machine_quota must be a non-negative integer"},
		{"non-integer quota", "zagros", "abc", "48", http.StatusBadRequest, "machine_quota must be a non-negative integer"},
		{"zero quota is ok", "zagros", "0", "48", http.StatusOK, ""},
		{"empty quota is ok", "zagros", "", "48", http.StatusOK, ""},
		{"non-integer ttl", "zagros", "5", "abc", http.StatusBadRequest, "ttl_hours must be an integer"},
		{"ttl below min", "zagros", "5", "0", http.StatusBadRequest, "ttl_hours must be between"},
		{"ttl above max", "zagros", "5", "800", http.StatusBadRequest, "ttl_hours must be between"},
		{"empty ttl is ok", "zagros", "5", "", http.StatusOK, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{}
			form.Set("env", tc.env)
			form.Set("machine_quota", tc.quota)
			form.Set("ttl_hours", tc.ttl)

			w := postInviteCreate(t, s.Routes(), s.csrfNonce, form)

			if tc.wantStatus == http.StatusOK {
				// OK redirects or renders
				if w.Code != http.StatusOK && w.Code != http.StatusSeeOther && w.Code != http.StatusFound {
					t.Errorf("got status %d, want OK/Redirect; body: %s", w.Code, w.Body.String())
				}
			} else {
				if w.Code != tc.wantStatus {
					t.Errorf("got status %d, want %d; body: %s", w.Code, tc.wantStatus, w.Body.String())
				}
				if !strings.Contains(w.Body.String(), tc.wantError) {
					t.Errorf("body missing %q, got: %s", tc.wantError, w.Body.String())
				}
			}
		})
	}
}
