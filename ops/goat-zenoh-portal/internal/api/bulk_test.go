package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/config"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/pgtest"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

// bulkTestServer stands a minimal Server up with an in-tempdir SQLite
// registry + a pre-seeded vendor. Returns the server + operator key.
func bulkTestServer(t *testing.T, vendorPrefix string) (*Server, string) {
	t.Helper()
	reg, err := registry.Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open registry: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })

	now := int64(1700000000)
	v := &registry.Vendor{
		Prefix:      vendorPrefix,
		APIKeyHash:  "$2a$10$placeholderhash",
		DisplayName: "Bulk Test",
		ApprovedAt:  &now,
		Approver:    ptrString("test"),
	}
	if err := reg.CreateVendor(t.Context(), v); err != nil {
		t.Fatalf("create vendor: %v", err)
	}

	cfg := &config.Config{
		OperatorAPIKey: "test-op-key",
	}
	srv := New(cfg, reg, nil, nil, nil, nil, nil, nil)
	return srv, "test-op-key"
}

func ptrString(s string) *string { return &s }

func bulkRequest(method, path, ct, opKey string, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, path, body)
	if ct != "" {
		r.Header.Set("Content-Type", ct)
	}
	if opKey != "" {
		r.Header.Set("Authorization", "Bearer "+opKey)
	}
	return r
}

func TestExportTopics_yaml_emptyVendor(t *testing.T) {
	srv, op := bulkTestServer(t, "vendor-a")

	w := httptest.NewRecorder()
	req := bulkRequest("GET", "/api/vendors/vendor-a/topics/export?format=yaml", "", op, nil)
	srv.Routes().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "vendor: vendor-a") {
		t.Fatalf("export body missing vendor field: %s", body)
	}
}

func TestImportTopics_dryRun_createOnly(t *testing.T) {
	srv, op := bulkTestServer(t, "vendor-b")
	yamlBody := `vendor: vendor-b
topics:
  - key_pattern: vendor-b/sys/topic1/v1
    format: json
    description: first
  - key_pattern: vendor-b/sys/topic2/v1
    format: cbor
`
	w := httptest.NewRecorder()
	req := bulkRequest("POST", "/api/vendors/vendor-b/topics/import?dry_run=true",
		"application/yaml", op, strings.NewReader(yamlBody))
	srv.Routes().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var diff importDiff
	if err := json.NewDecoder(w.Body).Decode(&diff); err != nil {
		t.Fatalf("decode diff: %v", err)
	}
	if !diff.DryRun || diff.Summary["create"] != 2 {
		t.Fatalf("diff summary unexpected: %+v", diff)
	}

	// Dry-run means no writes — list should be empty.
	w2 := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w2,
		bulkRequest("GET", "/api/vendors/vendor-b/topics", "", op, nil))
	if !strings.Contains(w2.Body.String(), `"topics":[]`) {
		t.Fatalf("expected empty topics after dry-run, got: %s", w2.Body.String())
	}
}

func TestImportTopics_apply_thenReplace(t *testing.T) {
	srv, op := bulkTestServer(t, "vendor-c")

	// First apply: two topics.
	step1 := `{"vendor":"vendor-c","topics":[
		{"key_pattern":"vendor-c/sys/a/v1","format":"json"},
		{"key_pattern":"vendor-c/sys/b/v1","format":"json"}
	]}`
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w,
		bulkRequest("POST", "/api/vendors/vendor-c/topics/import",
			"application/json", op, bytes.NewBufferString(step1)))
	if w.Code != http.StatusOK {
		t.Fatalf("apply step1: %d %s", w.Code, w.Body.String())
	}

	// Replace-mode import that drops `b` and adds `c`.
	step2 := `{"vendor":"vendor-c","topics":[
		{"key_pattern":"vendor-c/sys/a/v1","format":"json"},
		{"key_pattern":"vendor-c/sys/c/v1","format":"json"}
	]}`
	w2 := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w2,
		bulkRequest("POST", "/api/vendors/vendor-c/topics/import?mode=replace",
			"application/json", op, bytes.NewBufferString(step2)))
	if w2.Code != http.StatusOK {
		t.Fatalf("apply step2: %d %s", w2.Code, w2.Body.String())
	}
	var diff importDiff
	if err := json.NewDecoder(w2.Body).Decode(&diff); err != nil {
		t.Fatalf("decode diff: %v", err)
	}
	if diff.Summary["create"] != 1 || diff.Summary["retire"] != 1 || diff.Summary["unchanged"] != 1 {
		t.Fatalf("replace diff unexpected: %+v", diff)
	}
}

func TestImportTopics_vendorMismatchRejected(t *testing.T) {
	srv, op := bulkTestServer(t, "vendor-d")
	body := `{"vendor":"someone-else","topics":[{"key_pattern":"vendor-d/x/y/v1","format":"json"}]}`
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w,
		bulkRequest("POST", "/api/vendors/vendor-d/topics/import",
			"application/json", op, strings.NewReader(body)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on vendor mismatch, got %d: %s", w.Code, w.Body.String())
	}
}
