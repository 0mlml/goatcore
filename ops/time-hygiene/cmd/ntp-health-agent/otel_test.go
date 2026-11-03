// otel_test.go — pure-function + in-memory-exporter coverage for the
// OpenTelemetry plumbing in otel.go (Block 27C / 50B.4 second producer).

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/usareur-af/goatcore/ops/time-hygiene/internal/state"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
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
		otel.SetTracerProvider(prev)
	})
	return sr
}

// TestPollSpan_Success: nil err → status Ok, dbb.poll_outcome=success,
// dbb.ntp_class echoes the recorded NtpState.
func TestPollSpan_Success(t *testing.T) {
	sr := installRecorder(t)

	s := state.NtpState{
		HostID:         "vega-A",
		NtpClass:       state.ClassHealthy,
		NtpClassStr:    state.ClassHealthy.String(),
		SelectedSource: state.SourcePeerNTS,
		Stratum:        2,
		RefID:          "192.168.1.1",
		RmsOffsetNs:    1234,
	}
	_, span := startPollSpan(context.Background())
	recordPollSpan(span, "vega-A", s, 0, nil)
	span.End()

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	got := spans[0]
	if got.Name() != "ntp-health-agent.poll" {
		t.Errorf("span name = %q", got.Name())
	}
	if got.Status().Code != codes.Ok {
		t.Errorf("span status = %v, want Ok", got.Status().Code)
	}
	attrs := attrMap(got.Attributes())
	if attrs[attrPollOutcome] != attrPollOutcomeOK {
		t.Errorf("dbb.poll_outcome = %q, want %q", attrs[attrPollOutcome], attrPollOutcomeOK)
	}
	if attrs[attrNTPClass] != "healthy" {
		t.Errorf("dbb.ntp_class = %q", attrs[attrNTPClass])
	}
	if attrs[attrHostID] != "vega-A" {
		t.Errorf("dbb.host_id = %q", attrs[attrHostID])
	}
	if attrs[attrStratum] != int64(2) {
		t.Errorf("dbb.stratum = %v, want 2", attrs[attrStratum])
	}
	if attrs[attrRefID] != "192.168.1.1" {
		t.Errorf("dbb.ref_id = %q", attrs[attrRefID])
	}
	if attrs[attrRMSOffsetNs] != int64(1234) {
		t.Errorf("dbb.rms_offset_ns = %v, want 1234", attrs[attrRMSOffsetNs])
	}
	if attrs[attrConsecutiveErrors] != int64(0) {
		t.Errorf("dbb.consecutive_errors = %v, want 0", attrs[attrConsecutiveErrors])
	}
}

// TestPollSpan_Failure: non-nil err → status Error, dbb.poll_outcome=
// chrony_query_failed, RecordError emits a span event.
func TestPollSpan_Failure(t *testing.T) {
	sr := installRecorder(t)

	s := state.NtpState{
		HostID:      "vega-B",
		NtpClass:    state.ClassUnknown,
		NtpClassStr: state.ClassUnknown.String(),
	}
	_, span := startPollSpan(context.Background())
	recordPollSpan(span, "vega-B", s, 3, errors.New("chrony socket unreachable"))
	span.End()

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	got := spans[0]
	if got.Status().Code != codes.Error {
		t.Errorf("span status = %v, want Error", got.Status().Code)
	}
	attrs := attrMap(got.Attributes())
	if attrs[attrPollOutcome] != attrPollOutcomeFailure {
		t.Errorf("dbb.poll_outcome = %q, want %q", attrs[attrPollOutcome], attrPollOutcomeFailure)
	}
	if attrs[attrConsecutiveErrors] != int64(3) {
		t.Errorf("dbb.consecutive_errors = %v, want 3", attrs[attrConsecutiveErrors])
	}
	if len(got.Events()) == 0 {
		t.Errorf("expected RecordError to add an event; got 0")
	}
}

// TestPollSpan_NoOpProvider verifies that with the default no-op global
// provider (no SDK installed) the span path runs without panic. This is the
// production "OTLP endpoint not configured" path.
func TestPollSpan_NoOpProvider(t *testing.T) {
	otel.SetTracerProvider(noop.NewTracerProvider())
	t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })

	s := state.NtpState{NtpClassStr: "healthy"}
	_, span := startPollSpan(context.Background())
	recordPollSpan(span, "host", s, 0, nil)
	span.End()
}

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
