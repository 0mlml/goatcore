# Relay per-packet service time (compute cost) — implementation spec

**Status:** designed-not-built. Handoff spec for the goat-relay/relay-area
session. Companion to the relay-fleet health dashboard
(`relay-health-dashboard.md`, which currently
declares this out of scope and points here).

## Goal

Measure the relay's **per-packet compute cost** — the time the relay spends to
receive a packet and pass it along, **excluding network transit**: NIC → kernel
→ goat-relay userspace (forwarding decision) → kernel → NIC. Report the
**distribution**, not just an average: **mean, mode, p99, and p99.9999** ("six
9s"). This is the "where the rubber meets the road" number — it tells you the
real cost the relay adds to every relayed datagram.

## Why it isn't obtainable from the current prober

The relay-health prober is **blackbox** + `--deep` reads only:
- `/proc/net/dev` — aggregate byte/packet **counters** (no per-packet timing).
- `docker stats` — container CPU%/mem% (an average, not per-packet, and at idle
  reads `0.00%`).

Nothing in the stack records when an individual packet entered vs. left. So this
is a **measurement-doesn't-exist** gap, not a rendering gap. A CPU-time÷packets
"proxy" is an average only (no mode/percentiles), conflates all process work,
and is ~0 at idle — it does **not** deliver the distribution and must not be
shipped as if it does.

## Measurement approach — recommended: app-level HDR histogram in goat-relay

Two candidate instrumentation points. **Recommendation: app-level, in the
`goat-relay` Rust process** (the relay rewrite, ADR 0709).

**Why app-level over eBPF XDP→tc:** the relay forwards opaque WireGuard/relay
frames between peers; the ingress packet and the egress packet differ (different
destination, relay framing). Correlating "this ingress packet *is* the one that
egressed" at the eBPF layer needs a shared identifier the wire doesn't cleanly
give you. **Inside the process it's the same buffer** — the relay reads a frame,
makes a forwarding decision, writes it out — so per-packet correlation is free
and exact, and it directly measures "relay actions" (the userspace segment that
dominates and is what we control).

**Mechanics:**
1. In the goat-relay forwarding hot path, stamp `t_in = Instant::now()` right
   after the `recv` returns a frame, and `t_out` right before/after the
   corresponding `send`. `service_ns = (t_out - t_in)`.
2. Record `service_ns` into an in-process **HDR histogram** (e.g. the `hdrhistogram`
   crate) — log-bucketed, so accurate tail percentiles **including p99.9999** are
   feasible **without storing every sample** (a few KB of buckets covers ns→s).
   One histogram per relay; optionally per-direction or per-transport.
3. Keep a rolling window (e.g. reset/rotate every N seconds, or keep cumulative +
   a windowed copy) so the dashboard shows recent behavior, not all-time.

**Overhead:** two `Instant::now()` (a `clock_gettime`/`rdtsc`) + one histogram
record per forward — sub-100ns, negligible vs. the forward itself. Gate behind a
config flag (`GOAT_RELAY_SERVICE_TIME_HIST=1`) so it's off unless wanted.

**eBPF as a later refinement (optional phase 2):** XDP ingress + tc egress
timestamping gives the **kernel-stack segments** (NIC→kernel, kernel→NIC) that
app-level can't see, for a full NIC-to-NIC breakdown. Deploy as a libbpf/bcc/
bpftrace tool via the relay role. Defer — the userspace segment is the dominant,
controllable cost; the kernel path is mostly fixed.

## Percentiles, sample volume, and honesty

- **p99.9999 needs ~10⁷ samples** to be statistically real (one in a million).
  At a relay's idle pps that takes a very long accumulation, so the tail is only
  meaningful **under sustained load** — drive it with the load apparatus
  ([ADR 1012](../adr/1012-load-testing-discipline.md),
  [`zenoh-loadtest.md`](zenoh-loadtest.md) patterns, or a direct packet
  generator against a relay). The dashboard must show **n** and flag tail
  percentiles as **"needs sustained load"** when n is too small — never present
  a p99.9999 computed from 200 samples as if it's six-nines.
- Report mean, **mode** (the busiest histogram bucket — the typical cost),
  p99, p99.9999, plus n and the window. HDR histograms give all of these from
  the bucket counts.
- This is a **measured distribution**, not a proxy — keep it that way. If
  instrumentation is off / unavailable, render grey "not instrumented," never an
  estimate.

## Export + transport

Emit a **compact histogram snapshot** (bucket boundaries + counts, or the
HDR-histogram serialized form) the dashboard can read and compute percentiles
from client-side. Two transport options, matching the existing dashboard tension
(see relay-health-dashboard.md §"Automating --deep"):
- **`--deep` / SSM fetch** — the prober runs a command that reads the relay's
  histogram snapshot (a file the relay writes, or a localhost metrics endpoint).
  Simplest; inherits the operator/automation creds story.
- **Relay self-publish** — the relay POSTs its snapshot to a mgmt-host ingest
  endpoint (no SSM). The more blackbox-aligned end state.

Reuse the dashboard's **freshness** + **n** + honesty conventions for the
snapshot (stale snapshot → grey, never a false-fresh reading).

## Dashboard integration

Add a **"relay service time"** panel to each relay row (the dashboard is already
row-structured with a left checks column + right metrics column):
- Headline stats: `mean · mode · p99 · p99.9999` (µs/ns), each with units.
- A small **histogram or CDF** mini-chart (reuse the sparkline machinery / SVG).
- Honesty line: `n=… · window …`, and `tail needs sustained load` when n is
  below the threshold for the displayed percentile.
- Grey "not instrumented" when the relay isn't running the instrumented build.

## Deployment + blast radius (the gate)

This is a **goat-relay code change** → image rebuild → **deploy to the relay
fleet** (the `goat-relay` ansible role + the relay images). That touches every
production relay, so it's an explicit operator-gated rollout, not a dashboard-
only change. The instrumentation flag defaults **off**; enable per-fleet.

## Build checklist (phases)

1. **goat-relay:** add the `Instant` stamps + `hdrhistogram` in the forward path
   behind `GOAT_RELAY_SERVICE_TIME_HIST`; expose a snapshot (file or localhost
   endpoint). Unit-test the timing + histogram.
2. **Snapshot fetch:** extend the prober (`--deep`) to read + embed the snapshot,
   or stand up the relay self-publish path.
3. **Dashboard:** the service-time panel (stats + histogram + honesty n/window/
   tail-flag).
4. **Load + tail validation:** drive a relay under sustained load to populate
   p99.9999; confirm n-gating + the tail-confidence flag behave honestly.
5. **Rollout:** rebuild the relay image, deploy fleet-wide behind the flag,
   live-validate on ZAGROS.

## Honesty invariants (carry from the rest of the dashboard)

- Measured distribution only — no proxy dressed up as latency.
- Always show `n`; gate tail percentiles on sufficient samples.
- Stale snapshot → grey, never false-fresh.
- "not instrumented" is an honest state, not a zero.
