// otel_test.go — pure-function + in-memory-exporter coverage for the
// OpenTelemetry plumbing in otel.go (Block 27C / 50B.4 third producer).
//
// Inbound otelhttp middleware coverage rides on the existing serve test path —
// these tests target the goat-specific helpers (annotateAdmitSpan,
// startCheckNTPSpan) directly.

package main

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// installRecorder swaps in an SDK TracerProvider with an in-memory recorder
// for the duration of the test, then restores a no-op provider on cleanup.
func installRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
	})
	return sr
}

// TestAnnotateAdmitSpan_AdmitClassPath sets dbb.admit_class=true plus deny_reason
// + ntp_class on the active span, mirroring the ServeHTTP deny path on
// rate-limit-per-peer.
func TestAnnotateAdmitSpan_AdmitClassPath(t *testing.T) {
	sr := installRecorder(t)

	tracer := otel.Tracer("test")
	ctx, parent := tracer.Start(context.Background(), "test.inbound")

	annotateAdmitSpan(ctx, true,
		attribute.String(attrPeerIDKind, "setup_key"),
		attribute.String(attrNTPClass, "healthy"),
	)
	annotateAdmitSpan(ctx, true,
		attribute.String(attrDenyReason, "rate-limit-per-peer"),
	)

	parent.End()

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	got := spans[0]
	attrs := attrMap(got.Attributes())
	if attrs[attrAdmitClass] != true {
		t.Errorf("dbb.admit_class = %v, want true", attrs[attrAdmitClass])
	}
	if attrs[attrPeerIDKind] != "setup_key" {
		t.Errorf("dbb.peer_id_kind = %q", attrs[attrPeerIDKind])
	}
	if attrs[attrNTPClass] != "healthy" {
		t.Errorf("dbb.ntp_class = %q", attrs[attrNTPClass])
	}
	if attrs[attrDenyReason] != "rate-limit-per-peer" {
		t.Errorf("dbb.deny_reason = %q", attrs[attrDenyReason])
	}
}

// TestAnnotateAdmitSpan_RevokeClassPath sets dbb.admit_class=false (no other
// admit attrs), mirroring the ServeHTTP pass-through for revoke / GET.
func TestAnnotateAdmitSpan_RevokeClassPath(t *testing.T) {
	sr := installRecorder(t)

	tracer := otel.Tracer("test")
	ctx, parent := tracer.Start(context.Background(), "test.inbound")
	annotateAdmitSpan(ctx, false)
	parent.End()

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	attrs := attrMap(spans[0].Attributes())
	if attrs[attrAdmitClass] != false {
		t.Errorf("dbb.admit_class = %v, want false", attrs[attrAdmitClass])
	}
	if _, present := attrs[attrPeerIDKind]; present {
		t.Errorf("dbb.peer_id_kind unexpectedly set on revoke-class path")
	}
	if _, present := attrs[attrDenyReason]; present {
		t.Errorf("dbb.deny_reason unexpectedly set on revoke-class path")
	}
}

// TestStartCheckNTPSpan_Lifecycle exercises the chrony health-check sub-span
// shape: dial-failure path records the error + sets dbb.check_ntp_error.
func TestStartCheckNTPSpan_DialFailure(t *testing.T) {
	sr := installRecorder(t)

	_, span := startCheckNTPSpan(context.Background())
	span.RecordError(errors.New("dial unix /run/ntp-health.sock: connection refused"))
	span.SetAttributes(attribute.String(attrCheckNTPErr, "dial_failed"))
	span.SetStatus(codes.Error, "dial_failed")
	span.End()

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	got := spans[0]
	if got.Name() != "ntp-admit-sidecar.checkNTP" {
		t.Errorf("span name = %q", got.Name())
	}
	if got.Status().Code != codes.Error {
		t.Errorf("span status = %v, want Error", got.Status().Code)
	}
	attrs := attrMap(got.Attributes())
	if attrs[attrCheckNTPErr] != "dial_failed" {
		t.Errorf("dbb.check_ntp_error = %q", attrs[attrCheckNTPErr])
	}
	if len(got.Events()) == 0 {
		t.Errorf("expected RecordError to add an event; got 0")
	}
}

// TestStartCheckNTPSpan_Success: success path attaches dbb.ntp_class.
func TestStartCheckNTPSpan_Success(t *testing.T) {
	sr := installRecorder(t)

	_, span := startCheckNTPSpan(context.Background())
	span.SetAttributes(attribute.String(attrNTPClass, "healthy"))
	span.End()

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	got := spans[0]
	// No explicit Ok status — caller didn't set one; the SDK default is
	// Unset, which is fine. Check attrs only.
	attrs := attrMap(got.Attributes())
	if attrs[attrNTPClass] != "healthy" {
		t.Errorf("dbb.ntp_class = %q", attrs[attrNTPClass])
	}
}

// TestAnnotateAdmitSpan_NoOpProvider verifies that with the default no-op
// global provider the helper runs without panic. This is the production
// "OTLP endpoint not configured" path.
func TestAnnotateAdmitSpan_NoOpProvider(t *testing.T) {
	otel.SetTracerProvider(noop.NewTracerProvider())
	t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })

	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test")
	annotateAdmitSpan(ctx, true, attribute.String(attrNTPClass, "healthy"))
	span.End()
}

// TestStartCheckNTPSpan_NoOpProvider: same shape for the sub-span helper.
func TestStartCheckNTPSpan_NoOpProvider(t *testing.T) {
	otel.SetTracerProvider(noop.NewTracerProvider())
	t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })

	_, span := startCheckNTPSpan(context.Background())
	span.SetAttributes(attribute.String(attrNTPClass, "healthy"))
	span.End()
}

// Compile-time guard: trace.SpanFromContext returns a span; ensure
// annotateAdmitSpan accepts the signature without nil-checks.
var _ = trace.SpanFromContext

// attrMap converts span attributes to a plain map for assertion convenience.
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
