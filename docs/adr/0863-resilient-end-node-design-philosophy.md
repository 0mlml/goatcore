# ADR 0863 — Resilient end-node design philosophy

**Status:** Proposed 2026-05-24.
**Parent of:** [ADR 0861](0861-minimalism-under-network-constraints.md) (web instantiation — minimalism-under-network-constraints doctrine), [ADR 0862](0862-two-tier-web-architecture-canonical-coprocessor.md) (web instantiation — Tier-1-canonical / Tier-2-coprocessor architectural decision).
**First web instance:** [`docs/design/flashlight.md`](https://github.com/usareur-af/goatstuff/blob/main/docs/design/flashlight.md).

## Context

When we extracted [minimalism-under-network-constraints](0861-minimalism-under-network-constraints.md) from designing flashlight, we noticed that roughly half the principles were web-tactical (Brotli vs ZSTD, SVG vs Canvas, Progressive Enhancement vs Resumability, `EventSource` / `BroadcastChannel`, the 14 KB TCP initcwnd budget, Speculation Rules) and roughly half were *not web-specific at all*. The non-web-specific half describes how *any* user-facing or end-node technology should behave under DIL (Disconnected, Intermittent, Low-bandwidth) operating conditions — a regime borrowed from tactical-communications research, where the principles originated long before the web existed.

The project has at least two forthcoming non-web surfaces that will operate under the same DIL conditions and would inherit these principles cleanly:

- **goat-client** (Block 76) — cross-platform daemon + GUI for wg-cp0 onboarding. Mobile + desktop + headless. Operators on degraded networks bootstrapping new peers.
- **snitch-app** (Block 77) — fleet-visibility surface with a mobile probe. Battery + bandwidth constrained by definition.

Both will face the same engineering questions the minimalism doctrine answered for web: what's the canonical experience? what augments? what degrades gracefully? where does compute live? what does the operator see when the network is bad?

Writing the cross-cutting philosophy once at the parent level — and instantiating it per surface — is the same cost-effective shape that justified the minimalism doctrine being doctrine rather than a flashlight one-off.

## Decision

**Adopt a parent end-node design philosophy** covering principles that generalize beyond web to any user-facing or end-node technology operating in a DIL regime.

The principles below are the *generalizing seven* — the subset of the 14 minimalism principles that survives the "would this still make sense for a CLI tool / mobile app / embedded GUI / desktop client?" test.

### The seven principles

1. **Canonical workhorse + optional coprocessor asymmetry.** A user-facing system has a *canonical* path that always works. Optional augmentations layer on top, reading from the canonical state — they never replace it, and their failure mode never tears it down. Examples beyond web: database core + extensions; CLI core + plugins; mobile-app offline-capable core + cloud-sync coprocessor; safety-critical core (DO-178C / IEC 61508) + convenience features.

2. **Usable on first byte / minimum-functionality-first.** The system is useful before any optional augmentation loads. First paint precedes any load decision; first useful work happens immediately. Examples beyond web: CLI startup time; mobile cold-start budget; embedded boot-to-useful time; hardware that works without configuration.

3. **Declarative state in the invocation artifact** (URL / command line / deep link / config object). The artifact that summons the surface IS the surface's state. Bookmark = share = reproduce. Examples beyond web: `tool --focus X` for CLIs; mobile deep links; terminal session reproducibility; `kubectl --context` style state-via-argument.

4. **Graceful degradation in both directions.** Bad network → optional features stay unloaded; canonical carries on. Bad compute / low memory / suspend → optional features evict; canonical carries on. The degradation surface is symmetric, never one-sided. Examples beyond web: classic resilient-systems pattern; distributed-systems literature; aviation multi-failure tolerance; medical-device safe-state behavior.

5. **Compute placement is operator-controllable.** The same logic is portable between "on the device" and "on the server" without code changes; the operator picks per-invocation. Examples beyond web: edge-computing literature broadly; ML inference on-device vs cloud; database query pushdown vs pull-and-filter; game engine client-vs-server compute.

6. **Honest naming, not silent dead-ends.** When a feature requires an unavailable capability, the surface says so explicitly. Hidden features that silently do nothing are forbidden — they erode operator trust and make degradation invisible. Examples beyond web: industrial-control alarm design; API design (no false-success returns); UI design generally.

7. **DIL operating regime is the named target.** The surface declares which DIL states (Disconnected / Intermittent / Low-bandwidth) it serves and what each looks like. The states are designed-for, not accidents. Examples beyond web: tactical-comms research where the vocabulary originated; satellite-link design; DTN (Delay-Tolerant Networking) store-carry-forward model; nuclear-control-room HMI design.

### What this philosophy IS

- A set of design properties any user-facing end-node surface should be reviewable against.
- A vocabulary (DIL regimes, canonical + coprocessor, declarative invocation, etc.) that makes design conversations across surface types interoperable.
- A parent under which platform-specific doctrines (the minimalism web doctrine being the first) instantiate.

### What this philosophy is NOT

- Not a framework. Not a library. Not a checklist that mechanically produces good designs.
- Not a substitute for the tactical decisions each surface type still has to make (its compression algorithm, its rendering primitives, its specific failure modes).
- Not applicable to server-internal protocols, bulk data movement, real-time media streaming, or anything where the user isn't directly observing under DIL conditions (see *Scope* below).

## Scope (in / out)

**In scope:**

- Any user-facing or end-node technology where the operator is *directly observing or interacting* with the surface AND may be under DIL conditions.
- Specifically: web surfaces (covered tactically by ADR 0861); cross-platform desktop / mobile GUI clients (goat-client Block 76, snitch-app Block 77); CLI tools that operators run interactively under degraded conditions; embedded operator consoles with display.

**Out of scope:**

- **Server-internal protocols** (Zenoh fabric, Spock replication, wg-cp0). Those have different concerns (throughput, consistency, partition tolerance) governed by other ADRs.
- **Bulk data movement** (container image distribution per Block 75 / ADR 0838; bulk historical data retrieval; software updates). Different shape — content-addressing, cache tiers, resumable transfers.
- **Real-time media streaming** (video / audio per Block 35 video-fabric). Different shape — codec-aware adaptive streaming, bitrate ladders.
- **Headless daemons with no user surface.** Their concerns (resource use, durability, idempotence) are governed by other discipline (production-readiness Block 50).
- **Asynchronous batch workloads** where no operator is waiting on the result.

## Alternatives considered

- **Keep doctrine web-specific only.** What we shipped initially. Cost: future end-node surfaces re-derive the same principles independently, with drift. Goat-client and snitch-app are both already known-coming; not extracting the philosophy means they each invent it locally. Rejected once we noticed the philosophy was load-bearing for ≥3 surfaces.

- **Rewrite the minimalism doctrine to be general.** Mix the seven generalizing principles with the seven web-tactical ones in a single document. Cost: muddies both audiences. A web reviewer trying to apply tactical decisions has to wade through generalities; a CLI/mobile reviewer trying to apply principles has to filter out web specifics. Rejected for clarity.

- **Don't extract the philosophy at all; let each surface re-derive from primitives.** Cost: the same shape we use for engineering principles already (ADR 0859 easy rollback is a parent; per-service tier-0 sweep is its instantiation), so consistency argues for extraction. Rejected for consistency with existing engineering-principle pattern.

- **Extract the philosophy as a design doc only, not an ADR.** Cost: design docs document *how*, ADRs document *why with alternatives*. The "why these seven and not other things" question deserves an ADR — otherwise future reviewers see seven principles and wonder which were considered and rejected. Rejected for accountability.

## Consequences

**Positive:**

- Goat-client (Block 76), snitch-app (Block 77), and any future end-node surface have a single parent philosophy to inherit from. Their per-surface design docs declare adoption and note deviations rather than re-deriving the principles.
- Vocabulary becomes interoperable across surface types. A design review for snitch-app can use the same "canonical + coprocessor asymmetry" framing as a flashlight review.
- The boundary between this philosophy and adjacent concerns (bulk-asset distribution, video streaming, server-internal protocols) is explicit — readers know when they're outside the philosophy's coverage.

**Negative / cost:**

- Premature-abstraction risk: the principles are extracted from one shipped instance (flashlight, and not even shipped — Phase A planning only) plus two designed-but-not-built instances (goat-client, snitch-app). If those designs diverge materially when they enter implementation, we may need to revise the principles.
- Mitigation: keep this ADR short and principle-only. Defer tactical specialization (per-platform compression, per-platform UI primitives, per-platform failure modes) to per-surface doctrines. The first revision target is when goat-client GUI or snitch-app design lands and we learn what we got wrong.
- Additional ADR surface to maintain in the index. One more file in the 08XX series.

**Neutral:**

- The seven principles are extracted from real engineering practice (tactical-comms research, local-first computing, resilient-systems literature) and are *not* speculative. We are *naming* what's already true in established fields, not inventing.

## Composition

- **Children (web instantiation):**
  - [ADR 0861 — Minimalism under network constraints (doctrine adoption)](0861-minimalism-under-network-constraints.md). Web-specific instantiation; adds 7 web-tactical principles, 5 web-specific conscious rejections, 3 web-tactical locked-in decisions on top of this parent.
  - [ADR 0862 — Two-tier web architecture: Tier-1 canonical, Tier-2 coprocessor](0862-two-tier-web-architecture-canonical-coprocessor.md). Web instantiation of the principle-1 asymmetry; locks in the specific Tier-1 (HTML + SSE + JS) / Tier-2 (Rust→WASM) shape.

- **Siblings (engineering-principle ADRs):**
  - [ADR 0112 — Tech strategy: workload-shape language boundary](0112-tech-strategy-workload-shape-language-boundary.md). Adjacent — language choice by workload shape.
  - [ADR 0859 — Easy rollback as an engineering principle](0859-easy-rollback-engineering-principle.md). Same shape as this — principle ADR with per-service instantiations.

- **Forthcoming instantiations (not yet written):**
  - **goat-client GUI doctrine** (Block 76). When goat-client's GUI design enters implementation, declare adoption of this philosophy and add cross-platform-GUI-specific principles (Flutter / Tauri / Electron decisions; mobile single-VPN-slot per F-109; etc.).
  - **snitch-app doctrine** (Block 77). Same pattern; mobile-specific tactical layer.

## Tracker

[`docs/design/minimalism-under-network-constraints.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/minimalism-under-network-constraints.md) (web instantiation — the first reading-order entry point); [`docs/design/flashlight.md`](https://github.com/usareur-af/goatstuff/blob/main/docs/design/flashlight.md) (worked first instance). Track: `flashlight-and-minimalism`, in-flight as Block 89.

When the first non-web instantiation enters design, a new design doc joins the trio and references this ADR as its parent philosophy.
