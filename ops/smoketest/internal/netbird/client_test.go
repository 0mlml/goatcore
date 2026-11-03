package netbird

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// The client sends the management API token on every request, so it must not
// complete a handshake with a server whose certificate it cannot verify. An
// httptest TLS server presents a self-signed certificate, which stands in for
// anyone intercepting the connection.
func TestClientRefusesUnverifiedServerAndKeepsToken(t *testing.T) {
	var sawToken atomic.Bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			sawToken.Store(true)
		}
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	c := NewClient(strings.TrimPrefix(srv.URL, "https://"), "synthetic-token")
	if _, err := c.ListPeers(context.Background()); err == nil {
		t.Fatal("ListPeers succeeded against a server with an untrusted certificate")
	}
	if sawToken.Load() {
		t.Fatal("the API token reached a server whose certificate was not verified")
	}
}
