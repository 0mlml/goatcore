package api

// The API's streaming endpoints depend on a flusher surviving the whole
// middleware chain. It did not: statusWriter embeds the http.ResponseWriter
// INTERFACE, which promotes Header/Write/WriteHeader and nothing else, so
// `w.(http.Flusher)` was false no matter what the real writer could do.
// StreamSSE takes that assertion at its top and bails, so /api/topics/stream
// answered 200 with an empty body and never delivered an event.

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Every wrapper the response passes through must keep Flush reachable. Assert
// it per layer so a future wrapper that drops it names itself here rather than
// silently turning streaming back off.
func TestStreaming_FlusherSurvivesEachWrapper(t *testing.T) {
	rec := httptest.NewRecorder() // the stand-in for the real ResponseWriter

	sw := &statusWriter{ResponseWriter: rec, status: http.StatusOK, principal: "anon"}
	if _, ok := interface{}(sw).(http.Flusher); !ok {
		t.Error("statusWriter does not implement http.Flusher — SSE cannot stream through it")
	}

	gw := &gzipResponseWriter{ResponseWriter: sw}
	if _, ok := interface{}(gw).(http.Flusher); !ok {
		t.Error("gzipResponseWriter does not implement http.Flusher")
	}
}

// Reaching the interface is not enough — the call has to arrive at the bottom.
// This drives the real chain (statusWriter under gzipResponseWriter, the order
// Routes() builds) and checks the recorder actually saw a flush.
func TestStreaming_FlushReachesTheUnderlyingWriter(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec, status: http.StatusOK, principal: "anon"}
	gw := &gzipResponseWriter{ResponseWriter: sw}

	gw.Header().Set("Content-Type", "text/event-stream")
	gw.WriteHeader(http.StatusOK)
	if _, err := gw.Write([]byte(": heartbeat\n\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	gw.Flush()

	if !rec.Flushed {
		t.Error("Flush did not reach the underlying writer — events would sit in the buffer")
	}
	if got := rec.Body.String(); got != ": heartbeat\n\n" {
		t.Errorf("body = %q, want the heartbeat written through unmodified", got)
	}
	if enc := rec.Header().Get("Content-Encoding"); enc != "" {
		t.Errorf("event stream got Content-Encoding %q; must stay identity", enc)
	}
}
