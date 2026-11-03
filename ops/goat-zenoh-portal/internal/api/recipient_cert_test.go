package api

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/config"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/pgtest"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/web"
)

// recipientCertServer builds a portal whose recipient-cert store holds one
// anchor ("acme") with a stand-in PEM body.
func recipientCertServer(t *testing.T) (http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	reg, err := registry.Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open registry: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })
	keysDir := filepath.Join(dir, "recipient-keys")
	if err := os.MkdirAll(keysDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pem := "-----BEGIN CERTIFICATE-----\nZmFrZQ==\n-----END CERTIFICATE-----\n"
	if err := os.WriteFile(filepath.Join(keysDir, "acme.pem"), []byte(pem), 0o644); err != nil {
		t.Fatal(err)
	}
	staticFS, _ := fs.Sub(web.Static, "static")
	srv := New(&config.Config{OperatorAPIKey: "test-op-key", RecipientKeysDir: keysDir}, reg, nil, nil, nil, nil, nil, staticFS)
	return srv.Routes(), "test-op-key"
}

func getRecipientCert(h http.Handler, key, query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/api/access/recipient-cert?"+query, nil)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRecipientCert_ServesThePersistedLeaf(t *testing.T) {
	h, key := recipientCertServer(t)
	rec := getRecipientCert(h, key, "anchor=acme")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "BEGIN CERTIFICATE") {
		t.Fatalf("body is not a PEM: %s", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/x-pem-file" {
		t.Fatalf("wrong content type %q", ct)
	}
}

func TestRecipientCert_UnknownAnchorIs404WithRotateRemedy(t *testing.T) {
	h, key := recipientCertServer(t)
	rec := getRecipientCert(h, key, "anchor=nobody")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "rotate") {
		t.Fatalf("404 must carry the rotation remedy: %s", rec.Body.String())
	}
}

func TestRecipientCert_RequiresAuth(t *testing.T) {
	h, _ := recipientCertServer(t)
	if rec := getRecipientCert(h, "", "anchor=acme"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestRecipientCert_RefusesNonAnchorShapes(t *testing.T) {
	h, key := recipientCertServer(t)
	for _, bad := range []string{
		"anchor=..%2Fescape", "anchor=a%2Fb", "anchor=UPPER", "anchor=", "anchor=.hidden",
	} {
		if rec := getRecipientCert(h, key, bad); rec.Code != http.StatusBadRequest {
			t.Fatalf("query %q: want 400, got %d", bad, rec.Code)
		}
	}
}

func TestRecipientCert_UnconfiguredStoreIs503(t *testing.T) {
	reg, err := registry.Open(pgtest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reg.Close() })
	staticFS, _ := fs.Sub(web.Static, "static")
	srv := New(&config.Config{OperatorAPIKey: "k"}, reg, nil, nil, nil, nil, nil, staticFS)
	if rec := getRecipientCert(srv.Routes(), "k", "anchor=acme"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}
}
