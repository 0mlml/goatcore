# Real-protocol end-to-end validation harness (Block 50G)

A real-protocol netbird daemon end-to-end test class. Closes the validation gap that allowed F-105 (signal-server federated NotFound regression) and F-106 (Traefik streaming-RPC 60s timeout) to ship to production undetected: every existing test class validated *components* (DB replication, identity flows, container health, gRPC unit tests) but **no test ever ran a real `netbird` daemon end-to-end against a real `signal-server` + real `netbird-mgmt` + real Traefik until 2026-05-09 operator-laptop dogfood**, which is when the bugs surfaced.

This is the missing tier. Sibling to ADR 1008 (soak discipline) and ADR 1012 (load discipline). Component is **Tier-A: in-process Go test** (CI-fast); Tier-B: **docker-compose full-stack** (closer to prod). Both are required; one without the other misses different failure modes.

## Why neither soak nor load alone catches this class

| Test class | What it validates | What it misses |
|---|---|---|
| Unit / table-driven tests in `signal/`, `management/` | Function-level correctness, single-process behavior | gRPC layer + Traefik + cross-component contracts |
| 2-node Spock soak ([ADR 1008](../adr/1008-soak-testing-discipline.md)) | Postgres replication, identity flows | Real netbird daemon never runs |
| Wave 3 §3.4 federation-load harness | Synthetic gRPC load on signal-server | Used DB-direct setup-key seed → unauthenticatable rows → no real daemon ever connected |
| Goat-relay tier-4 bench env ([ADR 0709](../adr/0709-block-33-relay-rewrite-architecture.md)) | Relay-side throughput + kTLS hand-off | Mgmt-plane + signal-plane not in scope |
| Operator-fired smoke runbooks | Component health (`docker ps`, `curl /healthz`) | "Connected" status string ≠ "actually serving" |

The hole: **a real `netbird` daemon, post-enroll, reading status + sending traffic, against a real signal-server + Traefik-fronted mgmt-server.** F-105 and F-106 both live in that hole.

## Harness shape — Tier-A (in-process Go test)

Lifts upstream's pattern from [`netbirdio/netbird/client/cmd/testutil_test.go`](https://github.com/netbirdio/netbird/blob/main/client/cmd/testutil_test.go).

```
ops/test/realprotocol-e2e/
├── README.md                     # how to run
├── go.mod                        # depends on goat signal-server + the goat netbird fork
├── harness/
│   ├── signal_server.go          # spin in-process goat signal-server (with federation_peers list)
│   ├── management_server.go      # spin in-process netbird-mgmt (Postgres testcontainer or SQLite)
│   ├── traefik.go                # OPTIONAL — wrap mgmt in real Traefik via testcontainer
│   ├── netbird_daemon.go         # spin in-process netbird daemon, run `up`
│   └── peer_pair.go              # 2-peer convenience: enroll + ping over WireGuard
└── tests/
    ├── signal_health_probe_test.go     # F-105 regression guard
    ├── traefik_streaming_rpc_test.go   # F-106 regression guard
    ├── peer_to_peer_test.go            # full happy path: 2 daemons, ping over mesh
    └── federated_signal_test.go        # 2 signal-servers federated, peer-A → peer-B across them
```

Why in-process: fast (no Docker pull), no port conflicts (listen on `:0`), runs in CI on every PR, easy debug. **Tier-A acceptance:** under 30s wall-clock for the full class; runs in `go test` standard tier.

## Harness shape — Tier-B (docker-compose full-stack)

Mirrors the production mgmt-host compose stack as closely as possible: real Traefik, real Postgres-Spock, real signal-server, real netbird-mgmt. Spins up two-node "site-A + site-B" simulacrum on the same host. Adds **real daemon containers** (not the production binary; a slim Go test container that links the daemon as a library and runs `up`).

```
ops/test/realprotocol-e2e/compose/
├── docker-compose.yml            # 2 mgmt-stacks (A + B) + 2 daemons + Traefik on each
├── netbird-test-daemon/          # slim daemon image (go.mod replaces the netbird fork → local)
│   ├── Dockerfile
│   └── main.go                   # `up`, then read status, then send-receive a probe
└── tests/
    ├── run-suite.sh              # bring up, run go-tests against the live stack, tear down
    └── verdicts.jsonl            # JSONL output, schema = same as soak drivers
```

Tier-B acceptance: under 5min wall-clock; runs nightly on CI and on every signal-server / netbird-mgmt commit. Captures Traefik streaming-deadline class bugs that Tier-A's in-process harness can't see (no Traefik in Tier-A).

## Reproduction targets

The harness MUST detect each of these as a failure when re-introduced:

### F-105 — signal-server unknown-RemoteKey contract

```go
// In signal_health_probe_test.go
func TestSignalHealthProbeMatchesUpstreamSilentDrop(t *testing.T) {
    sig := harness.StartGoatSignalServer(t)
    daemon := harness.StartNetbirdDaemon(t, withSignalAddr(sig.Addr))

    // Wait for daemon to register + run its first IsHealthy() probe.
    require.Eventually(t, func() bool {
        return daemon.Status().Signal == "Connected"
    }, 30*time.Second, 100*time.Millisecond,
        "daemon's signal status should be Connected; F-105 regression if Disconnected with NotFound on dummy")
}
```

When F-105's regression is reintroduced (signal-server returns `codes.NotFound` on Send to "dummy"), the daemon's IsHealthy probe fails, status flips to Disconnected, test fails with the exact regression message.

### F-106 — Traefik streaming-RPC timeout

```go
// In traefik_streaming_rpc_test.go (Tier-B only — needs real Traefik)
func TestJobStreamSurvivesIdle90Seconds(t *testing.T) {
    stack := harness.StartFullStackWithTraefik(t)
    daemon := harness.StartNetbirdDaemon(t, withMgmtURL(stack.MgmtURL))

    // Daemon's Job stream is server-streaming; with no peer config changes
    // the server will be idle for the full 90s. F-106 default Traefik
    // killed this at 60s with 504.
    time.Sleep(90 * time.Second)
    status := daemon.Status()
    require.NotContains(t, status.LastError, "504",
        "Job stream should not have been killed by Traefik responding-timeout; F-106 regression")
    require.NotContains(t, status.LastError, "Gateway Timeout")
}
```

### Federation propagation correctness

```go
// In federated_signal_test.go
func TestPeerToPeerAcrossFederatedSignalServers(t *testing.T) {
    sigA := harness.StartGoatSignalServer(t, withNode("A"))
    sigB := harness.StartGoatSignalServer(t, withNode("B"), withFederationPeers(sigA.Addr))
    sigA.SetFederationPeers(sigB.Addr) // bidirectional

    mgmt := harness.StartManagement(t, withSignal(sigA))

    daemonA := harness.StartNetbirdDaemon(t, withSignalAddr(sigA.Addr), withMgmt(mgmt))
    daemonB := harness.StartNetbirdDaemon(t, withSignalAddr(sigB.Addr), withMgmt(mgmt))

    // peer-A sends to peer-B via signal — should traverse federation:
    // daemonA → sigA (local miss) → forward to sigB → deliver to daemonB
    require.Eventually(t, func() bool {
        return daemonA.PingPeer(daemonB.PublicKey()) == nil
    }, 30*time.Second, 200*time.Millisecond,
        "peer-A should reach peer-B via federated signal; otherwise federation forward is broken")
}
```

This catches **forward-path correctness**, not just NotFound contract. If signal-server federation logic regresses (e.g., the Forward RPC stops being called, or Broadcast loses the deadline budget), this test fails.

## Hooking in (CI integration)

- **Tier-A**: add to `.github/workflows/signal-server.yml` (already exists for unit tests) under a new job `realprotocol-e2e-tier-a`. Runs on every push to `signal-server/**` AND on every push to the netbird fork.
- **Tier-B**: new workflow `.github/workflows/realprotocol-e2e-tier-b.yml`. Runs nightly + on changes to `ops/compose/mgmt-host/**` (Traefik config), `signal-server/**`, or the netbird fork submodule. Surfaces Tier-B-only failures (Traefik / multi-container interactions).
- **Pin-regression hook**: when third-party image bumps land (`traefik:v3.X` → `v3.Y`, `postgres:14.X` → `14.Y`), the pin-regression workflow ([ADR 1008 §"Pin-regression hook"](../adr/1008-soak-testing-discipline.md)) runs Tier-A and Tier-B before merging.

## Acceptance + promotion gate

Block 50G ships in stages:

1. **50G.1 — Tier-A scaffolding** (1-2 days). Harness directory + go.mod + signal-server start helper + netbird daemon start helper. Reproduces F-105 in green-then-red form. NO mgmt + NO Traefik yet (signal-only).
2. **50G.2 — F-105 regression test** (0.5 days). Asserts the silent-drop contract end-to-end against the running daemon. Locks in F-105's fix.
3. **50G.3 — Tier-A management-server start helper** (1 day). Mgmt + signal + 1 daemon. Peer enrollment.
4. **50G.4 — Tier-A peer-to-peer test** (1 day). 2 daemons exchanging pings over WireGuard.
5. **50G.5 — Tier-B docker-compose full-stack** (2-3 days). Real Traefik. Reproduces F-106.
6. **50G.6 — F-106 regression test** (0.5 days). 90s idle Job stream survival.
7. **50G.7 — Federation forward-path test** (1 day). 2 signal-servers, peer-to-peer across them.
8. **50G.8 — CI wiring** (0.5 day). Workflows + pin-regression hook.

Promotion gate (Block 50G → "production-readiness substrate live"): all 8 sub-blocks land, AND a real-fleet regression intentionally re-introduced in a sandbox branch is caught by the harness. The latter validates the harness actually catches what it's supposed to (ADR 1015 Tier-1 review-style "did the test catch the bug?" check).

## Out of scope

- **Multi-site Spock replication** — covered by 2-node Spock soak ([ADR 1010](../adr/1010-ory-on-spock-mindful-operation.md)).
- **High-load / chaos** — that's Block 38 continuous-architectural-chaos and ADR 1012 load-testing-discipline. This harness focuses on **correctness under modest concurrency**, not capacity.
- **Real WireGuard kernel module** — Tier-A uses userspace WireGuard (Go `wireguard-go`); Tier-B uses kernel WG inside the daemon container. Production-shape kernel WG validation belongs in tier-4 bench env, not here.

## Cross-refs

- ADRs: [`1008`](../adr/1008-soak-testing-discipline.md), [`1012`](../adr/1012-load-testing-discipline.md), [`1015`](../adr/1015-code-review-discipline.md), [`1011`](../adr/1011-findings-register.md)
- Findings: F-007 (the validation gap that hid F-105 + F-106 until today), F-105, F-106
- Implementation: Block 50G (production-readiness real-protocol E2E)
- Upstream pattern lifted from: [`netbirdio/netbird/client/cmd/testutil_test.go`](https://github.com/netbirdio/netbird/blob/main/client/cmd/testutil_test.go)
