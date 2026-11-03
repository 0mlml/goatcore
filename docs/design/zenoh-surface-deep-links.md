# zenoh-surface-deep-links — cross-surface navigation contract

| Field | Value |
|---|---|
| Status | Design (authored 2026-05-27). Phase A planning, except hop 7 + the `identity=` param, built 2026-08-17. |
| Scope | URL-only deep-link contract among the four zenoh-adjacent web surfaces — **panoscope** (whole-fabric COP), **stethoscope** (per-target diagnosis), **flashlight** (focused live tail), **portal** (registry + catalog). No runtime coupling; each surface constructs outbound links from a small sibling-base-URL set and consumes a small inbound query-param vocabulary. Standardizes the existing one-off `panoscope → portal /schema.html?key=<topic>` link as the canonical pattern. |
| Related | `panoscope.md`, [`stethoscope.md`](https://github.com/usareur-af/goatstuff/blob/main/docs/design/stethoscope.md) §"Complementarity with sibling surfaces", [`flashlight.md`](https://github.com/usareur-af/goatstuff/blob/main/docs/design/flashlight.md) §"Complementarity with panoscope", [`portal-schema-publishing.md`](portal-schema-publishing.md), [`portal-vendor-progress.md`](portal-vendor-progress.md). |

## What this is

A documented contract so the operator can hop from one surface to another while *preserving the thing they're looking at* — a peer, a topic, or a vendor — without re-typing, re-selecting, or re-deriving context.

The four surfaces answer four distinct questions about the same fabric (per stethoscope.md):

| Surface | Question |
|---|---|
| Portal | What's *declared*? |
| Panoscope | What does the fabric look like *right now*? |
| Flashlight | What's happening in *this slice* under any conditions? |
| Stethoscope | Is *this target* healthy? |

Operators move between those questions constantly. Stethoscope flags a topic as Fail → operator wants the live wire bytes (flashlight) → then the broader fabric context (panoscope) → then back to portal to update the schema declaration. Today most of those hops are manual: copy the topic key, switch tab, paste into another surface's input box. The hops are real and frequent — they're already documented in §"Complementarity" sections across stethoscope.md and flashlight.md — they just aren't *implemented uniformly* and there's no shared rulebook.

The contract has two halves:

- **Outbound** — each surface knows how to construct deep links into the other three. This needs a small per-surface config holding the sibling base URLs plus a tiny URL-building helper.
- **Inbound** — each surface accepts a small canonical query-param vocabulary so links land correctly. Two surfaces (flashlight, stethoscope) already do; panoscope adds inbound handling here; portal extends an existing pattern.

This is *not* a SPA router, *not* a runtime API, *not* a shared state store. Just URLs.

## The seed pattern (already in production)

Panoscope's side-panel renders a `↗` next to every topic that links to the portal's schema viewer:

```ts
// ops/panoscope/frontend/src/main.ts:1368
link.href = `${portalBase}/schema.html?key=${encodeURIComponent(topic)}`;
```

`portalBase` reaches the frontend via `/api/legend`, which serves it out of the backend config field `portal_schemas_url` (transformed to a base URL by stripping `/api/schemas`). The whole loop is: **config field → meta endpoint → renderer state → URL builder.** Five small pieces, no runtime coupling between panoscope and portal.

This contract generalizes that loop to all four surfaces and adds a documented vocabulary so each surface can target any other.

## The named hops

Seven hops, drawn from operator flows already named in the four surface docs. Hop 7 (peer → owning identity) is built; the rest remain Phase A planning. Anything outside this list is deferred — the goal is the highest-signal subset, not exhaustive pair-wise wiring.

| # | Direction | Trigger | Target URL | Operator flow |
|---|---|---|---|---|
| 1 | panoscope → stethoscope | side-panel "diagnose this peer / topic / vendor ↗" | `stethoscope/?vendor=<v>` or `?topic=<key>` | "panoscope shows something looks off — get me a verdict" |
| 2 | panoscope → flashlight | side-panel "tail this topic / this peer ↗" | `flashlight/?key=<topic>` or `?vendor=<v>` | "show me actual sample payloads streaming" |
| 3 | any → portal | per-topic `↗` link (already in panoscope; same shape in stethoscope topic-cards + flashlight event-rows) | `portal/schema.html?key=<topic>` | "what's the declared schema for this?" |
| 4 | stethoscope / flashlight → panoscope | header "see this in panoscope ↗" | `panoscope/?peer=<id>` or `?vendor=<v>` or `?key=<topic>` | "the diagnosis points at X — show me X in the fabric" |
| 5 | stethoscope ↔ flashlight | header "is this healthy? ↗" / "watch this in flashlight ↗" | sibling URL preserving the target | "switch modes without changing focus" (the user's exact framing) |
| 6 | portal catalog → stethoscope, flashlight | per-row "diagnose ↗" + "tail ↗" buttons on `vendor.html`, `topics.html`, `progress.html` | sibling URL built from row state | "I'm browsing the registry — let me check what's actually working" |
| 7 | any peer-bearing surface → panoscope `/who` | per-row "Owner" cell | `panoscope/who?identity=<handle>` | "this peer is the one costing us / the one that is slow — who do I ask about it" (built 2026-08-17; first caller is the peerview demand + peers pages) |

What is **not** in this contract:
- Multi-target deep links (stethoscope verdicts referencing 3 vendors don't link to panoscope multi-select — defer).
- Saved-view sharing / preset libraries (URL is the share; that's the whole substrate).
- Portal inspector → panoscope (panoscope's inspector *is* its side panel; duplicative).
- Mutating cross-surface actions (every link is a read; mutations stay on portal admin).

## URL parameter canon

One vocabulary across all four surfaces. Where a surface already uses a different name, it keeps that name as a backwards-compatible alias *and* adds the canonical name as the preferred form.

| Param | Meaning | Surfaces that consume it | Notes |
|---|---|---|---|
| `key=<zenoh-key-expr>` | A topic key, possibly with Zenoh wildcards (`**`, `*`) | panoscope, flashlight, portal (`schema.html`, `inspector.html`) | The Zenoh-native term. Use over `topic=`. |
| `vendor=<vendor-tag>` | A vendor short-tag (matches portal's `vendor` field) | panoscope, flashlight, stethoscope, portal | Stethoscope already uses this. Portal's `vendor.html` currently uses `v=`; adds `vendor=` as preferred alias. **Anchor-aware on slot-pool goatnets:** receiving surfaces must match the tag against the key's first segment AND its anchor-stripped vendor segment (`<anchor>/<vendor>/…` per ADR 1025) — a first-segment-only interpretation goes silently empty, and these deep links propagate that blindness across surfaces. Shared rule: `panoscope_core::key_vendor_matches`. |
| `peer=<peer-id>` | A peer (Zenoh ZID or schema-decl display name) | panoscope (new) | New param. Stethoscope's `?vendor=` covers most diagnose-this-peer flows because most peer-level checks are vendor-scoped; `peer=` is panoscope-only for selecting one specific Zenoh participant. |
| `topic=<zenoh-key-expr>` | Stethoscope-specific alias for `key=` | stethoscope | Stethoscope's docs + design assets already use `topic=`. Both forms accepted; `key=` becomes the documented preferred form. |
| `system=<system-tag>` | A system (vendor sub-namespace) | stethoscope | Stethoscope-specific. Auto-detected from key shape per stethoscope picker. |
| `role=<role-class>` | A role class (sensor / effector / data-service / c2 / observer / sink) | flashlight | Flashlight-specific. Resolved server-side via panoscope-core peer registry. |
| `site=<site-tag>` | A site short-name | flashlight, panoscope + stethoscope (env-health) | Flashlight-specific historically; env-health scopes a domain reading to a site. |
| `identity=<handle>` | The identity answerable for something — a person's onboarding handle, or the custodian of a machine | panoscope `/who` (built) | Added 2026-08-17. The inbound half of "who owns this peer": any surface showing a peer links its owner to `panoscope/who?identity=<handle>`, first consumer [`peer-demand-surface.md`](peer-demand-surface.md). `handle=` is accepted as an alias. **It FILTERS, and selects only when the term picks out exactly one row** — the caller holds a handle while a roster row is keyed by its portal anchor, which on a slot-pool goatnet is a UUID and not the handle; a term matching nothing says so rather than showing a full roster that looks like the link failed. Distinct from `peer=`: that one selects a Zenoh participant, this one selects who answers for it. |
| `domain=<env-health-domain>` | An environment-health domain (`fabric` / `time` / future `gnss`/`power`/`thermal`) | panoscope (env band), stethoscope (env target), `goat health` | New (per [`environment-health-surface.md`](environment-health-surface.md) / [ADR 1036](../adr/1036-environment-health-surface.md)). A panoscope env-band chip → stethoscope `?domain=time&site=<s>`; a stethoscope env verdict → `goat health --domain time`. Preserves the *domain* being inspected the way `key=`/`vendor=` preserve a topic/vendor. |

**Encoding.** Every value is `encodeURIComponent`'d on construction. Zenoh key expressions contain `/` which must be percent-encoded when used as a query value (already the case in the panoscope→portal link).

**Ambiguity resolution.** When a surface receives both `vendor=` and `key=`, the more specific takes precedence (`key=` over `vendor=` over `peer=`). When a surface receives both `key=` and `topic=`, `key=` wins (the canonical name). When a surface receives a param it doesn't understand, it ignores it silently — the URL stays shareable across surfaces that handle different subsets.

## Implementation primitives

The contract reuses substrate the surfaces already have. Three small additions:

### 1. Per-surface sibling-base-URL config

Each surface's backend config gains a `siblings` block (or equivalent) carrying the base URLs of the other three. Panoscope already has `portal_schemas_url`; this extends the pattern symmetrically.

```rust
// example shape — panoscope config addition
pub struct SiblingUrls {
    pub portal: Option<String>,      // e.g. "https://portal.nova.netbird.nova.example"
    pub stethoscope: Option<String>,
    pub flashlight: Option<String>,
}
```

All fields are `Option<String>` — a surface deployed in isolation (no portal, no stethoscope yet) renders links conditionally. Already the panoscope contract for `portalBase`.

Wired into existing Ansible role defaults (`ops/ansible/roles/{panoscope,stethoscope,flashlight}/defaults/main.yml`) and portal config. Mesh-DNS names are predictable per-site (`*.nova.netbird.nova.example` on nova-a; `*.prod.netbird.goat.example` on prod) so the role default is the obvious value.

### 2. Meta endpoint exposing sibling URLs to the frontend

Each backend exposes the resolved sibling URLs on a meta endpoint. Panoscope's existing `/api/legend` is the precedent — extends to carry all three sibling URLs instead of just `portal_base_url`. Stethoscope and flashlight add equivalent endpoints (`/api/meta`).

Portal serves a static `<head>` `<meta>` tag or a small `/api/siblings` JSON so its server-rendered HTML pages can build links client-side without baking URLs into the template.

### 3. Outbound link builder

A tiny helper in each frontend that takes `(siblingsConfig, kind, value)` and returns a URL. Pseudo-shape:

```ts
function siblingUrl(
  siblings: SiblingUrls,
  target: "panoscope" | "stethoscope" | "flashlight" | "portal-schema",
  params: { key?: string; vendor?: string; peer?: string; topic?: string }
): string | null
```

Returns `null` when the target's base URL is unset (so the renderer can hide the link rather than emit a dead href). Lives per-surface initially — the contract is the URL shape, not the helper; surfaces converge on a shared helper later if the duplication becomes painful.

### 4. Inbound URL param handling (the gap)

The implementation gap by surface:

- **Panoscope** — On load, parses `?peer=` / `?vendor=` / `?key=` from `location.search` and pre-selects the matching node(s). `peer=` → that peer; `vendor=` → every peer carrying the tag; **`key=` → the topic's publisher peer(s)** — the real graph nodes whose side panel lists their publishes (incl. the linked topic), which is the useful "show me X in the fabric" landing. (The first cut resolved `key=` to a synthetic `topic:<key>` node, but those fold off the canvas and carry no side panel, so a topic deep-link silently landed on the default view — fixed 2026-06-17 to resolve via the snapshot's publisher→subscriber edges, exact-match on unfolded edges and namespace-prefix on server-folded singletons-super edges.) Resolution retries across snapshots until the target materializes. Reuses the existing selection substrate (`renderer.setSelection(...)`); URL state is one-way (URL → selection) at v1, no `pushState` on subsequent operator selection. (Reverse — selection writes URL — is a v2 sophistication.)
- **Stethoscope** — already handles `?vendor=` / `?topic=` / `?system=` via Phase B target picker. Adds `?key=` as alias for `?topic=` (one line in the parser).
- **Flashlight** — already handles `?key=` / `?vendor=` / `?role=` / `?site=`. No change.
- **Portal `vendor.html`** — already handles `?v=`. Adds `?vendor=` as alias.
- **Portal `schema.html`, `inspector.html`** — already handles `?key=`. No change.
- **Portal `vendor.html`, `topics.html`, `progress.html`** — add per-row "diagnose ↗" and "tail ↗" buttons (outbound, not inbound). Reads sibling URLs from the meta endpoint described above.

## Architectural through-line

```text
                            operator
                               │
                               ▼
                +-------------------------------+
                |   surface A (e.g. panoscope)  |
                |                               |
                |   /api/legend or /api/meta    |◀────────────────┐
                |   ────────────────            |                 │
                |   { panoscope_base, ... }     |                 │
                |                               |                 │
                |   frontend: siblingUrl(...)   |                 │
                |   → <a href="…?key=foo">      |                 │
                +---------------+---------------+                 │
                                │                                 │
                                │ MPA navigation                  │
                                ▼                                 │
                +---------------+---------------+                 │
                |   surface B (e.g. flashlight) |                 │
                |                               |                 │
                |   on-load: parse ?key= / ?…   |                 │
                |   → narrow subscription /     |                 │
                |     pre-select target         |                 │
                |                               |                 │
                |   Ansible role default        |                 │
                |   → siblings.{panoscope,…}    |─────────────────┘
                +-------------------------------+
```

No runtime coupling. Surfaces don't call each other's APIs. Each surface reads its sibling URLs from its own deploy-time config (Ansible role default) and emits links the operator clicks. The links are the only contract.

## Phased rollout

Three small phases. Each ships as one PR with the doc-drift refresh in the same change-set per documentation discipline.

### Phase A — Planning (this PR)

- `docs/design/zenoh-surface-deep-links.md` (this doc, new).
- `docs/project/in-flight.md` Block 94 entry (new).
- `CLAUDE.md` pointer added under the **Inspection surfaces** bullet.
- `docs/project/active-work/tracks/zenoh-surface-links.yaml` (this track).

Gate to Phase B: operator review + approval of the contract + param canon.

### Phase B — Panoscope inbound + meta endpoint expansion

Mutates panoscope only. Adds:
- `SiblingUrls` config block + Ansible role defaults for the three sibling URLs.
- `/api/legend` returns the full sibling set (preserving the existing `portal_base_url` field for backwards compat).
- Frontend `URLSearchParams` on-load → pre-selects `?peer=` / `?vendor=` / `?key=` via the existing selection substrate.
- Outbound `siblingUrl()` helper (TypeScript) — pure addition.
- Side-panel `diagnose ↗` + `tail ↗` buttons on every selected peer + cluster-hull + topic row, plus the existing portal `↗` link generalized through the same helper.

Behavior-preserving for existing portal hop; new buttons appear when sibling URLs are configured.

Gate to Phase C: live-fire on nova-a — operator clicks a panoscope `diagnose ↗`, lands on stethoscope; clicks `tail ↗`, lands on flashlight; both with the target preserved.

### Phase C — Stethoscope, flashlight, portal symmetric wiring

Three small parallel surface mutations:
- **Stethoscope** — `?key=` alias accepted in the target picker; header `see this in panoscope ↗` + `watch this in flashlight ↗` buttons appear next to the brand link; `/api/meta` exposes sibling URLs (consumed by the JS for the new header buttons).
- **Flashlight** — header `is this healthy? ↗` (stethoscope) link, alongside the existing `open in panoscope ↗`. Per-row event "tail this exact key ↗" stays in flashlight; per-row "diagnose ↗" links out to stethoscope. `/api/meta` exposes sibling URLs.
- **Portal** — `vendor=` alias on `vendor.html`; per-row `diagnose ↗` + `tail ↗` buttons on `vendor.html`, `topics.html`, `progress.html`. Reads sibling URLs from a new `/api/siblings` (or a `<meta>` tag in the template) populated by the portal's existing config substrate.

Gate to canon: all six hops in the table above work end-to-end against the nova-a deployment.

## What's NOT in v1

Explicit non-scope to preserve the "URL-only, no runtime coupling" property:

- **SPA history pushing.** Operator selections inside a surface don't update the URL. Reload reverts to the inbound state (or the default if none). v2 may add `history.pushState` for shareable per-selection URLs.
- **Multi-target deep links.** A stethoscope verdict touching 3 vendors doesn't construct a panoscope multi-select URL. Single target per hop in v1.
- **Cross-surface authentication / session sharing.** Each surface authenticates independently via its existing mesh-only allowlist. Operator may need to OAuth into one before the other on first session.
- **Shared TypeScript link-builder package.** Each frontend implements `siblingUrl()` independently in v1. If the duplication becomes painful (third repeated bug, fourth deferred-param case), promote to a tiny shared package — but not before.
- **Backend-to-backend cross-surface API calls.** Stethoscope already pulls from panoscope's `/api/snapshot`; that's an existing read relationship, not a deep-link concern. This contract is exclusively about *operator navigation*.
- **`peer=` on surfaces other than panoscope.** Stethoscope and flashlight think in vendor / topic / key terms; a peer-level link from them is a v2 conversation.

## Open questions

Recorded so they're addressed before Phase B lands.

- **Sibling-URL discovery vs static config.** v1 hardcodes sibling URLs in each surface's Ansible role default. Sites with all four surfaces use the same DNS pattern (`*.<site>.netbird.<zone>`) so the defaults are predictable, but a misnamed sibling produces a dead link. A shared `sites.yml` group_var with one row per surface would centralize. Probably worth doing if Phase C surfaces it as a real footgun.
- **Portal `vendor.html` `v=` rename schedule.** `v=` stays as a permanent alias by default. If portal is the only surface with the inconsistency long-term, a follow-on PR could deprecate `v=` after a quarter — but the cost of keeping both is one line in the parser, so probably never worth removing.
- **Stethoscope `topic=` rename schedule.** Same as `v=` above. `topic=` is in stethoscope's wireframe-bundle URLs and operator muscle memory; `key=` is the canonical form. Both stay accepted indefinitely.
- **Cross-site links.** A nova-a panoscope side-panel today can only link to nova-a's other surfaces (Ansible-role defaults are nova-a-scoped). A cross-site operator who wants to diagnose a topic that exists on multiple sites navigates by hand. Multi-site link composition is a separate Block 84 / partner-peering-portal conversation.

## Living document

Updates land via PR. The first material revision target is when Phase B ships and an operator actually hops panoscope → stethoscope on a real diagnosis flow.
