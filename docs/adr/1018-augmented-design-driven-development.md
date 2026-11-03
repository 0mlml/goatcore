# ADR 1018 — Augmented Design-Driven Development (ADDD)

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-26 |
| Steward | @contributorc |
| Repo scope | goat core + every consumer-repo where the same Block discipline applies |
| Supersedes | — |
| Superseded by | — |
| Related | [`1014-codex-pattern-adr-to-enforced-lint.md`](1014-codex-pattern-adr-to-enforced-lint.md), [`1015-code-review-discipline.md`](1015-code-review-discipline.md), [`1016-complexity-budget-substrate-encapsulation.md`](1016-complexity-budget-substrate-encapsulation.md), [`0006-multi-agent-human-concurrency-governance.md`](0006-multi-agent-human-concurrency-governance.md), [`0017-claude-code-session-isolation-pilot.md`](0017-claude-code-session-isolation-pilot.md), [`0839-continuous-handoff-baton.md`](0839-continuous-handoff-baton.md), [`0859-easy-rollback-engineering-principle.md`](0859-easy-rollback-engineering-principle.md). Companion design at [`docs/design/design-system.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/design-system.md) (Block 91 — first instance of the canon-tier pattern, install of the GOAT-Net design system from a claude.ai/design bundle). |

## TL;DR (for a layperson)

The agentic shift has changed the pace at which code can be written;
the same shift can change the pace at which the *design specification*
of what to build is authored, if we let it. Augmented Design-Driven
Development (ADDD) names the practice of authoring full design specs
in AI-augmented design tools (claude.ai/design today; other tools as
the ecosystem matures), handing the output into the repo as canon, and
then building to those specs with the same agentic force we already
bring to bear on the code. The first instance landed in Block 91 — the
GOAT-Net design system was authored in claude.ai/design, vendored into
[`docs/design-system/`](https://github.com/usareur-af/goatdocs/tree/main/docs/design-system) as the source of truth, and
plugged into Claude Code sessions via a `SKILL.md` so every new UI
surface is built against the spec automatically. Per-product wireframes
follow a parallel pattern at `docs/design-assets/<product>/`,
each one consuming the canon. The discipline is the right default for
new features and new products, but never gates code velocity — when
no designer is available, the team ships and ADDD adopts the work
retroactively.

## TL;DR (for a sponsor)

We have already changed how fast code gets written by adopting an
agentic operating model. The bottleneck moved upstream: design specs
authored at human-design-tool speed cannot keep up with code authored
at agentic speed. ADDD names the practice of moving the design-spec
authoring layer to the same agentic-speed tier — author specs in
AI-augmented design tools (claude.ai/design first, others as they
mature), commit them into the repo with provenance preserved, and
build to those specs with the agentic stack we already trust. Cost is
disciplined adoption of a new authoring surface and a small amount of
process scaffolding. Benefit is removing the spec-authoring bottleneck
that would otherwise cap agentic code velocity. ADDD is a default, not
a gate — features ship without it when designers aren't available;
retroactive adoption is supported.

## TL;DR (for SCA-R)

ADDD is a standing engineering-process discipline. Bundles vendored
into the repo at two tiers: (1) **canon tier** — one fleet-wide
bundle at [`docs/design-system/`](https://github.com/usareur-af/goatdocs/tree/main/docs/design-system) carrying the
system-wide visual + verbal vocabulary plus its `SKILL.md` agentic
routing point; (2) **per-product tier** — one bundle per product /
surface at `docs/design-assets/<product>/`
carrying wireframes, prototypes, and interaction reference, each
consuming the canon and optionally shipping an
`INSTRUCTIONS-FOR-IMPLEMENTING-AGENT.md` for product-specific
implementation guidance. Both tiers preserve provenance in `_origin/`
or equivalent. The pattern composes with — does not replace — the
existing discipline ADRs (1014/1015/1016/0006/0859). No new
compliance surface; bundles are canon documentation, treated as
siblings of `docs/design/` and `docs/adr/`. Residual risk: the
authoring tool is owned by a vendor; the bundle preserves enough
provenance + content that we can keep the design canon even if the
tool goes away.

## §1 Vocabulary

- **ADDD** — Augmented Design-Driven Development. The discipline
  defined by this ADR.
- **Authoring surface** — the AI-augmented tool where the spec is
  drafted (today: claude.ai/design; future: whatever the ecosystem
  matures into).
- **Bundle** — the self-contained output of an authoring-surface
  session, vendored into the repo as the source of truth for that
  spec. Contains everything a downstream surface needs to build
  against the spec (tokens, components, wireframes, schemas, examples,
  voice rules, etc.) plus provenance preserved in `_origin/` or
  equivalent.
- **Canon-tier bundle** — fleet-wide design canon at
  [`docs/design-system/`](https://github.com/usareur-af/goatdocs/tree/main/docs/design-system). One per project.
  Carries tokens, components, voice, brand, UI kits. Ships
  `SKILL.md` (see below). First instance: PR #497, the GOAT-Net
  design system.
- **Per-product-tier bundle** — per-product wireframe /
  prototype reference at `docs/design-assets/<product>/`.
  One per product or surface. Consumes the canon. Carries HTML/CSS/JS
  prototype output + conversation transcript + screenshots + brief.
  First instance: `docs/design-assets/stethoscope/`.
- **Provenance preservation** — every bundle carries the handoff
  README + the chat transcript (or comparable artifact) from the
  authoring-surface session, so future readers can trace any bundle
  back to the conversation that produced it. Canon-tier uses
  `_origin/`; per-product-tier typically uses `chats/`. Filename is
  not load-bearing; the discipline is.
- **`SKILL.md` — canon-tier only.** Lives ONLY in the canon-tier
  bundle (`docs/design-system/SKILL.md`). It is the agentic routing
  point that every Claude Code session passes through when authoring
  UI in the design system's domain. There is exactly one SKILL.md per
  project. Per-product bundles do NOT ship `SKILL.md` — they're not
  fleet-wide routing, they're product-specific reference.
- **`INSTRUCTIONS-FOR-IMPLEMENTING-AGENT.md` — per-product-tier,
  optional.** A per-product bundle MAY ship this file to give an
  implementing agent extra clarity about where the bundle fits in
  the doc chain, what to build vs. what not to build, and which
  pieces in the bundle are normative vs. illustrative. It is optional
  — a bundle without it is still valid. When present, it sits
  alongside the authoring-tool's own README (which is verbatim what
  the tool generated; not edited).
- **First-instance pair (Block 91 + stethoscope-design-assets)** —
  the GOAT-Net design system install at PR #497 established the
  canon-tier pattern; the stethoscope design-assets bundle at
  track `stethoscope-design-assets` established the per-product-tier
  pattern. Together they define the convention this ADR codifies.
- **Phase A / Phase B-N** — Phase A is the install-as-canon (bundle
  lands, zero consumer changes, risk floor). Phase B onward is
  per-consumer adoption, each its own PR, additive, with rollback at
  the image-SHA level per [ADR 0859](0859-easy-rollback-engineering-principle.md).

## §2 Context

The agentic shift has compressed code authoring time by an order of
magnitude. The Block 91 / stethoscope-design-assets pair is the
demonstration that the same compression is available at the
**design-spec** layer if the authoring surface is adopted with the
same discipline as the code-side stack:

- The GOAT-Net design system existed as an authoring-surface session
  before it existed in the repo. The session produced a full visual +
  verbal canon (tokens, components, two UI kits, preview library,
  voice rules) in a single round, not a multi-week bespoke-design
  cycle. Handoff was a self-contained bundle, not a Figma file or PDF
  spec. Provenance preserved in `_origin/`. Invocation via `SKILL.md`
  so every Claude Code session that authors a UI surface goes through
  the canon automatically. Phase A install was zero-consumer-change
  (risk floor); Phases B-G migrate one surface per PR additively with
  image-SHA-level rollback covering each migration independently.
- The stethoscope wireframe bundle followed weeks later — same
  authoring surface, same self-contained-bundle handoff shape, same
  provenance discipline, but scoped to one product. The bundle
  consumed the design-system canon (tokens + components imported
  verbatim from the Block 91 bundle into the wireframe's CSS) and
  lived at `docs/design-assets/stethoscope/`. No fleet-wide SKILL.md
  — the routing point is the canon's; the per-product bundle ships
  optional `INSTRUCTIONS-FOR-IMPLEMENTING-AGENT.md` for clarity on
  what's normative vs. illustrative.

The pattern is general. Visual canon and product wireframes are the
first two tiers. Plausible future instances include:

- A schema-authoring bundle that produces canonical protobuf shapes
  + per-language code examples + decoder fixtures (relevant to
  Block 23 rapid adaptation per [ADR 0512](0512-rapid-adaptation-with-ai.md)).
- An architecture-authoring bundle producing canonical diagrams +
  ADR-stub templates + per-component implementation hints (relevant
  to the [architecture spine](https://github.com/usareur-af/goatdocs/blob/main/docs/design/architecture-spine.md)).
- A briefing-authoring bundle producing canonical decks + voice +
  audience-tailored variants (relevant to the existing brief /
  dataflows / patterns surfaces).
- A test-authoring bundle producing canonical test fixtures + edge
  cases + harness scaffolding (relevant to [ADR 1008](1008-soak-testing-discipline.md)
  + [ADR 1012](1012-load-testing-discipline.md)).

None of those instances are committed by this ADR. The ADR codifies
the pattern that makes any of them low-friction.

## §3 Decision

### The two-tier bundle placement

**Canon tier — fleet-wide design canon.**
- Location: [`docs/design-system/`](https://github.com/usareur-af/goatdocs/tree/main/docs/design-system). One per project.
- Contents: tokens, components, voice, brand mark, diagram library,
  UI kits, preview library.
- Provenance: `_origin/` subdirectory carrying the handoff README +
  chat transcripts from the authoring session.
- Agentic routing: ships `SKILL.md` at the bundle root. This is the
  ONE invocation point every Claude Code session passes through when
  authoring UI in the design system's domain. There is exactly one
  `SKILL.md` per project.
- Lifecycle: permanent. The canon evolves; it does not get archived.
  Versioning + migration through additive change.

**Per-product tier — per-product wireframes and prototypes.**
- Location: `docs/design-assets/<product>/`.
  One bundle per product, surface, or feature.
- Contents: HTML/CSS/JS prototype output from the authoring surface
  + brief that fed the conversation + screenshots + whatever else
  the authoring session produced.
- Provenance: `chats/` (or equivalent) subdirectory carrying the
  conversation transcripts.
- Authoring-tool README: a `README.md` at bundle root carrying the
  authoring tool's verbatim coding-agent instructions. Not edited
  after handoff.
- Optional clarity layer: `INSTRUCTIONS-FOR-IMPLEMENTING-AGENT.md`
  alongside the README, authored by the steward / track owner, to
  give the implementing agent extra clarity about where the bundle
  fits in the doc chain, what to build vs. not to build, what's
  normative vs. illustrative, and when the design doc and the bundle
  disagree. Optional — a bundle is valid without it; recommended
  when the doc chain is non-obvious or the bundle's internal
  structure shouldn't be copied verbatim.
- No `SKILL.md` at this tier. Per-product bundles are reference, not
  fleet-wide routing. The canon's `SKILL.md` is the only routing
  point; per-product bundles are consumed downstream of it.
- Lifecycle: typically absorbed at production-implementation time.
  When the product ships and the design-of-record reflects everything
  the bundle taught, the bundle MAY be deleted (or left as a historical
  artifact). The steward of the product's track makes the call.

### The mode-of-use discipline

- **Author specs in AI-augmented design tools** rather than bespoke
  human-only tools, whenever the task allows it. The discipline is
  tool-shape-agnostic, not tool-name-bound.
- **Hand off as a self-contained bundle**, not as a link to the
  authoring tool. Files are flat, importable, human-readable. The
  authoring tool can disappear without taking the spec with it.
- **Preserve provenance** in `_origin/` (canon tier) or `chats/`
  (per-product tier). Future readers + future Claude Code sessions
  can trace any decision back to the conversation that produced it.
- **Vendor as documentation canon**, sibling of `docs/design/` and
  `docs/adr/`. Bundles live at the documented paths above. Treat them
  with the same care as ADRs — they are decisions, not drafts.
- **The canon-tier `SKILL.md` is the agentic invocation point.**
  Without it the canon is just static text; with it, the canon is
  load-bearing in the agentic flow.
- **Per-product bundles consume the canon by import**, not by
  re-derivation. A wireframe's CSS imports the canon's `colors_and_type.css`
  and `components.css` verbatim; it does not re-style from scratch.
- **Phase A install-as-canon = zero consumer changes.** Risk floor.
  The bundle lands, the design doc lands, the in-flight entry +
  CLAUDE.md pointer land, but no consumer surface visibly changes.
- **Phases B-N migrate consumers additively**, one PR per consumer,
  with image-SHA-level rollback per [ADR 0859](0859-easy-rollback-engineering-principle.md)
  so any individual migration can be rolled back without disturbing
  the others.
- **Block-completion-review applies** per [ADR 1015](1015-code-review-discipline.md).
  Each migration PR runs the standing checklist.

### The default-but-never-blocking provision

ADDD is the **right default** for new feature work and new product
work — whenever a feature merits a design pass, the path of least
resistance should run through an authoring surface that produces a
bundle the team builds against.

But ADDD is **never a gate on code velocity.** If no designer is
available (the operator and the agent are the only people in the
session, the bundle would take longer than the implementation
itself, the work is small enough that the design conversation
doesn't add signal), the team ships without ADDD and the discipline
adopts the work retroactively when a designer next picks the area
up.

In practice this means:

- New feature merits design pass + designer available → ADDD path:
  authoring surface → bundle → implementation. Canonical.
- New feature merits design pass + designer NOT available → ship.
  Implementation is built against the canon (the design-system
  bundle still applies — its `SKILL.md` routes the session through
  tokens + components + voice regardless). When a designer next
  picks up the area, they author a retrospective bundle that captures
  what got built + the next iteration's direction. Iterate forward,
  not backward.
- Small change / bugfix / one-screen tweak → no ADDD pass needed.
  Existing design-of-record + design-system canon are sufficient.
- Existing product needs visual / interaction rework → ADDD pass
  when a designer is available; retroactive adoption when not.

The discipline supports retroactive adoption by design: every bundle
is self-contained, provenance is preserved, the canon's `SKILL.md`
keeps routing whether or not a per-product bundle exists. A product
without a `docs/design-assets/<product>/` bundle is not broken — it
is unblocked work that ADDD has not yet absorbed.

### What ADDD is NOT

- Not a replacement for human design judgment. The authoring surface
  is a force multiplier on a human (or agent) with taste; it is not
  the source of taste.
- Not a license to skip code review or block-completion review.
  Bundles get the same scrutiny as code. The discipline ADRs
  (1014/1015) apply.
- Not vendor lock-in. The bundle preserves enough content +
  provenance that the canon survives the authoring tool going away.
- Not a mandate to retroactively re-author every existing spec.
  Existing `docs/design/` content stays as-is; ADDD applies to new
  spec authoring + selective replacement when the existing artifact
  is clearly past due.
- Not a gate on shipping. See the "never-blocking" provision above —
  absence of a designer is not a reason to slow code velocity.

## §4 Alternatives considered

- **Status quo — bespoke human-only spec authoring.** Rejected as
  the default because it bottlenecks at the upstream of the agentic
  code stack. The agentic code stack already produces output faster
  than a human-only spec author can keep specifications current.
  Retained as the never-blocking fallback when no designer is
  available — the team ships and ADDD adopts the work retroactively.
- **Skip bundles; reference the authoring surface by link.**
  Rejected because the authoring surface is a vendor product; the
  bundle is the durability story. Without the bundle the canon
  evaporates when the link breaks.
- **In-repo authoring (markdown specs hand-written by humans + agents
  together in PRs).** This is what we do today for `docs/design/`.
  ADDD does not replace it; it adds a complementary surface for the
  cases where the authoring tool's affordances (visual editor,
  component preview, voice + token consistency check) outweigh the
  cost of leaving the repo. Specs that are pure prose stay in
  `docs/design/`; specs that carry visual / token / component
  vocabularies move to the ADDD path.
- **Single-tier bundle hierarchy (canon-only, no per-product tier).**
  Rejected because the canon-tier shape is fleet-wide and permanent,
  while per-product wireframes are scoped + sometimes ephemeral.
  Forcing per-product wireframes into the canon-tier shape would
  pollute the canon. Two tiers with distinct lifecycles, distinct
  routing semantics, and distinct file conventions keep the system
  legible.
- **Symmetric `SKILL.md` at both tiers.** Considered + rejected.
  `SKILL.md` is fleet-wide agentic routing; there is exactly one
  per project. Per-product bundles are scoped reference, not routing.
  Using the same filename at both tiers would suggest symmetry that
  doesn't exist (routing vs. reference) and would let an implementing
  agent discover a per-product SKILL.md and mistake it for a routing
  point. The per-product tier ships an optional `INSTRUCTIONS-FOR-IMPLEMENTING-AGENT.md`
  instead — distinct filename, distinct purpose, signaled by its
  name.
- **Tool-specific ADRs (one per authoring surface).** Rejected
  because the discipline outlives any specific tool. This ADR is
  tool-shape-bound (AI-augmented authoring surface that produces a
  flat self-contained bundle), not tool-name-bound.

## §5 Consequences

**Positive.**

- Removes the upstream bottleneck on agentic code velocity when a
  designer is available.
- Standardizes how AI-augmented design output enters the repo, so
  the second, third, fourth instance lands without re-deriving
  process.
- Composes cleanly with existing discipline ADRs — no replumbing of
  code review, block completion, or rollback.
- Provenance preservation makes future audit straightforward.
- The canon's `SKILL.md` routes every Claude Code session through
  the canon automatically — the canon defends itself.
- Two-tier separation keeps the canon clean (permanent, fleet-wide)
  while letting per-product wireframes come + go on product
  timelines.
- Never-blocking provision means ADDD adoption doesn't slow code
  velocity in the moments when it can't keep up.

**Negative.**

- New dependency on an authoring surface that is a vendor product.
  Mitigated by bundle self-containedness + provenance preservation,
  but real.
- Adds a discipline operators have to learn (canon tier vs.
  per-product tier, when to author in an authoring surface vs. write
  markdown directly, when retroactive adoption applies).
- Bundle drift risk: an authoring session can produce a bundle that
  rhymes with but does not match earlier bundles' conventions.
  Mitigated by treating bundles as ADR-equivalent canon (review
  applies) but not eliminated.
- We do not yet know how many of the plausible future canon-tier
  instances (schema authoring, architecture authoring, etc.)
  actually work in practice. The first canon-tier instance
  (Block 91) + first per-product-tier instance (stethoscope) are
  the only empirical data points. Trigger conditions below define
  when we revisit.
- Retroactive adoption is structurally allowed but adds drift risk
  (the design-of-record is built before the bundle that would
  normally lead it). Mitigation: the canon's `SKILL.md` keeps
  routing through the system-wide vocabulary regardless of whether
  the per-product bundle exists yet.

## §6 Assessor view (SCA-R)

ADDD is documentation-canon discipline; no new runtime surface, no
new identity surface, no new network exposure. The bundles vendored
under `docs/design-system/` and `docs/design-assets/<product>/` are
tracked content, reviewed via the existing code-review discipline
([ADR 1015](1015-code-review-discipline.md)) + block-completion
checklist. Provenance preservation in `_origin/` (canon tier) or
`chats/` (per-product tier) satisfies the audit-trail control class.
Composes with the complexity-budget discipline
([ADR 1016](1016-complexity-budget-substrate-encapsulation.md)) — a
bundle that exceeds its surface budget surfaces in the leak register
the same way any other substrate does. Residual risk is vendor-tool
dependency; mitigated by the self-contained bundle shape (the canon
survives the tool). The never-blocking provision is a deliberate
exemption from ADDD as a hard gate; it does NOT exempt the code from
existing code-review or block-completion discipline.

## §7 Trigger conditions for revisit

- When a second canon-tier instance (schema, architecture, briefing,
  test) is attempted and fails to match the Block 91 shape — that
  signals the canon-tier frame is too narrow or too wide.
- When a second per-product-tier instance (a wireframe bundle for a
  product other than stethoscope) fails to match the
  stethoscope-design-assets shape — that signals the per-product
  frame is too narrow or too wide.
- When the authoring surface (claude.ai/design today) ships
  capabilities that change the shape of the bundle materially
  (export formats, multi-author sessions, etc.).
- When the agentic code-side stack itself changes shape such that
  the upstream-bottleneck analysis no longer holds.
- When the project adopts a second authoring-surface vendor
  alongside claude.ai/design — at that point the ADR explicitly
  generalizes to multi-surface and the `SKILL.md` mechanism may
  need to be revisited.
- When a bundle is found in production carrying canon that drifted
  from the source-of-truth conversation in its provenance directory
  — that signals the review discipline needs strengthening.
- When the never-blocking provision is invoked frequently enough
  that retroactive adoption becomes the steady state — that signals
  designer availability is the bottleneck and may need addressing
  upstream (more designers, faster authoring surfaces, looser canon).
