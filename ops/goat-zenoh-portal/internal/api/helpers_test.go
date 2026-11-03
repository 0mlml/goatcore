package api

// helpers_test.go — shared test helpers retained after the onboarding engine
// was stripped from the portal (docs/design/onboarding-extraction-plan.md slice
// 5). onboardTestServer + postJSON were general-purpose (a test server over a
// temp registry + a bearer POST); they previously lived in the deleted onboard
// test files but are used by surviving tests (sponsor lookup, access probation),
// so they move here.

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/config"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/pgtest"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/web"
)

// onboardTestServer builds an api server over a fresh temp registry with the
// real embedded static FS (so intake-hint-backed handlers load their presets)
// and returns its routes + the operator key.
func onboardTestServer(t *testing.T) (http.Handler, string) {
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
	srv := New(&config.Config{OperatorAPIKey: "test-op-key"}, reg, nil, nil, nil, nil, nil, staticFS)
	return srv.Routes(), "test-op-key"
}

// postJSON sends an operator-bearer JSON POST and returns the recorder.
func postJSON(t *testing.T, h http.Handler, key, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// seedPrincipal creates an approved org/vendor row carrying the ADR 1071
// sponsorship annotation (authorized + max tier) — the fixture the surviving
// sponsorship-lookup + scoping tests build on.
//
// It mints the token through generateAPIKey rather than accepting a made-up
// string, and RETURNS it: since F-371 a bearer that isn't shaped like a real
// portal token is rejected before any lookup, so a fixture with a hand-written
// key would authenticate in the test and nowhere else.
func seedPrincipal(t *testing.T, reg *registry.Registry, prefix string, authorized bool, maxTier string) string {
	t.Helper()
	key, err := generateAPIKey(prefix)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	hash, digest, err := hashAPIKey(key)
	if err != nil {
		t.Fatalf("hash key: %v", err)
	}
	now := time.Now().Unix()
	approver := "operator"
	v := &registry.Vendor{
		Prefix:            prefix,
		APIKeyHash:        hash,
		APIKeySHA256:      digest,
		DisplayName:       prefix,
		ApprovedAt:        &now,
		Approver:          &approver,
		SponsorAuthorized: authorized,
		SponsorMaxTier:    maxTier,
	}
	if err := reg.CreateVendor(t.Context(), v); err != nil {
		t.Fatalf("create principal %s: %v", prefix, err)
	}
	return key
}
