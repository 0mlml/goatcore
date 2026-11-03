# relay-probe — the relay-health prober + self-publish, as one Go binary

Typed replacement for the relay-health bash: `relay-health-probe.sh`
(465 lines) + `metrics-publish.sh` (107) grew into structured-data programs
(rate-diffing `/proc/net/dev`, history rings, `printf`-assembled JSON, `docker
logs` parsing) that had to keep a **byte-identical `deep` shape across two
files**. This consolidates them into one static, dependency-free binary with two
subcommands sharing **one `Deep` struct** ([`deep.go`](deep.go)) — so the wire
shape can't drift.

Why Go (ADR 0112 Go/CLI lane): a single **static, cross-compiled** binary
(`CGO_ENABLED=0`, stdlib only) drops onto any relay with no toolchain — *more*
portable than the old `bash` + `python3` + `ss` + `curl` chain. It's the same
shape as the other blackbox tools (`signal-federation-probe`, `smoketest`,
`snitch`), and the natural vehicle for the ADR-1055 per-peer relay stats.

```
relay-probe publish   # runs ON a relay: self-publish host metrics (ADR 1058)
relay-probe probe     # (planned) blackbox-probe the relay fleet -> JSON
```

`publish` reads the same env the systemd `EnvironmentFile` already provides
(`RELAY_NAME`, `RELAY_METRICS_INGEST_URL`, `RELAY_METRICS_BEARER_TOKEN`, optional
`RELAY_METRICS_INGEST_URL2`/`_TOKEN2`, `GOAT_RELAY_CONTAINER`, `HIST_KEEP`,
`STATE_DIR`, `PEERS_WINDOW_MIN`), so it's a drop-in swap for `metrics-publish.sh`.

## Status — track `relay-probe-go`, phased

1. **✅ Phase 1: `publish` ported + the shared `Deep` struct.** Builds, vets,
   cross-compiles amd64 + arm64.
2. **✅ Phase 2: `probe` ported** — the blackbox fleet prober (`:443` enroll
   ok-ratio, `:2333` rathole, `:3478` STUN, `:33073` ws/tcp + QUIC
   Version-Negotiation handshake, latency, `--ingest-dir` fold). `--deep` SSM +
   `--alert-sink` are the noted deferrals.
3. **✅ Phase 3: deploy-swap (publish side)** — the `goat-relay` role's
   `relay_metrics_publisher` flag (`bash`|`binary`, default `bash`): flip to
   `binary` and the controller cross-compiles this stdlib-only binary for the
   host arch (no release/token, cf. F-232), ships it, and the systemd unit runs
   `relay-probe publish`. Bash stays installed → rollback is flipping the flag
   (ADR 0859). CI (`.github/workflows/relay-probe.yml`) guards the cross-compile.
   **Live ZAGROS deploy pending** (restarts nothing — `--tags relay_metrics`; the
   timer restart is the only churn). The `probe`-side swap in the `relay-health`
   role is the remaining Phase-3 leg.
4. **✅ Phase 4 (built): per-peer relay stats** ([ADR 1055](../../docs/adr/1055-relay-observability-logging-stats-anomaly-processor.md))
   — the relay counters + stats endpoint + `publish`'s per-peer fold + the
   dashboard distribution/hog view all merged; lights up live once Phase 3 is
   deployed with `relay_metrics_publisher=binary` + the relay serves
   `--stats-addr` (both default-on in the roles). Reuses `goat top`'s envelope
   BAND; see `docs/design/relay-health-dashboard.md` + `relay-observability.md` §2b.

Cross-refs: F-235,
[ADR 0112](../../docs/adr/0112-tech-strategy-workload-shape-language-boundary.md),
[ADR 1058](../../docs/adr/1058-relay-metrics-self-publish-ingest.md).
