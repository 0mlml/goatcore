# Slot alias display layer — human names over UUID anchors

| | |
|---|---|
| Status | Design (authored 2026-06-10). [ADR 1039](../adr/1039-slot-alias-display-layer.md). Designed-not-built. |
| Problem | The per-uuid slot anchor ([ADR 1025](../adr/1025-zenoh-dynamic-tenant-authz-slot-pool.md)) — e.g. `e7a9c75b532e3d434bb09fefd3ce1ec7` — is the right machine identity but is unreadable and clutters every human surface (`goat`, panoscope, stethoscope, portal). Humans need short, meaningful names; machines still need the UUID. |
| Invariant | The UUID stays the **sole canonical key** in every machine path (ACL rule, cert CN, wire topic key, storage key, audit record). The alias is a **presentation projection that is never written anywhere load-bearing.** |

## 1. The problem, precisely

[ADR 1025](../adr/1025-zenoh-dynamic-tenant-authz-slot-pool.md) makes one 32-hex UUID the **stable identity in three places at once**: the **cert CN**, the **Zenoh ACL slot rule** (`<uuid>/**`), and the **enforced write anchor** under which the whole taxonomy nests (`<uuid>/<class>/<site>/<id>/…`). That tri-binding is deliberate and correct — it is why a slot is exactly one predictable prefix. It is also irreducible: the UUID cannot be *replaced* with a friendly name without re-introducing the forgeable-namespace and prefix-prediction problems ADR 1025 closed. So the only safe move is to **project over** it, not swap it.

The cost today is purely human-factors: operators copy/paste 32-hex strings, can't eyeball "whose topic is this?", and every visualization is a wall of hex. panoscope already mitigates with lossy **truncation** (`e7a9c75b…`), which collides (two slots can share a prefix) and still carries no meaning.

The portal **already holds the raw material**: every slot mint records `SlotID` (the UUID), `SystemHandle` (a human handle, e.g. `alpha-operator`), and `DisplayName` (free-text title). The mapping exists; it is just unconstrained and unsurfaced.

## 2. Vocabulary

Define these before the architecture; the rest of the doc uses them exactly.

- **Slot anchor / UUID** — the 32-hex slot identity from ADR 1025. Cert CN, ACL slot, write-namespace prefix. **Canonical. Immutable. Machine-only.**
- **Slot alias** — a short, unique, human-meaningful name bound to exactly one UUID, e.g. `alpha-operator`. **Presentation only. Mutable. Never load-bearing.** Formalized from the portal's existing `system_handle`.
- **Display title** — the portal's free-text `display_name` (may have spaces/caps: "Alpha — Operator"). Cosmetic label for detail views; **not** used for resolution. Distinct from the alias.

A slot has exactly one UUID, exactly one alias, and an optional title. UUID ⇄ alias is **bijective** within a goatnet.

## 3. The invariant (why this isn't a fragility factor)

> **The alias is never written into any machine path. The UUID is the only thing the router, ACL, cert, storage, and audit ever see.**

Every fragility risk in a name-over-ID system traces to one root: the friendly name leaking into a place that load-bears. Close that one root and the rest follows. The codebase already runs this exact pattern twice — "English is the arbiter, `--json` stays English" ([goat-cli-i18n](goat-cli-i18n.md)) and "class is bound to identity, not a forgeable DSCP byte" ([zenoh-qos-and-prioritization](zenoh-qos-and-prioritization.md)). Same shape: canonical underneath, friendly on top, a hard rule that the top never leaks down.

Concretely, the alias appears **only**:
- on input boundaries, where it is immediately resolved to a UUID *before* any wire/ACL/storage action, and
- on output boundaries, where a UUID is rendered as its alias *after* the canonical value is already settled.

It never appears in: a Zenoh key on the wire, an ACL rule, a cert, rocksdb/MinIO storage keys, the audit log record, or a `--json` field.

## 4. Architecture

Two components and two boundaries.

### 4.1 Alias registry — portal is the source of truth

The portal mints slots, so it owns the mapping. Promote `system_handle` into a **constrained, unique alias**:

- **Unique per goatnet** — a DB unique index on `(system_handle)`. One alias ⇄ one UUID. Collision is rejected at mint/rename time, not discovered later.
- **Shape-disjoint from UUIDs** — charset `[a-z0-9][a-z0-9-]{1,62}`, and **must not be parseable as a 32-hex UUID**, and **must not contain `/`**. This is load-bearing for the resolver (§4.2): it lets any token be classified as alias-or-UUID by shape alone, deterministically, and stops an alias from ever masquerading as a namespace segment.
- **Reserved-word guarded** — cannot equal a top-level namespace class: `ops`, `volatile`, `release`, `community`, `loadtest`, `system` (deprecated), or any ADR 0844 root. Extends the portal's existing `ValidVendorPrefix`.
- **Goatnet-local** — `alpha-operator` on ZAGROS ≠ `alpha-operator` on nova-a. The alias is *memorable + unique-within-goatnet*, not globally unique (Zooko's triangle: the UUID is the secure+global leg, the alias is the memorable+local leg). Cross-goatnet / federated references ([ADR 1020](../adr/1020-zenoh-federated-peering-trust-model.md)) carry the **UUID** and render with the *local* alias if known, else the UUID. **Aliases do not travel; UUIDs do.**

Surfaced as a small, cacheable read endpoint — `GET /api/aliases` returning `[{slot_id, system_handle, display_name}]` (or fold the two new fields into `GET /api/vendors`). Operator-gated like the rest of the registry read API; flat-readable within the goatnet if community-read is on (it reveals only names, not access).

### 4.2 Resolver — shared, in `panoscope-core`

One resolver, reused by `goat`, panoscope, stethoscope, and the portal web UI so they cannot diverge (same lesson as the [data-access-cop](data-access-cop.md) shared graph). Two pure functions over a cached map:

```
resolve_input(token) -> Result<Uuid>      // INPUT boundary
render(uuid) -> Display                    // OUTPUT boundary
```

- **`resolve_input`** — UUID-shaped token → use verbatim. Otherwise treat as an alias and look it up. **Unknown alias → fail closed** with a real error (`no slot aliased 'foo'` + nearest matches), *never* a silent passthrough. Silent passthrough is the trap: a typo'd alias would become a phantom literal prefix that the router silently denies — recreating F-177 one layer up. Fail-closed at the boundary is the whole point of having the resolver.
- **Reserved fabric roots pass through.** `ops/`, `attest/` and `system/` are namespaces, not identities: the portal's `ValidVendorPrefix` (`ops/goat-zenoh-portal/internal/registry/registry.go`) refuses to register any of them as an org prefix ([ADR 0844](../adr/0844-zenoh-deployment-topology-and-acl-v1.md) / [ADR 1035](../adr/1035-operator-namespace-ops-canonical.md)), so one can never become a phantom write prefix and the fail-closed rule has nothing to protect. Before this, every `ops/<domain>/health/**` key on a slot-pool goatnet died on `no slot aliased 'ops'`, and the commands printed in the health runbooks only worked with a `--literal` none of them carried. The alias lookup runs first, so a goatnet that does carry an `ops` alias is unaffected — the pass-through is additive. Same class of exemption as the wildcard-segment pass-through: read-only matching, no write prefix at risk.
- **`render`** — alias if known; truncated UUID (`e7a9c75b…`) as graceful fallback if not; `alpha-operator (e7a9c75b…)` in a "both" mode for disambiguation/audit views.
- **Key-path rewriting** — for a key expression `alias/sub/topic`, `resolve_input` rewrites only the **leading segment** (`alias` → `uuid`), never the trailing taxonomy (`sub/topic` is ADR 0844-descriptive and stays verbatim). The wire key is always `uuid/sub/topic`. Reverse on render.

#### 4.2.1 `Labeled` — the canonical output envelope

When a backend serializes an identity to a UI (JSON to a browser), it ships the coupled pair, computed **once** at the backend boundary, so the frontend never resolves — it just renders `alias ?? id`. This is the one output shape every serializing surface uses instead of inventing per-surface fields (the consolidation the [2026-06-11 audit](#) flagged):

```
struct Labeled { id: String, alias: Option<String> }   // alias skipped on the wire when None
AliasRegistry::label(uuid)     -> Labeled   // a bare ANCHOR identity (peer/slot → name)
AliasRegistry::label_key(key)  -> Labeled   // a full KEY (<anchor>/taxonomy → projected key)
Labeled::display()             -> String    // alias ?? truncate(id), for terminal/server callers
```

`id` is always the canonical UUID (machine key, deep-link, copy target — never dropped, §3); `alias` is the operator-facing name/projected-key, `None` when nothing projects (random ZID, unknown anchor, portal-down). Two constructors because identities come in two shapes — a bare anchor (`label`, used for a node/peer) and a full key expression (`label_key`, used for a topic, where the leading anchor projects but the taxonomy stays verbatim). Consumers render `alias ?? id`; a UUID-only payload is the natural degraded shape. panoscope's `NodeView.slot_alias` is the flattened equivalent (the load-bearing `peer_id` field predates the envelope and stays flat to avoid churning every frontend reference); new serializers (stethoscope `/api/index`) use `Labeled` directly.

### 4.3 Resilience — aliases never block operation

Surfaces fetch the alias map from the portal and **cache** it (TTL + explicit refresh). If the portal is unreachable — the denied edge, an island, a cold start — or the goatnet is per-vendor (no slot pool, so no aliases exist), the registry is **empty**, and the resolver **degrades to truncated-UUID display and passes input through verbatim** (an empty registry has no basis to reject a token — it may be a literal key like `org11x/**`; fail-closed applies only when aliases *do* exist, §4.2). The fabric still runs; aliases are an enhancement, never a dependency. This preserves the "run itself at the edge" property, keeps the resolver a no-op on per-vendor goatnets, and means the alias layer can never be a new outage cause.

### 4.4 Honesty — a degraded registry must say so

§4.3 makes an empty registry safe. It does not make it **legible**, and that
gap has its own cost. Four very different situations render identically as a
screen full of truncated UUIDs:

1. no API key was ever wired into the surface, so it never polls at all;
2. the key is wired but the portal has never answered;
3. the portal answered before and the latest poll failed;
4. the portal answered and this goatnet genuinely assigns no aliases.

Only (4) is a normal steady state. (1) is a deploy gap that will never
self-heal on its own, and it is the one that actually bit: zagros-prod's
panoscope was deployed before its `panoscope_portal_api_key` group_var
existed — and eight hours before its portal came up — so it ran a full day
showing raw UUIDs while its portal held every alias, with nothing on screen
and nothing above `debug!` in the log to say why (2026-08-05).

So each surface carries an **alias-registry health** alongside the registry:

| `state` | Meaning | Operator action |
|---|---|---|
| `disabled` | No portal API key configured; the refresher never polls | Wire `<surface>_portal_api_key` in group_vars, re-apply the role |
| `unreached` | Configured, but no poll has ever succeeded | Check portal reachability + that it holds the key being presented |
| `stale` | Succeeded before, latest poll failed; registry is last-good | Usually self-heals on the next tick; check the portal if it persists |
| `ok` | Current. `aliases: 0` here genuinely means none are assigned | None |

Two rules keep this honest rather than decorative:

- **`ok` with zero aliases is a real answer, and the only one that means
  "none assigned."** Zero under any other state says nothing about the
  goatnet. A surface must never collapse the state into the count.
- **`disabled` is loud once, not silent and not every tick.** It is a
  misconfiguration, so it warns on the first poll with the group_var to set;
  subsequent ticks drop to `debug!` so a long-running surface doesn't spam.
  The other failure states already warned per-occurrence and still do.

Implemented in `panoscope-core::alias::{AliasRegistryHealth,
AliasRegistryState}` so every surface reports one shape. Panoscope serves it
on `GET /api/legend` as `alias_registry` and renders it in the legend strip
next to the schema resolver; the cell goes amber with the reason and the fix
in its tooltip whenever `state != ok`. Stethoscope and flashlight hold the
same registry and can adopt the same field without a new contract.

## 5. Fragility factors — each named and closed

| Risk | Closed by |
|---|---|
| Alias leaks into ACL / cert / wire key / storage / audit | §3 invariant + shape rule (no `/`, not UUID-shaped) + `--json`/audit always-UUID |
| "Is this token an alias or a UUID?" ambiguity | Shape-disjointness (§4.1) — deterministic, no heuristic |
| Typo'd alias silently denied | Fail-closed `resolve_input` (§4.2) |
| Two slots claim the same alias | DB unique index, enforced at mint/rename (§4.1) |
| Portal down → surfaces break | Degrade to UUID display + UUID-only input (§4.3) |
| Degraded display looks identical to "no aliases exist" | Per-surface registry health with four distinct states, served on the surface's API and rendered in its status strip (§4.4) |
| Audit / replay integrity | Store UUID only; alias is a render-time join (§6) |
| Rename churn breaking ACLs/certs/topics | **None** — alias is non-load-bearing, so rename is a projection update with zero machine-path impact (§6). This is the payoff of the invariant. |
| Cross-goatnet reference shows wrong/missing alias | Aliases are goatnet-local; federated refs carry the UUID, render with local alias or fall back to UUID (§4.1) |

## 6. Mutability and audit

Because the alias never load-bears, **renaming a slot's alias is cheap and safe** — it updates one row in the registry and zero ACL rules, certs, topics, or stored keys. This is the property that makes aliases usable for humans (they *will* want to rename).

Audit integrity is preserved by storing the **UUID only** in every audit record. Historical logs render the **current** alias via a render-time join, with a `(was: <prior>)` affordance when a rename has occurred (the registry keeps rename history). Snapshot-at-event rendering is deliberately *not* used — it would require writing the alias into the record, violating §3. Decision recorded in [ADR 1039](../adr/1039-slot-alias-display-layer.md).

## 7. Surfaces

- **`goat` CLI** — accept an alias anywhere a slot/prefix/key is taken (`goat doctor --topic alpha-operator/hello`, `goat pub`, `goat sub`, `goat access`); render incoming `<uuid>/…` keys as `<alias>/…`. `--json` **always** carries the UUID (machine contract). `--uuid` / `--raw` flag forces canonical display. This is the primary ergonomic win.
- **panoscope** — BUILT (§9.4): nodes labeled by `slot_alias`, UUID canonical in `peer_id` + copy/deep-link.
- **stethoscope** — BUILT: the diagnose target is alias-aware end-to-end — `?topic=alpha-operator/obs/v1` resolves to the canonical anchor before checks run (`canonicalize_target`, fail-closed on unknown alias, same contract as `goat pub`); the hero/title/picker render the alias-projected key while the SSE diagnose URL + Block-94 sibling deep-links keep the canonical `key=` UUID; and `/api/index` ships topics as the `Labeled` envelope so the picker typeahead shows friendly names and matches the needle against alias OR UUID. Vendor/system targets are a different namespace and pass through untouched. **Deferred:** alias-rendering identity strings *inside* individual check findings (the target + picker are the high-value surfaces; finding bodies are a follow-on).
- **portal web** — show alias as the primary label, UUID + title in detail; the alias edit/rename surface (operator-gated per §8).
- **audit / `--json` / OTLP** — UUID only on the wire; alias is a viewer-side join (§6).

## 8. Open decisions (ADR 1039)

1. **Canonical alias field** — `system_handle` is the strict unique alias; `display_name` stays free-text title. (Recommended; folds cleanly onto the existing portal model.)
2. **Who renames** — operator-only vs. self-service for a slot's own alias. (Mint already sets `system_handle`; rename authority is the open question — likely operator-only at sandbox stage, matching `topic` write authority.)
3. **Audit rendering** — current-alias-with-`(was: X)` (recommended, §6) vs. snapshot (rejected — violates §3).

## 9. Build increments

Independently shippable, each its own PR; UUID-only behavior is the safe default at every step, so partial rollout never breaks anything.

1. **Portal — BUILT.** `system_handle`/alias model (`internal/registry/slots.go`, `ValidAlias`, `AllocateSlot`, `ListAliases`) + `GET /api/aliases` (`internal/api/server.go` → `handleListAliases`, `internal/api/handlers.go`), alias↔slot uniqueness enforced in the tx.
2. **`panoscope-core` — BUILT.** `AliasRegistry` (`crates/panoscope-core/src/alias.rs`): `resolve_input` / `resolve_key` / `render` / `render_key` / `render_both`, the 32-hex shape classifier (`is_anchor_shaped`), `alias_of(uuid) -> Option`, fail-closed-on-known-set semantics, empty-registry passthrough; plus the `Labeled { id, alias }` output envelope (§4.2.1) with `label` (anchor) / `label_key` (key) constructors + `display()`; 12 unit tests, no live portal needed. Portal client `list_aliases()` in `portal.rs`.
3. **`goat` CLI — BUILT.** `resolve_key` on input (`cmd/pubsub.rs`, `cmd/doctor.rs`), `render_key` on output (`goat-cli/src/alias.rs`); `--json` stays UUID.
4. **panoscope — BUILT (this track, `panoscope-alias-resolver`); deploy/live-fire (increment 6) remains.** Two halves, §11. Status: 4a (render) BUILT; 4b(i) (publisher ZID-pin) BUILT; 4b(ii) (anchor-aware peer derivation) BUILT; 4c (write-root fallback, F-278) BUILT.
   - **4a. Render aliases — BUILT.** Backend: `SharedState.aliases: Arc<RwLock<AliasRegistry>>` (`src/zenoh_client.rs`) refreshed by `main.rs::spawn_alias_refresher` polling `PortalClient::list_aliases()` on the schema cadence — a failed/disabled poll keeps the prior registry (a portal blip never blanks names back to UUIDs). `NodeView` (`src/snapshot_view.rs`) gains one display field, `slot_alias: Option<String>`, set in `build_nodes_view` via the new `AliasRegistry::alias_of(uuid) -> Option<String>` (Some only for a known 32-hex anchor; `None` for a random ZID / unknown anchor / empty registry, so the existing `display_name`/truncated-UUID path is reached unchanged). The raw UUID stays the canonical `peer_id` per §3. Frontend: `slot_alias` outranks `display_name` in both the canvas `nodeLabel` (`render.ts`) and the side-panel title (`main.ts`), and the detail panel shows a `slot alias` row above the canonical `peer_id` row (the `render_both` shape: human name + full UUID for copy/deep-link). **Not done in 4a — alias-projecting the `publishes` topic list:** each publish's leading segment is the node's *own* anchor, already shown as the node label, and the publish strings are load-bearing click/deep-link keys (`topic:<uuid>/…` selection targets, canonical-UUID per §3) — projecting them to aliases would desync the drill-in for low marginal value, so `publishes` stays UUID-anchored. Topic-*node* title aliasing is also deferred (the frontend synthesizes topic nodes from edges and has no registry; it would need either the registry shipped to the browser or backend-side topic-node synthesis).
   - **4b. Make the peer identity *be* the anchor** — so the rendered alias is the peer's identity, not a second hex blob beside it. Two sub-changes: (i) **publisher ZID-pin — BUILT.** `cmd/pubsub.rs::open_zenoh_session` reads the leaf cert's Subject CN via `slot_anchor_from_cert` and, **only when it is `is_anchor_shaped`** (a 32-hex slot-pool anchor = cert CN = `sha256(salt:slot_idx)[:32]`, exactly 16 bytes / the ZID ceiling), pins the Zenoh session `id` to it (`config.insert_json5("id", …)`), so the transport ZID, the ACL slot, the namespace anchor, and panoscope's node id are one UUID end-to-end — and the node identity is *stable across restarts* (the bigger win) and renders as the alias. Self-gating on shape means legacy/shared certs (nova-a `CN=<vendor>-trial-peer`, the synth fabric's shared cert) are **not** anchor-shaped → keep a random ZID (pre-4b behavior, no collision); a cert parse failure also skips the pin (fail-open — a cosmetic pin never blocks the session). Applies to `goat pub` only — `goat sub` and the tail lens deliberately do NOT pin: zenoh permits one transport per ZID, so a later same-cert pub displaced a pinned live subscriber silently; a pure subscriber earns no provenance from the pin, so it keeps a random ZID (`open_session_readonly`), trading subscriber-census alias attribution for never starving a live subscriber. The ZID is not an authz handle (§10), so this is display/transport plumbing only. (ii) **anchor-aware peer derivation — BUILT.** panoscope's `peer_id_from_topic` (`src/zenoh_client.rs`) returns the bare leading segment when it `is_anchor_shaped` (the 32-hex anchor is the whole identity under the slot-pool model), instead of the legacy `<vendor>/<system>` pair, so the ZID path and the topic-fallback path converge on the same id (no split-brain node); legacy non-anchor topics keep the pair.
   - **4c. Write-root alias fallback — BUILT (F-278, 2026-08-02).** The 4b convergence assumed publishers pin ZID=anchor; live ZAGROS showed almost none do (57 peers, 1 alias hit, while the registry held 141 rows) — a publisher attaching SourceInfo with a random session ZID defeats the `alias_of(peer_id)` join even though the aliased anchor is the leading segment of every key it publishes. `write_root_alias` in `build_nodes_view` (`src/snapshot_view.rs`): when the peer_id lookup misses and the publisher's `publishes` share **exactly one** anchor-shaped root, the node takes that slot's alias. Sound because `<uuid>/**` roots are single-owner (ADR 1105 — co-publish lives under `shared/…`, never anchor-shaped); two-plus distinct roots stay hex (ambiguous). Subscribers deliberately stay hex — subscription-root attribution is unsound, and F-273 randomizes subscriber ZIDs on purpose. Display-only per §3; peer_id stays the canonical ZID.
   - **Synth caveat (does not apply to real publishers):** ZAGROS's synth fabric collapses many peers onto one cert + shared `slot_idx` (`group_vars/zagros_hosts/synth-vendors.yml`), so 4b(i) can't pin a unique ZID per synth peer (collision); synth peers keep random ZIDs and rely on 4a alias-render of their *shared* anchor (`notional`). Real publishers (one cert = one slot = one peer) get the full 1:1 unification.
5. **stethoscope — BUILT (this track).** Diagnose target alias-aware end-to-end (`crates/stethoscope/src/routes.rs`): `canonicalize_target` resolves a topic-target alias to the canonical anchor before checks (fail-closed on unknown, `goat pub` contract), the hero/title/picker render the alias-projected key while the SSE URL + Block-94 deep-links keep canonical `key=` UUID, and `/api/index` ships topics as the `Labeled` envelope (the second real serializer of the shape) so the picker typeahead shows + matches friendly names. Registry loaded per-request (mirrors `goat` CLI). Vendor/system targets untouched. 4 new unit tests. **Deferred:** alias-rendering identities inside check-finding bodies.
6. **Deploy + live-fire on ZAGROS — DONE (2026-06-11, M-058).** panoscope + stethoscope deployed `sha-97a6a01`; a real `goat pub` publisher (cert CN = `alpha-operator` anchor `e7a9c75b…`) rendered in panoscope with `peer_id` == its anchor + `slot_alias` `alpha-operator`; the 25 synth nodes correctly kept random ZIDs / no false alias; stethoscope `/api/index` shipped the `Labeled` envelope (43 topics projected) and `?topic=alpha-operator/…` resolved + rendered the alias in the hero. The whole layer is built + verified in prod.

## 10. ZID is not an authz handle (why 4b is display-plumbing, not a security change)

The Zenoh **ZID** is a transport/session identifier minted by `zenoh::open` (random unless `config.id` is set). It is **not** an ACL subject — Zenoh's `access_control` keys on cert common-name, never on ZID — so pinning it changes *nothing* about authorization; it only makes the displayed/transport identity equal the already-enforced anchor. The ZID's real jobs are routing, the admin namespace (`@/router/<zid>`, `@/peer/<zid>`), and the HLC timestamp source-id. Leaving it random was never wrong for security; it just leaked a meaningless hex into the COP where the meaningful anchor belongs.

## 11. Cross-links

[ADR 1025](../adr/1025-zenoh-dynamic-tenant-authz-slot-pool.md) (the UUID anchor this projects over) · [ADR 0844](../adr/0844-zenoh-deployment-topology-and-acl-v1.md) (the taxonomy nesting below the anchor) · [data-access-cop.md](data-access-cop.md) (shared resolver pattern; the access picture renders aliases) · [zenoh-surface-deep-links.md](zenoh-surface-deep-links.md) (deep links carry UUIDs, render aliases) · goat-cli.md (the CLI surface) · F-177 (the silent-denial trap the fail-closed resolver avoids re-creating).
