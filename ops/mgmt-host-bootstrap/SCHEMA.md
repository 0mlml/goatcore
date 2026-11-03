# `mgmt-host-bootstrap` — OTLP attribute schema

First wide-event producer landed under Block 27C / 50B.4.
Each invocation of the CLI emits **one** root span per subcommand to the
OpenTelemetry Collector at `OTEL_EXPORTER_OTLP_ENDPOINT`.

Anchors:

- ADR [`0822`](../../docs/adr/0822-wide-event-observability-otlp-openobserve.md)
- Design [`observability-wide-events.md`](../../docs/design/observability-wide-events.md)
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
| `service.name` | string | hardcoded `"mgmt-host-bootstrap"` | Standard OTel semantic convention |
| `service.version` | string | `version` const in [`main.go`](main.go) | Bumped per-release |
| `service.namespace` | string | `$DBB_SITE_ID` (default `"unknown"`) | Per [observability-wide-events.md OTLP attribute mapping](../../docs/design/observability-wide-events.md#otlp-attribute-mapping) — site_id rides as namespace |
| `service.instance.id` | string | `os.Hostname()` (default `"unknown"`) | The mgmt-host actually running the CLI |
| `dbb.site_role` | string | `$DBB_SITE_ROLE` (default `"mgmt"`) | Free-form site-role label (e.g. `"mgmt"`, `"obs"`) |

## Span attributes (set per invocation)

Span name pattern: **`mgmt-host-bootstrap.<subcommand>`**.

| Attribute | Type | Always set? | Source |
|---|---|---|---|
| `dbb.cli_subcommand` | string | yes | Cobra `cmd.Use` (e.g. `create-account`, `create-setup-key`, `rotate-setup-key`, `revoke-peer`, `list-peers`, `list-setup-keys`) |
| `dbb.dsn_host` | string | yes (may be empty) | Host portion of `--dsn` / `$PGDSN`; password / user / dbname **never** included |
| `dbb.records_affected` | int64 | yes (post-RunE) | Rows touched by this invocation; 0 on idempotent replay |
| `dbb.exit_code` | int64 | yes | 0 on success or already-applied; 1 on error |
| `dbb.already_applied` | bool | only on idempotent replay | Set to `true` when `runIdempotent` returns `errAlreadyApplied`; absent otherwise |

## Span status

| RunE outcome | OTel span status | Span events |
|---|---|---|
| `nil` | `Ok` | none |
| `errAlreadyApplied` | `Ok` (description `"already-applied"`) | none |
| any other error | `Error` (description = `err.Error()`) | one `RecordError` event with stack |

The `errAlreadyApplied` path is special: span marked Ok, withSpan returns
`nil` to cobra so the CLI exits 0 — preserves the existing UX where each
subcommand prints `already-applied: <key>` then exits cleanly.

## Trace context propagation

`mgmt-host-bootstrap` is a one-shot CLI with no upstream caller; every
invocation produces a fresh root span with no incoming `trace.id` /
`span.id` propagation. Downstream calls inside the CLI (postgres queries
via sqlx + lib/pq) do not currently emit OTel-instrumented spans —
adding pgx-side or sqlx-side child spans is queued for follow-on work
under Block 50B.6 circuit-breaker
+ Block 27C wider rollout.

## Sampling

100% trace sampling at v1 — CLI invocations are infrequent and
sampling has no value until volume justifies it. Configurable via the
OTel-standard env vars `OTEL_TRACES_SAMPLER` /
`OTEL_TRACES_SAMPLER_ARG` if the operator needs to limit volume during
a soak.

## Failure modes (predicted)

| Mode | Behavior | Rationale |
|---|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` unset | No-op shutdown; default no-op tracer; spans created but not exported | "OTel disabled" must be structurally indistinguishable from "OTel enabled" at call sites |
| Endpoint malformed | `initOTel` returns error; logged to stderr; CLI continues without telemetry | Telemetry must never block CLI use |
| Endpoint unreachable | `BatchSpanProcessor` retries with backoff; deferred `shutdown(ctx)` bounded to 5s in `main()` | Worst case: 5s slowdown on CLI exit |
| OTel SDK panic | Caught by Go's runtime in the SDK's own goroutines; CLI's foreground work unaffected | One-shot CLI; SDK-internal panic does not corrupt the in-flight tx |

## Out of scope at v1

- **Logs / metrics OTLP signals** — the OTel logs SDK is alpha as of
  v0.17; metrics offer little incremental value over the existing
  Prometheus probes. Traces only at v1.
- **Per-step span events** for connect / query / commit — coarse
  one-span-per-invocation is sufficient at CLI granularity. If a future
  consumer needs sub-command timing, add events via
  `span.AddEvent("query", trace.WithAttributes(...))` inside
  `runIdempotent`'s callback.
- **Instrumented postgres driver** — sqlx/lib/pq doesn't surface a
  per-statement hook. Migrating to `pgx` with `tracelog.TraceLog` is
  deferred to whichever block first needs per-statement spans.

## Verification

| Layer | Test | Location |
|---|---|---|
| Pure functions (DSN parse, endpoint parse, resource build) | `go test -short` | [`otel_test.go`](otel_test.go) |
| In-memory span recording (success / error / already-applied / no-op) | `go test -short` | [`otel_test.go`](otel_test.go) |
| Live SDK + goroutine-leak surface | `go test -short` (covered by `goleak.VerifyTestMain`) | [`otel_test.go`](otel_test.go) `TestInitOTel_LiveSDKShutdown` |
| Real binary emits OTLP/HTTP | `go test -tags=integration` | [`cmd_integration_test.go`](cmd_integration_test.go) `TestEndToEnd_OTLPEmit` |
| Production end-to-end | Operator-fired against the site's OTLP collector; spans visible in its UI | Logged in `docs/project/validation-state.md` |
