# `ntp-admit-sidecar` — OTLP attribute schema

Third wide-event producer landed under Block 27C / 50B.4.
Each inbound proxy request emits **one** root span via otelhttp middleware;
admit-class requests open a child span for the chrony health-check probe.

Anchors:

- ADR [`0822`](../../../../docs/adr/0822-wide-event-observability-otlp-openobserve.md)
- Design [`observability-wide-events.md`](../../../../docs/design/observability-wide-events.md)
- Scoping `block-27c-scoping-2026-05-05.md`

## Schema-evolution discipline

Per `fabric-schema-evolution.md`:

- **Additive changes** (new attribute, new event type) — no version bump;
  consumers tolerate absence.
- **Non-additive changes** (rename, type change, removal) — bump
  `dbb.schema_version` for this producer; old version stays queryable
  for the warm-tier retention window.

Current schema version: **1** (initial). Increment in this file in the
same commit that lands a non-additive change.

## Resource attributes (set once at process init)

| Attribute | Type | Source | Notes |
|---|---|---|---|
| `service.name` | string | hardcoded `"ntp-admit-sidecar"` | Standard OTel semantic convention |
| `service.version` | string | `version` const in [`otel.go`](otel.go) | Bumped per-release |
| `service.namespace` | string | `$DBB_SITE_ID` (default `"unknown"`) | Per [observability-wide-events.md OTLP attribute mapping](../../../../docs/design/observability-wide-events.md#otlp-attribute-mapping) — site_id rides as namespace |
| `service.instance.id` | string | `os.Hostname()` (default `"unknown"`) | The mgmt-host actually running the sidecar |
| `dbb.site_role` | string | `$DBB_SITE_ROLE` (default `"mgmt"`) | Free-form site-role label (e.g. `"mgmt"`, `"obs"`) |
| `dbb.listen` | string | `--listen` flag value | Process listen address (e.g. `127.0.0.1:8080`) |

## Span shape

```
ntp-admit-sidecar.{HTTP method}     ← otelhttp root span; standard HTTP attrs
  └─ ntp-admit-sidecar.checkNTP     ← only on admit-class requests; bounded by --ntp-timeout
```

The `ntp-admit-sidecar.{method}` span name uses the configured otelhttp
SpanNameFormatter — the URL path is captured by the standard `url.path`
attribute, keeping span-name cardinality bounded.

## Span attributes — root (set by ServeHTTP)

| Attribute | Type | When set | Source |
|---|---|---|---|
| `dbb.admit_class` | bool | always | `isAdmitClass(method, path)` — true for the four admit-class HTTP/REST shapes + the gRPC `Login` method |
| `dbb.peer_id_kind` | string | admit-class only | `setup_key` (any of `Setup-Key` / `X-Nb-Setup-Key` / `Grpc-Metadata-Setup-Key` headers present) or `src_ip` (fallback) |
| `dbb.ntp_class` | string | admit-class only | Verdict from `ntp-health-agent` over its Unix socket: one of `healthy` / `warning` / `degraded` / `unknown` |
| `dbb.deny_reason` | string | only on deny path | `ntp-unhealthy` / `rate-limit-per-peer` / `rate-limit-global` |

Standard HTTP semantic-convention attributes (`http.method`, `http.status_code`,
`url.path`, `url.scheme`, `network.protocol.version`, `client.address`, etc.)
are attached by the otelhttp middleware automatically and are NOT duplicated
under `dbb.*`.

## Span attributes — `checkNTP` child

| Attribute | Type | When set | Source |
|---|---|---|---|
| `dbb.ntp_class` | string | on successful decode | Verdict from `ntp-health-agent` (echoed onto the parent span too via `annotateAdmitSpan`) |
| `dbb.check_ntp_error` | string | on probe failure | `dial_failed` (Unix socket unreachable) or `decode_failed` (JSON unmarshal error) |

## Span status

| Outcome | Root span status | `checkNTP` child status |
|---|---|---|
| Pass-through (revoke / GET / non-admit) | otelhttp default (Ok or Error from HTTP code) | n/a (no child) |
| Admit-class allowed | otelhttp default | implicit Unset (caller doesn't set Ok) |
| Admit-class denied — NTP unhealthy | 503 → otelhttp Error | `Error` + `RecordError` event |
| Admit-class denied — rate-limit | 429 → otelhttp Error | implicit Unset (NTP probe succeeded) |

## Trace context propagation

The otelhttp middleware extracts the W3C Trace Context from the inbound
request (if present) and uses it as the parent for the root span. When no
context is present (typical for direct peer enrollment), the span is a new
root. The `checkNTP` child inherits the inbound context and is a true child
span of the inbound root.

The proxied call to the management backend does NOT currently propagate
the trace context — adding `otelhttp.Transport` wrapping on the `httputil.
ReverseProxy.Transport` is queued under Block 27C
follow-on producers (signal-server is the natural correlation-ID source
peer).

## Sampling

100% trace sampling at v1. Admit-class call rate is bounded structurally
by the per-peer (default 20/24h) and global (default 1000/24h) rate
limiters; revoke / pass-through volume is much lower. Configurable via
the OTel-standard env vars `OTEL_TRACES_SAMPLER` / `OTEL_TRACES_SAMPLER_ARG`.

## Failure modes (predicted)

| Mode | Behavior | Rationale |
|---|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` unset | No-op shutdown; default no-op tracer; spans created but not exported | "OTel disabled" must be structurally indistinguishable from "OTel enabled" at call sites |
| Endpoint malformed | `otelinit.Init` returns error; logged to stderr; sidecar continues without telemetry | Telemetry must never block the proxy |
| Endpoint unreachable | `BatchSpanProcessor` retries with backoff; deferred `shutdown(ctx)` bounded to 5s in `main()` | Worst case: 5s slowdown on SIGTERM |
| OTel SDK panic | Caught by Go's runtime in the SDK's own goroutines; proxy unaffected | `panicMiddleware` already covers per-request panics; SDK-internal panic does not affect proxy or chrony probe |

## Out of scope at v1

- **`/metrics` listener spans** — separate loopback listener, infrequent
  scrape pattern; otelhttp wrap deferred until the metrics span volume has
  a debugging use case.
- **Backend proxy span** — the inbound otelhttp span captures outer
  latency, which is what matters operationally. Adding an
  `otelhttp.Transport` wrap on `httputil.ReverseProxy.Transport` lands
  when signal-server (next producer in Block 27C)
  becomes the correlation-ID source.
- **Per-rate-limiter span** — the deny-reason attribute on the root span
  is sufficient signal at v1; per-limiter sub-spans add no actionable info.

## Verification

| Layer | Test | Location |
|---|---|---|
| `annotateAdmitSpan` + `startCheckNTPSpan` (admit / revoke / NTP success / NTP dial failure) | `go test -short` | [`otel_test.go`](otel_test.go) |
| Init / resource / endpoint parse | `go test -short` | [`../../internal/otelinit/otelinit_test.go`](../../internal/otelinit/otelinit_test.go) |
| Production end-to-end | Operator-fired against the per-site OpenTelemetry Collector; spans visible in OpenObserve | Logged in `docs/project/validation-state.md` when operator runs the soak |
