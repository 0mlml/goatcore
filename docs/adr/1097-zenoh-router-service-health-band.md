# ADR 1097 — Zenoh router service-health as a relay-dashboard band (durable, wedge-aware)

**Status:** Accepted — built; deploy + live-fire to both relay dashboards tracked in `in-flight.md`.
**Repo scope:** goat core — `ops/relay/health/zenoh-health-probe.sh`, `ops/relay/health/relay-metrics-ingest.py`, `ops/relay/health/relay-health.html`, the `relay-health` ansible role + `group_vars/zagros_hosts` + `group_vars/zagros_relay_health`, the two relay-health compose overlays.
**Related:** `docs/design/zenoh-router-health.md` · `relay-health-dashboard.md` · [ADR 1058](1058-relay-metrics-self-publish-ingest.md) (the ingest + tiered-sqlite rail this reuses) · F-248 · F-253 · [ADR 1036](1036-environment-health-surface.md) (the on-bus health surface — deliberately *not* the home for this history; see Alternatives).

## Plain-language summary

The zenoh router can wedge in a way nothing watches: its container healthcheck
tests REST liveness only, but the transport accept path can seize — new
connections stall while existing ones flow — and the fix-era detection
(the container-health alert) only fires *after* it is already `unhealthy`. This
adds a **zenoh routers band** to the relay-fleet dashboard that shows the signals
which actually reveal a wedge (a real new-session open latency, CLOSE-WAIT
growth, FD headroom), with history that **survives the router restarting** —
because a restart is exactly what erases the evidence.

## Context

- F-248 (FD exhaustion) and F-253 (admission seizure) both left the router
  `running` while broken; the healthcheck was the only signal and it fired late.
  The findings named the missing piece explicitly: the CLOSE-WAIT / open-latency
  **trend**, which no surface kept.
- The relay dashboard already has the machinery for a self-published domain with
  a three-level drilldown and its own durable store: the token-gated ingest +
  tiered SQLite (ADR 1058) behind the site-host band. It deliberately avoids
  Prometheus so it stays an outage-time surface.
- The router lives on the Thor; its transport/socket/FD state is only readable
  from the container's netns / `/proc` (not the adminspace) — a co-located probe.

## Decision

**Add a "zenoh routers" band cloned from the site-host band.** A root systemd
timer on the Thor (`zenoh-health-probe.sh`) reads the router's `docker inspect`,
container-netns sockets (`nsenter ss`), `/proc/<pid>/fd`, `docker stats`, REST
adminspace, and a real mTLS **new-session open** (the seizure signal the
healthcheck can't make), and POSTs to the relay-metrics ingest, which folds it
into a new `zenoh_series` tier set and exports `zenoh-health.json` /
`zenoh-series.json`. The single-file dashboard renders a band → cards → history
drilldown over them.

Durability comes for free from the rail: the sqlite store lives in the **ingest**
container, a different process from the router, so the history outlives every
router bounce.

**Deployed to both dashboards.** The probe POSTs to a comma-separated ingest list
(the cloud/public box + the on-Thor private box), exactly like host-net-publish,
so each dashboard stores the band with independent, restart-surviving history.

**Honesty preserved.** Unmeasured → grey `—` / `unmeasured`, never a fabricated
number; stale → greyed `NOT REPORTING`, never a guessed red or stale green.
When the mTLS open probe has no client cert, admission grades **grey** ("can't
confirm admission"), not green off a fast REST — F-253 proved REST can answer
while the transport is wedged.

## Alternatives considered

- **On-bus `EnvHealthV1{domain:"zenoh"}` publisher (ADR 1036 surface).** Would
  render at `goat health` / panoscope / `goat top`, but those publishers persist
  nothing (brokerless reveal-only) — they cannot satisfy the "keep the history
  across a restart" requirement, which is the whole point. Rejected as the home
  for this; a live-glance domain remains a possible additive follow-on.
- **Prometheus + a Grafana panel.** Prometheus is co-resident on the Thor and
  dies with it; the relay dashboard exists precisely to survive that. The
  container-health textfile metric already covers the coarse `unhealthy` verdict
  in Prometheus — this band adds the rich, restart-surviving signals the
  outage-time surface needs.
- **Extend the container healthcheck to do an mTLS open.** Would still only
  produce a binary verdict with no trend, and a wedged healthcheck can't publish;
  an external co-located probe both catches the seizure and keeps the history.

## Consequences

- A wedge is now *visible as it builds* (CLOSE-WAIT / open-latency rising), not
  only after `unhealthy` — and the evidence survives the restart that clears it,
  closing the F-253 "restart destroys the wedged state" gap for triage.
- One more Thor-side root timer and a small ingest surface; both fail-safe (the
  band is absent, not broken, where the probe/ingest isn't enabled).
- The mTLS open probe wants a client leaf; without it the load-bearing signal is
  grey (honest) rather than live — a one-line group_var to enable.
