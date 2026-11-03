package api

// The controls the security pass on ADR 1184 added: a backstop on every
// request body, a bound on what the ETag layer holds, store faults that never
// reach a caller as text, a request id that carries nothing, an access log
// that records every refusal, and a portal that never produces a 429 of its
// own (the proxy's errors middleware would replace it).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

func TestBackstop_EveryAPIBodyIsBounded(t *testing.T) {
	var got error
	h := func(w http.ResponseWriter, r *http.Request) {
		_, got = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	}
	body := bytes.Repeat([]byte("x"), maxRequestBody+1)
	rec := httptest.NewRecorder()
	chain(t, h).ServeHTTP(rec, httptest.NewRequest("POST", "/api/anything", bytes.NewReader(body)))
	var tooLarge *http.MaxBytesError
	if !errors.As(got, &tooLarge) {
		t.Fatalf("a %d-byte body was read whole under /api: err = %v", len(body), got)
	}

	got = nil
	rec = httptest.NewRecorder()
	chain(t, h).ServeHTTP(rec, httptest.NewRequest("POST", "/api/anything", bytes.NewReader(body[:maxRequestBody])))
	if got != nil {
		t.Errorf("a body at the cap was refused: %v", got)
	}
}

func TestBackstop_UncappedDecoderAnswers413(t *testing.T) {
	var req struct {
		Content string `json:"content"`
	}
	h := func(w http.ResponseWriter, r *http.Request) {
		if err := decodeJSON(w, r, &req); err != nil {
			writeBodyError(w, err, "bad_request", "body must be JSON")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
	big := `{"content":"` + strings.Repeat("x", maxJSONBody) + `"}`
	rec := httptest.NewRecorder()
	chain(t, h).ServeHTTP(rec, httptest.NewRequest("POST", "/api/x", strings.NewReader(big)))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	var p Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || p.Code != "payload_too_large" {
		t.Errorf("body = %s (%v)", rec.Body.String(), err)
	}

	rec = httptest.NewRecorder()
	chain(t, h).ServeHTTP(rec, httptest.NewRequest("POST", "/api/x", strings.NewReader("not json")))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"code":"bad_request"`) {
		t.Errorf("malformed body: %d %s", rec.Code, rec.Body.String())
	}
}

func TestETag_BodyPastTheCapStreamsWithoutAValidator(t *testing.T) {
	big := bytes.Repeat([]byte("y"), maxETagBody+1)
	h := revalidated(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		// Two writes, so the spill happens with bytes already held.
		_, _ = w.Write(big[:maxETagBody])
		_, _ = w.Write(big[maxETagBody:])
	})
	rec := httptest.NewRecorder()
	conditionalMiddleware(h).ServeHTTP(rec, httptest.NewRequest("GET", "/api/big", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if tag := rec.Header().Get("ETag"); tag != "" {
		t.Errorf("a body past the cap carries ETag %q", tag)
	}
	if !bytes.Equal(rec.Body.Bytes(), big) {
		t.Errorf("body: %d bytes, want %d", rec.Body.Len(), len(big))
	}

	small := revalidated(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(big[:maxETagBody])
	})
	rec = httptest.NewRecorder()
	conditionalMiddleware(small).ServeHTTP(rec, httptest.NewRequest("GET", "/api/small", nil))
	if rec.Header().Get("ETag") == "" || rec.Body.Len() != maxETagBody {
		t.Errorf("a body at the cap: ETag %q, %d bytes", rec.Header().Get("ETag"), rec.Body.Len())
	}
}

func TestRegistryError_StoreFaultIsRedacted(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("X-Request-Id", "rq-test")
	writeRegistryError(rec, errors.New("insert topic: UNIQUE constraint failed: topics.key_pattern"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "UNIQUE") || !strings.Contains(rec.Body.String(), serverFaultDetail["db"]) {
		t.Errorf("driver text on the wire: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	_, err := (&registry.Registry{}).CreateTopic(context.Background(), &registry.Topic{VendorPrefix: "-bad-"})
	if !errors.Is(err, registry.ErrInvalid) {
		t.Fatalf("a validation failure does not answer to ErrInvalid: %v", err)
	}
	writeRegistryError(rec, err)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"code":"validate"`) {
		t.Errorf("validation: %d %s", rec.Code, rec.Body.String())
	}
}

var bootShape = regexp.MustCompile(`^rq-\d+-\d+$`)

func TestRequestID_CarriesNothing(t *testing.T) {
	s := &Server{}
	a, b := s.nextRequestID(), s.nextRequestID()
	if a == b || !strings.HasPrefix(a, "rq-") || len(a) != 19 {
		t.Errorf("ids = %q, %q", a, b)
	}
	if bootShape.MatchString(a) {
		t.Errorf("id %q is the boot-time shape", a)
	}
}

func TestAccessLog_EveryRefusalIsRecorded(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }
	rec := httptest.NewRecorder()
	chain(t, ok).ServeHTTP(rec, httptest.NewRequest("GET", "/api/fine", nil))
	if strings.Contains(buf.String(), `"msg":"access"`) {
		t.Errorf("a success was logged at Info: %s", buf.String())
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/fine", nil)
	req.Header.Set("Api-Version", "9.0.0")
	chain(t, ok).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	line := buf.String()
	if !strings.Contains(line, `"msg":"access"`) || !strings.Contains(line, `"status":400`) ||
		!strings.Contains(line, `"request_id":"`+rec.Header().Get("X-Request-Id")+`"`) {
		t.Errorf("the version refusal left no access line: %s", line)
	}
	if strings.Contains(line, "9.0.0") {
		t.Errorf("the supplied version was echoed into the log: %s", line)
	}
}

func TestStdlibRewrite_DropsAGzipCoding(t *testing.T) {
	sw := &statusWriter{ResponseWriter: httptest.NewRecorder(), api: true}
	sw.Header().Set("Content-Type", "text/plain; charset=utf-8")
	sw.Header().Set("Content-Encoding", "gzip")
	sw.WriteHeader(http.StatusNotFound)
	if ce := sw.Header().Get("Content-Encoding"); ce != "" {
		t.Errorf("Content-Encoding = %q on a rewritten body", ce)
	}
}

// The proxy's errors middleware replaces every 429 that passes through it
// with the sidecar's page, the portal's own included. So the portal must not
// produce one; its rate limiting is the proxy's.
func TestPortal_NeverProducesA429(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(src, []byte("StatusTooManyRequests")) || bytes.Contains(src, []byte("WriteHeader(429)")) {
			t.Errorf("%s produces a 429; the errors middleware would replace it", name)
		}
	}
}
