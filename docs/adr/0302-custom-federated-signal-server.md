# ADR 0302 — Custom federated signal-server

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0204-multi-site-active-active.md`](0204-multi-site-active-active.md), [`0300-netbird-as-mesh-control-plane.md`](0300-netbird-as-mesh-control-plane.md), `../design/ha-multi-site.md` |

## TL;DR (for a layperson)

The signal-server is the small service that helps peers
introduce themselves to each other when they are about to start
a direct WireGuard connection. Out of the box, NetBird's signal-
server runs in only one place. For multi-site active-active, we
needed a version where the signal-servers at each site
introduce themselves to each other and forward signaling
messages between sites. We built that custom version from
scratch in Go.

## Diagram



Source: [`diagrams/0302-federated-signal-server.mmd`](diagrams/0302-federated-signal-server.mmd).

## Context

NetBird's stock signal-server is single-instance. Multi-site
active-active ([`0204`](0204-multi-site-active-active.md))
requires that a peer enrolled at site A and a peer enrolled at
site B can complete the WG signaling exchange even though their
local signal-servers do not share state. The fix is signal-
server federation.

Federation has structural choices:

1. **Shared state via the same database (Spock-replicated).**
   Adds cross-site database write traffic for ephemeral
   signaling; not a good fit.
2. **Forward RPC between signal-servers.** A peer connects to
   its local signal-server; if the destination peer is
   registered elsewhere, the local server forwards to the
   right server.

We picked Forward RPC.

## Decision

We built a custom federated signal-server in Go (`signal-server/`
in the repo). Properties:

- **Per-site instance** runs on every mgmt-host (Router-A and
  Router-B both run an instance).
- **Peer registry** is in-memory per instance; race-safe
  registration and deregistration.
- **Forward RPC** between instances: full mesh, every signal-
  server knows every other.
- **Per-peer Forward timeout** to avoid head-of-line blocking
  on a slow remote.
- **Correlation IDs** on every message for end-to-end
  observability.
- **Relay fallback**: if a Forward fails, the message is sent
  via the relay path so the exchange can still complete with
  higher latency.
- **Prometheus metrics** integrated.
- **CI**: race-detector tests, end-to-end tests, concurrent
  registration tests.

Implementation tracker: signal-server commits in
`git log signal-server/`. Custom build because no upstream
project provides federated semantics with the partition-tolerance
properties we need.

## Alternatives considered

- **Use stock signal-server, run one centrally.** Single point
  of failure; violates Island Mode. Rejected.
- **Run independent signal-servers, accept that cross-site
  signaling fails.** Cripples active-active. Rejected.
- **Use NATS or similar message bus for signaling.** Adds an
  operational dependency for a use case (signaling) that does
  not justify it. Rejected.

## Consequences

**Positive.** Cross-site peer signaling works without quorum.
Per-peer timeouts prevent slow remotes from cascading.
Correlation IDs make debugging tractable. Federation is full-mesh
so any single signal-server failure does not partition the
fleet.

**Negative.** Custom-built component to maintain. We accept;
the alternative was either Island Mode violation or feature loss.

## Assessor view (SCA-R)

Signal-server federation serves CP-2 (partition tolerance),
SC-5 (DoS — single signal-server failure absorbed), and AU-2
(audit — correlation IDs let us trace cross-site signaling).
The component is built from source ([`0702`](0702-build-from-source-mgmt-plane.md))
and ships through the release pipeline.

## Trigger conditions for revisit

- Upstream NetBird gains federated signal-server with
  comparable semantics. Migrate to upstream; retire the
  custom build.
- Forward RPC scale becomes a problem (likely after ~50
  sites). Re-examine; consider hierarchical federation.

---

*See `GLOSSARY.md` for terms and acronyms.*
