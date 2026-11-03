package api

// The full wrapper chain Routes() builds (middleware, gzip, conditional),
// driven with handlers that stand in for the mux: a stream must still reach
// the client through every layer, a 5xx must not carry the handler's raw
// message, and the health 503 is a problem body with its health members.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/config"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/pgtest"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

// chain is Routes() with the given handler in place of the mux.
func chain(t *testing.T, h http.HandlerFunc) http.Handler {
	t.Helper()
	cfg, err := config.LoadFromFlags(nil)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	s := &Server{cfg: cfg}
	return s.middleware(gzipMiddleware(conditionalMiddleware(h)))
}

func TestChain_EventStreamStillFlushesThroughEveryWrapper(t *testing.T) {
	flushed := 0
	h := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for i := 0; i < 3; i++ {
			_, _ = io.WriteString(w, "data: tick\n\n")
			http.NewResponseController(w).Flush()
			flushed++
		}
	}
	srv := httptest.NewServer(chain(t, h))
	defer srv.Close()
	req, _ := http.NewRequest("GET", srv.URL+"/api/topics/stream", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := (&http.Client{Transport: &http.Transport{DisableCompression: true}}).Do(req)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if got := string(body); got != strings.Repeat("data: tick\n\n", 3) {
		t.Errorf("stream body = %q", got)
	}
	if enc := resp.Header.Get("Content-Encoding"); enc != "" {
		t.Errorf("Content-Encoding = %q on an event stream", enc)
	}
	if flushed != 3 {
		t.Errorf("flushed %d times, want 3", flushed)
	}
}

func TestChain_ServerFaultDetailIsFixedAndLogged(t *testing.T) {
	raw := "open /var/lib/portal/schemas/x.fds: no such file or directory"
	h := func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusInternalServerError, "store", raw)
	}
	rec := httptest.NewRecorder()
	chain(t, h).ServeHTTP(rec, httptest.NewRequest("GET", "/api/x", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	var p Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("body: %v: %s", err, rec.Body.String())
	}
	if p.Detail != serverFaultDetail["store"] || p.Error.Message != p.Detail {
		t.Errorf("detail = %q, legacy message = %q", p.Detail, p.Error.Message)
	}
	if strings.Contains(rec.Body.String(), "/var/lib") {
		t.Errorf("raw error reached the wire: %s", rec.Body.String())
	}
}

func TestChain_PanicStaysGeneric(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) { panic("secret: dsn=postgres://x") }
	rec := httptest.NewRecorder()
	chain(t, h).ServeHTTP(rec, httptest.NewRequest("GET", "/api/x", nil))
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "dsn=") {
		t.Errorf("status %d body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"internal server error"`) {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestChain_NoSniffOnEveryResponse(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, map[string]int{"n": 1}) }
	rec := httptest.NewRecorder()
	chain(t, h).ServeHTTP(rec, httptest.NewRequest("GET", "/index.html", nil))
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if rec.Header().Get("Api-Version") != "" {
		t.Error("Api-Version stamped outside /api")
	}
}

func TestHealth_503IsAProblemWithHealthMembers(t *testing.T) {
	cfg, err := config.LoadFromFlags(nil)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	reg, err := registry.Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	_ = reg.Close() // every probe now fails with database text
	s := &Server{cfg: cfg, reg: reg}
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/health", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != problemContentType {
		t.Errorf("Content-Type = %q", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if body["code"] != "registry_unavailable" || body["status"] != float64(503) {
		t.Errorf("code/status = %v/%v", body["code"], body["status"])
	}
	if body["detail"] != "registry probe failed" || body["reason"] != "registry probe failed" {
		t.Errorf("detail/reason = %v/%v", body["detail"], body["reason"])
	}
	if body["registry"] != "unavailable" || body["health"] != "unhealthy" || body["version"] != APIVersion || body["uptime"] == nil {
		t.Errorf("health members missing: %v", body)
	}
	if s := rec.Body.String(); strings.Contains(s, "sql") || strings.Contains(s, "closed") {
		t.Errorf("probe error reached the wire: %s", s)
	}
}

func TestVersion_MajorComparison(t *testing.T) {
	cases := map[string]bool{"": true, "0.2.0": true, "0.9": true, "0": true, "1.0.0": false, "9.7.junk": false, "junk": false, "-1": false}
	for in, want := range cases {
		if got := apiVersionAccepted(in); got != want {
			t.Errorf("apiVersionAccepted(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestValidate_CappedNamesFieldOnly(t *testing.T) {
	long := strings.Repeat("é", 33)
	err := capped(fieldCap{"format", "json", maxFormat}, fieldCap{"format", long, maxFormat})
	if err == nil || strings.Contains(err.Error(), long) || !strings.Contains(err.Error(), "format must be at most 32") {
		t.Errorf("err = %v", err)
	}
	if capped(fieldCap{"format", strings.Repeat("é", 32), maxFormat}) != nil {
		t.Error("32 runes must pass a 32 cap")
	}
}
