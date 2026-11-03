package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/hydra"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/kratos"
)

func TestUserDetailReadFailuresAreNotEmpty(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "unreadable"}[failed], func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.Contains(r.URL.Path, "sessions") {
					if failed {
						http.Error(w, "synthetic upstream failure", http.StatusServiceUnavailable)
						return
					}
					_, _ = w.Write([]byte("[]"))
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"id": "id-1", "state": "active", "traits": map[string]any{"email": "synthetic@example.invalid"}})
			}))
			defer upstream.Close()
			s := newUsersAPITestServer(t, kratos.NewClient(upstream.URL))
			s.Hydra = map[string]*hydra.Client{"tenant": hydra.NewClient(upstream.URL)}
			req := httptest.NewRequestWithContext(t.Context(), "GET", "/users/id-1?idp=tenant", nil)
			req.Header.Set("X-Forwarded-Groups", "PLATFORM-ADMIN")
			req.Header.Set("X-Forwarded-Email", "admin@example.invalid")
			rec := httptest.NewRecorder()
			s.Routes().ServeHTTP(rec, req)
			if rec.Code != 200 {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			for _, text := range []string{"Could not load active sessions", "Could not load consent grants", "Revoke all sessions"} {
				if strings.Contains(body, text) != failed {
					t.Errorf("failed=%v, presence of %q", failed, text)
				}
			}
			for _, text := range []string{"No active sessions", "No consent grants", "Active sessions (0)"} {
				if strings.Contains(body, text) == failed {
					t.Errorf("failed=%v, presence of %q", failed, text)
				}
			}
		})
	}
}

func TestUnknownIdentityUsesSafeErrorLayout(t *testing.T) {
	kc, _ := fakeKratosRevokeSessionsTargetError(t, http.StatusNotFound)
	s := newUsersAPITestServer(t, kc)
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/users/missing?idp=tenant", nil)
	req.Header.Set("X-Forwarded-Groups", "PLATFORM-ADMIN")
	req.Header.Set("X-Forwarded-Email", "admin@example.invalid")
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code < 400 {
		t.Fatalf("unexpected status %d", rec.Code)
	}
	for _, text := range []string{"<nav>", "/static/style.css", "role=\"alert\"", "</html>"} {
		if !strings.Contains(rec.Body.String(), text) {
			t.Errorf("missing %s", text)
		}
	}
	for _, text := range []string{"kratos.example.test", "Kratos GET", "<pre>"} {
		if strings.Contains(rec.Body.String(), text) {
			t.Errorf("leaked %s", text)
		}
	}
}

func TestRotationURLDetailDoesNotEraseOperatorRemedy(t *testing.T) {
	kc, _ := fakeKratos2FA(t, "id-1", "synthetic@example.invalid", http.StatusOK, http.StatusNoContent, posturePasswordOnly)
	portal, _ := fakeRotatePortal(t, http.StatusUnauthorized, map[string]any{"error": map[string]any{"message": "invalid token at https://internal.example.invalid/path"}})
	s := newUsersAPITestServer(t, kc)
	s.Cfg.PortalInternalURL = portal.URL
	s.Cfg.PortalLookupToken = "synthetic-token"
	s.Cfg.NotifyPortalURL = "https://portal.example.invalid"
	w := postRotate(t, s, s.csrfNonce, true)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", w.Code)
	}
	for _, text := range []string{"PORTAL_LOOKUP_TOKEN", "nothing was rotated"} {
		if !strings.Contains(w.Body.String(), text) {
			t.Errorf("missing remedy %q", text)
		}
	}
	if strings.Contains(w.Body.String(), "internal.example.invalid") {
		t.Error("upstream URL leaked")
	}
}
