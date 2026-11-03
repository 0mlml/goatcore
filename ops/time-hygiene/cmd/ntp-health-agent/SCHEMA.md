# `ntp-health-agent` — OTLP attribute schema

Second wide-event producer landed under Block 27C / 50B.4.
Each chronyd poll cycle emits **one** root span to the OpenTelemetry
Collector at `OTEL_EXPORTER_OTLP_ENDPOINT`.

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
| `service.name` | string | hardcoded `"ntp-health-agent"` | Standard OTel semantic convention |
| `service.version` | string | `version` const in [`otel.go`](otel.go) | Bumped per-release |
| `service.namespace` | string | `$DBB_SITE_ID` (default `"unknown"`) | Per [observability-wide-events.md OTLP attribute mapping](../../../../docs/design/observability-wide-events.md#otlp-attribute-mapping) — site_id rides as namespace |
| `service.instance.id` | string | `os.Hostname()` (default `"unknown"`) | The mgmt-host actually running the agent |
| `dbb.site_role` | string | `$DBB_SITE_ROLE` (default `"mgmt"`) | Free-form site-role label (e.g. `"mgmt"`, `"obs"`) |
| `dbb.host_id` | string | `--host-id` flag or `os.Hostname()` | Daemon-level identity attached at init via `otelinit.Opts.Extra` |

## Span attributes (set per poll cycle)

Span name pattern: **`ntp-health-agent.poll`**.

| Attribute | Type | Always set? | Source |
|---|---|---|---|
| `dbb.host_id` | string | yes | Same value as the resource-level `dbb.host_id`; carried on the span too so a flat span query joins host without a resource-attribute lookup |
| `dbb.ntp_class` | string | yes | One of `healthy` / `warning` / `degraded` / `unknown` (from `state.NtpClass.String()`) |
| `dbb.stratum` | int64 | yes | Chrony-reported stratum (0 on unknown) |
| `dbb.ref_id` | string | yes (may be empty) | Chrony's RefID, decoded to ASCII or dotted IPv4 |
| `dbb.rms_offset_ns` | int64 | yes | Chrony RMS offset in nanoseconds |
| `dbb.selected_source` | string | yes | One of `gnss` / `peer_nts` / `wan_nts` / `orphan` / `none` |
| `dbb.consecutive_errors` | int64 | yes | Consecutive-error count BEFORE this cycle (the count this cycle starts with, not after) |
| `dbb.poll_outcome` | string | yes | `success` on nil err; `chrony_query_failed` on err from `Conn.GetTracking` |

## Span status

| Poll outcome | OTel span status | Span events |
|---|---|---|
| `nil` (success) | `Ok` | none |
| chrony query error | `Error` (description = `err.Error()`) | one `RecordError` event with stack |

## Trace context propagation

`ntp-health-agent` is a long-lived daemon with no upstream caller; every
poll cycle is its own root span (no incoming `trace.id` / `span.id`).
There are no downstream traced calls today — `internalchrony.GetTracking`
talks to a local Unix socket and is not OTel-instrumented; child spans
will appear if/when a future caller (e.g. a Block-23 queryable peer)
pulls poll history through this daemon.

## Sampling

100% trace sampling at v1. Cycle frequency is configurable
(`--poll-interval`, default 10s with ±10% jitter); at the design
ceiling this is well under the
collector throughput. Configurable via the OTel-standard env vars
`OTEL_TRACES_SAMPLER` / `OTEL_TRACES_SAMPLER_ARG` if a soak surfaces
cardinality / volume pressure.

## Failure modes (predicted)

| Mode | Behavior | Rationale |
|---|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` unset | No-op shutdown; default no-op tracer; spans created but not exported | "OTel disabled" must be structurally indistinguishable from "OTel enabled" at call sites |
| Endpoint malformed | `otelinit.Init` returns error; logged to stderr; daemon continues without telemetry | Telemetry must never block the daemon |
| Endpoint unreachable | `BatchSpanProcessor` retries with backoff; deferred `shutdown(ctx)` bounded to 5s in `main()` | Worst case: 5s slowdown on SIGTERM |
| OTel SDK panic | Caught by Go's runtime in the SDK's own goroutines; daemon's foreground work unaffected | Per-cycle pollLoop and accept loops have their own `safego.Spawn` recovery; SDK-internal panic does not corrupt poll state |

## Out of scope at v1

- **HTTP `/health` and `/metrics` handler spans** — infrequent loopback
  calls, already covered by Prometheus, low operator value relative to
  span volume. Add via otelhttp middleware if a debugging case justifies.
- **Unix-socket handler spans** — same — infrequent, low value.
- **OTel logs / metrics signals** — traces only at v1 (logs SDK still
  alpha as of v0.17; metrics overlap with the existing Prometheus surface).

## Verification

| Layer | Test | Location |
|---|---|---|
| Span name + attributes (success / failure / no-op) | `go test -short` | [`otel_test.go`](otel_test.go) |
| Init / resource / endpoint parse | `go test -short` | [`../../internal/otelinit/otelinit_test.go`](../../internal/otelinit/otelinit_test.go) |
| Production end-to-end | Operator-fired against the site's OTLP collector; spans visible in its UI | Logged in `docs/project/validation-state.md` when operator runs the soak |
