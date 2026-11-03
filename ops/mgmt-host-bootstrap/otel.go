// otel.go — OpenTelemetry trace instrumentation for mgmt-host-bootstrap.
//
// First wide-event producer (Block 27C / Block 50B.4). Each CLI invocation
// creates one root span named mgmt-host-bootstrap.<subcommand> with the
// per-command attributes documented in SCHEMA.md, exported via OTLP/HTTP
// to OTEL_EXPORTER_OTLP_ENDPOINT.
//
// Telemetry is best-effort: init failure is logged to stderr but never
// blocks CLI use; shutdown is bounded by the caller's context (5s in
// main()) so a slow / unreachable endpoint cannot stall an operator.
//
// Anchors:
//   docs/adr/0822-wide-event-observability-otlp-openobserve.md
//   docs/design/observability-wide-events.md
//   docs/project/block-27c-scoping-2026-05-05.md
//   ops/mgmt-host-bootstrap/SCHEMA.md
package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

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

const (
	tracerName = "github.com/usareur-af/goatcore/ops/mgmt-host-bootstrap"

	// Per-span attributes (dbb.* convention per
	// docs/design/observability-wide-events.md §"OTLP attribute mapping").
	attrCLISubcommand   = "dbb.cli_subcommand"
	attrDSNHost         = "dbb.dsn_host"
	attrRecordsAffected = "dbb.records_affected"
	attrExitCode        = "dbb.exit_code"
	attrAlreadyApplied  = "dbb.already_applied"

	// Resource-level attribute (alongside standard service.* attrs).
	attrSiteRole = "dbb.site_role"
)

// initOTel configures the global OpenTelemetry trace provider from env.
//
// When OTEL_EXPORTER_OTLP_ENDPOINT is unset, returns a no-op shutdown and
// leaves the default no-op tracer provider in place — withSpan() still
// runs uniformly but emits nothing. This keeps "OTel disabled" structurally
// indistinguishable from "OTel enabled, span path runs" for the call sites.
//
// Resource attributes set:
//
//	service.name        = "mgmt-host-bootstrap"
//	service.version     = const version (main.go)
//	service.namespace   = $DBB_SITE_ID (default "unknown")
//	service.instance.id = os.Hostname() (default "unknown")
//	dbb.site_role       = $DBB_SITE_ROLE (default "mgmt")
//
// Returns the TracerProvider's Shutdown so the caller can flush spans
// before exit. Caller is responsible for bounding the shutdown context.
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

// otlpHTTPOptionsFromEndpoint converts an OTEL_EXPORTER_OTLP_ENDPOINT URL
// into the otlptracehttp client options. Recognizes http/https schemes and
// preserves the URL path (defaults to /v1/traces if empty per OTLP spec).
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

// buildResource assembles the OTel resource describing this CLI process.
func buildResource() *resource.Resource {
	siteID := envOrDefault("DBB_SITE_ID", "unknown")
	siteRole := envOrDefault("DBB_SITE_ROLE", "mgmt")
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown"
	}
	return resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName("mgmt-host-bootstrap"),
		semconv.ServiceVersion(version),
		semconv.ServiceNamespace(siteID),
		semconv.ServiceInstanceID(hostname),
		attribute.String(attrSiteRole, siteRole),
	)
}

func envOrDefault(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// dsnHost extracts the host portion of a libpq-style or URL-form Postgres
// DSN. Used to populate dbb.dsn_host without leaking secrets — host only;
// password / user / dbname never on the span.
//
// Returns "" if the DSN is empty or the host cannot be safely identified.
func dsnHost(dsn string) string {
	if dsn == "" {
		return ""
	}
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return ""
		}
		return u.Hostname()
	}
	for _, kv := range strings.Fields(dsn) {
		if v, ok := strings.CutPrefix(kv, "host="); ok {
			return v
		}
	}
	return ""
}

// withSpan wraps fn in a root span named "mgmt-host-bootstrap.<cmdUse>".
//
// Span lifecycle:
//
//   - dbb.cli_subcommand is set from cmdUse before fn runs.
//   - extraAttrs are attached as a single SetAttributes call.
//   - fn may set additional attributes (e.g. dbb.records_affected) on span.
//   - On fn returning errAlreadyApplied: span gets dbb.already_applied=true,
//     dbb.exit_code=0, status Ok, and withSpan returns nil to the caller
//     (so cobra exits 0 — matches the existing already-applied UX where
//     each RunE called emitAlreadyApplied(...) then returned nil).
//   - On any other error: span records the error, gets dbb.exit_code=1,
//     status Error, and withSpan returns the error unchanged.
//   - On nil error: dbb.exit_code=0, status Ok, withSpan returns nil.
//
// The span is always ended via defer regardless of return path.
func withSpan(
	ctx context.Context,
	cmdUse string,
	extraAttrs []attribute.KeyValue,
	fn func(ctx context.Context, span trace.Span) error,
) error {
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "mgmt-host-bootstrap."+cmdUse,
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	attrs := make([]attribute.KeyValue, 0, len(extraAttrs)+1)
	attrs = append(attrs, attribute.String(attrCLISubcommand, cmdUse))
	attrs = append(attrs, extraAttrs...)
	span.SetAttributes(attrs...)

	err := fn(ctx, span)
	switch {
	case errors.Is(err, errAlreadyApplied):
		span.SetAttributes(
			attribute.Bool(attrAlreadyApplied, true),
			attribute.Int(attrExitCode, 0),
		)
		span.SetStatus(codes.Ok, "already-applied")
		return nil
	case err != nil:
		span.RecordError(err)
		span.SetAttributes(attribute.Int(attrExitCode, 1))
		span.SetStatus(codes.Error, err.Error())
		return err
	default:
		span.SetAttributes(attribute.Int(attrExitCode, 0))
		span.SetStatus(codes.Ok, "")
		return nil
	}
}
