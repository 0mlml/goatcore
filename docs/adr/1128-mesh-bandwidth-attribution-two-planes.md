# ADR 1128 — Mesh bandwidth attribution: two planes, never summed

- **Status:** Accepted
- **Date:** 2026-08-13
- **Deciders:** platform
- **Related:** ADR 1036 (environment-health surface), ADR 1045 (QoS throughput
  envelopes), ADR 1058 (relay-health token-gated ingest), ADR 1123 (client
  demand must not amplify), ADR 0815 (metrics), F-289 (a declared scrape target
  with nothing behind it),
  [mesh-bandwidth-attribution.md](../design/mesh-bandwidth-attribution.md),
  [node-net-health.md](../design/node-net-health.md),
  [grafana-dashboard-suite.md](../design/grafana-dashboard-suite.md)

## Context

The operational question is preemptive, not forensic: **for a given mesh host,
how much of its up/down pipe is being consumed, by which category of work, and
which peers are driving each category** — asked *before* the link saturates,
not after an operator notices pages loading slowly.

Nothing on the fleet answers it. A 2026-08-13 audit of every dashboard, metric
and observability surface found five partial views and three structural gaps.

**What exists, and what each is missing:**

| Surface | Has | Missing |
|---|---|---|
| `edge-http-red.json` | Traefik req/s + latency **per router and per service** | No bytes — Traefik's Prometheus output has no size counter. No client dimension. |
| `fabric-tenants.json` | `goat_audit_sample_bytes_total{vendor}`, `goat_audit_samples_total{vendor,key_pattern}` | Byte-accurate with tenant + topic-family drill-down, but it is the audit subscriber's view of *published samples*, not wire bytes on any host |
| `node-exporter-full.json` | `node_network_{receive,transmit}_bytes_total` | One host only (below); no category split |
| `mesh-control-plane.json` | signal-server liveness, goroutines, RSS; blackbox probes | Zero byte accounting for the netbird control plane |
| `capacity-forecast.json` | `predict_linear` over disk / memory / TSDB | No network arm at all |
| `node-net-health` (ADR 1036 `net` arm) | Per-WG-peer rx/tx deltas **and** socket→pid/comm/container attribution, same host same moment | Socket rows carry no bytes; hourly; buckets are *security* buckets (mesh/fabric/baseline/undeclared), not workload categories |

**Three structural gaps, verified rather than assumed:**

1. **No denominator exists.** `link_capacity`, `uplink_mbps`, `bandwidth_mbps`
   and `capacity_bps` return zero hits across `ops/` and `docs/design/`. There
   is nothing in the repository to divide bytes/s by, so "percent of available
   pipe" cannot be computed honestly today.
2. **Prometheus sees one host.** `prometheus.yml` job `node-exporter` has
   exactly one static target, `host.docker.internal:9100`, labelled
   `{{ site_id }}-mgmt`; `cadvisor` and `process-exporter` likewise. There is no
   fleet-wide per-mesh-host network series in Prometheus at all.
3. **The HTTP bytes are collected and then discarded.** The mgmt-host compose
   runs Traefik with `--accesslog=true --accesslog.format=json`, and Vector
   already tails that container. The `app_http_ocsf` transform extracts
   `ClientHost` / `RequestHost` / `RequestPath` / `ServiceName` /
   `DownstreamStatus` and drops the size fields.

## Decision

### 1. Two planes, never summed

The naive design — one stacked chart of "bytes by category" — is **wrong on
this substrate**, and wrong in a way that silently doubles numbers.

A Zenoh message from host A to host B is counted at two layers on the same
host at the same instant:

- as **WireGuard UDP** on the physical NIC (the encrypted outer envelope), and
- as **TCP :7447** on the netbird interface (the plaintext inner payload).

These are the same bytes seen twice. Summing them is not a rounding error; it
is a category error, and it inflates exactly the number an operator would use
to decide whether a link has headroom.

Therefore attribution is presented as **two planes that are never added
together**:

| Plane | Question it answers | Measured at | Categories |
|---|---|---|---|
| **Transport** | "How much of my physical pipe is in use?" | physical NIC + per-WG-peer counters | `mesh-p2p`, `mesh-relayed`, `off-mesh`, `unattributed` |
| **Workload** | "Who and what is consuming the mesh?" | in-tunnel sockets + Traefik access log | the closed category grammar (§2) |

The transport plane is the one with a capacity denominator. The workload plane
explains it. The difference between the transport plane's mesh total and the
workload plane's sum is **encapsulation overhead plus unattributed traffic**,
and it is rendered as its own explicit series — never hidden, never folded into
a neighbouring category to make the chart tidy.

### 2. A closed category grammar

Categories are a **closed enum**, defined once in
`ops/observability/bandwidth-categories.yaml` and consumed by every producer.
An unrecognised category is not invented at render time — it renders as its raw
key, the F-328 lesson (a surface that silently dropped rows whose section its
own HTML did not recognise). A typo in a route map produces a visible
`other-declared` row, never a vanished one.

### 3. Declared capacity is declared, and says so

`net_link_capacity` carries `{down_mbps, up_mbps, source, asserted_on}` where
`source ∈ {measured, declared, unknown}`. Where `source: unknown`, the
dashboard shows **raw bytes/s and the words "capacity not declared"** — it does
not fabricate a percentage. This follows the relay-health rule (a `--deep`
throughput number or nothing, never a fabricated %) and ADR 1045's honesty
about declared-vs-measured expectations.

### 4. It lands in Grafana, not in a new surface

The dashboard is `ops/compose/goat-obs/grafana/dashboards/mesh-bandwidth-attribution.json`.

Building it as a sixth inspection surface was rejected. It is a time-series
capacity question with a drill-down, which is Prometheus+Grafana's native
shape; and under ADR 1123 a new operator-facing surface that queries on request
is exactly the thing the fleet has just finished bounding. Grafana reading an
existing scrape amplifies nothing.

### 5. Reuse the scrape paths that already exist

No new ports and no new scrape jobs for the HTTP plane: Vector already runs a
`prometheus_exporter` sink on `logship_metrics_port`, scraped as
`job=goat_logship`, so the byte counters attach as an additional input to the
sink that is already there. The non-HTTP plane writes a Prometheus textfile
into `/var/lib/node_exporter/textfile`, the collector directory the
`node-exporter` role already provisions.

## Consequences

- Per-mesh-host `node_network_*` requires the existing `node-exporter` role to
  be deployed to mesh hosts and bound to the **mesh IP** — the obs host's own
  exporter binds the docker bridge gateway deliberately, which is not reachable
  from the mesh. A new `node_exporter_listen_addr` default keeps it off LAN.
- Per-peer HTTP attribution has a cardinality cost. `goat_http_peer_bytes_total`
  omits `service` to bound the product to `peers × categories × directions`,
  and is toggleable. At the ADR 1122 envelope (10k+ peers) the per-peer family
  must be off; the budget is stated in the design doc rather than discovered.
- `nnh-snapshot.py` gains a second, lighter invocation mode on its own timer.
  The hourly full snapshot — the security inventory — is unchanged in shape and
  cadence, so the ADR 1036 `net` envelope and its grading are untouched.
- The workload plane's coverage is honest but partial on day one: the netbird
  control plane, replication, image pulls, audit offload and backup are
  attributed from TCP socket byte counters, which exist for TCP and not for UDP.
  UDP work other than WireGuard (chiefly DNS and NTP) is small and is reported
  as `not measured` rather than as zero.

## Alternatives rejected

- **One stacked chart of all categories.** Double-counts tunnel payload against
  tunnel envelope. This is the central error the two-plane split exists to
  prevent.
- **Per-packet capture / netflow / eBPF flow accounting.** Gives exact
  attribution and costs a new privileged data path, a new daemon on every host,
  and a per-packet budget on small edge hardware. The socket-counter plus
  access-log approach reuses two taps that already exist and is sufficient to
  answer the capacity question.
- **A sixth inspection surface.** §4.
- **Deriving capacity from observed peak throughput.** Peak observed is a floor
  on capacity, not a measure of it; a link that has never been driven hard would
  report a small pipe and then show 95% utilisation on a trivial load.
- **Attributing HTTP bytes from Traefik's Prometheus metrics.** Traefik exposes
  no size counter; only the access log carries `DownstreamContentSize`.
