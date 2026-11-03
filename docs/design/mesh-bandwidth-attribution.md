# Mesh bandwidth attribution — how full is the pipe, and what is filling it

> **60-second summary:** For a given mesh host, how much of its up/down link is
> in use, which category of work is consuming it, and which peers drive each
> category — asked **before** the link saturates. The load-bearing design point
> is that this is **two planes that are never added together**: a Zenoh message
> crossing the mesh is counted twice on one host at one instant (as WireGuard
> UDP on the NIC, as TCP :7447 inside the tunnel), so a single stacked chart
> would double exactly the number an operator uses to judge headroom. Lands as
> a Grafana dashboard in goat-obs, not as a new inspection surface.

| | |
|---|---|
| Status | Built. Collector, rules, dashboard and CI; percent-of-link panels are blank until a capacity is declared — see §8. |
| Anchor ADR | [ADR 1128](../adr/1128-mesh-bandwidth-attribution-two-planes.md) |
| Related | [ADR 1036](../adr/1036-environment-health-surface.md) (the `net` domain this extends) · [ADR 1045](../adr/1045-qos-throughput-envelopes.md) (per-actor expectation) · [ADR 1123](../adr/1123-client-demand-must-not-amplify.md) (why not a new surface) · [ADR 1058](../adr/1058-relay-metrics-self-publish-ingest.md) (never a fabricated %) |
| Reference designs | [`node-net-health.md`](node-net-health.md) · [`grafana-dashboard-suite.md`](grafana-dashboard-suite.md) · `relay-health-dashboard.md` · [`qos-throughput-envelopes.md`](qos-throughput-envelopes.md) |

---

## 1. The question, and why nothing answered it

**"Is this host's pipe filling up, with what, and driven by whom?"** — asked
preemptively, not after someone notices pages loading slowly.

A 2026-08-13 audit of every dashboard, metric and observability surface found
five partial views and three structural gaps. The partial views are real work
and none of them is wrong; they simply each measure a different denominator,
and none measures **bytes per mesh host**.

| Surface | Has | Missing |
|---|---|---|
| `edge-http-red.json` | Traefik req/s + latency per router and per service | No bytes — Traefik's Prometheus output has no size counter. No client dimension. |
| `fabric-tenants.json` | `goat_audit_sample_bytes_total{vendor}` + `{key_pattern}` | Byte-accurate with tenant drill-down, but it is the audit subscriber's view of *published samples*, not wire bytes on any host |
| `node-exporter-full.json` | `node_network_{receive,transmit}_bytes_total` | One host only; no category split |
| `mesh-control-plane.json` | signal-server liveness, goroutines, RSS | No byte accounting at all for the control plane |
| `capacity-forecast.json` | `predict_linear` over disk / memory / TSDB | No network arm |
| `node-net-health` | Per-WG-peer rx/tx **and** socket→pid/comm/container, same host same moment | Socket rows had no bytes; hourly; buckets are *security* buckets |

The three gaps, each verified rather than assumed:

1. **No denominator existed.** `link_capacity`, `uplink_mbps`, `bandwidth_mbps`,
   `capacity_bps` — zero hits across `ops/` and `docs/design/`. Percent-of-pipe
   had nothing to divide by.
2. **Prometheus saw one host.** `job: node-exporter` had exactly one static
   target, `host.docker.internal:9100`; `cadvisor` and `process-exporter`
   likewise.
3. **The HTTP bytes were collected and discarded.** Traefik runs with
   `--accesslog.format=json` and Vector already tails it; `app_http_ocsf`
   extracted five fields and dropped the sizes.

## 2. Two planes, never summed

This is the part a reasonable person gets wrong on the first attempt, so it is
stated before any mechanism.

A Zenoh message from host A to host B is counted **twice on host A at the same
instant**:

```
   application  ─────────────────────────────►  TCP :7447        ── WORKLOAD plane
                                                    │                (what the mesh carries)
                                                    ▼
   WireGuard    ─────────────────────────────►  UDP to peer      ── TRANSPORT plane
                                                    │                (what the pipe carries)
                                                    ▼
   physical NIC ─────────────────────────────►  eno1 / eth0
```

The tunnel payload and the encrypted envelope carrying it are the same bytes at
two layers. Adding them is not a rounding error; it inflates precisely the
figure used to decide whether a link has headroom.

| Plane | Answers | Measured at | Denominator |
|---|---|---|---|
| **Transport** | How much of my physical pipe is in use? | physical NIC + per-WG-peer counters | declared link capacity |
| **Workload** | Who and what is consuming the mesh? | in-tunnel sockets + Traefik access log | mesh transport total |

The transport plane's classes **sum to the NIC total by construction** —
`off-mesh` is defined as the exact remainder — so it cannot quietly lose bytes.
The workload plane's remainder is `unattributed`, drawn always and explicitly.

### 2.1 The second double-count, one layer down

Found while building the dashboard, and worth naming because it is the same
mistake wearing different clothes: **Traefik's browser-facing sockets are
visible to both taps.** `ss` counts the socket; the access log accounts for the
same bytes broken out by surface category. Stacking both would have inflated
the workload plane by the whole of the web tier.

Resolved by categorising connections **accepted on :80/:443** as
`http-delegated` and excluding that row from the workload stack, so the two
taps partition rather than overlap. The key is the **local** port — that is
what separates an accepted request on :443 from an outbound image pull *to* a
remote :443, which must stay `image-pull`.

The row is still emitted. Comparing it against the HTTP plane's total is a free
cross-check: two independent taps on one flow, so their ratio near 1.0 means
they agree, and near 0 means the access log has stopped.

### 2.2 The third double-count, found in production

The two above were caught in review. The third was not, and it was the largest.

First live data showed 127 MB rx in a single workload
category. All of it was `rathole` on :2333 — **the relay tunnel**. Those bytes
are the transport plane's `mesh-relayed`, seen one layer down: the tunnel
*carrying* the mesh, not work riding on it.

So sockets that ARE the mesh transport — the rathole relay tunnels, the netbird
control connection, WireGuard itself — get the `transport-underlay` category
and are excluded from the workload stack, exactly as `http-delegated` is. Three
instances of one mistake, and the pattern is worth stating plainly:

> **Whenever a byte is visible to two taps at two layers, the answer is a
> partition and an excluded row — never a sum.**

The row is still emitted. Comparing it against the transport plane is another
free cross-check.

### 2.3 What the socket tap cannot see, and why that is not a bug

The same first-light run showed **zero fabric bytes**, which had two causes.

One was a real defect: `classify()` tests mesh *before* fabric — correct for a
risk inventory, where "is this on the mesh" is the first question — so a zenoh
session to a mesh peer arrives as `bucket: mesh` and never reached a
`bucket == "fabric"` test. Fabric is now decided on port/comm instead.

The other is structural and stays. On a single-Thor goatnet the zenoh router
and its clients are containers on the same docker bridge, and the collector
deliberately excludes flows with both ends in the bridge range — they are
intra-node plumbing, not the node's connection graph. **Fabric traffic that
never leaves the host is therefore invisible to the socket tap, by design.**
Where the fabric spans hosts it crosses the mesh and is counted. This is a
coverage boundary to know, not a hole to plug: closing it would mean counting
loopback and bridge traffic against a physical link that never carries it.

## 3. The denominator is declared, and says so

`node_exporter_link_capacity` in group_vars carries
`{down_mbps, up_mbps, source, asserted_on}`, rendered to
`goat_link_capacity_bytes_per_second{direction,source}`.

| `source` | Meaning |
|---|---|
| `measured` | A throughput test was run and the result written down |
| `declared` | The contracted / ISP-plan / NIC-spec figure |
| `unknown` | Nobody has said. **Panels show raw bytes/s and "capacity not declared" — never a fabricated percent.** |

Default is `unknown` and fail-closed: a host nobody has characterised must not
inherit someone else's pipe size. The `> 0` gate in the recording rules means
such a host produces **no utilisation series at all**, so both the panels and
the alerts are silent for it rather than confidently wrong. There is a promtool
case asserting exactly that, because a regression would be invisible — the
panels would simply start showing plausible numbers.

`asserted_on` ages the claim: `LinkCapacityDeclarationStale` fires at a year,
on the grounds that a capacity nobody has re-checked is a claim, not a fact,
and every percentage rests on it.

**Deriving capacity from observed peak was rejected.** Peak observed is a floor
on capacity, not a measure of it; a link never driven hard would report a small
pipe and then show 95% utilisation under a trivial load.

## 4. Where the bytes come from

Two taps, deliberately complementary — neither covers the other's blind spot.

| Tap | Covers | Blind to |
|---|---|---|
| **Socket counters** (`ss -tinpH`, node-net-health `--fast`) | Long-lived TCP: fabric, replication, image pulls, mesh control, backup, audit offload | UDP (kernel byte counters are a TCP facility); connections that open and close between samples |
| **Traefik access log** (Vector `log_to_metric`) | Every HTTP request including short-lived ones, with client and route | Anything not through Traefik |

Non-WireGuard UDP — chiefly DNS and NTP — is reported under the `not-measured`
category rather than as zero, because zero would be a lie.

Per-peer WireGuard counters come from `netbird status --json`, which the daemon
already maintains for its own connection management; the collector reads it
passively. That is where `mesh-p2p` vs `mesh-relayed` comes from, and it means
**relay-forwarded traffic needed no new collection at all**.

### 4.1 Counters, not deltas

Socket byte counters are cumulative-per-socket. Rates come from deltas against
the previous sample, floored at 0 (a reused 4-tuple looks like a counter reset).
Those deltas accumulate into a **persisted running total**, and the running
total is what the textfile exposes — a textfile publishing a per-interval delta
would make `rate()` nonsense. Losing the state file resets a counter to zero,
which Prometheus already handles as a counter reset.

### 4.2 Two timers, not one

`--fast` runs on its own 60s timer. The hourly full snapshot — the ADR 1036
security inventory — is unchanged in shape and cadence, and the two keep
**separate delta baselines** so neither one's interval depends on when the other
last ran. Coupling them would have forced a choice between an hourly bandwidth
chart and 60× the inventory cost.

The fast timer carries no `RandomizedDelaySec`, unlike the hourly one: its
output is a rate, the sample gap is the denominator, and jittering it makes
every derived figure slightly wrong for no benefit.

## 5. The category grammar is closed

One definition — `ops/observability/bandwidth-categories.yaml` — consumed by
every producer and enforced by `ops/ci/check-bandwidth-categories.sh`.

Closed means an unrecognised key **renders raw rather than being dropped**, the
lesson from F-328 (a surface that silently discarded rows whose section its own
HTML did not recognise). A typo produces a visible odd row, never a vanished
one.

Categories in brief — the file carries the full *why* for each:

| Group | Keys |
|---|---|
| Web (access log) | `surface-static` `surface-page` `surface-stream` `api` `identity` |
| Fabric + control | `fabric` `mesh-control` |
| Bulk (the ones that saturate) | `replication` `image-pull` `audit-offload` `backup` `filedrop` |
| Infrastructure | `observability` `messaging` `time` |
| Structural | `http-delegated` `other-declared` `undeclared` `not-measured` `unattributed` |
| Transport plane | `mesh-p2p` `mesh-relayed` `off-mesh` |

Three of these earn their place by being easy to omit:

- **`mesh-relayed`** is the scarcest and only metered-and-billed path, and the
  same payload is paid for twice (node→relay, relay→peer). A peer quietly
  losing its P2P path is a common, expensive regression; `RelayedMeshShareHigh`
  exists for it.
- **`surface-stream`** scales with *concurrent viewers*, not request count, so
  it behaves nothing like a page load and must not share a bucket with one.
- **`time`** is tiny, and is listed precisely because it is tiny — it is the one
  an operator must be able to confirm is *not* being starved.

### The socket numerator spans namespaces; the NIC denominator does not

netview rendered **"126% of this pipe"** on 2026-08-15. A workload class cannot
exceed the interface carrying it, so the ratio itself was the bug. Root cause,
after six hypotheses were eliminated by measurement:

`goat_net_workload_bytes_total` is built by `nnh-snapshot.py`, which scans the
host netns **and every container network namespace** (`netns_targets()` /
`nsenter`, :299-360). `goat:link_bits_per_second:rx` sums **physical interfaces
only** — its device filter explicitly excludes `veth.*`, `docker.*`, `br-.*`.

So the numerator counts bytes whose carrier the denominator deliberately drops.
Measured:

| | |
|---|---|
| Physical NIC (the denominator) | 10.29 Mbit/s |
| veth + bridge (excluded from it) | **28.09 Mbit/s** |

Each ingress byte crosses the bridge several times — `docker-proxy` → traefik's
veth → the service's veth — so the numerator samples a pool ~2.7× larger than
what it is divided by. The excursions correlate with load because that is when
container-internal traffic is heaviest. Over one 7-hour span, 13.3% of samples
exceeded 1.0, peaking at 1.375.

**Rule.** A "share of the physical pipe" may only use **host-netns** sockets.
Container-netns sockets are the same bytes one hop later — the ADR 1128
"two planes, never summed" trap, appearing between namespaces rather than
between layers.

**Fixed 2026-08-17.** `goat_net_workload_bytes_total` now carries a
`netns="host"|"container"` label, so a consumer can ask the question it actually
means:

| Question | Selector |
|---|---|
| Share of the **physical pipe** | `netns="host"` |
| Total work on this node | no selector |

netview's two NIC-relative figures (`relay_tunnel_rx_bps`, `own_workload_rx_bps`)
select `netns="host"`. The impossible-ratio guard stays as a backstop — it should
now never fire, which makes it a useful alarm rather than a fig leaf.

Counters written before the label are emitted as `netns="unknown"` rather than
dropped, so the series does not appear to reset on upgrade. Note the label does
add a one-time discontinuity: old unlabelled series end and labelled ones begin,
which `rate()` sees as a counter reset.

### A second, independent defect in the same numerator

`node-net-health-attribution.service` takes **~145 s per run** (measured: finish
1786792429.98 → 1786792575.66) and restarts immediately, so its counter
increments about every 150 s no matter what `OnUnitActiveSec=60s` says. A 5 m
`rate()` window therefore holds roughly **two** increments, and Prometheus
extrapolates to the window edges.

This is real and worth fixing — `SOCKET_RI` now widens the window for every
socket-derived series — but **it is not the cause of the inversion**: widening
5m → 30m moved the frequency from 13.3% to 8.1% and made the *maximum* worse
(1.265 → 1.375). Recorded because it is the kind of plausible explanation that
would otherwise be adopted without testing.

### Name a class by what the bytes ARE, never by the machine they came from

The transport chart on netview once labelled its largest row **"relay ingress
tunnel (rathole)"**, sitting directly beside **"mesh relayed (WireGuard)"**.
Both names lead with *relay*, and they mean entirely unrelated things — one is
public HTTPS reaching this host through a tunnel, the other is WireGuard that
failed to establish P2P. An operator read the pair as relay-fleet load sharing a
denominator with host load and objected (2026-08-14). They were right about what
it implied; the arithmetic was never mixed.

Every row in that chart is `goat_net_workload_bytes_total{host=<this host>}` —
counters on **one machine's NIC**, summing to its downlink by construction. The
relays are separate machines, in other locations, with their own capacity, and
none of their counters are read. But a row named after the relay, described in
terms of the relay's haproxy job, points the reader at a machine whose capacity
is nowhere in the number.

Two rules follow, and they are cheap:

1. **Name the class by what the bytes are on the host being measured**, not by
   the hop they traversed. `inbound service traffic (public HTTPS, via tunnel)`
   says how they arrived without claiming the arrival point is the subject.
2. **State the denominator in the panel**, and say what is *not* in it. A chart
   whose denominator is implied will be given one by the reader, and on a
   multi-machine substrate they will pick the wrong machine.

The same trap is live wherever a per-host chart carries a class named for
another tier — this is the ADR 1128 "two planes, never summed" rule applied one
level down, to *labels* rather than to arithmetic.

Category assignment for sockets comes from the node-net-health **baseline
registry** in group_vars, which gains an optional `category:` on each rule. That
keeps the map beside the egress contract it annotates. A declared rule with no
category renders as `other-declared` — visible, because it means the contract
knows about traffic the category map does not.

## 6. Cardinality budget

Stated rather than discovered.

| Family | Cardinality | Default |
|---|---|---|
| `goat_net_workload_bytes_total{category,direction}` | ~40/host | on |
| `goat_net_transport_bytes_total{class,direction}` | 4/host | on |
| `goat_net_peer_bytes_total{peer,class,direction}` | peers × 4 | on |
| `goat_net_workload_peer_bytes_total{category,peer,direction}` | peers × categories × 2 | toggle |
| `goat_http_bytes_total{category,service,direction}` | categories × services × 2 | on |
| `goat_http_peer_bytes_total{category,client,direction}` | clients × categories × 2 | toggle |

At a ~90-peer goatnet the per-peer families are ~3.4k series per host, which is
fine. At the [ADR 1122](../adr/1122-scale-envelope-systems-vs-peers-pod-tier.md)
10k-peer envelope they are not, and both must be off:
`node_net_health_peer_attribution` and `logship_http_peer_attribution`.

`goat_http_peer_bytes_total` deliberately **drops the `service` label** —
carrying both would make the count the product of clients *and* services.

## 7. What is deployed where

Two Prometheus flavours exist and both are wired.

| Stack | Config | Scrape coverage | Rules |
|---|---|---|---|
| goat-obs | `ops/compose/goat-obs/prometheus/prometheus.yml` | new `node-exporter-mesh` job, targets rendered from `obs_mesh_node_targets`, omitted entirely when empty | `rule_files: rules/*.yml` picks it up |
| obs-host | `roles/obs-host/templates/prometheus.yml.j2` | existing `obs_scrape_targets`, now also labelled `host` | added to the `obs_rule_files` allowlist |

The `host` label is the join key throughout. Two traps here, both hit during
the build:

- The collector originally emitted its own `node` label while the scrape
  supplies `host`. Prometheus renames a colliding series label to
  `exported_host`, so every `by (host)` rule would have matched nothing —
  silently. The textfile now carries **no host-identifying label at all**.
- `obs-host` labelled targets `target_name`, not `host`. A `host` label was
  added **alongside** it (not as a rename — existing dashboards key on
  `target_name`).

## 8. Status, and what is deliberately not done

First light immediately produced three corrections that review had not — the
capacity textfile was unparseable in its default branch (§8.2), the relay
tunnel was being counted as workload (§2.2), and fabric read zero (§2.3). All
three are fixed and deployed. **Deploying is part of the design process here,
not the end of it**; none of the three was findable from the repository.

Not done, each for a stated reason:

- **Per-packet / netflow / eBPF flow accounting** was rejected: exact
  attribution at the cost of a privileged data path, a new daemon per host, and
  a per-packet budget on Jetson hardware. Two existing taps answer the capacity
  question.
- **Enforcement.** This observes. Throttling a category that is over budget is
  [ADR 1045](../adr/1045-qos-throughput-envelopes.md)'s *enforce* step and
  Block 17's egress proxy, and neither is in scope here.

### 8.2 What first light corrected

Deployed 2026-08-13. Within minutes, three defects that no amount of reading
would have surfaced:

| Found | How |
|---|---|
| Capacity textfile unparseable in its DEFAULT branch — a Jinja whitespace trim ate the newline after `# TYPE`, so node-exporter rejected the file and served none of it | `node_textfile_scrape_error 1`, not the ansible recap, which said `changed=12 failed=0` and was correct |
| The relay tunnel counted as workload — 127 MB rx of `rathole`, which is the transport plane one layer down | Reading the actual category split instead of assuming it |
| `fabric` read zero — `classify()` tests mesh before fabric, so zenoh-over-mesh never reached the fabric branch | Same |

The first is the one worth remembering: **an absent capacity metric and an
honestly undeclared capacity render identically** — blank percent panels. The
bug and the correct behaviour were indistinguishable on the very surface built
to show them. Only `node_textfile_scrape_error` separated them, and that is why
`ops/ci/check-textfile-exposition.sh` now renders every branch and lints it.

### 8.1 One verification gap, named

**The VRL is not validated by any automated gate.** Nothing in CI compiles the
Vector config, and `vector validate` cannot be trusted for VRL — it reported
"Validated" on a config that then crash-looped, because Vector compiles VRL
only at **topology build** (container start). The blast radius matters: it is
one config file, so a VRL error takes the whole goat-logship pipeline down, not
just these metrics.

What *was* done instead: the template renders to valid YAML, the rendered VRL
was read, and the category rules were replayed against realistic Traefik
`RequestPath` values — which is how two defects were caught before deploy:

- **`RequestPath` carries the query string** (it is `RequestURI`). Without
  stripping it, `/app.js?v=abc123` — the normal shape of a cache-busted bundle
  — misses the `\.js$$` anchor and lands in `surface-page`, quietly moving the
  largest class of static bytes into the dynamic-page row.
- **`$` in a regex must be written `$$`.** Vector runs env-var interpolation
  over the config before VRL sees it; a `$1` in this same template once
  crash-looped the collector with `Missing environment variable "1"`.

**Validate at deploy, on the target**, before trusting a green play:

```bash
sudo timeout 6 docker run --rm \
  -v /opt/goat-logship/vector.yaml:/etc/vector/vector.yaml:ro \
  timberio/vector:<pinned-tag> --config /etc/vector/vector.yaml
# clean start logs "Vector has started"; grep for error[E / Configuration error
```

## 9. Reading the dashboard

`Mesh Bandwidth Attribution` (uid `mesh-bandwidth-attribution`), four blocks:

1. **Transport plane** — utilisation stats, the stacked physical pipe with the
   declared ceiling as a dashed non-stacked overlay, and top peers.
2. **Workload plane** — stacked categories (socket categories excluding
   `http-delegated`, plus the HTTP plane), with `unattributed` in dark red.
3. **Drill-down** — pick `$category`; get the peers and the HTTP clients driving
   it, and its trend alone.
4. **Is this trustworthy right now** — sample age, sample gap, blind transport
   planes, peers and sockets sampled, and the web/socket cross-check.

Three habits worth having:

- **Never compare a workload total to a transport total.** Different layers.
- **A rising `unattributed` share means the map is stale**, not that traffic
  vanished. `BandwidthAttributionCoverageLow` fires below 50% for an hour.
- **Blank is not zero.** A host with no declared capacity shows blank
  percentages; a host whose collector has stopped shows aged values and trips
  `BandwidthAttributionStale`. Both are "unknown", and the honesty block says
  which.
