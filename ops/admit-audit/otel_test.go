// otel_test.go — pure-function + in-memory-exporter coverage for the
// OpenTelemetry plumbing in otel.go (Block 27C / 50B.4 fourth producer).

package main

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
)

// installRecorder swaps in an SDK TracerProvider with an in-memory
// recorder for the duration of the test, then restores a no-op
// provider on cleanup so the global tracer reaches a quiescent state
// before goleak.VerifyTestMain inspects goroutines.
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

func TestOTLPHTTPOptionsFromEndpoint(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
		wantErr  bool
		wantOpts int
	}{
		{"https-no-path", "https://collector:4318", false, 1},
		{"http-explicit-path", "http://10.0.0.1:4318/v1/traces", false, 3},
		{"http-root-path", "http://collector:4318/", false, 2},
		{"missing-host", "://nope", true, 0},
		// Bare "host:port" parses as scheme=host, opaque=port — no Host
		// field. We require a real URL form; reject so callers see a
		// clear error rather than a silently-broken exporter.
		{"missing-scheme", "collector:4318", true, 0},
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
		"service.name":      "admit-audit",
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
	// service.instance.id is os.Hostname() — assert non-empty rather
	// than pin the value so test runs on any host.
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
	prev, hadPrev := os.LookupEnv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if err := os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT"); err != nil {
		t.Fatalf("unset env: %v", err)
	}
	t.Cleanup(func() {
		if hadPrev {
			_ = os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", prev)
		}
	})

	t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })

	shutdown, err := initOTel(context.Background())
	if err != nil {
		t.Fatalf("initOTel without endpoint should not error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("initOTel returned nil shutdown func")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		t.Errorf("shutdown returned error: %v", err)
	}
}

// TestStartPollSpan_RecordPollSpan_Success verifies the happy path:
// span name, summary attributes, status Ok.
func TestStartPollSpan_RecordPollSpan_Success(t *testing.T) {
	sr := installRecorder(t)

	_, span := startPollSpan(context.Background())
	recordPollSpan(span, "vega", 12, 2, 5, 3, 4, nil)
	span.End()

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	s := spans[0]
	if s.Name() != "admit-audit.poll" {
		t.Errorf("span name = %q, want admit-audit.poll", s.Name())
	}
	if s.Status().Code != codes.Ok {
		t.Errorf("span status = %v, want Ok", s.Status().Code)
	}

	attrs := attrMap(s.Attributes())
	if attrs[attrSiteName] != "vega" {
		t.Errorf("dbb.site_name = %q", attrs[attrSiteName])
	}
	if attrs[attrPeersTotal] != int64(12) {
		t.Errorf("dbb.peers_total = %v, want 12", attrs[attrPeersTotal])
	}
	if attrs[attrAdmitsDetected] != int64(2) {
		t.Errorf("dbb.admits_detected = %v, want 2", attrs[attrAdmitsDetected])
	}
	if attrs[attrPollOutcome] != attrPollOutcomeOK {
		t.Errorf("dbb.poll_outcome = %q, want %q", attrs[attrPollOutcome], attrPollOutcomeOK)
	}
	if attrs[attrWindowTotal] != int64(5) {
		t.Errorf("dbb.window_total = %v", attrs[attrWindowTotal])
	}
	if attrs[attrWindowPerUserMax] != int64(3) {
		t.Errorf("dbb.window_per_user_max = %v", attrs[attrWindowPerUserMax])
	}
	if attrs[attrWindowDistinctKeys] != int64(4) {
		t.Errorf("dbb.window_distinct_keys = %v", attrs[attrWindowDistinctKeys])
	}
}

// TestRecordPollSpan_Failure verifies the DB-error path: status Error,
// poll_outcome=db_query_failed, error recorded as a span event.
func TestRecordPollSpan_Failure(t *testing.T) {
	sr := installRecorder(t)
	wantErr := errors.New("synthetic db boom")

	_, span := startPollSpan(context.Background())
	recordPollSpan(span, "vega", 0, 0, 0, 0, 0, wantErr)
	span.End()

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	s := spans[0]
	if s.Status().Code != codes.Error {
		t.Errorf("span status = %v, want Error", s.Status().Code)
	}
	attrs := attrMap(s.Attributes())
	if attrs[attrPollOutcome] != attrPollOutcomeFailure {
		t.Errorf("dbb.poll_outcome = %q, want %q", attrs[attrPollOutcome], attrPollOutcomeFailure)
	}
	if len(s.Events()) == 0 {
		t.Errorf("expected RecordError to add an event; got 0")
	}
}

// TestRecordAdmitEvent verifies a per-admit span event lands on the
// poll-cycle span with the expected attribute set.
func TestRecordAdmitEvent(t *testing.T) {
	sr := installRecorder(t)

	_, span := startPollSpan(context.Background())
	now := time.Now()
	p := peerRow{
		ID:           "peer-abc",
		AccountID:    "acct-123",
		Key:          "wg-public-key",
		IP:           "100.127.0.5",
		Name:         "vega-orin-c",
		UserID:       "user-deddflanders",
		MetaHostname: "orin-c.vega.local",
	}
	recordAdmitEvent(span, p, now)
	recordPollSpan(span, "vega", 1, 1, 1, 1, 1, nil)
	span.End()

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	s := spans[0]
	events := s.Events()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	ev := events[0]
	if ev.Name != "admit" {
		t.Errorf("event name = %q, want admit", ev.Name)
	}
	em := attrMap(ev.Attributes)
	if em[attrEventPeerID] != "peer-abc" {
		t.Errorf("dbb.peer_id = %q", em[attrEventPeerID])
	}
	if em[attrEventAccountID] != "acct-123" {
		t.Errorf("dbb.account_id = %q", em[attrEventAccountID])
	}
	if em[attrEventSourceUser] != "user-deddflanders" {
		t.Errorf("dbb.source_user = %q", em[attrEventSourceUser])
	}
	if em[attrEventWGKey] != "wg-public-key" {
		t.Errorf("dbb.wg_key = %q", em[attrEventWGKey])
	}
	if em[attrEventPeerIP] != "100.127.0.5" {
		t.Errorf("dbb.peer_ip = %q", em[attrEventPeerIP])
	}
	if em[attrEventMetaHostname] != "orin-c.vega.local" {
		t.Errorf("dbb.meta_hostname = %q", em[attrEventMetaHostname])
	}
}

// TestInitOTel_LiveSDKShutdown brings up the full SDK against an
// unreachable endpoint, exercises a span through the public helpers,
// then shuts down. goleak.VerifyTestMain in main_test.go catches any
// goroutine the BatchSpanProcessor leaves behind on shutdown.
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

	// Exercise the span path. Exporter will fail to dial, but
	// BatchSpanProcessor swallows that — we just need the goroutine
	// surface up.
	_, span := startPollSpan(context.Background())
	recordPollSpan(span, "vega", 0, 0, 0, 0, 0, nil)
	span.End()

	shutdownCtx, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel2()
	if err := shutdown(shutdownCtx); err != nil {
		// Shutdown may surface the exporter's dial error; acceptable
		// as long as the function returns rather than hanging — the
		// goleak check validates the goroutine cleanup independent of
		// the error value.
		t.Logf("shutdown returned (non-fatal): %v", err)
	}
}

// TestStartPollSpan_NoOpProvider verifies that with the default no-op
// global provider (no SDK installed) startPollSpan + recordPollSpan
// + recordAdmitEvent run uniformly without panicking. This is the
// production "OTLP endpoint not configured" path.
func TestStartPollSpan_NoOpProvider(t *testing.T) {
	otel.SetTracerProvider(noop.NewTracerProvider())
	t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })

	_, span := startPollSpan(context.Background())
	recordAdmitEvent(span, peerRow{ID: "peer-1"}, time.Now())
	recordPollSpan(span, "vega", 1, 1, 1, 1, 1, nil)
	span.End()
	// Reaching here without panicking is the assertion.
}

// attrMap converts span attributes to a plain map for assertion
// convenience. Returns native Go types so test cases use the same
// types they pass in.
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
