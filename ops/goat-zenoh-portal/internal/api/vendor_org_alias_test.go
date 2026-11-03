package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ADR 1052 Phase 1 — vendor→org machine rename, back-compat-first. These tests
// pin the three guarantees the deprecation window rests on: the /api/orgs*
// aliases reach the same handlers, the bulk wire dual-reads/dual-emits org and
// the deprecated vendor key, and the legacy-vendor-surface counter only counts
// the deprecated routes/keys (the Phase-4 removal gate).

// legacyHits reads the legacy-vendor-surface counter off /api/health.
func legacyHits(t *testing.T, srv *Server) uint64 {
	t.Helper()
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, httptest.NewRequest("GET", "/api/health", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("health status %d: %s", w.Code, w.Body.String())
	}
	var h struct {
		LegacyVendorSurfaceHits uint64 `json:"legacy_vendor_surface_hits"`
	}
	if err := json.NewDecoder(w.Body).Decode(&h); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	return h.LegacyVendorSurfaceHits
}

func TestOrgAlias_listParity(t *testing.T) {
	srv, op := bulkTestServer(t, "alias-co")

	// The /api/orgs alias must reach the same listVendors handler as
	// /api/vendors and return the seeded org.
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, bulkRequest("GET", "/api/orgs", "", op, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/orgs status %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "alias-co") {
		t.Fatalf("GET /api/orgs did not list the seeded org: %s", w.Body.String())
	}
}

func TestListVendors_dualEmitsOrgsAndVendors(t *testing.T) {
	srv, op := bulkTestServer(t, "list-co")

	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, bulkRequest("GET", "/api/orgs", "", op, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	// The listing dual-emits both keys (ADR 1052 Phase 1) so a migrated client
	// can read `orgs` while an un-migrated one still reads `vendors`.
	var body struct {
		Orgs []struct {
			Prefix string `json:"prefix"`
		} `json:"orgs"`
		Vendors []struct {
			Prefix string `json:"prefix"`
		} `json:"vendors"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Orgs) != 1 || body.Orgs[0].Prefix != "list-co" {
		t.Fatalf("orgs key missing/wrong: %+v", body.Orgs)
	}
	if len(body.Vendors) != 1 || body.Vendors[0].Prefix != "list-co" {
		t.Fatalf("vendors key missing/wrong (dual-emit): %+v", body.Vendors)
	}
}

func TestOrgAlias_scopedTopicsImport(t *testing.T) {
	srv, op := bulkTestServer(t, "alias-imp")

	// Import through the /api/orgs alias path, with the new `org` body key.
	body := `org: alias-imp
topics:
  - key_pattern: alias-imp/sys/t/v1
    format: json
`
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, bulkRequest("POST",
		"/api/orgs/alias-imp/topics/import?dry_run=true", "application/yaml", op,
		strings.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("import via /api/orgs status %d: %s", w.Code, w.Body.String())
	}
	var diff importDiff
	if err := json.NewDecoder(w.Body).Decode(&diff); err != nil {
		t.Fatalf("decode diff: %v", err)
	}
	if diff.Summary["create"] != 1 {
		t.Fatalf("expected one create via org alias, got %+v", diff)
	}
	// Dual-emit: the diff carries both org and the deprecated vendor key.
	if diff.Org != "alias-imp" || diff.Vendor != "alias-imp" {
		t.Fatalf("diff must dual-emit org+vendor, got org=%q vendor=%q", diff.Org, diff.Vendor)
	}
}

func TestBulkExport_dualEmitsOrgAndVendor(t *testing.T) {
	srv, op := bulkTestServer(t, "dual-co")
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, bulkRequest("GET",
		"/api/orgs/dual-co/topics/export?format=yaml", "", op, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("export status %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "org: dual-co") {
		t.Fatalf("export missing org key: %s", body)
	}
	if !strings.Contains(body, "vendor: dual-co") {
		t.Fatalf("export missing deprecated vendor key (dual-emit): %s", body)
	}
}

func TestBulkImport_dualReadsLegacyVendorKey(t *testing.T) {
	srv, op := bulkTestServer(t, "legacy-co")

	// A file that uses ONLY the deprecated `vendor` key must still import.
	body := `vendor: legacy-co
topics:
  - key_pattern: legacy-co/sys/t/v1
    format: json
`
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, bulkRequest("POST",
		"/api/orgs/legacy-co/topics/import?dry_run=true", "application/yaml", op,
		strings.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("legacy-vendor-key import status %d: %s", w.Code, w.Body.String())
	}
	var diff importDiff
	if err := json.NewDecoder(w.Body).Decode(&diff); err != nil {
		t.Fatalf("decode diff: %v", err)
	}
	if diff.Summary["create"] != 1 {
		t.Fatalf("legacy vendor-key file did not import: %+v", diff)
	}
}

func TestLegacyVendorSurfaceCounter(t *testing.T) {
	srv, op := bulkTestServer(t, "count-co")

	base := legacyHits(t, srv)

	// A hit on the legacy /api/vendors route bumps the counter.
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, bulkRequest("GET", "/api/vendors", "", op, nil))
	if got := legacyHits(t, srv); got != base+1 {
		t.Fatalf("legacy route hit: counter %d, want %d", got, base+1)
	}

	// A hit on the /api/orgs alias does NOT bump the counter.
	after := legacyHits(t, srv)
	w = httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, bulkRequest("GET", "/api/orgs", "", op, nil))
	if got := legacyHits(t, srv); got != after {
		t.Fatalf("org alias hit must not bump counter: %d, want %d", got, after)
	}

	// Importing a file with the deprecated `vendor` body key (even via the org
	// route) counts as continued vendor-surface use.
	before := legacyHits(t, srv)
	body := `vendor: count-co
topics:
  - key_pattern: count-co/sys/t/v1
    format: json
`
	w = httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, bulkRequest("POST",
		"/api/orgs/count-co/topics/import?dry_run=true", "application/yaml", op,
		strings.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("import status %d: %s", w.Code, w.Body.String())
	}
	if got := legacyHits(t, srv); got != before+1 {
		t.Fatalf("legacy vendor body-key: counter %d, want %d", got, before+1)
	}
}

// ADR 1052 Phase-4 audit: the one-time bundle download route was
// registered with a plain HandleFunc and so never got an /api/orgs* twin nor
// fed the legacy counter. It is now dual-served: /api/org-bundle is primary,
// /api/vendor-bundle is the deprecated, counted alias.
func TestOrgBundleAlias_routesAndCounter(t *testing.T) {
	srv, _ := bulkTestServer(t, "bundle-co")

	// Both routes must reach handleVendorBundle: a bogus token yields the
	// handler's JSON 404 ("bundle token not found"), distinct from the router's
	// plain "404 page not found" that an unregistered path would return.
	for _, path := range []string{
		"/api/org-bundle/bundle-co/sometoken.tar.gz",
		"/api/vendor-bundle/bundle-co/sometoken.tar.gz",
	} {
		w := httptest.NewRecorder()
		srv.Routes().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s status %d: %s", path, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "bundle token not found") {
			t.Fatalf("%s did not reach handleVendorBundle (got %q)", path, w.Body.String())
		}
	}

	// The org-bundle path is NOT counted; the legacy vendor-bundle path IS
	// (closing the Phase-1 counter blind spot — F-233).
	base := legacyHits(t, srv)
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, httptest.NewRequest("GET", "/api/org-bundle/bundle-co/x.tar.gz", nil))
	if got := legacyHits(t, srv); got != base {
		t.Fatalf("org-bundle must not bump counter: %d, want %d", got, base)
	}
	w = httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, httptest.NewRequest("GET", "/api/vendor-bundle/bundle-co/x.tar.gz", nil))
	if got := legacyHits(t, srv); got != base+1 {
		t.Fatalf("legacy vendor-bundle must bump counter: %d, want %d", got, base+1)
	}
}

func TestTopicListing_dualEmitsOrgPrefix(t *testing.T) {
	srv, op := bulkTestServer(t, "topic-co")

	// Create a topic via the bulk import (apply).
	body := `org: topic-co
topics:
  - key_pattern: topic-co/sys/t/v1
    format: json
`
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, bulkRequest("POST",
		"/api/orgs/topic-co/topics/import", "application/yaml", op,
		strings.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("import status %d: %s", w.Code, w.Body.String())
	}

	// List topics and assert the row dual-emits org_prefix == vendor_prefix
	// (ADR 1052 Phase 1 — same anchor, two keys).
	w = httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, bulkRequest("GET", "/api/orgs/topic-co/topics", "", op, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list status %d: %s", w.Code, w.Body.String())
	}
	var body2 struct {
		Topics []struct {
			OrgPrefix    string `json:"org_prefix"`
			VendorPrefix string `json:"vendor_prefix"`
			KeyPattern   string `json:"key_pattern"`
		} `json:"topics"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body2.Topics) != 1 {
		t.Fatalf("expected 1 topic, got %+v", body2.Topics)
	}
	tp := body2.Topics[0]
	if tp.OrgPrefix != "topic-co" {
		t.Fatalf("org_prefix missing/wrong: %q", tp.OrgPrefix)
	}
	if tp.VendorPrefix != "topic-co" {
		t.Fatalf("vendor_prefix (deprecated dual-emit) missing/wrong: %q", tp.VendorPrefix)
	}
}
