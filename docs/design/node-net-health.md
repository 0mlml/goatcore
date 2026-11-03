# Node net-health — per-node connection inventory on the environment-health surface

Per goatnet node: an **hourly network health check + connection inventory**,
published reveal-only as the `net` domain of the environment-health surface —
a new `oneof` arm on the [ADR 1036](../adr/1036-environment-health-surface.md)
envelope, exactly the extension path that ADR anticipated (precedent: the mesh
domain, [ADR 1042](../adr/1042-mesh-link-health-domain.md)). The complement of the
relay-health dashboard: relays aren't mesh peers so
a blackbox prober watches them from outside; **nodes ARE mesh peers, so each
node watches itself and reveals what it sees.**

Questions it answers, per node: how many connections does this node hold,
which are active vs stale, what throughput/latency rides each — and, the
security question underneath: **is this node talking to anything it shouldn't
be?**

## The closed-graph property (why this is tractable)

A goat node's expected connection graph is **closed**: mesh (WireGuard peers)
+ fabric (zenoh sessions) + a short declared baseline (enrollment, time,
updates). Anything else is worth looking at. That property is what turns "list
every socket" from noise into signal — the collector classifies **every**
socket on the node (listening + established, TCP/UDP) into exactly four
buckets:

1. **mesh** — WireGuard peer traffic: sockets on the netbird interface /
   to-from mesh IPs (`100.64.0.0/10`), plus the WireGuard UDP transport
   itself.
2. **fabric** — zenoh sessions (`:7447` mTLS et al.; the zenohd/client
   processes and their links).
3. **declared baseline** — matched a rule in the per-node-class **baseline
   registry** (below). Every rule carries its *why*; the registry doubles as
   the node-class **egress contract** — a compliance artifact in its own
   right for the ConMon pipeline (ADR 1078).
4. **undeclared** — everything else. Rendered as a first-class **risk list**
   with process attribution (pid/comm, cgroup → container name), remote
   `ip:port`, and **first-seen / last-seen persisted across snapshots**.
   Presence of undeclared connections **degrades the node's health grade**
   (amber "N undeclared", never silently green), and each is also emitted as
   an OCSF event through goat-logship (ADR 1078) so it lands in the
   cyber-logging/ConMon stream — the first live producer for the network
   class that has been dormant there.

Loopback-only sockets (both ends on `127.0.0.0/8`/`::1`) are intra-node, not
part of the node's connection *graph*, and are excluded from classification.
A listener bound to `0.0.0.0`/a routable interface is part of the graph and
classifies like any other socket.

## Boundary (explicit, to prevent scope creep)

- **Observation, not enforcement.** Blocking undeclared egress is the
  already-designed egress-proxy cascade (Block 17,
  [egress-proxy.md](egress-proxy.md), ADR 0806). This collector is that
  design's **observability precursor and measured-reality feeder**: the
  baseline registry it validates hourly is exactly the allowlist the egress
  proxy will enforce. It never drops, resets, or firewalls anything.
- **Hourly snapshot inventory, not a continuous IDS.** Per-packet /
  per-syscall / flow capture stays with goat-logship + auditd (ADR 1078
  Phase 3). An adversary active only *between* snapshots is out of scope
  here by design; the snapshot's job is drift and persistence, not
  real-time intrusion detection.
- Not in scope: per-packet relay service time (stays
  [relay-packet-service-time.md](relay-packet-service-time.md),
  designed-not-built); historical retention beyond the existing
  downsampled-series pattern; non-ZAGROS goatnets in the first rollout (the
  roles are goatnet-parameterized; ZAGROS is the live-fire target).

## Collector (per node, hourly + on-demand)

**The collector enumerates container network namespaces, not just the host's**
`ss` reports the namespace it runs in, so for as long as the snapshot
ran only on the host, every socket belonging to a container with its own netns
was structurally invisible — measured on an ZAGROS management host as 2 sockets for the Zenoh
router against 71 in its own `/proc/net/tcp`, and workload attribution
coverage of 7.2%. The tell was that BOTH catch-all buckets read flat zero: an
incomplete category map makes them large, so nothing was arriving to be
categorised at all and adding rules to the egress contract could not have moved
the number. It now runs the same `ss` inside each distinct container namespace
via `nsenter`, keeping one parser and one definition of what a socket is.

Three things that follow, each a way to be wrong that is worse than the gap:

- **Containers sharing the HOST namespace are excluded** from enumeration —
  their sockets are already in the host scan, and counting them twice would
  inflate exactly the figure used to judge headroom. Under-reporting is visible
  as low coverage; over-reporting is invisible.
- **Byte counters are keyed by namespace.** Container address space is not
  unique: two containers on the default bridge can both hold `172.17.0.2:443`,
  and a `(local, remote)` key merged across namespaces attributes one
  container's traffic to another.
- **The bridge-listener rule is namespace-dependent.** A listener bound to
  `172.17.x` is plumbing when seen from the host, where that address belongs to
  the bridge; seen from inside the container it is the container's own address
  and the listener is the real service — `:443` on Traefik, `:7447` on the
  Zenoh router. Applying the host rule inside the namespace drops precisely the
  sockets the enumeration exists to find.

`goat_net_attribution_netns_targets` and `..._netns_read` report how much of
the node was actually enumerated, so a low coverage ratio can be read as
blindness rather than idleness. Until those two agree, no ratio computed from
the sample is honest.

A host-side snapshot script (systemd timer, hourly; run-now via
`systemctl start node-net-health-snapshot.service`) writes one JSON snapshot
atomically; a small publisher (templated on `goat-mesh-health` — a normal
mTLS Zenoh client peer) grades it and publishes the envelope. Two planes +
the classifier:

- **Mesh plane** — per-peer WireGuard state from the node's own
  `netbird status --json` (passive; netbird already measures this for its
  own connection management): peer, connection type P2P/relayed,
  last-handshake age, rx/tx byte deltas since the previous snapshot →
  throughput (the divisor = the real inter-snapshot gap, carried in the
  envelope), and latency where the daemon reports it (0 = not measured,
  rendered "not measured" — never derived from a proxy number).
  - **Stale definition (explicit):** a connected peer is **stale** when its
    WireGuard handshake age exceeds **3 × the WireGuard rekey window (120 s)
    = 6 minutes**. Rationale: the netbird persistent-keepalive (25 s) keeps
    NAT state alive but does not drive handshakes; under any traffic the
    handshake renews on the ~120 s rekey timer, so 3 rekey windows of
    silence is a peer that is enrolled-but-not-exchanging. A **disconnected**
    peer is neither active nor stale — absence is not illness (ADR 1036
    §3.4); per-peer liveness is panoscope's job.
- **Fabric plane** — zenoh sessions on nodes running a router/client with
  the admin space enabled (session count, per-session link stats where
  exposed). Where per-connection latency/throughput isn't natively exposed,
  the field renders **"not measured"** — honesty discipline.
- **Classifier** — full socket inventory (`ss -tunap` + cgroup → container
  resolution) → the four buckets above. Classification precedence: mesh →
  fabric → baseline → undeclared (first match wins; mesh/fabric are
  structural, the registry only ever *adds* allowed egress).

### Baseline registry (the node-class egress contract)

Declared in group_vars, per node-class, consumed by the collector role. Every
rule is named and carries its **why** — an unexplained allowlist row is
indistinguishable from a hole:

```yaml
# group_vars/<env>_hosts — the node-class egress contract.
net_baseline_node_class: mgmt-host
net_baseline_rules:
  - name: netbird-enrollment
    why: "enrollment + control to the relay ingress (netbird.<zone>:443)"
    proto: tcp
    remote_ports: [443]
    comm: [netbird]
  - name: chrony-anchors
    why: "declared time anchors only (universal-ntp-hygiene.md)"
    proto: udp
    remote_ports: [123, 4460]
    comm: [chronyd]
  - name: apt-mirrors
    why: "OS package updates"
    proto: tcp
    remote_ports: [80, 443]
    comm: [apt, http]
  - name: ghcr-pulls
    why: "image pulls (oci-goat-substrate.md)"
    proto: tcp
    remote_ports: [443]
    comm: [dockerd]
  - name: aws-ssm
    why: "SSM agent — cloud nodes only"
    proto: tcp
    remote_ports: [443]
    comm: [amazon-ssm-agen, ssm-agent-worke, ssm-session-wor]
  - name: acme
    why: "LE DNS-01/HTTP-01 cert renewal (Traefik)"
    proto: tcp
    remote_ports: [443]
    container: [traefik]
```

Matching semantics: a socket matches a rule when **every field the rule
specifies** matches (`proto`, `local_ports`/`remote_ports`, `remote_cidrs`,
`comm`, `container`); any matching rule → bucket 3, stamped with the rule
`name`. Fail-closed: no rule → undeclared. An over-broad rule is a review
problem, not a runtime one — the registry is PR-gated like any group_vars.

### Undeclared handling

- Persisted state (`/var/lib/node-net-health/undeclared-state.json`) keys
  each undeclared observation by `(remote ip:port, comm, container)` and
  carries first-seen/last-seen across snapshots — a one-off curl and a
  month-long implant look different on the page.
- Health: `undeclared_count > 0` → the node's envelope grades **amber**,
  reason `"N undeclared"`. Never silently green. (Red is not auto-assigned:
  this is an observation surface; escalation policy belongs to the ConMon
  side.)
- Each **newly seen** undeclared observation appends one NDJSON line to a
  local tail (`/var/lib/node-net-health/undeclared.ndjson`) that
  goat-logship tails and normalizes to OCSF **Network Activity (4001)** —
  the same local-tail pattern the audit-subscriber uses for `fabric:write`.
  Default-off logship source toggle (`logship_source_net_undeclared`), like
  every other source.

## Envelope (`net` domain arm)

`EnvHealthV1{domain:"net", scope:<node host_id>}` on **`ops/net/health/<node>`**,
new `oneof detail` arm (canonical schema
`ops/zenoh-trial-peers/time-health/proto/env_health_v1.proto`; the arm lands
with the collector PR):

```proto
message NodeMeshPeerV1 {
  string peer_fqdn        = 1;
  string peer_ip          = 2;
  bool   active           = 3;  // handshake age <= 3x rekey window (360 s)
  int64  handshake_age_ms = 4;
  string conn_type        = 5;  // "P2P" | "Relayed" | "" disconnected
  uint64 rx_bytes_delta   = 6;  // since previous snapshot
  uint64 tx_bytes_delta   = 7;
  double latency_ms       = 8;  // 0 = not measured (rendered as such)
}
message NodeFabricSessionV1 {
  string zid     = 1;
  string whatami = 2;           // router|peer|client
  uint32 links   = 3;
}
message NodeConnV1 {
  string bucket        = 1;     // mesh|fabric|baseline|undeclared
  string proto         = 2;     // tcp|udp
  string state         = 3;     // listen|estab|...
  string local_addr    = 4;
  string remote_addr   = 5;     // "" for listeners
  uint32 pid           = 6;
  string comm          = 7;
  string container     = 8;     // "" = host process
  string baseline_rule = 9;     // matching registry rule name (bucket=baseline)
  int64  first_seen_unix_ms = 10;  // persisted (undeclared)
  int64  last_seen_unix_ms  = 11;
}
message NetDetailV1 {
  repeated NodeMeshPeerV1      mesh_peers      = 1;
  uint32                       mesh_active     = 2;
  uint32                       mesh_stale      = 3;
  repeated NodeFabricSessionV1 fabric_sessions = 4;
  repeated NodeConnV1          conns           = 5;  // full classified inventory
  uint32                       undeclared_count = 6;
  string                       node_class      = 7;  // which egress contract applied
  uint32                       snapshot_gap_secs = 8; // real delta divisor provenance
}
// EnvHealthV1.oneof detail: NetDetailV1 net = 14;
```

(The tee-up called this `ConnHealthV1`; it lands as `NetDetailV1` to match
the `<Domain>DetailV1` convention of the existing arms.)

Grading (5-state honesty, worst grounded wins): snapshot missing/stale (>2×
cadence) → **unmetered**; undeclared > 0 → **amber**; all mesh peers stale
while the node claims connectivity → **amber**; otherwise **green** only when
grounded by a fresh snapshot. Reveal-only: each node self-publishes; a
passive reader rolls up; **no central poller of nodes** (brokerless-safe,
[zenoh-brokerless-consequences.md](zenoh-brokerless-consequences.md)).

## Rendering — three altitudes (the ADR 1036 pattern)

1. **`goat health`** gains a `net` domain row (same wire, `health_wire.rs`).
2. **Node-list page** — same frameworkless single-file pattern as
   relay-health, hosted beside it on the management host (`node-net-health.html`, fed by
   a `node-net-health.json` written by the passive roll-up reader): fleet
   node list → per-node drilldown with the connection table (peer · state ·
   active/stale · throughput · latency · vantage label — the vantage is the
   node itself, stamped like every relay-health number) and the undeclared
   risk list front and center.
3. **Stethoscope deep-link** per node (the cross-surface contract,
   [zenoh-surface-deep-links.md](zenoh-surface-deep-links.md)).

## Live-fire gates (ZAGROS)

- **B2 (collector):** one ZAGROS node publishes; active/stale verified against
  a real idle peer; a **seeded off-baseline connection** (a `curl` from the
  node to a non-baseline host) appears in the undeclared list **attributed
  (pid/comm) within one snapshot**, and its NDJSON line reaches the logship
  tail.
- **B3 (rollout):** hourly envelopes from all ZAGROS mesh nodes; the node-list
  page shows the real active/stale split; `goat health` shows the `net` row.

## Principles checklist

- Honesty: no fabricated %, no proxy latency, vantage labels on every
  number, grey/unmetered when unmeasured, amber never suppressed.
- Self-contained: node-list page has zero external assets; works off-mesh
  from the laptop against a copied JSON.
- Brokerless: reveal-only self-publish; passive roll-up; no central poller.
- Fail-closed: unmatched socket = undeclared; missing snapshot = unmetered,
  never green.
- Generalized: roles + registry are goatnet/node-class-parameterized; only
  the live-fire inventory is ZAGROS-specific.

## Status — deployed fleet-wide on ZAGROS 2026-07-15

Every ZAGROS fleet node runs the collector + publisher (`mgmt-host`,
`cloud-host` and `mesh-dns-host` classes);
the management host additionally runs the rollup reader + serves the node-list page at
`relay-health.<site>.<zone>/node-net-health.html` behind the same Ory admin
gate (verified 302→login unauthenticated). PRs #1613 (design), #1618
(pipeline), #1622 (rollup + page + `goat health --domain net`).

**Live-fire gates passed:**

- `goat health --domain net` received live envelopes (amber, every
  connection classified on the management host; per-node roll-up line + the undeclared table).
- **Seeded off-baseline connection** (held-open TCP to an external IP)
  appeared in the undeclared list **process-attributed within one snapshot**,
  and its NDJSON line landed in the goat-logship tail.
- active/stale split verified against the real fleet (before and after a
  mesh repair, by the 3× rekey rule — both readings consistent with ground truth).
- Classifier noise-tested live: intra-node exclusions remove most raw
  sockets; the ZAGROS management-host egress contract then leaves single-digit
  undeclared.

