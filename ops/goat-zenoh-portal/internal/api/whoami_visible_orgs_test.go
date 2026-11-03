package api

// whoami carries visible_orgs so the frontend header ("N orgs visible") has a
// cheap source. It previously came from the full /api/orgs listing, which every
// page fetched on load — ~63 KB of JSON to render one integer.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func whoamiVisibleOrgs(t *testing.T, h http.Handler, key string) (int, int) {
	t.Helper()
	r := httptest.NewRequest("GET", "/api/whoami", nil)
	r.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var res struct {
		VisibleOrgs int `json:"visible_orgs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode whoami: %v; body=%s", err, w.Body.String())
	}
	return w.Code, res.VisibleOrgs
}

func TestWhoami_VisibleOrgsCountsRegistryForOperator(t *testing.T) {
	h, reg, opKey, _ := lookupTestServer(t)

	code, n := whoamiVisibleOrgs(t, h, opKey)
	if code != http.StatusOK {
		t.Fatalf("whoami = %d", code)
	}
	if n != 0 {
		t.Fatalf("empty registry should report 0 visible orgs, got %d", n)
	}

	seedPrincipal(t, reg, "org-a", true, "developer")
	seedPrincipal(t, reg, "org-b", true, "developer")

	code, n = whoamiVisibleOrgs(t, h, opKey)
	if code != http.StatusOK {
		t.Fatalf("whoami after seeding = %d", code)
	}
	if n != 2 {
		t.Errorf("visible_orgs = %d, want 2", n)
	}
}

// A vendor/org principal gets the count too — the header renders on every page
// regardless of which tier is looking at it.
func TestWhoami_VisibleOrgsPresentForOrgPrincipal(t *testing.T) {
	h, reg, _, _ := lookupTestServer(t)
	orgAKey := seedPrincipal(t, reg, "org-a", true, "developer")
	seedPrincipal(t, reg, "org-b", true, "developer")

	code, n := whoamiVisibleOrgs(t, h, orgAKey)
	if code != http.StatusOK {
		t.Fatalf("whoami = %d", code)
	}
	if n != 2 {
		t.Errorf("visible_orgs = %d, want 2", n)
	}
}
