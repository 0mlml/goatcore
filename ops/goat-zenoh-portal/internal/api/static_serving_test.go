package api_test

// Regression cover for the static-asset delivery path: conditional GETs,
// content encoding, and the display-lexicon substitution that has to survive
// both. Before this, the frontend was served by a stock http.FileServer over an
// embed.FS — no ETag (embed reports a zero modtime, so not even Last-Modified),
// no Cache-Control and no compression, which meant every navigation re-fetched
// the whole ~215 KB page.

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"testing"
)

// noAutoGzip is a client that does NOT set Accept-Encoding for us. The stdlib
// transport otherwise adds gzip and transparently decodes it, which would hide
// exactly the behaviour under test.
func noAutoGzip() *http.Client {
	return &http.Client{Transport: &http.Transport{DisableCompression: true}}
}

func getWith(t *testing.T, url string, hdr map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := noAutoGzip().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

func TestStatic_ETagRevalidationReturns304(t *testing.T) {
	ts := newTestServer(t)

	resp := getWith(t, ts.URL+"/app.js", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first GET: expected 200, got %d", resp.StatusCode)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatal("no ETag on a static asset — clients have nothing to revalidate against")
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 {
		t.Fatal("empty body on first GET")
	}

	// Second request with the tag must come back empty.
	resp2 := getWith(t, ts.URL+"/app.js", map[string]string{"If-None-Match": etag})
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotModified {
		t.Fatalf("revalidation: expected 304, got %d", resp2.StatusCode)
	}
	body2, _ := io.ReadAll(resp2.Body)
	if len(body2) != 0 {
		t.Errorf("304 carried %d bytes of body; must carry none", len(body2))
	}
}

func TestStatic_GzipServedAndDecodesToIdentity(t *testing.T) {
	ts := newTestServer(t)

	identity := getWith(t, ts.URL+"/app.js", nil)
	defer identity.Body.Close()
	if enc := identity.Header.Get("Content-Encoding"); enc != "" {
		t.Fatalf("no Accept-Encoding sent but got Content-Encoding %q", enc)
	}
	plain, _ := io.ReadAll(identity.Body)

	gz := getWith(t, ts.URL+"/app.js", map[string]string{"Accept-Encoding": "gzip"})
	defer gz.Body.Close()
	if enc := gz.Header.Get("Content-Encoding"); enc != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", enc)
	}
	if v := gz.Header.Get("Vary"); !strings.Contains(v, "Accept-Encoding") {
		t.Errorf("Vary = %q, must include Accept-Encoding or caches will serve the wrong encoding", v)
	}
	compressed, _ := io.ReadAll(gz.Body)
	if len(compressed) >= len(plain) {
		t.Errorf("gzip body (%d B) not smaller than identity (%d B)", len(compressed), len(plain))
	}

	zr, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	round, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gzip decode: %v", err)
	}
	if !bytes.Equal(round, plain) {
		t.Error("gzip representation does not decode to the identity bytes")
	}
}

// A client that explicitly refuses gzip via q=0 must get identity. "gzip;q=0"
// contains the substring "gzip", which is why the naive check is wrong.
func TestStatic_GzipRefusedWithQZero(t *testing.T) {
	ts := newTestServer(t)
	resp := getWith(t, ts.URL+"/app.js", map[string]string{"Accept-Encoding": "gzip;q=0"})
	defer resp.Body.Close()
	if enc := resp.Header.Get("Content-Encoding"); enc == "gzip" {
		t.Error("client sent gzip;q=0 (an explicit refusal) but got gzip anyway")
	}
}

// The identity and gzip representations of the same asset must not share an
// ETag — they are different representations (RFC 9110 §8.8.1), and a cache that
// mixed them would hand a gzip body to a client that cannot decode it.
func TestStatic_ETagDiffersPerEncoding(t *testing.T) {
	ts := newTestServer(t)

	plain := getWith(t, ts.URL+"/app.js", nil)
	defer plain.Body.Close()
	gz := getWith(t, ts.URL+"/app.js", map[string]string{"Accept-Encoding": "gzip"})
	defer gz.Body.Close()

	if plain.Header.Get("ETag") == gz.Header.Get("ETag") {
		t.Errorf("identity and gzip share ETag %q", plain.Header.Get("ETag"))
	}
}

// Serving HTML from the precomputed table must still substitute the display
// lexicon — the tokens are resolved once at construction now rather than on
// every request, and a regression here would leak a raw __LEX_*__ marker.
func TestStatic_HTMLLexiconSubstitutionSurvivesPrecompute(t *testing.T) {
	ts := newTestServer(t)

	for _, path := range []string{"/", "/index.html", "/schema.html"} {
		resp := getWith(t, ts.URL+path, nil)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s: status %d", path, resp.StatusCode)
			continue
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("GET %s: Content-Type = %q", path, ct)
		}
		if bytes.Contains(body, []byte("__LEX_")) {
			t.Errorf("GET %s: unsubstituted lexicon token leaked into the served HTML", path)
		}
	}
}

func TestStatic_UnknownPathStill404s(t *testing.T) {
	ts := newTestServer(t)
	resp := getWith(t, ts.URL+"/no-such-asset.js", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for an absent asset, got %d", resp.StatusCode)
	}
}
