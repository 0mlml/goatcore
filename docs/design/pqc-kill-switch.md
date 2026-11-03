# PQC kill-switch — degradation-aware fallback to classical

Addresses F-259. Measurement
basis: M-088 (PQ costs ~1.2 KB
extra per handshake; larger flights retransmit more under loss) +
M-081 (PQ
loss-sensitivity is tail-only). Relates to [ADR 1079](../adr/1079-zenoh-pqc-hybrid-tls.md)
(zenoh PQ triggers/rollback) and rosenpass-intra-mesh-pqc.md.

## The requirement

Post-quantum key exchange is on by default at two independent layers — the Zenoh
transport (X25519MLKEM768) and the NetBird underlay (rosenpass PSK). Its cost is
sustainable in moderate-to-good conditions, but the larger PQ handshake can become
**the failure factor** on a severely degraded link where classical would still
complete. The fabric must be able to **fall back to classical, at either or both
layers, on demand and ideally automatically** — without a redeploy, without a
re-enrollment, and without breaking mixed fleets.

## Current state — an asymmetry (verified in code)

| Layer | On/off mechanism today | Runtime switch? |
|---|---|---|
| **Zenoh client** (panoscope-core) | `ensure_pq_crypto_provider()` first-installer-wins in the mTLS path (`crates/panoscope-core/src/tls.rs`); kx groups chosen from the runtime pqc-mode signal since Tier 1 (2026-07-17) | **Yes (Tier 1)** — `GOAT_PQC` env / `/var/lib/goat/pqc-mode`, latched per process at first TLS session; long-running surfaces flip on restart. |
| **Zenoh router** | Ansible image pin (`zenoh_image` → PQ / PQ-FIPS / upstream-ring); `rollback.sh zenoh` | **No runtime switch.** Redeploy granularity. |
| **NetBird rosenpass** | `--enable-rosenpass` / `--rosenpass-permissive`, per-peer; `rosenpass_enabled` group_var | **Yes** — `goat client pqc on\|off` (self) / `goat admin pqc … --peer\|--fleet` do `netbird down`+`up` in place, no re-enrollment. |
| **Health signal to drive it** | `net` (per-node self-published `ops/net/health/<node>`, hourly) + `mesh` (`ops/mesh/health/<site>`, 20s, hub vantage) | Signals exist (relayed-fallback=amber, staleness, throughput) but **no loss metric**, slow cadence, observation-only. |

Two facts shape the whole design:

1. **Zenoh already fails *open to classical*.** Down-negotiation is automatic and
   wire-proven: a classical endpoint against a PQ endpoint negotiates
   X25519, no HelloRetryRequest, session healthy. F-247 makes a late/absent
   provider install "stay classical, never broken." So the *fallback path* exists
   and is safe — what's missing is a **runtime lever to choose it** on a
   PQ-capable node.
2. **Rosenpass already has the lever we want** (`goat client/admin pqc`), including
   the correct semantics: in-place `down`+`up`, per-peer, permissive = graceful
   classical fallback, explicit `--enable-rosenpass=false` to disable. Zenoh should
   copy this shape, not invent a new one.

## Strategy — a three-tier ladder

### Tier 0 — what exists (use it now)

- **Rosenpass on-demand:** `goat client pqc off` (this host, no token) and
  `goat admin pqc off --peer <sel> | --fleet` already flip rosenpass at runtime,
  per-peer, no re-enrollment. This layer's kill-switch is **done**.
- **Zenoh coarse lever:** flipping the *router* image to the upstream (ring) build
  transparently pulls every PQ-capable client down to X25519 via negotiation — no
  client change. This is the fleet-wide emergency brake, but at **image-pin
  granularity** (`rollback.sh zenoh`), not per-node/per-condition.
- **Static:** a known-bad-link node can ship a classical build. Mixed fleets already
  interoperate.

Gap after Tier 0: no *per-node, runtime* switch for the **Zenoh client**, and no
**automatic** trigger for either layer.

### Tier 1 — a runtime Zenoh classical seam, unified under one command — **BUILT 2026-07-17**

Add the missing lever and make one command flip **both** layers on a node/fleet.

- **Zenoh client runtime toggle** (`panoscope-core/src/tls.rs`). One correction
  from the original sketch, verified against `zenoh-link-tls 1.9.0`: zenoh builds
  its `ClientConfig` from the **process-default rustls provider**
  (`ClientConfig::builder*` in its `utils.rs`; no per-session provider injection),
  so the seam is **provider-install time**, not per-session. The chokepoint
  resolves a runtime signal — `GOAT_PQC` env (per-process override), then
  `/var/lib/goat/pqc-mode`, then the per-user fallback `~/.config/goat/pqc-mode`
  (contents `classical`/`off` or `pq`/`on`; a garbled value keeps the default,
  never silently changes posture) — and installs either the PQ-preferring
  aws-lc-rs provider or the same provider restricted to
  `[X25519, secp256r1, secp384r1]`. The mode latches at a process's **first TLS
  session build**: every new process (each goat-cli invocation is one) honors the
  flag; a long-running surface flips on restart. No recompile, no redeploy.
- **Unified operator surface.** `goat client pqc on|off` (this host) and
  `goat admin pqc on|off --peer <sel>` (remote, detached over SSH) flip the Zenoh
  state file *in the same action* as the rosenpass `down`+`up` — file first,
  fail-fast before netbird so a half-flip can't happen locally. The rosenpass
  gotchas are kept (explicit `--enable-rosenpass=false`; `down`+`up`, never a
  flagless `up`). `--fleet` prints the per-peer plan for both layers. Remote
  verify log (`/tmp/goat-pqc-verify.log` on the peer) carries the
  quantum-resistance line plus the written zenoh mode.

After Tier 1: PQC is switchable off at **both layers, per-node and fleet, at
runtime, no redeploy** — the on-demand half of F-259 is closed. Remaining
per-node caveat: flipping an already-running long-lived surface (panoscope
server, health publishers) requires a process restart — the process-default
provider cannot be swapped after install.

### Tier 2 — degradation-aware autopilot (the real ask)

A node protects itself: when its own link degrades, it drops new handshakes to
classical automatically, and restores PQ when the link recovers.

- **Trigger source — the node's own health.** Each node self-publishes
  `ops/net/health/<node>` (brokerless, "no central poller", matches the
  denied-edge "run itself" doctrine). A local `pqc-autopilot` reads its *own* net
  (and mesh) health and actuates the Tier-1 switch — no central authority, so it
  keeps working when the mesh is exactly the thing that's broken.
- **Policy.** Drop-to-classical **fast** (protect) on degradation; restore-to-PQ
  **slow** (only after sustained recovery) — asymmetric hysteresis to avoid
  flapping. Degradation = the strongest signals available today: `conn_type ==
  Relayed` (fallback active), handshake staleness / `mesh_stale`, throughput
  collapse — plus a loss threshold once a loss metric exists.
- **Honest prerequisites (net-new, flagged):**
  1. **No loss metric today.** `net`/`mesh` latency is often unmeasured (`0`) and
     neither carries packet loss (deferred to snitch, not built). Autopilot needs a
     faster, loss-aware local signal — a lightweight per-link probe or the snitch
     path — before "loss high" can trigger it.
  2. **Cadence too slow.** net = hourly, mesh = 20s. Autopilot needs a sub-second-to
     -seconds local read, not the roll-up cadence.
  3. **Observation → actuation bridge.** Both health surfaces are explicitly
     reveal-only ("never drops, resets, or firewalls anything"). Wiring health to
     actuate crypto is a deliberate new boundary crossing — the autopilot is that
     bridge, and it must be as fail-open as the crypto (autopilot down ⇒ node keeps
     its last mode, defaulting to classical under doubt).

## What "off" means, and how it composes

- **Zenoh off** = the node offers only X25519 for new sessions → down-negotiates to
  classical. Composes cleanly with the existing automatic down-negotiation; mixed
  fleets are unaffected (a classical node was always accepted).
- **Rosenpass off** = explicit `--enable-rosenpass=false`; permissive peers already
  accept plain WireGuard, so no connectivity loss.
- **Direction of failure is always classical.** F-247 already fails open to
  classical; the kill-switch reinforces the same direction — under doubt, drop PQ,
  never drop the link. The one exception is the rosenpass **enforce** posture
  (non-permissive), which is fail-*closed* by design; a kill-switch must not fire
  against an enforced link without an explicit operator override.

## Design decisions

- **Autonomy: per-node self-driven, with operator override.** Central command
  cannot be relied on at the denied edge — the whole point. Autopilot is the
  default; `goat admin pqc` is the manual override (both directions).
- **Granularity: per-node first.** The Zenoh switch flips all of a node's sessions;
  per-*link* selection (classical to a bad peer, PQ to good peers) is a later
  refinement needing per-session kx choice keyed on the peer's link health.
- **The router lever stays** as the coarse fleet-wide brake (image-pin) — orthogonal
  and complementary to the per-node runtime switch.

## Ratification + build order

- Tier 1 is a bounded code change (panoscope-core kx-group runtime seam + goat-cli
  `pqc` command extension) — no ADR needed beyond noting it under ADR 1079.
- Tier 2's autopilot is an architectural decision (health→actuation) and a new
  trigger class ADR 1079 does not have (its triggers are human/strategic) — ratify
  via an **ADR 1079 amendment** (or a dedicated ADR) covering the degradation
  thresholds, hysteresis policy, and the fail-open autopilot boundary. Gate it on
  the loss-aware signal prerequisite.
