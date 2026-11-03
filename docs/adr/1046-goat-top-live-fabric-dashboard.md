# ADR 1046 — `goat top`: a single-pane live fabric dashboard as a fourth, terminal "collected operator pane" altitude

**Status:** Accepted — built + live-fired on ZAGROS 2026-06-14 (M-067): the dashboard renders real fabric weather + ranked producers honestly, coexists with concurrent `goat` sessions (read-only random-ZID), and the UI/UX refinements hold on live data. Core merged in #1159; the per-actor throughput-envelope BAND reveal column merged in #1160 ([ADR 1045](1045-qos-throughput-envelopes.md)) — its own ZAGROS live-fire (BANDs rendering against declared envelopes) is the remaining gate on the qos-throughput-envelopes track, not on this one.
**Repo scope:** goat core (`goat-cli`: a new `goat top` command + tui module + a refactor extracting the shared `EnvHealthV1`/footprint wire types into `health_wire.rs`)
**Related:** [ADR 1036](1036-environment-health-surface.md) (the one-contract / three-altitudes surface this extends with a fourth altitude) · [ADR 1034](1034-fabric-health-surface.md) (fabric load + per-producer footprint) · [ADR 1042](1042-mesh-link-health-domain.md) (mesh path/latency join) · [ADR 1040](1040-zenoh-advanced-pubsub-liveliness-adoption.md) gap 3 (presence) · [ADR 1019](1019-goat-cli-unified-operator-binary.md) (the binary) · [ADR 1039](1039-slot-alias-display-layer.md) (alias display) · [`goat-top.md`](../design/goat-top.md) (the design)

## Plain-language summary

`goat top` is the `top(1)` of the data fabric: one terminal screen, auto-refreshing, that shows every
environment-health domain (mesh / fabric / time / presence) at once plus a sortable ranking of the
producers driving the fabric. It is a **pure consumer** of the five health + footprint topics already
on the bus — it adds no producer and no enforcement. It inherits the environment-health honesty
discipline unchanged: never a false green; an ungrounded domain reads `idle`/`unmetered` (dim), a
stale beat shows its age and dims, every reading carries `n`. Host-process CPU/mem is deliberately
**v1-deferred** — no per-host process probe exists on the bus, and adding one is a new producer out of
scope for an aggregation-only consumer; v1 uses msgs/s + B/s as the resource proxy and names the
`ops/host/stats/<host>` publisher as the scoped v2 path.

## Context

- The environment-health surface (ADR 1036) defines three rendering altitudes: `goat health`
  (single-domain CLI glance), panoscope (web COP band), stethoscope (why-is-it-red). None answers the
  operator's actual ask — *"one screen, all domains, refreshing, ranking who's driving the net"* — in
  a terminal over a constrained / SSH link.
- Every input already exists on the bus and is already decoded by `goat health`
  (`crates/goat-cli/src/cmd/health.rs`): `ops/fabric/health/**`, `ops/fabric/footprint/**`,
  `ops/mesh/health/**`, `ops/time/health/**`, `ops/presence/health/**`. This is aggregation, not new
  plumbing.
- `goat ui`'s Tail lens (`crates/goat-cli/src/tui/tail.rs`) is the proven ratatui structure: a producer
  task owns the Zenoh session + subscribers and feeds an mpsc channel; a `spawn_blocking` synchronous
  event loop drains + redraws with guaranteed terminal teardown.
- "Resource usage of the producer processes" was part of the ask, but fabric health measures
  *throughput* off real traffic, not host CPU/RSS — there is no process-stats publisher on the bus.

## Decision

1. **`goat top` is a fourth altitude — the terminal "collected operator pane."** It composes the same
   `EnvHealthV1` domains + `ProducerFootprintV1` footprints, full-screen and refreshing, all at once.
   Complementary to the three existing altitudes, not a replacement for any.
2. **Pure consumer, brokerless-safe.** Subscribe-and-render only — no producer, no enforcement, no
   central roll-up. If it dies, the raw feeds and the enforcement planes are untouched.
3. **Latest-wins live model, bounded.** One current reading per domain; one footprint per peer anchor
   in a map. No unbounded history ring (it is a current-state dashboard, not a log). Footprints not
   refreshed within ~2.5× their publish window age out; a domain past its staleness window dims and
   shows its age — never rendered live.
4. **Honesty discipline inherited wholesale.** 5-state colors map to ratatui styles
   (green/amber/red/dim-`idle`/dim-`unmetered`); every chip + row shows `n`; never a false green. The
   ANSI-emitting `health.rs::colorize()` is not reused — ratatui styles via `Style::fg`.
5. **Producer table is the focus.** Sortable by msgs/s (default) / B/s / peer; peer rendered via the
   ADR 1039 alias registry (degrades to UUID); PATH + LATENCY joined from the mesh domain by peer,
   honestly `—` when mesh has no row.
6. **Host CPU/mem deferred to v2, scoped not dropped.** v1 resource proxy = msgs/s + B/s. v2 = an
   `ops/host/stats/<host>` publisher (mesh-health snapshot pattern) surfaced as a new `EnvHealthV1`
   `oneof` arm + a HOST-RESOURCE pane joined by host.
7. **Refactor first (one-hat).** Extract the shared wire types + helpers (`fmt_age`, `short_name`,
   color map) into `pub(crate) health_wire.rs` consumed by both `goat health` and `goat top`, keeping
   `goat health`'s tests green — avoids a third hand-copy of the prost structs.
8. **TTY-gated interactive screen; `--once` is the scriptable batch path.** The interactive dashboard
   calls `require_tty("goat top")` before any session open. `goat top --once` (the `top -bn1` analog)
   skips the gate, collects one bounded window, renders **one plain-text frame** via a second renderer
   off the same view model, and exits — usable under a pipe / CI. One view model → two renderers means
   the snapshot and the live screen cannot disagree about the honesty grading. The line-oriented
   `goat health` remains the single-domain scriptable path.

## Alternatives considered

- **Extend `goat health --watch` into a multi-pane view — rejected.** `--watch` scrolls beats
  line-by-line; scroll is not a dashboard. A dashboard is fixed panes refreshed in place with a
  bounded current-state model and a producer ranking — a different rendering contract that belongs in
  the TUI module, not bolted onto the line-oriented command.
- **Panoscope web environment band instead — complementary, not a substitute.** The web COP presumes a
  workstation + a path to the web surface. `goat top` serves the terminal / SSH / constrained-link
  altitude the COP cannot. Both render the same `EnvHealthV1`; they are different altitudes (ADR 1036
  §5), not competitors.
- **Add a host CPU/mem probe now — deferred to v2.** It is a new on-bus producer (a per-host
  process-stats agent), which an aggregation-only consumer should not require; named as the
  `ops/host/stats/<host>` v2 increment so it is scoped, not silently dropped.

## Consequences

- A new operator surface for "what is the fabric doing right now, and who's driving it," usable over
  SSH with no browser — at zero new bus producers and zero enforcement risk.
- One shared wire-types module removes the third hand-copy of the `EnvHealthV1`/footprint structs;
  future domain additions update one place for both consumers.
- The deferred host-resource pane is a known v2, gated on a new `ops/host/stats` producer.
- Live-fire on a sandbox goatnet (ZAGROS) → measurement, operator-gated (not part of this build).
```