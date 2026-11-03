// otel.go — OpenTelemetry trace instrumentation for ntp-admit-sidecar.
//
// Third wide-event producer (Block 27C / Block 50B.4). Each inbound proxy
// request becomes one root span via otelhttp middleware; admit-class requests
// open a child span for the chrony health probe so its latency surfaces
// distinct from outer request latency.
//
// Span shape:
//
//	ntp-admit-sidecar.{HTTP method}     (otelhttp default name)
//	  └─ ntp-admit-sidecar.checkNTP    (only for admit-class requests)
//
// Attributes on the inbound root span (added by ServeHTTP at admit-class
// classification time):
//
//	dbb.admit_class      bool   admit-class request? (vs revoke-class / pass-through)
//	dbb.peer_id_kind     string "setup_key" | "src_ip"   (admit-class only)
//	dbb.ntp_class        string verdict from ntp-health-agent (admit-class only)
//	dbb.deny_reason      string "ntp-unhealthy" | "rate-limit-per-peer" |
//	                            "rate-limit-global"        (only on deny path)
//
// HTTP-level attributes (`http.method`, `http.status_code`, `url.path`, etc.)
// come from otelhttp's standard semantic conventions.
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
//	ops/time-hygiene/cmd/ntp-admit-sidecar/SCHEMA.md
package main

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	tracerName = "github.com/usareur-af/goatcore/ops/time-hygiene/cmd/ntp-admit-sidecar"

	// Per-span attributes (dbb.* convention per
	// docs/design/observability-wide-events.md §"OTLP attribute mapping").
	attrAdmitClass  = "dbb.admit_class"
	attrPeerIDKind  = "dbb.peer_id_kind"
	attrNTPClass    = "dbb.ntp_class"
	attrDenyReason  = "dbb.deny_reason"
	attrCheckNTPErr = "dbb.check_ntp_error"
)

// version is the build-time service version reported as `service.version`.
// Bumped per release.
const version = "v0.1.0"

// startCheckNTPSpan opens a child span for the chrony health-check probe.
// Caller must span.End() when the probe completes; recording the span's
// status is the caller's responsibility (typically: nil err → status implicit
// Ok, non-nil err → RecordError + status Error).
//
// Returns the new context (carrying the active span) and the span itself.
// When the global provider is the default no-op, this is a cheap no-op.
func startCheckNTPSpan(ctx context.Context) (context.Context, trace.Span) {
	tracer := otel.Tracer(tracerName)
	return tracer.Start(ctx, "ntp-admit-sidecar.checkNTP",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
}

// annotateAdmitSpan attaches admit-class attributes to the inbound request
// span (the otelhttp-created root). Pulls span via trace.SpanFromContext —
// when the no-op provider is active, the returned span discards SetAttributes
// silently.
func annotateAdmitSpan(ctx context.Context, admitClass bool, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.Bool(attrAdmitClass, admitClass))
	span.SetAttributes(attrs...)
}
