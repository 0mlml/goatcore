# ADR 1057 — goat-relay keepalive liveness is credited by any inbound traffic, not only HealthCheck echoes

**Status:** Accepted — built in code (track `relay-flap-fix`); behavioral validation via integration test P0 + measurement M-072 before fleet rotation.
**Repo scope:** goat core — `goat-relay-protocol/src/keepalive.rs`, `goat-relay/src/listener/{ws,quic}.rs`.
**Related:** [ADR 0709](0709-block-33-relay-rewrite-architecture.md) (the Rust relay rewrite this hardens) · F-219 · F-217 / F-218 (the rest of the flap arc).

## Plain-language summary

The relay decides a peer is dead if it stops answering heartbeats. Previously *only* a heartbeat echo counted as "the peer is alive" — actual relayed data did not. So a peer busily relaying traffic that happened to miss one heartbeat echo (a lost control packet, a scheduler hiccup) was declared dead and disconnected. This decision makes **any** inbound frame from the peer count as proof it is alive; the heartbeat becomes the fallback liveness signal for genuinely idle connections, which is its real job.

## Context

- The relay's server-side keepalive `Sender` resets its failure counter on `on_response()`, called only from the inbound-HealthCheck arm of the reader.
- `DEFAULT_ATTEMPT_THRESHOLD = 1` (matches upstream Go `sender.go:11`) — zero tolerance: one missed echo per window = timeout.
- Upstream Go's relay has the same "only the HC ack resets liveness" behavior. So this is a **deliberate divergence**, recorded here per the deliberate-deltas discipline.

## Decision

Credit liveness on every successfully-parsed inbound frame (Transport included), via `KeepaliveSender::mark_alive()`, before dispatch in both the WS and QUIC readers. `on_response()` (the HC echo path) delegates to `mark_alive()`. The HealthCheck remains the liveness signal for idle connections; it loses its monopoly on busy ones.

## Alternatives considered

- **Keep upstream behavior (only HC echo counts).** Rejected: it is the direct cause of busy-session flap; upstream's choice is not load-bearing for us and we run a different traffic profile (CGNAT, relay-heavy).
- **Raise `attempt_threshold` instead.** A complementary mitigation (tolerate N missed echoes), but it only widens the window — it does not fix the root category error that data isn't evidence of life. We keep the threshold as a separate tunable; this ADR fixes the categorical gap.

## Consequences

- A peer is torn down only when it is genuinely silent across the full window — no false-positive timeouts on busy peers.
- The change is local to the server's liveness accounting; the wire protocol, the HC cadence, and the client are unchanged. Interop with the upstream client is unaffected (the client still echoes HCs; the relay simply also accepts data as proof).
- Deliberate delta from upstream — recorded so a future "sync with upstream" pass does not silently revert it.
