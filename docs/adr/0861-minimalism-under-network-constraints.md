# ADR 0861 — Minimalism under network constraints (doctrine adoption)

**Status:** Proposed 2026-05-24.
**Anchor design:** [`docs/design/minimalism-under-network-constraints.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/minimalism-under-network-constraints.md).
**First instance:** [`docs/design/flashlight.md`](https://github.com/usareur-af/goatstuff/blob/main/docs/design/flashlight.md).

## Context

The goat fabric is increasingly deployed into operating conditions that web orthodoxy quietly assumes away: satellite-over-VPN with seconds of jitter, LAN-only island mode with no WAN at all, rapid-bringup operators on a trickle of bandwidth bootstrapping a new site, partner devices we don't get to pick the network for, low-memory mobile browsers that get aggressively suspended. The dominant patterns of modern web design — single-page apps, framework-mediated reactivity, hydration — put a large up-front cost on the user before anything works. That cost is invisible on good networks and disqualifying on bad ones.

Tactical-communications research calls this regime **DIL** — *Disconnected, Intermittent, Low-bandwidth*. The project already has at least five forthcoming or planned surfaces that operate squarely inside DIL: goat-client GUI (Block 76), snitch-app (Block 77), partner-facing dashboards (Block 84 + Block 60 moon-pod observer), the LAN-only island surface (Block 87), and the rapid-bringup operator surface (Block 68). Writing the operating discipline once is meaningfully cheaper than rediscovering it five times.

Designing flashlight as a sibling-to-panoscope (focus-oriented Zenoh inspection that remains useful under bad network AND bad client compute) forced the question to a head: what *exactly* is the structural pattern that produces a useful tool under those conditions without abandoning modern client-side capability when conditions allow? The answer extracted as a doctrine, with flashlight as its first instance.

## Decision

**Adopt minimalism-under-network-constraints as project-wide doctrine** for any goat web surface where the operator might be in any DIL regime.

The doctrine is the design document [`docs/design/minimalism-under-network-constraints.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/minimalism-under-network-constraints.md). It establishes:

- The **DIL operating regime** as the named target — every doctrine-adopting surface declares which DIL states (Disconnected / Intermittent / Low-bandwidth) it serves and what each looks like.
- The **two-tier architectural pattern**:
  - **Tier 1 — canonical workhorse.** Server-rendered HTML over a long-lived streaming connection (SSE today). Vanilla JavaScript only, no framework. Owns the DOM, owns the data buffer, owns rendering. Always loads first; always works alone.
  - **Tier 2 — optional coprocessor.** A Rust→WASM module that reads Tier 1's state and produces *additional* views (decoded payloads, statistical overlays, analytics). Treats Tier 1 as a workhorse, never replaces it; gracefully evicts under bad conditions.

- **14 principles** governing the architecture, network discipline, graceful degradation, compute placement, and surface discipline. Listed in the anchor design doc.
- **5 conscious rejections** with reasoning recorded: WebTransport (loses middlebox traversal + `Last-Event-ID`); per-component partial hydration (Astro/Marko islands — wrong shape for real-time surfaces); hydration entirely; resumability frameworks (Qwik — framework floor cost exceeds Tier 1 budget); WebGPU for tiny visualizations (pipeline-creation cost dominates); generic Service-Worker caching (lifecycle bugs); frameworks in Tier 1.
- **3 locked-in tactical decisions** with rationale: Brotli compression on long-lived event streams + ZSTD on request-response endpoints; SVG for semantic objects (icons, status pills) + Canvas 2D for streaming visualization (sparklines, ribbons); Progressive Enhancement with `@preact/signals-core` (~1.5 KB) available as a *library* escape hatch if Tier 1 reactivity ever genuinely outgrows vanilla event handlers.
- **Equivalence-by-construction** where features have both Tier 1 and Tier 2 implementations: CI asserts byte-identical output on canned input streams. Catches drift between tiers at build time.
- **Operator-controllable compute placement.** The same Rust analytics crate compiles to both `wasm32-unknown-unknown` (Tier 2 browser) and native (a server-side `/api/compute` endpoint). The operator picks where compute lands per-request; no code changes required to reverse the choice.

A surface adopts the doctrine by landing its design doc with a one-sentence declaration: *"This surface adopts [minimalism-under-network-constraints.md]; deviations are noted below."* Deviations must be *named* deviations with rationale; silent drift from the doctrine is forbidden.

## Scope (in / out)

**In scope:**

- Goat web surfaces that may be loaded under any DIL regime (degraded network and/or degraded client compute).
- The two-tier pattern, the 14 principles, the 5 conscious rejections, and the 3 locked-in tactical decisions.
- The equivalence-test discipline for features that have both Tier 1 and Tier 2 implementations.
- Compute placement portability (dual-target Rust crate compiling to WASM + native).

**Out of scope:**

- Non-web surfaces: native desktop apps, CLI tools, daemons. Those have their own discipline (e.g., `goat-client` per ADR 0840, `snitch` per [`docs/design/snitch.md`](../design/snitch.md)). The *cross-cutting* principles those surfaces share with this doctrine are extracted into parent [ADR 0863 — Resilient end-node design philosophy](0863-resilient-end-node-design-philosophy.md).
- Production-grade real-time NOC dashboards on always-good networks. Doctrine still applies and produces a good outcome, but the constraints aren't load-bearing there; surfaces are free to deviate with rationale.
- Server-internal protocols (Zenoh, Spock, wg-cp0). Doctrine governs the *operator-facing surface*, not the substrate beneath it.
- Mobile-native apps. snitch-app (Block 77) is the canonical mobile surface; doctrine may inform but does not govern native mobile UI.
- **Bulk container image distribution.** Different shape (content-addressing, cache tiers, cosign verification). Covered by Block 75 + ADR 0838.
- **Streaming media (video, audio).** Different shape (HLS/DASH adaptive streaming, codec-aware bitrate ladders). Covered by Block 35 video-fabric.
- **Mission-data blob exchange.** Different shape (trust-domain crossing, data-conditioning, partner-namespace governance). Covered by Block 21 + [ADR 0821](0821-mission-partner-sharing-pattern.md).
- **Bulk historical sensor-data retrieval.** Query/range/paging over archived NDJSON; identified gap, see backlog stub `bulk-historical-sensor-data-retrieval-doctrine`.
- **Static document / briefing / image delivery under DIL** (PDFs, decks, arbitrary images). Progressive rendering + chunked transfer concerns; identified gap, see backlog stub `progressive-bulk-asset-delivery-under-dil`. The briefing and dataflow pages operate without doctrine today.

## Alternatives considered

- **Status-quo ad-hoc-per-surface.** Each surface (panoscope, portal, isitup, brief, dataflows) is engineered independently; the operator experience under bad network is whatever each surface happens to produce. We have empirical evidence this produces inconsistent outcomes (panoscope works under degraded conditions because Block 82Q's self-aware-degradation work happened to land; isitup degrades silently because nothing pushed back). Rejected — doesn't scale to ≥5 forthcoming DIL-relevant surfaces.

- **Full-SPA-everywhere with offline-first toolkit.** Adopt a modern SPA framework (Qwik / Solid / Astro) + a CRDT library (Yjs / Automerge) + a Service-Worker app-shell across all surfaces. Excellent at one extreme (collaborative editors), wrong shape at the other (real-time-data inspection tools). Framework floor cost (~30-50 KB for the smallest hello-world) exceeds the entire Tier 1 budget on its own. Rejected — solves a problem we don't have (framework state to hydrate / resume / collaborate on) at a cost we can't afford.

- **WASM-everywhere as Tier 1.** Single Tier — Rust→WASM frontend that takes over the page immediately. Beautiful end-to-end typing; impossible cold-load profile under bad networks. A typical Leptos hello-world ships ~500 KB compressed; non-trivial UIs ship 1-3 MB. On a 100 kbps tactical link, 1 MB = ~80 s before the page does anything. Rejected — disqualifies the DIL operating regime by construction.

- **Feature-parity-with-graceful-degradation.** Tier 1 and Tier 2 implement *identical* feature surfaces, with Tier 2 as a transparent accelerator. Considered seriously; landed as the early framing. The duplication tax is real (every analytics feature = JS implementation + Rust implementation + equivalence test + drift-detection). Some features (protobuf decode against arbitrary descriptors, SIMD-class compute, schema diff over large windows) genuinely cannot live in cheap Tier 1 without exceeding the first-paint budget. Pivoted to "feature parity where cheap, Tier-2-only where expensive, honest labels where they differ." The doctrine captures this as Principle 11 (honest naming) and Principle 12 (equivalence-by-construction *where features overlap*).

- **Tier-2-primary with Tier-1-fallback.** Make Tier 2 the canonical surface; Tier 1 is the degraded fallback when Tier 2 fails or never loads. Two problems: (1) the page is blank or stub-rendered until Tier 2 either loads or fails, breaking "usable on first byte"; (2) under good conditions, operators never exercise Tier 1, so it bit-rots and the fallback stops actually working. Rejected — inverts the asymmetry that makes the pattern survive.

- **Adopt this doctrine but as flashlight-specific, not project-wide.** Considered. The doctrine cost the same to write either way. Five forthcoming surfaces (goat-client GUI, snitch-app, partner dashboards, the LAN-only island surface, rapid-bringup) benefit from the same discipline; writing it once and applying it five times is cost-effective. Rejected scoping it narrow.

## Consequences

**Positive:**

- A new DIL-relevant surface has a single doctrine to reference rather than re-deriving the discipline from primitives. The doctrine names the principles, the rejections, and the decisions so design reviewers have something concrete to interrogate.
- Equivalence-by-construction CI catches drift between Tier 1 and Tier 2 at build time, not in production.
- Operator-controllable compute placement (browser-WASM vs server-`/api/compute`, same Rust code) gives operators a real lever for degraded clients without code changes.
- Aligns with state-of-the-art expert consensus on SSE-vs-WebSocket/WebTransport, on URL-as-state hypermedia patterns, on local-first principles (Kleppmann / Ink & Switch), and on the 14 KB initcwnd budget — recorded in the doctrine doc's "validation against state of the art" section so the alignment reads as a choice rather than a parochial invention.

**Negative / cost:**

- Two-tier architecture means two test paths. "Useful without Tier 2" has to be defended in code review every time someone adds a feature; easy to accidentally regress Tier 1 by making it dependent on a function that only exists in Tier 2. Mitigated by a CI smoke test that loads the page with WASM blocked and asserts core surfaces still work.
- The 14 principles + 5 rejections + 3 decisions is a non-trivial doctrine surface. New contributors adopting the doctrine for a new surface have to read it. Mitigated by writing the doctrine doc to be read in one sitting (~15-minute read) and by flashlight serving as a worked first instance.
- Doctrine evolves as we learn what works. The first material revision target is when flashlight ships (Phase E gate) and we learn what we got wrong.
- The factor-out of `panoscope-core` (Phase B) imposes one-time mutation cost on panoscope. Behavior is preserved exactly; equivalence asserted by existing panoscope test suite + a Playwright smoke against a running panoscope.

## Composition

- **Parent: [ADR 0863 — Resilient end-node design philosophy](0863-resilient-end-node-design-philosophy.md).** This ADR is the *web instantiation* of the parent philosophy; seven principles inherit (canonical workhorse + coprocessor, usable-on-first-byte, declarative state in invocation artifact, graceful degradation in both directions, operator-controllable compute placement, honest naming, DIL operating regime framing). The web-tactical seven (compression algorithms, SVG vs Canvas, Progressive Enhancement, web platform primitives, 14 KB initcwnd, Speculation Rules) are this ADR's own. Future non-web instantiations (goat-client GUI Block 76, snitch-app Block 77) are siblings under the same parent.
- **Sibling: [ADR 0862 — Two-tier web architecture: Tier-1 canonical, Tier-2 coprocessor](0862-two-tier-web-architecture-canonical-coprocessor.md).** Records the load-bearing architectural decision *inside* this doctrine separably.
- **[ADR 0112 — Tech strategy: workload-shape language boundary](0112-tech-strategy-workload-shape-language-boundary.md).** Doctrine's "Rust→WASM for compute / vanilla JS for surface" choice composes with the Rust-for-hot-path / Go-for-CLI rule. flashlight's dual-target `flashlight-analytics` crate is a Rust workload that happens to compile to two targets.
- **[ADR 0859 — Easy rollback as an engineering principle](0859-easy-rollback-engineering-principle.md).** Sibling engineering-principle ADR; same shape (principle ADR backed by design doc + tier-0 sweep). flashlight is a tier-0 rollback adopter from day one.
- **[ADR 0006 — Concurrency governance for multi-agent + human teaming on a shared trunk](0006-multi-agent-human-concurrency-governance.md).** The doctrine's phase-gated implementation (Phase A planning → B factor-out → C Tier 1 demo → D analytics crate → E Tier 2 integration) is governed by the per-track active-work yaml mechanism.
- **[ADR 1015 — Code-review discipline](1015-code-review-discipline.md).** Doctrine deviations require named rationale; that's a T2-reviewer check.

## Tracker

[`docs/design/minimalism-under-network-constraints.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/minimalism-under-network-constraints.md) (doctrine) and [`docs/design/flashlight.md`](https://github.com/usareur-af/goatstuff/blob/main/docs/design/flashlight.md) (first instance). Track: `flashlight-and-minimalism` for the doctrine + flashlight build, in-flight as Block 89.
