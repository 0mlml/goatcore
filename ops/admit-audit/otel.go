// otel.go — OpenTelemetry trace instrumentation for admit-audit.
//
// Fourth wide-event producer (Block 27C / Block 50B.4). Each peer-table
// poll cycle creates one root span named admit-audit.poll with the
// per-cycle attributes documented in SCHEMA.md, exported via OTLP/HTTP
// to OTEL_EXPORTER_OTLP_ENDPOINT.
//
// The poll cycle is the right tracing surface — every fleet-wide admit
// signal surfaces here, and the cycle is naturally bounded so each
// invocation maps to one span. Newly-detected admits are attached as
// span events (one per admit) rather than nested spans — admits are
// O(N) within a single cycle and a flat event stream is the cheaper
// shape both at emit time and on the OpenObserve query side. /metrics
// and /healthz handlers are NOT instrumented at v1 — those are
// loopback-only Prometheus + container-healthcheck calls already
// covered by Prometheus + serve a different operator audience.
//
// Telemetry is best-effort: init failure is logged to stderr but never
// blocks the daemon; shutdown is bounded so a slow / unreachable
// endpoint cannot stall SIGTERM cleanup.
//
// admit-audit is its own Go module so it carries its own per-binary
// otel.go (mgmt-host-bootstrap precedent) rather than importing the
// time-hygiene internal/otelinit package — cross-module sharing is
// per-binary copy per the Block 27C scoping note's handoff.
//
// Anchors:
//
//	docs/adr/0822-wide-event-observability-otlp-openobserve.md
//	docs/design/observability-wide-events.md
//	docs/project/block-27c-scoping-2026-05-05.md
//	ops/admit-audit/SCHEMA.md
package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
	"go.opentelemetry.io/otel/trace"
)

// version is the build-time service version reported as `service.version`.
// Bumped per release. Held here so tests can pin the resource shape.
const version = "v0.1.0"

const (
	tracerName = "github.com/usareur-af/goatcore/ops/admit-audit"

	// Resource-level attribute (alongside standard service.* attrs).
	attrSiteRole = "dbb.site_role"

	// Per-span attributes (dbb.* convention per
	// docs/design/observability-wide-events.md §"OTLP attribute mapping").
	attrSiteName            = "dbb.site_name"
	attrPeersTotal          = "dbb.peers_total"
	attrAdmitsDetected      = "dbb.admits_detected"
	attrPollOutcome         = "dbb.poll_outcome"
	attrWindowTotal         = "dbb.window_total"
	attrWindowPerUserMax    = "dbb.window_per_user_max"
	attrWindowDistinctKeys  = "dbb.window_distinct_keys"
	attrPollOutcomeOK       = "success"
	attrPollOutcomeFailure  = "db_query_failed"

	// Per-event attributes for span events emitted on each new admit.
	// Event name: "admit". Attributes carry the same fields the JSON-line
	// emit carries; wg_key + ip are deliberately included because admit
	// audit IS the audit-trail purpose of the sidecar.
	attrEventPeerID       = "dbb.peer_id"
	attrEventAccountID    = "dbb.account_id"
	attrEventSourceUser   = "dbb.source_user"
	attrEventWGKey        = "dbb.wg_key"
	attrEventPeerIP       = "dbb.peer_ip"
	attrEventMetaHostname = "dbb.meta_hostname"
)

// initOTel configures the global OpenTelemetry trace provider from env.
//
// When OTEL_EXPORTER_OTLP_ENDPOINT is unset, returns a no-op shutdown
// and leaves the default no-op tracer provider in place — span-emitting
// code paths run uniformly regardless. "OTel disabled" is structurally
// indistinguishable from "OTel enabled" at the call sites.
//
// Resource attributes set:
//
//	service.name        = "admit-audit"
//	service.version     = const version
//	service.namespace   = $DBB_SITE_ID (default "unknown")
//	service.instance.id = os.Hostname() (default "unknown")
//	dbb.site_role       = $DBB_SITE_ROLE (default "mgmt")
//
// Returns the TracerProvider's Shutdown so the caller can flush spans
// before exit. Caller is responsible for bounding the shutdown context
// (typically 5s).
func initOTel(ctx context.Context) (func(context.Context) error, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		// No endpoint → leave global provider at default no-op.
		return func(context.Context) error { return nil }, nil
	}

	opts, err := otlpHTTPOptionsFromEndpoint(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse OTEL_EXPORTER_OTLP_ENDPOINT: %w", err)
	}

	exporter, err := otlptrace.New(ctx, otlptracehttp.NewClient(opts...))
	if err != nil {
		return nil, fmt.Errorf("create OTLP exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(buildResource()),
	)
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}

// otlpHTTPOptionsFromEndpoint converts an OTEL_EXPORTER_OTLP_ENDPOINT
// URL into the otlptracehttp client options. Recognizes http/https
// schemes and preserves the URL path (defaults to /v1/traces if empty
// per OTLP spec).
func otlpHTTPOptionsFromEndpoint(endpoint string) ([]otlptracehttp.Option, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	if u.Host == "" {
		return nil, fmt.Errorf("endpoint %q missing host", endpoint)
	}
	opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(u.Host)}
	if u.Scheme == "http" {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	if u.Path != "" && u.Path != "/" {
		opts = append(opts, otlptracehttp.WithURLPath(u.Path))
	}
	return opts, nil
}

// buildResource assembles the OTel resource describing this process.
func buildResource() *resource.Resource {
	siteID := envDefault("DBB_SITE_ID", "unknown")
	siteRole := envDefault("DBB_SITE_ROLE", "mgmt")
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown"
	}
	return resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName("admit-audit"),
		semconv.ServiceVersion(version),
		semconv.ServiceNamespace(siteID),
		semconv.ServiceInstanceID(hostname),
		attribute.String(attrSiteRole, siteRole),
	)
}

// startPollSpan creates the root span for one poll cycle. Returns the
// new context (carrying the active span) and the span itself; the
// caller must span.End() when done.
//
// Detached from any inbound trace context — pollOnce has no upstream
// caller; each cycle is its own root.
func startPollSpan(ctx context.Context) (context.Context, trace.Span) {
	tracer := otel.Tracer(tracerName)
	return tracer.Start(ctx, "admit-audit.poll",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
}

// recordAdmitEvent attaches one span event per detected admit. Called
// inside the poll-cycle span before the span ends.
//
// Event name "admit" matches the JSON-line `event` field admit-audit
// already emits, so OpenObserve queries can join the OTLP and
// stdout-JSON streams by a single string literal.
func recordAdmitEvent(span trace.Span, p peerRow, detectedAt time.Time) {
	span.AddEvent("admit",
		trace.WithTimestamp(detectedAt),
		trace.WithAttributes(
			attribute.String(attrEventPeerID, p.ID),
			attribute.String(attrEventAccountID, p.AccountID),
			attribute.String(attrEventSourceUser, p.UserID),
			attribute.String(attrEventWGKey, p.Key),
			attribute.String(attrEventPeerIP, p.IP),
			attribute.String(attrEventMetaHostname, p.MetaHostname),
		),
	)
}

// recordPollSpan attaches per-poll-cycle attributes to span and sets
// its status from the poll outcome. err is non-nil on DB query
// failure; peersTotal / admitsDetected / windowTotal etc. capture the
// outcome of the cycle regardless.
//
// Span status:
//   - err == nil: Ok, dbb.poll_outcome=success
//   - err != nil: Error, dbb.poll_outcome=db_query_failed,
//     err recorded as a span event
//
// Telemetry is best-effort — when the global provider is the default
// no-op (OTEL_EXPORTER_OTLP_ENDPOINT unset), span is a no-op and these
// calls do nothing.
func recordPollSpan(span trace.Span, site string, peersTotal, admitsDetected, windowTotal, windowPerUserMax, windowDistinctKeys int, err error) {
	span.SetAttributes(
		attribute.String(attrSiteName, site),
		attribute.Int(attrPeersTotal, peersTotal),
		attribute.Int(attrAdmitsDetected, admitsDetected),
		attribute.Int(attrWindowTotal, windowTotal),
		attribute.Int(attrWindowPerUserMax, windowPerUserMax),
		attribute.Int(attrWindowDistinctKeys, windowDistinctKeys),
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
