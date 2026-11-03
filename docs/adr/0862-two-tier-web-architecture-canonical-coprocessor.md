# ADR 0862 — Two-tier web architecture: Tier-1 canonical, Tier-2 coprocessor

**Status:** Proposed 2026-05-24.
**Anchor design:** [`docs/design/minimalism-under-network-constraints.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/minimalism-under-network-constraints.md) §"The two-tier pattern".
**First instance:** [`docs/design/flashlight.md`](https://github.com/usareur-af/goatstuff/blob/main/docs/design/flashlight.md).

## Context

ADR 0861 adopts minimalism-under-network-constraints as project-wide doctrine. The doctrine's load-bearing architectural choice is the **two-tier asymmetry** — Tier 1 is canonical and always running; Tier 2 is a coprocessor that reads from Tier 1's state and adds derived views. This ADR records that architectural decision separately so reviewers can revisit the tier model without revisiting the whole doctrine.

The motivating design conversation for flashlight surfaced multiple plausible shapes for layering a modern Rust→WASM analytics frontend on top of a lean HTML+SSE foundation:

- **Tier 2 primary, Tier 1 fallback.** WASM is the canonical experience; the vanilla JS path activates only when WASM is unavailable.
- **Feature parity in both tiers with deterministic equivalence.** Same surface in both; Tier 2 is a transparent accelerator. Tier 1 is a complete-but-slower mirror.
- **Tier 1 canonical, Tier 2 coprocessor.** Tier 1 owns the data path and rendering; Tier 2 reads Tier 1's state and produces *additional* views.

Each shape produces a different operational profile under degraded conditions, a different failure mode, and a different long-term maintenance shape. The decision is non-trivial.

## Decision

**Adopt the Tier-1-canonical / Tier-2-coprocessor asymmetry as the architectural shape for every minimalism-doctrine-adopting surface.**

Structurally:

1. **Tier 1 owns the data path.** The streaming connection (Server-Sent Events today), the in-memory event ring buffer, and the DOM all belong to Tier 1. Tier 1 is built with server-rendered HTML + ~50-200 KB of vanilla JavaScript; no framework, no client-side router, no virtual DOM.
2. **Tier 1 always loads first.** First paint precedes any Tier 2 load decision. The page is fully usable for its primary surgical-inspection task with Tier 1 alone — *not* a stub, *not* a placeholder waiting for Tier 2.
3. **Tier 2 reads Tier 1's state, never writes it.** When Tier 2 (a Rust→WASM analytics module) loads, JavaScript forwards each new event into the WASM module via `instance.exports.push_event(ptr, len)`. The WASM module never opens its own streaming connection, never owns the DOM, never owns the event buffer.
4. **Tier 2 augments, never replaces.** Tier 2 produces *additional* panels (decoded payloads, rolling-window statistics, schema diff, anomaly highlights). It does not replace a Tier 1 panel with a "better" Tier 2 version. CSS-class `.tier-2-only` makes augmentation surfaces visible only when WASM is loaded; the same surfaces are hidden in Tier 1, with honest naming (e.g., "decode requires analytics module — load now?") rather than silent dead-ends.
5. **Tier 2 evicts cleanly under bad conditions.** On WASM crash, OOM, eviction by mobile-browser suspend, or operator-requested unload, the module is dropped, `.tier-2-only` surfaces hide, and Tier 1 continues *unchanged*. Tier 1 was already running and keeps running; nothing has to be torn down or re-mounted.

The asymmetry is the load-bearing property. Tier 1 has *zero knowledge* of Tier 2's existence; Tier 2 reads from Tier 1's stable interface (the ring buffer + the focused-subscription URL + a `BroadcastChannel` for events). This structural one-way dependency is what makes failure modes graceful by construction.

## Scope (in / out)

**In scope:**

- Every minimalism-doctrine-adopting surface (per ADR 0861).
- The structural asymmetry above; the one-way Tier-2-reads-Tier-1 dependency contract; the augmentation-not-replacement rule for Tier 2 surfaces; the `.tier-2-only` UI convention.
- Equivalence-by-construction CI for features that have implementations in both tiers (subset of features; many features are Tier-2-only by design).

**Out of scope:**

- The Tier 2 *load gate* (auto-detect via `navigator.connection.effectiveType` vs operator-opt-in vs always-on). That's per-surface tactical and recorded in the surface's design doc.
- The Tier 2 module's *internal architecture* (single Rust crate vs multi-crate workspace, dependency choices, etc.).
- Whether a particular feature belongs in Tier 1, Tier 2, or both. That's a per-feature decision in each surface's design doc, subject to the doctrine's first-paint-budget and honest-naming principles.

## Alternatives considered

- **Tier-2-primary with Tier-1-fallback.** Make WASM the canonical experience; Tier 1 activates only when WASM is unavailable. Problems: (1) page is blank or stub-rendered until Tier 2 either loads or fails, breaking "usable on first byte"; (2) under good conditions, operators never exercise Tier 1, so it bit-rots and the fallback stops actually working — the equivalent of a never-run disaster-recovery procedure; (3) inverts the asymmetry that makes failure modes graceful (Tier 2 failing now means tearing down the active rendering layer). Rejected.

- **Feature parity in both tiers with deterministic equivalence.** Both tiers implement *identical* feature surfaces; Tier 2 is a transparent accelerator. Considered seriously; was the early framing. Problems: (1) duplication tax is real — every analytics feature = JS implementation + Rust implementation + equivalence test + drift-detection across tiers; (2) some features (protobuf decode against arbitrary descriptors, SIMD-class compute, schema diff over large windows) genuinely cannot live in cheap Tier 1 without exceeding the first-paint budget — forcing parity here means either bloating Tier 1 with ~50 KB of `protobuf.js` or shipping no decode at all. Pivoted to the asymmetric model with the doctrine principle "honest naming, not silent dead-ends" for Tier-2-only features.

- **Single-tier WASM-everywhere.** Rust→WASM frontend takes over the page immediately; no vanilla-JS substrate. Beautiful end-to-end typing; impossible cold-load profile under bad networks (a typical Leptos hello-world ships ~500 KB compressed; non-trivial UIs ship 1-3 MB). Rejected by the doctrine; covered in ADR 0861 alternatives.

- **Single-tier vanilla-JS-everywhere.** Drop Tier 2 entirely; live with vanilla-JS performance and lose access to the analytics workloads (protobuf decode, schema diff, statistical work over long windows). Considered as a doctrine simplification; rejected because the use cases that benefit from WASM (real-time inspection of decoded payloads, sliding-window statistics, schema diff) are exactly what operators need from flashlight.

- **Per-component partial hydration (Astro / Marko 6 islands).** Cleaner mental model for content-heavy sites with isolated interactive widgets; wrong shape for real-time-data surfaces where the "interactive widget" is the whole page. Rejected; recorded in ADR 0861.

- **Resumability frameworks (Qwik).** Elegantly solves the hydration cost problem — for surfaces that *have* framework state to hydrate. We don't. Tier 1 has no framework state. Framework floor cost (~30-50 KB minimum) exceeds the entire Tier 1 budget. Rejected; recorded in ADR 0861.

## Consequences

**Positive:**

- **Failure modes are graceful by construction.** Tier 2 crashes / fails to load / never gets requested → Tier 1 was already running and keeps running. No re-mount, no tear-down, no "fallback experience" that needs separate testing.
- **First paint is non-negotiable.** Tier 1 ownership of the data path means the page is interactive within ~200 KB on the wire; Tier 2 is purely additive and never blocks.
- **Compute placement is operator-controllable.** Because Tier 2's analytics live in a separable Rust crate compiled to both WASM and native, the operator can shift compute from browser to server (`?compute=server`) without code changes. Same code path on both sides; equivalence asserted in CI.
- **Bit-rot resistance.** Operators on good networks exercise *both* tiers (Tier 1 renders, Tier 2 augments). Tier 1 is never a "neglected fallback" — it's the canonical path everyone uses, every load.
- **CI surface is well-defined.** Equivalence between Tier 1 and Tier 2 *where features overlap* is byte-identical-output-on-canned-input. Easy to assert, easy to read in PR diffs.

**Negative / cost:**

- **Two rendering paths to maintain.** When a feature has both a Tier 1 and Tier 2 implementation, both need to be kept working. Mitigated by limiting the overlap surface — most features are Tier-1-only (the cheap basics) or Tier-2-only (the compute-heavy work); only a few sit in both.
- **Discipline overhead in code review.** "Useful without Tier 2" has to be defended every PR — a tempting "this feature only makes sense with Tier 2 loaded" inadvertently regresses the doctrine. Mitigated by a CI smoke test that loads the page with WASM blocked (Content Security Policy) and asserts core surfaces still work.
- **Single-island upgrade boundary, not per-component.** A future surface that wants Astro-style per-component islands has to deviate from doctrine with named rationale; the doctrine's stance is that single-island is simpler to reason about and adequate for the surfaces we have.
- **Tier 2 module versioning vs Tier 1 deployment.** If Tier 1 ships an updated event schema and Tier 2 is cached at an older version, operators see staleness. Mitigated by a Tier 1 schema-version header in the SSE handshake; Tier 2 refuses to attach if mismatched and surfaces "analytics module is stale — reload" with honest naming.

## Composition

- **Parent philosophy: [ADR 0863 — Resilient end-node design philosophy](0863-resilient-end-node-design-philosophy.md).** This ADR is the web instantiation of the parent's principle-1 (canonical workhorse + optional coprocessor asymmetry). The asymmetric one-way dependency Tier 2 → Tier 1 inherits directly from the parent; the specific Tier-1 (HTML + SSE + JS) / Tier-2 (Rust→WASM) shape is the web tactical layer recorded here. Future non-web instantiations (goat-client GUI Block 76, snitch-app Block 77) re-instantiate the same parent principle with platform-tactical specifics.
- **[ADR 0861 — Minimalism under network constraints (doctrine adoption)](0861-minimalism-under-network-constraints.md).** This ADR records one of the load-bearing decisions inside that doctrine; revisiting this ADR without revisiting 0861 is valid (e.g., considering an asymmetry-direction change).
- **[ADR 0112 — Tech strategy: workload-shape language boundary](0112-tech-strategy-workload-shape-language-boundary.md).** The Tier 2 Rust crate compiled to both WASM and native fits the per-byte-hot-path / on-data-path-sidecar Rust rule.
- **[ADR 1015 — Code-review discipline](1015-code-review-discipline.md).** The "useful without Tier 2" property is a T2-reviewer check.
- **[ADR 0859 — Easy rollback as an engineering principle](0859-easy-rollback-engineering-principle.md).** flashlight and any other doctrine-adopting surface adopts the easy-rollback tier-0 ledger from day one; if a Tier 1 or Tier 2 image causes regression, one-command rollback applies.

## Tracker

[`docs/design/minimalism-under-network-constraints.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/minimalism-under-network-constraints.md) §"The two-tier pattern" (architecture summary) and [`docs/design/flashlight.md`](https://github.com/usareur-af/goatstuff/blob/main/docs/design/flashlight.md) §"Architecture" (worked first instance). Track: `flashlight-and-minimalism`, in-flight as Block 89.
