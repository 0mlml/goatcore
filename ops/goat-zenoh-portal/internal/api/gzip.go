package api

import (
	"compress/gzip"
	"net/http"
	"strings"
	"sync"
)

// gzipMiddleware compresses eligible responses on the fly.
//
// The static frontend does NOT come through here — staticHandler serves its own
// precomputed gzip (the asset set is fixed, so compressing it per request would
// burn CPU for a result known at startup). This covers the API, where the
// payloads are generated per request: /api/orgs alone was 63 KB of JSON on the
// wire with no encoding negotiated.
//
// Three behaviours worth stating, because each is a way this kind of middleware
// usually goes wrong:
//
//   - text/event-stream is passed through uncompressed. /api/topics/stream is a
//     real SSE endpoint; buffering it would stall events behind the threshold
//     check below and turn a live tail into a hang.
//   - A response that already carries Content-Encoding is left alone, so nothing
//     double-encodes.
//   - Flush is forwarded (through the gzip writer when compressing), so any
//     handler that streams keeps streaming.
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !acceptsGzip(r) {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzipResponseWriter{ResponseWriter: w}
		defer gw.Close()
		next.ServeHTTP(gw, r)
	})
}

var gzipWriterPool = sync.Pool{
	New: func() any { return gzip.NewWriter(nil) },
}

// gzipResponseWriter decides per response whether to compress. It holds the
// first gzipMinSize bytes back so a small payload (a 59-byte JSON error) is not
// inflated by a gzip frame's overhead; once the body proves larger than the
// threshold it switches to the compressed path and never buffers again.
type gzipResponseWriter struct {
	http.ResponseWriter

	status      int
	wroteHeader bool
	// eligible is set at WriteHeader time from the response's own Content-Type.
	eligible bool
	// buf holds the pre-threshold bytes while we decide.
	buf      []byte
	decided  bool
	zw       *gzip.Writer
	pristine bool // true once we have committed to identity encoding
}

func (g *gzipResponseWriter) WriteHeader(status int) {
	if g.wroteHeader {
		return
	}
	g.wroteHeader = true
	g.status = status

	h := g.Header()
	ct := h.Get("Content-Type")
	g.eligible = h.Get("Content-Encoding") == "" &&
		compressible(ct) &&
		!strings.HasPrefix(strings.TrimSpace(ct), "text/event-stream") &&
		bodyAllowed(status)

	if !g.eligible {
		g.decided, g.pristine = true, true
		g.ResponseWriter.WriteHeader(status)
		return
	}
	// Header is deferred until the threshold decision, because it determines
	// whether Content-Encoding goes out at all.
	if !varies(h, "Accept-Encoding") {
		h.Add("Vary", "Accept-Encoding")
	}
}

// varies reports whether Vary already names the header.
func varies(h http.Header, name string) bool {
	for _, v := range h.Values("Vary") {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), name) {
				return true
			}
		}
	}
	return false
}

// bodyAllowed reports whether a status can carry a body worth compressing.
func bodyAllowed(status int) bool {
	switch {
	case status == http.StatusNoContent, status == http.StatusNotModified:
		return false
	case status == http.StatusPartialContent:
		// A 206 body is a byte range of the identity representation; encoding it
		// would make the range meaningless against the Content-Range offsets.
		return false
	case status >= 100 && status < 200:
		return false
	default:
		return true
	}
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	if !g.wroteHeader {
		g.WriteHeader(http.StatusOK)
	}
	if g.pristine {
		return g.ResponseWriter.Write(b)
	}
	if g.zw != nil {
		return g.zw.Write(b)
	}
	// Still deciding: accumulate until the threshold is crossed.
	g.buf = append(g.buf, b...)
	if len(g.buf) < gzipMinSize {
		return len(b), nil
	}
	if err := g.startGzip(); err != nil {
		return 0, err
	}
	return len(b), nil
}

// startGzip commits to the compressed path and flushes whatever was buffered
// into the gzip writer.
func (g *gzipResponseWriter) startGzip() error {
	g.decided = true
	h := g.Header()
	h.Set("Content-Encoding", "gzip")
	// The identity body's validator does not name this representation
	// (RFC 9110 §8.8.1); derive the gzip tag the way the static table does.
	if tag := h.Get("ETag"); tag != "" {
		h.Set("ETag", gzipETag(tag))
	}
	// The length of the identity body is no longer the length on the wire, and
	// we do not know the compressed length until we are done.
	h.Del("Content-Length")
	g.ResponseWriter.WriteHeader(g.status)

	zw := gzipWriterPool.Get().(*gzip.Writer)
	zw.Reset(g.ResponseWriter)
	g.zw = zw
	if len(g.buf) > 0 {
		_, err := zw.Write(g.buf)
		g.buf = nil
		return err
	}
	return nil
}

// commitIdentity writes a below-threshold body out uncompressed. Vary stays on
// the response: this URL still varies by Accept-Encoding in general (a larger
// body from the same handler would be compressed), so a shared cache must keep
// keying on it. It is also not ours alone to remove — deleting the header would
// drop any Vary another layer had set.
func (g *gzipResponseWriter) commitIdentity() {
	g.decided, g.pristine = true, true
	g.ResponseWriter.WriteHeader(g.status)
	if len(g.buf) > 0 {
		_, _ = g.ResponseWriter.Write(g.buf)
		g.buf = nil
	}
}

func (g *gzipResponseWriter) Flush() {
	if !g.wroteHeader {
		g.WriteHeader(http.StatusOK)
	}
	// A handler asking to flush wants bytes on the wire now, so stop waiting for
	// the size threshold and commit.
	if !g.decided {
		if err := g.startGzip(); err != nil {
			return
		}
	}
	if g.zw != nil {
		_ = g.zw.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (g *gzipResponseWriter) Close() {
	if !g.wroteHeader {
		// Handler wrote nothing at all; let the stdlib emit its own 200.
		return
	}
	if !g.decided {
		g.commitIdentity()
		return
	}
	if g.zw != nil {
		_ = g.zw.Close()
		gzipWriterPool.Put(g.zw)
		g.zw = nil
	}
}
