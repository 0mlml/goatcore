package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/sync/semaphore"
)

// reqCtx is the context used by tests; explicit for noctx-compliance.
var reqCtx = context.Background()

func TestIsAdmitClass(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		want   bool
	}{
		// HTTP REST admit-class
		{"POST setup-keys", http.MethodPost, "/api/setup-keys", true},
		{"POST setup-keys subpath", http.MethodPost, "/api/setup-keys/foo", true},
		{"PUT setup-keys/{id}", http.MethodPut, "/api/setup-keys/abc-123", true},
		{"POST peers", http.MethodPost, "/api/peers", true},
		{"POST peers nested", http.MethodPost, "/api/peers/x/y", true},
		{"POST groups", http.MethodPost, "/api/groups", true},
		{"PUT groups/{id}", http.MethodPut, "/api/groups/admins", true},
		// HTTP REST revoke-class (always pass-through — NOT admit-class)
		{"DELETE setup-keys/{id}", http.MethodDelete, "/api/setup-keys/abc", false},
		{"DELETE peers/{id}", http.MethodDelete, "/api/peers/x", false},
		{"DELETE groups/{id}", http.MethodDelete, "/api/groups/x", false},
		{"GET setup-keys", http.MethodGet, "/api/setup-keys", false},
		{"GET peers", http.MethodGet, "/api/peers", false},
		{"HEAD groups", http.MethodHead, "/api/groups", false},
		{"OPTIONS groups", http.MethodOptions, "/api/groups", false},
		// Other PUT/POST not on admit list
		{"POST users (not admit-class)", http.MethodPost, "/api/users", false},
		{"PUT users/{id} (not admit-class)", http.MethodPut, "/api/users/abc", false},
		// gRPC
		{"gRPC Login is admit-class", http.MethodPost, "/management.ManagementService/Login", true},
		{"gRPC Sync is NOT admit-class", http.MethodPost, "/management.ManagementService/Sync", false},
		{"gRPC GetServerKey NOT admit-class", http.MethodPost, "/management.ManagementService/GetServerKey", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isAdmitClass(tc.method, tc.path)
			if got != tc.want {
				t.Errorf("isAdmitClass(%q, %q) = %v, want %v", tc.method, tc.path, got, tc.want)
			}
		})
	}
}

func TestIsGRPC(t *testing.T) {
	if !isGRPC("/management.ManagementService/Login") {
		t.Error("expected gRPC for /management.ManagementService/Login")
	}
	if isGRPC("/api/setup-keys") {
		t.Error("did not expect gRPC for /api/setup-keys")
	}
	if isGRPC("/") {
		t.Error("did not expect gRPC for /")
	}
}

// 50B.7 — extractPeerID exercise. Ensures the documented header order holds
// and IP fallback strips the port.
func TestExtractPeerID(t *testing.T) {
	cases := []struct {
		name       string
		buildReq   func() *http.Request
		wantPrefix string
		wantKind   peerIDKind
	}{
		{
			"setup-key header",
			func() *http.Request {
				r := httptest.NewRequestWithContext(reqCtx, http.MethodPost, "/api/peers", nil)
				r.Header.Set("Setup-Key", "abc-123")
				return r
			},
			"sk:abc-123",
			peerIDKindSetupKey,
		},
		{
			"x-nb-setup-key header",
			func() *http.Request {
				r := httptest.NewRequestWithContext(reqCtx, http.MethodPost, "/api/peers", nil)
				r.Header.Set("X-NB-Setup-Key", "xyz-789")
				return r
			},
			"sk:xyz-789",
			peerIDKindSetupKey,
		},
		{
			"grpc-metadata-setup-key header",
			func() *http.Request {
				r := httptest.NewRequestWithContext(reqCtx, http.MethodPost, "/management.ManagementService/Login", nil)
				r.Header.Set("Grpc-Metadata-Setup-Key", "grpc-key")
				return r
			},
			"sk:grpc-key",
			peerIDKindSetupKey,
		},
		{
			"ip fallback strips port",
			func() *http.Request {
				r := httptest.NewRequestWithContext(reqCtx, http.MethodPost, "/api/peers", nil)
				r.RemoteAddr = "192.0.2.5:54321"
				return r
			},
			"ip:192.0.2.5",
			peerIDKindSrcIP,
		},
		{
			"ip fallback no port",
			func() *http.Request {
				r := httptest.NewRequestWithContext(reqCtx, http.MethodPost, "/api/peers", nil)
				r.RemoteAddr = "no-port"
				return r
			},
			"ip:no-port",
			peerIDKindSrcIP,
		},
		{
			"setup-key wins over IP",
			func() *http.Request {
				r := httptest.NewRequestWithContext(reqCtx, http.MethodPost, "/api/peers", nil)
				r.Header.Set("Setup-Key", "winning-key")
				r.RemoteAddr = "192.0.2.5:54321"
				return r
			},
			"sk:winning-key",
			peerIDKindSetupKey,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, kind := extractPeerID(tc.buildReq())
			if id != tc.wantPrefix {
				t.Errorf("id = %q, want %q", id, tc.wantPrefix)
			}
			if kind != tc.wantKind {
				t.Errorf("kind = %q, want %q", kind, tc.wantKind)
			}
		})
	}
}

// 50B.1 — concurrencyMiddleware should fast-fail with 503 + Retry-After: 1
// when the semaphore is exhausted. Critically, it must NOT block on Acquire.
func TestConcurrencyMiddlewareSaturation(t *testing.T) {
	// Cap of 1, then take it externally so the next request saturates immediately.
	sem := semaphore.NewWeighted(1)
	if !sem.TryAcquire(1) {
		t.Fatal("first TryAcquire failed; semaphore broken")
	}
	t.Cleanup(func() { sem.Release(1) })

	var hit atomic.Bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit.Store(true)
	})
	h := concurrencyMiddleware(sem, next)

	r := httptest.NewRequestWithContext(reqCtx, http.MethodPost, "/api/peers", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if hit.Load() {
		t.Error("next handler should NOT have been invoked under saturation")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
	if got := w.Header().Get("Retry-After"); got != "1" {
		t.Errorf("Retry-After = %q, want %q", got, "1")
	}
	if !strings.Contains(w.Body.String(), "saturation") {
		t.Errorf("body = %q, expected saturation message", w.Body.String())
	}
}

func TestConcurrencyMiddlewareReleasesOnReturn(t *testing.T) {
	sem := semaphore.NewWeighted(2)
	var hits atomic.Int32
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	})
	h := concurrencyMiddleware(sem, next)

	for i := 0; i < 5; i++ {
		r := httptest.NewRequestWithContext(reqCtx, http.MethodPost, "/api/peers", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK && w.Code != 0 { // httptest default 200
			t.Errorf("iter %d: status = %d, want OK (sequential calls release the slot)", i, w.Code)
		}
	}
	if hits.Load() != 5 {
		t.Errorf("hits = %d, want 5 (semaphore should release between calls)", hits.Load())
	}
}

// 50B.2 — panicMiddleware must absorb a panic in the inner handler so the
// process keeps serving subsequent requests, and increment the panic counter.
func TestPanicMiddlewareRecovers(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("synthetic boom")
	})
	h := panicMiddleware(next)

	// Should not propagate the panic out.
	r := httptest.NewRequestWithContext(reqCtx, http.MethodPost, "/api/peers", nil)
	w := httptest.NewRecorder()
	defer func() {
		if rec := recover(); rec != nil {
			t.Errorf("panic escaped panicMiddleware: %v", rec)
		}
	}()
	h.ServeHTTP(w, r)
}
