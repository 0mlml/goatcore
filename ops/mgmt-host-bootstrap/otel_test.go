// otel_test.go — pure-function + in-memory-exporter coverage for the
// OpenTelemetry plumbing in otel.go (Block 27C / 50B.4 first producer).
//
// DB-touching paths (each cmd's RunE end-to-end) are covered in
// cmd_integration_test.go behind the integration build tag.

package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// installRecorder swaps in an SDK TracerProvider with an in-memory recorder
// for the duration of the test, then restores a no-op provider on cleanup so
// the global tracer reaches a quiescent state before goleak.VerifyTestMain
// inspects goroutines.
func installRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = tp.Shutdown(ctx)
		otel.SetTracerProvider(prev)
	})
	return sr
}

func TestDSNHost(t *testing.T) {
	cases := []struct {
		dsn  string
		want string
	}{
		// libpq key=value
		{`host=postgres-a port=5432 user=postgres password=secret dbname=netbird sslmode=disable`, "postgres-a"},
		{`port=5432 host=10.99.0.1 user=postgres password=secret dbname=netbird`, "10.99.0.1"},
		{`port=5432 user=postgres dbname=netbird`, ""}, // no host=

		// URL form
		{"postgres://postgres:secret@db.local:5432/netbird?sslmode=disable", "db.local"},
		{"postgresql://user:pw@10.0.0.1/db", "10.0.0.1"},

		// Edge cases
		{"", ""},
		{"not a dsn at all", ""},
	}
	for _, tc := range cases {
		t.Run(tc.dsn, func(t *testing.T) {
			got := dsnHost(tc.dsn)
			if got != tc.want {
				t.Errorf("dsnHost(%q) = %q, want %q", tc.dsn, got, tc.want)
			}
			// Sanity: never leaks the password regardless of input shape.
			if strings.Contains(got, "secret") || strings.Contains(got, "pw") {
				t.Errorf("dsnHost(%q) leaked credential material: %q", tc.dsn, got)
			}
		})
	}
}

func TestOTLPHTTPOptionsFromEndpoint(t *testing.T) {
	// Build a no-op TracerProvider context first so we don't leak via the
	// exporter constructor — we only validate the option construction here.
	cases := []struct {
		name      string
		endpoint  string
		wantErr   bool
		wantOpts  int    // expected option count (host always; +insecure for http; +path when non-/)
		expectSub string // sub-string we expect somewhere in the formatted endpoint
	}{
		{"https-no-path", "https://collector:4318", false, 1, "collector"},
		{"http-explicit-path", "http://10.0.0.1:4318/v1/traces", false, 3, "10.0.0.1"},
		{"http-root-path", "http://collector:4318/", false, 2, "collector"},
		{"missing-host", "://nope", true, 0, ""},
		// Bare "host:port" parses as scheme=host, opaque=port — no Host
		// field. We require a real URL form; reject so callers see a clear
		// error rather than a silently-broken exporter.
		{"missing-scheme", "collector:4318", true, 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := otlpHTTPOptionsFromEndpoint(tc.endpoint)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if len(opts) != tc.wantOpts {
				t.Errorf("got %d opts, want %d", len(opts), tc.wantOpts)
			}
		})
	}
}

func TestBuildResource(t *testing.T) {
	t.Setenv("DBB_SITE_ID", "vega")
	t.Setenv("DBB_SITE_ROLE", "mgmt")

	res := buildResource()
	if res == nil {
		t.Fatal("buildResource returned nil")
	}

	want := map[string]string{
		"service.name":      "mgmt-host-bootstrap",
		"service.version":   version,
		"service.namespace": "vega",
		"dbb.site_role":     "mgmt",
	}
	got := map[string]string{}
	for _, kv := range res.Attributes() {
		got[string(kv.Key)] = kv.Value.AsString()
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("resource attr %q = %q, want %q", k, got[k], v)
		}
	}
	// service.instance.id is os.Hostname() — assert non-empty rather than
	// pin the value so test runs on any host.
	if got["service.instance.id"] == "" {
		t.Errorf("service.instance.id should be non-empty")
	}
}

func TestBuildResource_Defaults(t *testing.T) {
	t.Setenv("DBB_SITE_ID", "")
	t.Setenv("DBB_SITE_ROLE", "")

	res := buildResource()
	got := map[string]string{}
	for _, kv := range res.Attributes() {
		got[string(kv.Key)] = kv.Value.AsString()
	}
	if got["service.namespace"] != "unknown" {
		t.Errorf("default service.namespace = %q, want %q", got["service.namespace"], "unknown")
	}
	if got["dbb.site_role"] != "mgmt" {
		t.Errorf("default dbb.site_role = %q, want %q", got["dbb.site_role"], "mgmt")
	}
}

func TestInitOTel_NoEndpoint(t *testing.T) {
	// Save + restore env around the test.
	prev, hadPrev := os.LookupEnv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if err := os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT"); err != nil {
		t.Fatalf("unset env: %v", err)
	}
	t.Cleanup(func() {
		if hadPrev {
			_ = os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", prev)
		}
	})

	// Reset global at test exit so a stray real provider doesn't leak.
	t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })

	shutdown, err := initOTel(context.Background())
	if err != nil {
		t.Fatalf("initOTel without endpoint should not error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("initOTel returned nil shutdown func")
	}
	// Calling it must be a no-op + non-error.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		t.Errorf("shutdown returned error: %v", err)
	}
}

// TestWithSpan_Success verifies the happy path: span name, dbb.cli_subcommand
// attribute, dbb.exit_code=0, status Ok, no recorded error.
func TestWithSpan_Success(t *testing.T) {
	sr := installRecorder(t)

	called := false
	err := withSpan(context.Background(), "create-account",
		[]attribute.KeyValue{attribute.String(attrDSNHost, "postgres-a")},
		func(ctx context.Context, span trace.Span) error {
			called = true
			span.SetAttributes(attribute.Int64(attrRecordsAffected, 2))
			return nil
		})
	if err != nil {
		t.Fatalf("withSpan returned error: %v", err)
	}
	if !called {
		t.Fatal("fn was not called")
	}

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	s := spans[0]
	if s.Name() != "mgmt-host-bootstrap.create-account" {
		t.Errorf("span name = %q", s.Name())
	}
	if s.Status().Code != codes.Ok {
		t.Errorf("span status = %v, want Ok", s.Status().Code)
	}

	attrs := attrMap(s.Attributes())
	if attrs[attrCLISubcommand] != "create-account" {
		t.Errorf("dbb.cli_subcommand = %q", attrs[attrCLISubcommand])
	}
	if attrs[attrDSNHost] != "postgres-a" {
		t.Errorf("dbb.dsn_host = %q", attrs[attrDSNHost])
	}
	if attrs[attrExitCode] != int64(0) {
		t.Errorf("dbb.exit_code = %v, want 0", attrs[attrExitCode])
	}
	if attrs[attrRecordsAffected] != int64(2) {
		t.Errorf("dbb.records_affected = %v, want 2", attrs[attrRecordsAffected])
	}
	if _, present := attrs[attrAlreadyApplied]; present {
		t.Errorf("dbb.already_applied unexpectedly set on success path")
	}
}

// TestWithSpan_Error verifies error path: span records error, dbb.exit_code=1,
// status Error, and the error propagates back to the caller unchanged.
func TestWithSpan_Error(t *testing.T) {
	sr := installRecorder(t)
	wantErr := errors.New("synthetic db boom")

	err := withSpan(context.Background(), "revoke-peer", nil,
		func(ctx context.Context, span trace.Span) error {
			return wantErr
		})
	if !errors.Is(err, wantErr) {
		t.Fatalf("withSpan returned %v, want wrapping %v", err, wantErr)
	}

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	s := spans[0]
	if s.Status().Code != codes.Error {
		t.Errorf("span status = %v, want Error", s.Status().Code)
	}
	attrs := attrMap(s.Attributes())
	if attrs[attrExitCode] != int64(1) {
		t.Errorf("dbb.exit_code = %v, want 1", attrs[attrExitCode])
	}
	if len(s.Events()) == 0 {
		t.Errorf("expected RecordError to add an event; got 0")
	}
}

// TestWithSpan_AlreadyApplied verifies the idempotent-replay path: span gets
// dbb.already_applied=true + dbb.exit_code=0 + status Ok, and withSpan
// returns nil to cobra (so the CLI exits 0 — matches existing UX).
func TestWithSpan_AlreadyApplied(t *testing.T) {
	sr := installRecorder(t)

	err := withSpan(context.Background(), "create-setup-key", nil,
		func(ctx context.Context, span trace.Span) error {
			return errAlreadyApplied
		})
	if err != nil {
		t.Fatalf("withSpan should swallow errAlreadyApplied; got %v", err)
	}

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	s := spans[0]
	if s.Status().Code != codes.Ok {
		t.Errorf("span status = %v, want Ok", s.Status().Code)
	}
	attrs := attrMap(s.Attributes())
	if attrs[attrAlreadyApplied] != true {
		t.Errorf("dbb.already_applied = %v, want true", attrs[attrAlreadyApplied])
	}
	if attrs[attrExitCode] != int64(0) {
		t.Errorf("dbb.exit_code = %v, want 0", attrs[attrExitCode])
	}
}

// TestInitOTel_LiveSDKShutdown brings up the full SDK against an unreachable
// endpoint, exercises a span through withSpan, then shuts down. The
// goleak.VerifyTestMain in goleak_test.go catches any goroutine the
// BatchSpanProcessor leaves behind on shutdown.
func TestInitOTel_LiveSDKShutdown(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1") // unreachable on purpose

	prev := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdown, err := initOTel(ctx)
	if err != nil {
		t.Fatalf("initOTel: %v", err)
	}

	// Exercise the span path. Exporter will fail to dial, but BatchSpan-
	// Processor swallows that — we just need the goroutine surface up.
	if werr := withSpan(context.Background(), "list-peers", nil,
		func(ctx context.Context, span trace.Span) error {
			span.SetAttributes(attribute.Int64(attrRecordsAffected, 0))
			return nil
		}); werr != nil {
		t.Fatalf("withSpan: %v", werr)
	}

	shutdownCtx, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel2()
	if err := shutdown(shutdownCtx); err != nil {
		// Shutdown may surface the exporter's dial error; acceptable as long
		// as the function returns rather than hanging — the goleak check
		// validates the goroutine cleanup independent of the error value.
		t.Logf("shutdown returned (non-fatal): %v", err)
	}
}

// TestWithSpan_NoOpProvider verifies that with the default no-op global
// provider (no SDK installed) withSpan still runs fn and returns its result.
// This is the production "OTLP endpoint not configured" path — must not panic.
func TestWithSpan_NoOpProvider(t *testing.T) {
	otel.SetTracerProvider(noop.NewTracerProvider())
	t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })

	called := false
	err := withSpan(context.Background(), "list-peers", nil,
		func(ctx context.Context, span trace.Span) error {
			called = true
			// The noop span should accept SetAttributes without panicking.
			span.SetAttributes(attribute.Int64(attrRecordsAffected, 0))
			return nil
		})
	if err != nil {
		t.Fatalf("withSpan returned error: %v", err)
	}
	if !called {
		t.Fatal("fn was not called")
	}
}

// attrMap converts span attributes to a plain map for assertion convenience.
// Returns native Go types (string, int64, bool, float64) so test cases use
// the same types they pass in.
func attrMap(kvs []attribute.KeyValue) map[string]any {
	m := make(map[string]any, len(kvs))
	for _, kv := range kvs {
		switch kv.Value.Type() {
		case attribute.STRING:
			m[string(kv.Key)] = kv.Value.AsString()
		case attribute.INT64:
			m[string(kv.Key)] = kv.Value.AsInt64()
		case attribute.BOOL:
			m[string(kv.Key)] = kv.Value.AsBool()
		case attribute.FLOAT64:
			m[string(kv.Key)] = kv.Value.AsFloat64()
		default:
			m[string(kv.Key)] = kv.Value.String()
		}
	}
	return m
}
