package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

// Serving a bundle must leave a record that it was served. Before this, the
// only evidence a partner had ever fetched their credentials was atime on the
// mgmt-host filesystem: the one-time token is deliberately NOT consumed on
// download, so token state could never answer the question, and nothing else
// was written. Minted-but-never-fetched and fetched-but-never-used are
// different stalls with different remedies.
func TestBundleDownload_recordsFirstFetchAndCounts(t *testing.T) {
	srv, _ := bulkTestServer(t, "dl-co")
	ctx := t.Context()

	tarball := filepath.Join(t.TempDir(), "dl-co-onboarding.tar.gz")
	if err := os.WriteFile(tarball, []byte("not-really-a-tarball"), 0o600); err != nil {
		t.Fatalf("write tarball: %v", err)
	}
	if err := srv.reg.MarkBundle(ctx, "dl-co", "tok-abc", tarball); err != nil {
		t.Fatalf("mark bundle: %v", err)
	}

	before, err := srv.reg.GetVendor(ctx, "dl-co")
	if err != nil {
		t.Fatalf("get vendor: %v", err)
	}
	if before.BundleDownloadedAt != nil || before.BundleDownloadCount != 0 {
		t.Fatalf("minted bundle must not read as downloaded: at=%v count=%d",
			before.BundleDownloadedAt, before.BundleDownloadCount)
	}

	get := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		srv.Routes().ServeHTTP(w, httptest.NewRequest(
			"GET", "/api/org-bundle/dl-co/tok-abc.tar.gz", nil))
		return w
	}

	w := get()
	if w.Code != http.StatusOK {
		t.Fatalf("download status %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != "not-really-a-tarball" {
		t.Fatalf("bundle body not served: %q", w.Body.String())
	}

	first, err := srv.reg.GetVendor(ctx, "dl-co")
	if err != nil {
		t.Fatalf("get vendor: %v", err)
	}
	if first.BundleDownloadedAt == nil {
		t.Fatalf("first download must stamp bundle_downloaded_at")
	}
	if first.BundleDownloadCount != 1 {
		t.Fatalf("download count = %d, want 1", first.BundleDownloadCount)
	}

	// The token stays valid by design (siblings share a bundle), so a second
	// fetch must succeed, bump the count, and leave the FIRST-download stamp
	// alone — that stamp is the onboarding-progress signal.
	if w := get(); w.Code != http.StatusOK {
		t.Fatalf("second download status %d", w.Code)
	}
	second, err := srv.reg.GetVendor(ctx, "dl-co")
	if err != nil {
		t.Fatalf("get vendor: %v", err)
	}
	if second.BundleDownloadCount != 2 {
		t.Fatalf("download count = %d, want 2", second.BundleDownloadCount)
	}
	if *second.BundleDownloadedAt != *first.BundleDownloadedAt {
		t.Fatalf("first-download stamp moved: %d -> %d",
			*first.BundleDownloadedAt, *second.BundleDownloadedAt)
	}

	events, err := srv.reg.AuditEvents(ctx, "bundle-download", 10)
	if err != nil {
		t.Fatalf("audit events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 bundle-download audit events, got %d", len(events))
	}
	if !strings.Contains(events[0].Details, "dl-co-onboarding.tar.gz") {
		t.Errorf("audit event should name the bundle served; got %q", events[0].Details)
	}
}

// A failed download must not be recorded as one. A bad token never reaches the
// file, so the row has to stay untouched — otherwise the status surface counts
// probes as collections.
func TestBundleDownload_badTokenRecordsNothing(t *testing.T) {
	srv, _ := bulkTestServer(t, "dl-miss")
	ctx := t.Context()

	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, httptest.NewRequest(
		"GET", "/api/org-bundle/dl-miss/wrong-token.tar.gz", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", w.Code)
	}

	v, err := srv.reg.GetVendor(ctx, "dl-miss")
	if err != nil {
		t.Fatalf("get vendor: %v", err)
	}
	if v.BundleDownloadedAt != nil || v.BundleDownloadCount != 0 {
		t.Fatalf("failed download recorded: at=%v count=%d",
			v.BundleDownloadedAt, v.BundleDownloadCount)
	}
	events, err := srv.reg.AuditEvents(ctx, "bundle-download", 10)
	if err != nil {
		t.Fatalf("audit events: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("failed download must not audit; got %d events", len(events))
	}
}

// stageOf must name what HAPPENED. The ladder alone cannot locate a stall:
// L1 is something the portal did (minted a bundle), not something the person
// did, so an org sitting at L1 could be waiting on a download, a mesh
// enrolment, or a first publish.
func TestStageOf_namesFurthestPositiveEvidence(t *testing.T) {
	at := func(v int64) *int64 { return &v }

	cases := []struct {
		name string
		row  registry.VendorProgressRow
		want string
	}{
		{"nothing yet", registry.VendorProgressRow{}, "registered"},
		{"minted only", registry.VendorProgressRow{L1At: at(1)}, "bundle-minted"},
		{"downloaded", registry.VendorProgressRow{L1At: at(1), BundleDownloadedAt: at(2)}, "bundle-downloaded"},
		{"on the mesh", registry.VendorProgressRow{L1At: at(1), BundleDownloadedAt: at(2), L2At: at(3)}, "mesh-enrolled"},
		{"declared topics", registry.VendorProgressRow{L1At: at(1), L2At: at(3), RegisteredTopics: 2}, "topics-registered"},
		{"hello world", registry.VendorProgressRow{L1At: at(1), L3At: at(4)}, "hello-world-published"},
		{"real traffic", registry.VendorProgressRow{L1At: at(1), L3At: at(4), L4At: at(5)}, "publishing"},
		// The Origin Robotics shape: bundle collected, mesh peer up,
		// nothing declared, nothing published. It must NOT read as "minted".
		{"collected then stalled", registry.VendorProgressRow{
			L1At: at(1), BundleDownloadedAt: at(2), L2At: at(3),
		}, "mesh-enrolled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stageOf(tc.row); got != tc.want {
				t.Errorf("stageOf = %q, want %q", got, tc.want)
			}
		})
	}
}

// A row minted before the download column existed has a nil stamp because
// nobody was recording, not because nobody downloaded. No stage may assert
// that negative.
func TestStageOf_neverAssertsNotDownloaded(t *testing.T) {
	at := func(v int64) *int64 { return &v }
	got := stageOf(registry.VendorProgressRow{L1At: at(1)})
	if strings.Contains(got, "not-downloaded") || strings.Contains(got, "never") {
		t.Errorf("stage must not assert an unrecorded negative; got %q", got)
	}
}
