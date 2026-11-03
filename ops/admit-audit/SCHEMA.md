# `admit-audit` — OTLP attribute schema

Fourth wide-event producer landed under Block 27C / 50B.4.
Each peer-table poll cycle emits **one** root span (`admit-audit.poll`) to
the OpenTelemetry Collector at `OTEL_EXPORTER_OTLP_ENDPOINT`. Newly-detected
admits are attached as **span events** on the cycle span — one event per
admit — rather than nested spans.

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
| `service.name` | string | hardcoded `"admit-audit"` | Standard OTel semantic convention |
| `service.version` | string | `version` const in [`otel.go`](otel.go) | Bumped per-release |
| `service.namespace` | string | `$DBB_SITE_ID` (default `"unknown"`) | Per [observability-wide-events.md OTLP attribute mapping](../../docs/design/observability-wide-events.md#otlp-attribute-mapping) — site_id rides as namespace |
| `service.instance.id` | string | `os.Hostname()` (default `"unknown"`) | The mgmt-host actually running the sidecar |
| `dbb.site_role` | string | `$DBB_SITE_ROLE` (default `"mgmt"`) | Free-form site-role label (e.g. `"mgmt"`, `"obs"`) |

## Span attributes (set per poll cycle)

Span name: **`admit-audit.poll`** (constant).

| Attribute | Type | Always set? | Source |
|---|---|---|---|
| `dbb.site_name` | string | yes | `--site` / `$SITE_NAME` (e.g. `"vega"`); separate from `service.namespace` because operators want the site-name label even when site_id+site_name diverge |
| `dbb.peers_total` | int64 | yes (post-poll) | Row count from `SELECT … FROM peers`; 0 on DB error |
| `dbb.admits_detected` | int64 | yes (post-poll) | New peer rows detected this cycle (diff vs in-memory known set); 0 on DB error |
| `dbb.poll_outcome` | string | yes | `"success"` on clean cycle, `"db_query_failed"` when `queryPeers` errors |
| `dbb.window_total` | int64 | yes (post-poll) | Total admits within the rolling window; 0 on DB error |
| `dbb.window_per_user_max` | int64 | yes (post-poll) | Maximum per-source-user admit count within the rolling window; 0 on DB error |
| `dbb.window_distinct_keys` | int64 | yes (post-poll) | Distinct WireGuard pubkey count within the rolling window; 0 on DB error |

## Span events (one per detected admit)

Event name: **`admit`**. The string literal matches admit-audit's existing
JSON-line `event` field so OpenObserve queries can join the OTLP span-event
stream and the stdout-JSON stream by a single literal.

| Attribute | Type | Always set? | Source |
|---|---|---|---|
| `dbb.peer_id` | string | yes | `peers.id` |
| `dbb.account_id` | string | yes | `peers.account_id` |
| `dbb.source_user` | string | yes | `peers.user_id` (the user/setup-key that admitted this peer) |
| `dbb.wg_key` | string | yes | `peers.key` — WireGuard pubkey; intentionally on the event because the audit-trail purpose of admit-audit IS to log new keys |
| `dbb.peer_ip` | string | yes (may be empty) | `peers.ip` |
| `dbb.meta_hostname` | string | yes (may be empty) | `peers.meta_hostname` if the column exists; empty when absent |

Each event carries a per-event timestamp (`detectedAt`, the wallclock
moment the diff loop saw the row).

## Span status

| Outcome | OTel span status | Span events |
|---|---|---|
| `queryPeers` returns nil + 0+ admits detected | `Ok` | one per admit |
| `queryPeers` returns error | `Error` (description = `err.Error()`) | one `RecordError` event |

A poll cycle that emits zero admit events but completes cleanly is still
status `Ok` with `dbb.admits_detected=0`. A consumer querying for "which
sites are quiet" reads the `Ok`-+-zero rows; "which sites are erroring"
reads the `Error` rows.

## Trace context propagation

`admit-audit` is a polling sidecar with no upstream caller; every cycle
produces a fresh root span with no incoming `trace.id` / `span.id`
propagation. Downstream calls inside the cycle (the postgres `SELECT`
via lib/pq) do not currently emit OTel-instrumented spans — adding
pgx-side spans is queued for follow-on work under Block 50B.6
+ Block 27C wider rollout.

## Sampling

100% trace sampling at v1 — poll cadence is 30s default and per-cycle
volume is small. Each cycle's span carries up to a few admit events
(typical steady-state is zero). Configurable via the OTel-standard env
vars `OTEL_TRACES_SAMPLER` / `OTEL_TRACES_SAMPLER_ARG` if a soak driver
needs to limit volume.

## Failure modes (predicted)

| Mode | Behavior | Rationale |
|---|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` unset | No-op shutdown; default no-op tracer; spans created but not exported | "OTel disabled" must be structurally indistinguishable from "OTel enabled" at call sites |
| Endpoint malformed | `initOTel` returns error; logged to stderr; daemon continues without telemetry | Telemetry must never block the audit signal |
| Endpoint unreachable | `BatchSpanProcessor` retries with backoff; deferred `shutdown(ctx)` bounded to 5s in `main()` | Worst case: 5s slowdown on SIGTERM cleanup |
| OTel SDK panic | Caught by Go's runtime in the SDK's own goroutines; the auditor's foreground poll loop unaffected | Long-lived daemon; SDK-internal panic does not corrupt in-memory known-set |
| `dbb.wg_key` size on a high-churn site | OTLP exporter has 4MB default per-batch limit; admit batches are tiny in practice | If a future peer-rotation event ever pushes the budget, batch chunking is the OTel-side fix; not an admit-audit concern |

## Out of scope at v1

- **Logs / metrics OTLP signals** — the OTel logs SDK is alpha; the
  existing Prometheus surface is unchanged. Traces only at v1.
- **HTTP server (`/metrics`, `/healthz`) instrumentation** — loopback
  Prometheus + container-healthcheck calls; `otelhttp` middleware
  buys nothing at v1. Add if a future consumer needs request-shaped
  spans.
- **Per-row `peers` query attribute** — the SELECT has no per-row
  hooks; instrumenting at the row level would require migrating to
  pgx + `tracelog.TraceLog`. Deferred to whichever block first needs
  per-statement spans.

## Verification

| Layer | Test | Location |
|---|---|---|
| Pure functions (endpoint parse, resource build) | `go test ./...` | [`otel_test.go`](otel_test.go) |
| In-memory span recording (success / error / per-admit event / no-op) | `go test ./...` | [`otel_test.go`](otel_test.go) |
| Live SDK + goroutine-leak surface | `go test ./...` (covered by `goleak.VerifyTestMain` in [`main_test.go`](main_test.go)) | [`otel_test.go`](otel_test.go) `TestInitOTel_LiveSDKShutdown` |
| Production end-to-end | Operator-fired against the per-site OpenTelemetry Collector once the sidecar redeploys with this build; spans visible in OpenObserve | Logged in `docs/project/validation-state.md` |
