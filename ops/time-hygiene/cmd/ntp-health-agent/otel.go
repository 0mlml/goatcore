// otel.go — OpenTelemetry trace instrumentation for ntp-health-agent.
//
// Second wide-event producer (Block 27C / Block 50B.4). Each chronyd poll
// cycle creates one root span named ntp-health-agent.poll with the per-cycle
// attributes documented in SCHEMA.md, exported via OTLP/HTTP to
// OTEL_EXPORTER_OTLP_ENDPOINT.
//
// The poll cycle is the right tracing surface — every fleet-wide chrony
// degradation surfaces here, and the cycle is naturally bounded so each
// invocation maps to one span. /health and /metrics handlers are NOT
// instrumented at v1 — those are infrequent loopback calls already covered
// by Prometheus + serve a different operator audience.
//
// Telemetry is best-effort: init failure is logged to stderr but never
// blocks the daemon; shutdown is bounded so a slow / unreachable endpoint
// cannot stall SIGTERM cleanup.
//
// Anchors:
//
//	docs/adr/0822-wide-event-observability-otlp-openobserve.md
//	docs/design/observability-wide-events.md
//	docs/project/block-27c-scoping-2026-05-05.md
//	ops/time-hygiene/cmd/ntp-health-agent/SCHEMA.md
package main

import (
	"context"

	"github.com/usareur-af/goatcore/ops/time-hygiene/internal/state"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	tracerName = "github.com/usareur-af/goatcore/ops/time-hygiene/cmd/ntp-health-agent"

	// Per-span attributes (dbb.* convention per
	// docs/design/observability-wide-events.md §"OTLP attribute mapping").
	attrHostID             = "dbb.host_id"
	attrNTPClass           = "dbb.ntp_class"
	attrStratum            = "dbb.stratum"
	attrRefID              = "dbb.ref_id"
	attrRMSOffsetNs        = "dbb.rms_offset_ns"
	attrSelectedSource     = "dbb.selected_source"
	attrConsecutiveErrors  = "dbb.consecutive_errors"
	attrAttested           = "dbb.ntp_attested"
	attrPollOutcome        = "dbb.poll_outcome"
	attrPollOutcomeOK      = "success"
	attrPollOutcomeFailure = "chrony_query_failed"
)

// version is the build-time service version reported as `service.version`.
// Bumped per release (kept here rather than main so the value is import-able
// by tests that pin the resource shape).
const version = "v0.1.0"

// recordPollSpan attaches per-poll-cycle attributes to span and sets its
// status from the poll outcome. err is non-nil on chrony query failure;
// s is the post-poll state (degraded to ClassUnknown on err per a.poll's
// existing contract).
//
// Span status:
//   - err == nil: Ok, dbb.poll_outcome=success
//   - err != nil: Error, dbb.poll_outcome=chrony_query_failed,
//     err recorded as a span event
//
// Telemetry is best-effort — when the global provider is the default no-op
// (OTEL_EXPORTER_OTLP_ENDPOINT unset), span is a no-op and these calls do
// nothing. The caller is expected to span.End() when its lifecycle is done
// (typically: defer span.End() at the start of poll-and-record).
func recordPollSpan(span trace.Span, hostID string, s state.NtpState, consecutiveErrors int, err error) {
	span.SetAttributes(
		attribute.String(attrHostID, hostID),
		attribute.String(attrNTPClass, s.NtpClassStr),
		attribute.Int(attrStratum, int(s.Stratum)),
		attribute.String(attrRefID, s.RefID),
		attribute.Int64(attrRMSOffsetNs, s.RmsOffsetNs),
		attribute.String(attrSelectedSource, string(s.SelectedSource)),
		attribute.Int(attrConsecutiveErrors, consecutiveErrors),
		attribute.Bool(attrAttested, s.Attested),
	)
	if err != nil {
		span.SetAttributes(attribute.String(attrPollOutcome, attrPollOutcomeFailure))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}
	span.SetAttributes(attribute.String(attrPollOutcome, attrPollOutcomeOK))
	span.SetStatus(codes.Ok, "")
}

// startPollSpan creates the root span for one poll cycle. Returns the new
// context (carrying the active span) and the span itself; the caller must
// span.End() when done.
//
// Detached from any inbound trace context — pollLoop has no upstream caller;
// each cycle is its own root.
func startPollSpan(ctx context.Context) (context.Context, trace.Span) {
	tracer := otel.Tracer(tracerName)
	return tracer.Start(ctx, "ntp-health-agent.poll",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
}
