package api

// Unit cover for the API response-compression middleware. Driven with synthetic
// handlers rather than real endpoints so the awkward cases — a streaming
// response, an already-encoded body, a payload too small to be worth a gzip
// frame — are all reachable.

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serveThrough(h http.HandlerFunc, acceptEncoding string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/api/thing", nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	rec := httptest.NewRecorder()
	gzipMiddleware(h).ServeHTTP(rec, req)
	return rec
}

func jsonHandler(payload string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, payload)
	}
}

func TestGzipMiddleware_CompressesLargeJSON(t *testing.T) {
	payload := `{"orgs":[` + strings.Repeat(`{"prefix":"acme","display_name":"Acme"},`, 200) + `null]}`
	rec := serveThrough(jsonHandler(payload), "gzip")

	if enc := rec.Header().Get("Content-Encoding"); enc != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", enc)
	}
	if rec.Body.Len() >= len(payload) {
		t.Errorf("compressed %d B not smaller than source %d B", rec.Body.Len(), len(payload))
	}
	zr, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	got, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gzip decode: %v", err)
	}
	if string(got) != payload {
		t.Error("decoded body does not match what the handler wrote")
	}
}

// A gzip frame costs ~20 bytes of overhead, so compressing a short error
// response makes it bigger. Below the threshold the body must go out as-is.
func TestGzipMiddleware_SmallPayloadNotCompressed(t *testing.T) {
	payload := `{"error":{"code":"auth","message":"missing bearer token"}}`
	rec := serveThrough(jsonHandler(payload), "gzip")

	if enc := rec.Header().Get("Content-Encoding"); enc != "" {
		t.Errorf("Content-Encoding = %q on a %d-byte body; want identity", enc, len(payload))
	}
	if rec.Body.String() != payload {
		t.Errorf("body = %q, want %q", rec.Body.String(), payload)
	}
}

// /api/topics/stream is SSE. Buffering it behind the size threshold would stall
// events, so event streams must pass straight through and Flush must reach the
// underlying writer.
func TestGzipMiddleware_EventStreamPassesThroughAndFlushes(t *testing.T) {
	flushed := 0
	h := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for i := 0; i < 3; i++ {
			_, _ = io.WriteString(w, "data: tick\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
				flushed++
			}
		}
	}
	rec := serveThrough(h, "gzip")

	if enc := rec.Header().Get("Content-Encoding"); enc != "" {
		t.Errorf("Content-Encoding = %q on an event stream; must be identity", enc)
	}
	if flushed != 3 {
		t.Errorf("Flusher reached %d times, want 3", flushed)
	}
	if got := rec.Body.String(); got != strings.Repeat("data: tick\n\n", 3) {
		t.Errorf("stream body = %q", got)
	}
}

// The static handler serves its own precomputed gzip. Anything arriving here
// with Content-Encoding already set must not be encoded a second time.
func TestGzipMiddleware_DoesNotDoubleEncode(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 4096)
	h := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
	rec := serveThrough(h, "gzip")

	if enc := rec.Header().Values("Content-Encoding"); len(enc) != 1 || enc[0] != "gzip" {
		t.Errorf("Content-Encoding = %v, want exactly one gzip", enc)
	}
	if !bytes.Equal(rec.Body.Bytes(), body) {
		t.Error("pre-encoded body was re-encoded")
	}
}

func TestGzipMiddleware_NoAcceptEncodingLeavesBodyAlone(t *testing.T) {
	payload := strings.Repeat(`{"k":"v"},`, 500)
	rec := serveThrough(jsonHandler(payload), "")

	if enc := rec.Header().Get("Content-Encoding"); enc != "" {
		t.Errorf("Content-Encoding = %q with no Accept-Encoding", enc)
	}
	if rec.Body.String() != payload {
		t.Error("body altered for a client that did not ask for encoding")
	}
}

// A non-compressible type must be left alone however large it is.
func TestGzipMiddleware_SkipsNonCompressibleTypes(t *testing.T) {
	body := bytes.Repeat([]byte{0xff, 0xd8, 0xff}, 2048)
	h := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
	rec := serveThrough(h, "gzip")

	if enc := rec.Header().Get("Content-Encoding"); enc != "" {
		t.Errorf("Content-Encoding = %q on image/jpeg", enc)
	}
	if !bytes.Equal(rec.Body.Bytes(), body) {
		t.Error("jpeg body altered")
	}
}

func TestGzipMiddleware_NoContentStaysBodiless(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNoContent)
	}
	rec := serveThrough(h, "gzip")

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("204 carried %d bytes", rec.Body.Len())
	}
	if enc := rec.Header().Get("Content-Encoding"); enc != "" {
		t.Errorf("Content-Encoding = %q on a 204", enc)
	}
}
