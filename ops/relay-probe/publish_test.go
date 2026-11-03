package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// The direct (second) ingest leaves the relay over public DNS carrying a bearer
// token, by default the primary one. A server whose certificate does not
// verify, here httptest's self-signed one, must never receive that token.
func TestDirectClientRefusesUnverifiedIngest(t *testing.T) {
	var sawToken atomic.Bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			sawToken.Store(true)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if code := post(directClient(), srv.URL+"/relay-a", "synthetic-token", []byte(`{}`)); code != 0 {
		t.Fatalf("post to an unverified ingest returned HTTP %d, want a TLS failure (0)", code)
	}
	if sawToken.Load() {
		t.Fatal("the bearer token reached an ingest whose certificate was not verified")
	}
}

// A verifiable ingest still gets the post: the fix must not stop delivery.
func TestDirectClientPostsToVerifiedIngest(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := directClient()
	c.Transport.(*http.Transport).TLSClientConfig.RootCAs = srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	if code := post(c, srv.URL+"/relay-a", "synthetic-token", []byte(`{}`)); code != http.StatusNoContent {
		t.Fatalf("post to a verified ingest returned HTTP %d, want 204", code)
	}
}

// The primary post hairpins through this relay's rathole :443 with the same
// bearer token. Whatever answers on that port must prove the ingest name.
func TestHairpinClientRefusesUnverifiedIngest(t *testing.T) {
	var sawToken atomic.Bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			sawToken.Store(true)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := hairpinClientVia(srv.Listener.Addr().String(), "example.com")
	if code := post(c, "https://example.com/relay-metrics-ingest/relay-a", "synthetic-token", []byte(`{}`)); code != 0 {
		t.Fatalf("hairpin post to an unverified ingest returned HTTP %d, want a TLS failure (0)", code)
	}
	if sawToken.Load() {
		t.Fatal("the bearer token reached a hairpin ingest whose certificate was not verified")
	}
}

// A hairpin ingest whose certificate covers the SNI still gets the post.
func TestHairpinClientPostsToVerifiedIngest(t *testing.T) {
	var path string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.Host + r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := hairpinClientVia(srv.Listener.Addr().String(), "example.com")
	c.Transport.(*http.Transport).TLSClientConfig.RootCAs = srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	if code := post(c, "https://example.com/relay-metrics-ingest/relay-a", "synthetic-token", []byte(`{}`)); code != http.StatusNoContent {
		t.Fatalf("hairpin post to a verified ingest returned HTTP %d, want 204", code)
	}
	if path != "example.com/relay-metrics-ingest/relay-a" {
		t.Fatalf("hairpin request reached %q, want the ingest host and path", path)
	}
}
