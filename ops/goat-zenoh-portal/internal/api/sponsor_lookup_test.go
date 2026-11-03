package api

// ADR 1072 — the surviving portal half of sponsor-scoped onboarding: the
// sponsorship annotation's Ory-login binding (sponsor_email, set via the
// grant lever) and the read-only resolves the id-admin-console delegates to
// (whoami sponsor forms + the sponsorship-by-email lookup).

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/config"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/pgtest"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/web"
)

// lookupTestServer mirrors sponsorshipTestServer but also configures the
// console↔portal service secret the lookup accepts.
func lookupTestServer(t *testing.T) (http.Handler, *registry.Registry, string, string) {
	t.Helper()
	reg, err := registry.Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open registry: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })
	staticFS, err := fs.Sub(web.Static, "static")
	if err != nil {
		t.Fatalf("static fs: %v", err)
	}
	srv := New(&config.Config{OperatorAPIKey: "test-op-key", NotifyToken: "svc-token"}, reg, nil, nil, nil, nil, nil, staticFS)
	return srv.Routes(), reg, "test-op-key", "svc-token"
}

func lookupGet(t *testing.T, h http.Handler, bearer, email string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", "/api/orgs/sponsorship?email="+email, nil)
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// The grant lever binds a login email; the lookup resolves it for the
// operator bearer AND the console service token, refuses everyone else, and
// forgets the binding on revoke.
func TestSponsorshipLookup_EndToEnd(t *testing.T) {
	h, reg, opKey, svcToken := lookupTestServer(t)
	seedPrincipal(t, reg, "sponsor-a", true, "developer")

	// Grant with the email binding via the public lever (case-normalized).
	w := postJSON(t, h, opKey, "/api/orgs/sponsor-a/sponsorship", `{"authorized":true,"max_tier":"developer","email":"person-62@example.org"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("grant with email = %d; %s", w.Code, w.Body.String())
	}

	for _, bearer := range []string{opKey, svcToken} {
		w := lookupGet(t, h, bearer, "jeroen@example.org")
		if w.Code != http.StatusOK {
			t.Fatalf("lookup (bearer=%q) = %d; %s", bearer, w.Code, w.Body.String())
		}
		var res struct {
			Vendor         string `json:"vendor"`
			Authorized     bool   `json:"sponsor_authorized"`
			SponsorMaxTier string `json:"sponsor_max_tier"`
			SponsorEmail   string `json:"sponsor_email"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res.Vendor != "sponsor-a" || !res.Authorized || res.SponsorMaxTier != "developer" || res.SponsorEmail != "jeroen@example.org" {
			t.Fatalf("lookup payload = %+v", res)
		}
	}

	// A participant key is NOT enough; neither is no bearer at all.
	if w := lookupGet(t, h, "key-a", "jeroen@example.org"); w.Code != http.StatusUnauthorized {
		t.Fatalf("vendor-key lookup = %d, want 401", w.Code)
	}
	if w := lookupGet(t, h, "", "jeroen@example.org"); w.Code != http.StatusUnauthorized {
		t.Fatalf("bare lookup = %d, want 401", w.Code)
	}
	// Unknown email → 404.
	if w := lookupGet(t, h, opKey, "nobody@example.org"); w.Code != http.StatusNotFound {
		t.Fatalf("unknown lookup = %d, want 404", w.Code)
	}
	// Revoke clears the binding.
	if w := postJSON(t, h, opKey, "/api/orgs/sponsor-a/sponsorship", `{"authorized":false}`); w.Code != http.StatusOK {
		t.Fatalf("revoke = %d; %s", w.Code, w.Body.String())
	}
	if w := lookupGet(t, h, opKey, "jeroen@example.org"); w.Code != http.StatusNotFound {
		t.Fatalf("post-revoke lookup = %d, want 404", w.Code)
	}
}

// Two annotations bound to one login refuse (ErrAmbiguous → 409), never guess.
func TestSponsorshipLookup_AmbiguousRefuses(t *testing.T) {
	h, reg, opKey, _ := lookupTestServer(t)
	seedPrincipal(t, reg, "org-one", true, "developer")
	seedPrincipal(t, reg, "org-two", true, "developer")
	for _, org := range []string{"org-one", "org-two"} {
		if w := postJSON(t, h, opKey, "/api/orgs/"+org+"/sponsorship", `{"authorized":true,"max_tier":"developer","email":"dupe@example.org"}`); w.Code != http.StatusOK {
			t.Fatalf("grant %s = %d", org, w.Code)
		}
	}
	if w := lookupGet(t, h, opKey, "dupe@example.org"); w.Code != http.StatusConflict {
		t.Fatalf("ambiguous lookup = %d, want 409; %s", w.Code, w.Body.String())
	}
}

// whoami carries the sponsor's other recorded forms (bound email) so the
// console can scope participant keys.
func TestWhoami_CarriesSponsorForms(t *testing.T) {
	h, reg, opKey, _ := lookupTestServer(t)
	orgKey := seedPrincipal(t, reg, "sponsor-a", true, "developer")
	if w := postJSON(t, h, opKey, "/api/orgs/sponsor-a/sponsorship", `{"authorized":true,"max_tier":"developer","email":"jeroen@example.org"}`); w.Code != http.StatusOK {
		t.Fatalf("grant = %d", w.Code)
	}
	r := httptest.NewRequest("GET", "/api/whoami", nil)
	r.Header.Set("Authorization", "Bearer "+orgKey)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("whoami = %d; %s", w.Code, w.Body.String())
	}
	var res struct {
		Vendor       string `json:"vendor"`
		SponsorEmail string `json:"sponsor_email"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res.Vendor != "sponsor-a" || res.SponsorEmail != "jeroen@example.org" {
		t.Fatalf("whoami payload = %+v", res)
	}
}
