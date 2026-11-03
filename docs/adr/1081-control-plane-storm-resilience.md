# ADR 1081 — Control-plane storm resilience: four layers + lazy connections

| Field | Value |
|---|---|
| Status | Accepted (build order committed; each layer gated on its own measured verification, not on merge) |
| Date | 2026-07-15 |
| Repo scope | goat core + NetBird fork (`ops/netbird/patches/`) |
| Related | [`../design/control-plane-storm-resilience.md`](../design/control-plane-storm-resilience.md) (canonical detail) · M-085 · `../design/relay-health-dashboard.md` §"Site network hosts" (the trigger-signal source) · [ADR 1033](1033-zenoh-qos-prioritization-reserved-mission-slice.md) (fabric-QoS boundary) · [ADR 1058](1058-relay-metrics-self-publish-ingest.md) |

## TL;DR (for a layperson)

A degraded site uplink can collapse a goatnet for many hours. The trigger
is the weak link, but the *collapse* is self-inflicted: when the link drops
their connections, every device retries its connection signaling at a
fixed, aggressive pace regardless of whether it works — undeliverable
retries that keep the thin pipe saturated so nothing can recover. We are fixing the behavior, not the weather: clients
back off when signaling fails (1), the signal server refuses load it cannot
deliver (2), the site host protects control traffic on its uplink (3), and
operators get a measured, audited "storm brake" (4). Separately, we will
evaluate NetBird's lazy-connection mode, which avoids establishing the
mostly-idle peer connections whose churn multiplies signaling load.

## Decision

Build, in this order, each behind its own measured gate:

1. **Client retry discipline** (NetBird fork patches): exponential backoff
   with full jitter on signal offer retries + stream re-registration;
   per-peer-pair circuit breaker (open after M consecutive `notfound`,
   half-open on the mgmt network-map peer-online event). The cure: retry
   load must *decay* when delivery fails.
2. **Signal-server load shedding** (the repo-owned `signal-server/` tree —
   our from-scratch federated signal server, NOT the netbird fork; direct
   source edits built by `.github/workflows/signal-server.yml` — env-flagged):
   per-sender token bucket with growing retry-after; negative cache for
   absent recipients rejected before body read. Containment for unpatched
   or hostile clients.
3. **Site-host egress survivability** (mgmt-host baseline role): `cake` at
   ~90% of the *measured* line rate (host-net-health provides it), strict
   control-plane priority over bulk, rathole client reconnect backoff +
   shorter heartbeat, BBR on site hosts. Host-local shaping only — the
   ADR 1033 "no DSCP for fabric classes" rule is untouched.
4. **Operator storm brake** (`goat admin storm brake`, goat-cli + site-host
   guardrail): flush (signal restart) + tighten (halve L2 limits, bounded
   window) + shed (pause declared non-essential publishers) + announce
   (OperatorActionV1 + dashboard degraded notice). Manual first;
   auto-engage is a separately-gated follow-on with hysteresis and a
   once-per-hour cap.
5. **Lazy connections evaluation** (config, not fork): bench-gate NetBird's
   lazy-connection mode (pair-count + signal-rate before/after, first-packet
   latency on fabric paths, rosenpass-permissive + ACL interplay). Adopt
   fleet-wide only on a passing gate. Coordinates with the concurrent
   mesh-ACL work — ACL scoping and lazy mode both shrink the active pair
   set and compound.

**Explicitly rejected:** lengthening WireGuard/rosenpass rekey lifetimes to
reduce signaling — rekey is in-band between peers and never touches the
signal server; the constants are protocol-fixed forward-secrecy bounds.
Also out of scope here: moving mgmt/signal to the cloud (separate
architectural discussion; this plan makes the control plane survivable
wherever it runs, which the denied edge requires anyway).

## Evidence (measured, 2026-07-15)

The signal server receives roughly twelve times what it can forward,
consuming most of a degraded uplink with futile retries; loss bursts and
bufferbloat follow, and the mesh drops to a small fraction of its peers.
After recovery, with the uplink healthy, the blind-retry load persists
structurally: most signal messages remain undeliverable, and a signal flush
alone does not restore the mesh
—
which is why layer 4 without layers 1–2 just schedules the next collapse,
and why layer 1 is first.

## Alternatives

- **Better uplink / cloud hosting only** — moves the cliff, keeps the
  self-amplifying behavior; unacceptable for the denied edge.
- **Longer crypto lifetimes** — rejected above (no signaling relationship).
- **Brake only** — measured insufficient.

## Consequences

Fork surface grows by two small, separately-revertible client patches (the
standing fork-deltas-are-patches rule applies); the repo-owned signal
server gains two env-flagged knobs (0 = off); site hosts gain an `egress_shaping_enabled`
toggle (BBR is x86-mgmt-host-only — some site-host kernels lack it, M-087;
cake fixed-cap is deferred on variable satellite uplinks pending autorate); goat-cli gains an audited admin verb. Every layer's gate is a
bench or live-fire measurement logged to `docs/measurements/` — merge does
not mean done.
