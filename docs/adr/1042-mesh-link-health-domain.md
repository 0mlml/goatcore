# ADR 1042 — Mesh-link health as the third environment-health domain: a passive hub-vantage meter on netbird's own measurements

**Status:** Accepted (2026-06-12 — deployed + live-fired on a sandbox goatnet the same day, M-065: a real `amber` reading for peers on relay fallback, and the stop-the-timer honesty probe degraded to `unmetered`, never a stale color)
**Repo scope:** goat core (`goat-mesh-health` meter + `mesh-health` role + the `infra-mesh-health` router ACL grant + the `MeshDetailV1` arm of `EnvHealthV1`)
**Related:** [ADR 1036](1036-environment-health-surface.md) (the cross-domain contract this plugs into) · [ADR 1034](1034-fabric-health-surface.md) (the meter pattern) · [ADR 1035](1035-operator-namespace-ops-canonical.md) (the `ops/` namespace) · [`environment-health-surface.md`](../design/environment-health-surface.md) §4.3 (the design) · [`snitch.md`](../design/snitch.md) (the full-matrix NPM system this deliberately is not)

## Plain-language summary

Operators on a sandbox goatnet want a standing answer to "how are the WireGuard mesh
links doing — and is anyone riding the relay fallback?" without standing up snitch's full
collector/Postgres/dashboard stack per goatnet. The environment-health surface (ADR 1036) was built
to take new domains cheaply, and netbird already measures per-peer latency and direct-vs-relayed for
its own connection management. So the **mesh** domain is a small passive meter: a host-side systemd
timer snapshots `netbird status --json`; a containerized mTLS Zenoh client grades each link honestly
(direct→green, relayed→amber, disconnected→idle — liveness is panoscope's job; stale snapshot→
unmetered, never a false green) and publishes `ops/mesh/health/<site>` as `EnvHealthV1{domain:"mesh"}`.
Because `ops/**` is router-persisted and audit-captured, the historical "was that partner relayed all
afternoon?" question is answered by the existing replay path with zero new storage.

## Context

- A sandbox goatnet onboards external partners whose mesh links (direct vs relayed, latency) are the first thing
  to check when "the fabric feels slow" — today that means ad-hoc SSH + `netbird status -d`, one
  vantage at a time, with no history.
- snitch (Block 61) answers this *fully* — pairwise matrix, jitter/loss, events, gateway
  fingerprinting — but per-goatnet it costs a collector + Postgres + its own dashboard + probe keys
  + probes on every peer. `snitch_enabled: false` on the sandbox group_vars is a deliberate deferral.
- ADR 1036 §4.3 anticipated exactly this plug-in shape: a per-host/per-site signal, a passive roller,
  a `oneof` arm, no new surfaces. Fabric and time domains are already live.

## Decision

1. **`mesh` is the third domain of the environment-health surface.** Topic `ops/mesh/health/<site>`,
   envelope `EnvHealthV1{domain:"mesh"}` with a new `MeshDetailV1` arm (oneof tag 12): per-link rows
   (peer fqdn/ip, color, connected, direct, conn_type, latency_ms, relay_address, handshake age,
   direct↔relayed transition count) + the vantage host + peers_total/connected.
2. **Passive, from netbird's own measurements.** The meter reads a host-side snapshot of
   `netbird status --json` — the link facts netbird already maintains. No ICMP, no injected probes
   (the env-health passive-only discipline). Active jitter/loss probing stays out of v1; if it ever
   lands it follows the "probe only when quiet" roadmap arm, or you deploy snitch.
3. **Hub vantage, v1.** One meter on the site's hub host grades the links *that host* sees. That covers
   hub↔peer — the links that matter for "can participants reach the fabric." Partner↔partner
   pairwise links are explicitly out of scope (snitch territory).
4. **Snapshot via host timer, meter in a container.** A systemd timer (`mesh-health-snapshot`,
   20s cadence) atomically writes the JSON; the container bind-mounts it read-only. This keeps the
   meter a pure Zenoh client (no daemon-socket coupling, no netbird binary in the image) and makes
   file mtime the honest staleness clock. The snapshot script socket-guards + timeouts the CLI call
   (the F-098 hang).
5. **Honesty mapping (§6 of the design):** connected+direct → green, escalated amber/red only by a
   *measured* latency (netbird reports `latency: 0` when it has no reading — an unmeasured latency
   never grades); connected+relayed → amber always (fallback active ≠ healthy posture, never green);
   disconnected → idle, **not red** (per-peer liveness is panoscope's job; an off-shift laptop is not
   a mesh failure); stale/missing snapshot → the whole reading is `unmetered`. `overall_color` =
   worst grounded link; `n` = connected links.
6. **Identity + ACL like every infra meter:** leaf CN `goat-mesh-health-trial-peer` (offline-CA
   mint), router subject `infra-mesh-health`, ingress-only rule `infra-w-mesh-health` on
   `ops/mesh/health/**`, in both ACL modes. New rule ⇒ the router must restart to load it;
   a CN mismatch is silent denial.
7. **History rides the existing substrate.** `ops/**` has a rocksdb storage (7d) and the audit
   subscriber excludes only `volatile/**`, so readings land in the MinIO audit tier (30d) and are
   readable via the Phase G replay path. The meter itself persists nothing.

## Alternatives considered

- **Per-goatnet snitch collector** — full-featured but a parallel observability stack (collector +
  Postgres + separate dashboard + per-peer probe keys) on a sandbox; stays the right tool when
  pairwise matrices / jitter / loss / fingerprinting are actually needed.
- **Speak gRPC to the netbird daemon socket from the meter** — version-coupled proto vendoring +
  a tonic build for what a one-line CLI snapshot provides; rejected for moving-parts cost.
- **Meter publishers on every peer (full matrix on the bus)** — partner peers don't run our
  daemons; per ADR 1025 their identity is a write-slot, not an infra CN. Out of scope by design.
- **Active ICMP probing in the meter** — breaks the env-health passive-only discipline and
  duplicates snitch; deferred to "probe only when quiet" roadmap if ever.
- **Grade disconnected peers red** — false-alarms on normal off-shift peers and double-reports
  panoscope's liveness; rejected (idle, ungrounded).

## Consequences

- Operators get a standing, honest, glanceable mesh-link reading co-located with fabric + time —
  and a free historical record via the audit tier — for the cost of one sibling meter.
- The reading is vantage-limited (hub only) and latency-only-when-measured; anyone needing more is
  pointed at snitch rather than this meter growing probes.
- A fourth `EnvHealthV1` arm raises the pressure to ship the ADR 1036 increment-4/5 renderers
  (`goat health` board, panoscope band); until then the read path is `goat sub` + the runbook.
- Router config gains one subject + one rule per goatnet; the slot-pool model makes the
  re-render safe (deterministic pool); a goatnet without it waits on the non-destructive ACL splice.

## Rollback

Stop the `mesh-health` compose project + disable `mesh-health-snapshot.timer` (role-installed units);
the ACL subject/rule are inert without the cert. No data migration — the meter persists nothing.
