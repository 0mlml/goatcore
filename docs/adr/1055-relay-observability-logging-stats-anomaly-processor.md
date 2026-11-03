# ADR 1055 — goat-relay observability: structured logging + self-stats endpoint + anomaly-classifying processor

**Status:** Proposed — drop counters built; structured logging, stats endpoint, and the anomaly processor are designed-not-built (plan C12). Design: `../design/relay-observability.md`.
**Repo scope:** goat core — `goat-relay/` (logging + stats), `ops/relay/health/` (the existing external prober + diagnostics page), the relay ansible role.
**Related:** F-228 · F-220 · `relay-health-dashboard.md` (the black-box external probe this complements) · [ADR 1036](1036-environment-health-surface.md) (the honesty-graded reveal discipline reused here).

## Plain-language summary

The relay currently emits only `eprintln!` — no structured logs, no metrics, no alerting — which is why diagnosing the flap took live SSH + grep + manual port probes. This ADR adopts: (1) structured logging via `tracing` with a per-connection span; (2) a white-box self-stats surface (drop/connection/keepalive/auth/churn counters) on a mesh-internal endpoint scraped by the existing relay-health dashboard; and (3) — per operator direction — an **anomaly-classifying processor** upstream of the diagnostics page, so the page renders *named dysfunction verdicts* (flapping / wedged / race / auth-dysfunction / DoS), not raw graphs a human must interpret.

## Context

- F-228: the relay is a black box to operators. Each of the five flap bugs maps to a signal that should have been a dashboard line but was invisible.
- An external diagnostics page already exists (relay-health dashboard) but probes public IPs — black-box reachability only, never internal state.

## Decision

Build, in order: `tracing`+JSON with a per-connection span; the stats counters — aggregate (drop/conn/keepalive/auth/churn) **and per-peer forwarded byte/frame counters** (design §2b: attached to the `ConnHandle` at the `routing.rs` forward-dispatch site where the drop counters already live, closing the code's own "per-peer attribution is deferred to the metrics surface" TODO) — plus a mesh-internal stats endpoint feeding relay-health `--deep`; an anomaly processor that pattern-matches dysfunction signatures (each mapped to a finding class, plus an unclassified catch-all) and emits honesty-graded verdicts (never a false green); the diagnostics page renders verdicts; alert rules fire on the verdicts. Detection/classification only — it reveals, it does not act. Full signatures + build order in the design doc.

## Alternatives considered

- **Raw metrics dashboard only (no processor).** Rejected per operator direction — leaving a human to spot race/wedge/flap patterns in raw graphs is what made this RCA slow; the page must classify.
- **Push metrics to the central observability stack instead of a relay endpoint.** Relays are not fabric peers and the external probe path must work when the mesh is down; pull from a relay-side endpoint keeps the black-box and white-box paths aligned. (OTLP/central audit can still consume it later.)
- **Verbose `eprintln` logging.** Rejected — unstructured, uncorrelated, and on the hot path it is a log-flood DoS.

## Consequences

- The next relay incident is a dashboard glance (named verdict + evidence), not an SSH session.
- New surface area on the relay (logging deps, a stats endpoint) — bounded, mesh-internal, read-only.
- Honesty discipline (ADR 1036) carries over: verdicts are graded, `insufficient-data` rather than assumed-good.
- The per-peer byte counters give the dashboard the two things `/proc/net/dev` structurally cannot: the true *relayed* byte-rate (not whole-NIC) and a **per-peer distribution** to spot power-law hogs for identity-keyed throttle/evict (feeds ADR 1033's enforce pillar; visualized by reusing `goat top`'s envelope BAND). The `relay-probe` binary (track `relay-probe-go`, Phase 4) is the consumer.
