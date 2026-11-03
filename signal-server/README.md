# signal-server

Custom federated signal server for the goat multi-site Netbird mesh.
Design context: `docs/design/ha-multi-site.md` §3-§4.

## What it does

- Implements the upstream Netbird `SignalExchange` gRPC service. Unmodified
  upstream clients connect and register their WireGuard public keys without any
  awareness that this isn't upstream.
- Adds `SignalFederation.Forward` — a server-to-server RPC. On a local registry
  miss, the serving signal server broadcasts the encrypted message to all peer
  signal servers. The one holding the target peer delivers it. That's what
  makes cross-site P2P work when peers at different sites are registered with
  different signal servers.
- Not a Netbird fork. Not in upstream. Owned entirely in this repo.

## Status

Scaffold: proto contracts + `Unimplemented*` server stubs +
multi-arch `Dockerfile` + GitHub Actions CI publishing to
`ghcr.io/usareur-af/signal-server`. Handlers return `Unimplemented` until we fill
them in — that's the next commit.

## Layout

```
signal-server/
├── cmd/signal-server/main.go   entrypoint (gRPC :10000, metrics :9090)
├── internal/server/            server state + RPC handlers
├── proto/                      gRPC protobuf contracts
├── gen/                        protoc output (generated at build time, gitignored)
├── Dockerfile
├── go.mod
└── README.md
```

## Build

CI (`.github/workflows/signal-server.yml`) builds multi-arch images
(`linux/amd64` + `linux/arm64`) and publishes to
`ghcr.io/usareur-af/signal-server` on every push to `main` that touches this
directory.

Local one-arch build:

```
cd signal-server
docker build -t signal-server:dev .
```

## Run

```
docker run --rm -p 10000:10000 -p 9090:9090 \
  ghcr.io/usareur-af/signal-server:latest \
  --node dev-a --grpc-addr :10000 --metrics-addr :9090
```

### Flags

- `--node` — node name; included in logs and metric labels.
- `--grpc-addr` (default `:10000`) — gRPC listen address (serves both
  SignalExchange and SignalFederation).
- `--metrics-addr` (default `:9090`) — HTTP listen address for `/metrics`
  and `/healthz`.
- `--federation-peers` — comma-separated list of peer signal-server
  addresses for cross-site federation. Empty = standalone.
- `--max-inflight` (default `1000`) — maximum concurrent in-flight gRPC
  handlers (unary + stream combined). Excess returns
  `codes.ResourceExhausted`. Set to 0 for unlimited.
- `--federation-circuit-consecutive-failures` (default `5`) —
  consecutive `Forward` failures to a single federation peer that trip
  its breaker open.
- `--federation-circuit-open-duration` (default `30s`) — how long the
  per-peer breaker stays open before allowing one half-open probe.

### Environment

- `SIGNAL_SENDER_RATE_LIMIT` / `SIGNAL_SENDER_RATE_BURST` /
  `SIGNAL_NEGATIVE_CACHE_TTL` — ADR 1081 L2 storm shedding, default off
  (see `internal/server/ratelimit.go` / `negcache.go`).
- `SIGNAL_FEDERATION_TOKEN` — shared bearer token on the federation
  surface (mesh-control-path Phase 3, ADR 1085 Amendment 1). When set,
  unauthenticated federation RPCs are rejected
  (`codes.Unauthenticated`) and the token is attached to every outbound
  `Forward`. Same value on every federating server of a goatnet; may be
  a comma-separated accept-list for rotation (server accepts all
  entries, client sends the first — ship `new,old`, flip, drop `,old`).
  Empty = auth off (mesh-only posture); a warning is logged if
  federation peers are configured without it, and an info line
  ("federation auth enforced") is logged when it is active — deploy
  plays assert that line to prove the running image implements fedauth.
- `SIGNAL_FEDERATION_AUTHORITY` — overrides the HTTP/2 `:authority` on
  outbound federation dials. Set to the peer's federation hostname on
  dual-path (proxy) topologies so the peer's Traefik router can
  Host-match the public leg. Empty = default (the dial target).
- `SIGNAL_FEDERATION_FORWARD_TIMEOUT_MS` — per-peer `Forward` deadline
  override. 0/unset = the 200 ms mesh-tuned default; dual-path goatnets
  set ~1000 so a mesh-flap failover onto the relay leg doesn't feed the
  circuit breaker spurious deadline failures. The broadcast budget
  scales with it (`max(500ms, 2×timeout)`).
- The negative cache records recipient-offline only when the federation
  broadcast was COMPLETE — every peer gave a definitive answer for that
  call. A breaker skip, RPC error, or timeout suppresses caching for
  that miss ("absent across federation" is unknowable then).

## Observability

By design (see `docs/design/ha-multi-site.md` §7.3),
observability is built in from day one:

- Structured JSON logs via `slog` with correlation IDs (`sender-key`,
  `remote-key`, `timestamp`) at every forward hop.
- Prometheus `/metrics` on `--metrics-addr`:
  - per-forward latency histograms
  - forward success / failure counters
  - connected-peer gauge
  - `signal_server_inflight_handlers{kind="unary"|"stream"}` gauge
    (Block 50B.1 bounded-concurrency limiter)
  - `signal_server_handler_rejected_total{reason="saturation"}`
    counter
  - `signal_server_panic_total{goroutine,kind}` counter (Block 50B.2
    panic-recovery surface for both background goroutines and gRPC
    handlers)
  - `federation_forward_timeout_total{peer}`,
    `federation_circuit_state{peer}` (0=closed, 1=open, 2=half-open),
    `federation_circuit_skipped_total{peer}`,
    `federation_forward_duration_seconds{peer}` histogram (Block 50B.6
    per-peer circuit-breaker telemetry)
