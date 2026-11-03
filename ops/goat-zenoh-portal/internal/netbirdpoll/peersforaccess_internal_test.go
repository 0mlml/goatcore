package netbirdpoll

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// White-box (same package) so it can tune accessCacheTTL to exercise the
// fresh-serve vs stale-degrade branches deterministically without waiting.

func TestPeersForAccessDisabled(t *testing.T) {
	_, warn, enabled := (&Client{}).PeersForAccess(context.Background())
	if enabled {
		t.Fatal("unconfigured client should report enabled=false")
	}
	if warn != "" {
		t.Errorf("disabled client should emit no warning, got %q", warn)
	}
}

func TestPeersForAccessCachesAndDegrades(t *testing.T) {
	var hits int64
	fail := atomic.Bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		if fail.Load() {
			http.Error(w, "boom", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`[{"id":"p1","name":"peer-acme-x","connected":true}]`))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")

	// 1. Cold cache → one live fetch, peers returned, no warning.
	peers, warn, enabled := c.PeersForAccess(context.Background())
	if !enabled || warn != "" || len(peers) != 1 {
		t.Fatalf("cold fetch: enabled=%v warn=%q peers=%d", enabled, warn, len(peers))
	}
	if got := atomic.LoadInt64(&hits); got != 1 {
		t.Fatalf("cold fetch should hit mgmt once, got %d", got)
	}

	// 2. Fresh cache → served without another fetch (THE fix: access never
	//    re-blocks on mgmt while the snapshot is warm).
	if _, warn, _ := c.PeersForAccess(context.Background()); warn != "" {
		t.Errorf("fresh cache should have no warning, got %q", warn)
	}
	if got := atomic.LoadInt64(&hits); got != 1 {
		t.Fatalf("fresh cache must not refetch, hits=%d", got)
	}

	// 3. Expire the cache + make mgmt fail → serves the STALE snapshot with an
	//    honest staleness warning, never blocks and never fabricates empty.
	orig := accessCacheTTL
	accessCacheTTL = 0
	defer func() { accessCacheTTL = orig }()
	fail.Store(true)
	peers, warn, enabled = c.PeersForAccess(context.Background())
	if !enabled || len(peers) != 1 {
		t.Fatalf("stale serve should still return the last snapshot, got peers=%d", len(peers))
	}
	if warn == "" {
		t.Error("stale serve must carry a staleness warning")
	}
}

func TestPeersForAccessColdFailureOmitsWithWarning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	// Never cached + mgmt failing → enabled, no peers, an honest omit warning
	// (distinct from the not-configured case).
	peers, warn, enabled := New(srv.URL, "tok").PeersForAccess(context.Background())
	if !enabled {
		t.Fatal("configured-but-failing client is still enabled")
	}
	if len(peers) != 0 || warn == "" {
		t.Errorf("cold failure should omit peers with a warning, got peers=%d warn=%q", len(peers), warn)
	}
}
