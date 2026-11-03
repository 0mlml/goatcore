# `goat who` — the identity common operating picture

**BLUF.** `goat who` is the **unified cross-layer identity common operating
picture**. Where [`goat top`](goat-top.md) answers "what is flowing and who is
loudest," `goat who` answers, for any entity, "who is this, what may it do across
**every identity layer** — NetBird mesh → Ory → SPIRE/SPIFFE → Zenoh — and what
is it doing with that access." It is a **searchable master-detail roster
browser**: a left-pane list of identities (declared ⊕ live) you filter and
navigate, and a detail pane that rolls the selected entity's cross-layer
permissions + activity into one view. The DATA layer distinguishes **read grants**
(may-read, ACL allow-list) from **live subscriptions** (is-reading, observed from
the fabric via the shared pub/sub census —
[`subscriber-consumption-visibility.md`](subscriber-consumption-visibility.md),
`Census::subscriptions_for_anchor`), so capability is never confused with actual
use; empty renders nothing (absence of an observation is not proof of no
subscription), never faked. Pure consumer + read-only portal queries —
it reserves nothing, enforces nothing, mints nothing.
([ADR 1047](../adr/1047-goat-who-identity-cop.md))

> **The bar (the operator's framing).** *"`who` is a quick view and
> drill-downable tool to see who has active permissions, what they are, and what
> they are being used for … able to look across permission levels of NetBird →
> Ory → SPIRE/SPIFFE → Zenoh and see role/permissions and last activity across
> them for any given entity."* The 0.4.9 rework (below) makes the roster deliver
> this: permissions are real and **instant** for the whole fleet (the W/R `0/0`
> bug and the per-row fetch bottleneck are fixed), the `STATE` column carries a
> **meaningful cross-layer status** instead of a uniform "active," and the
> roster is **activity-first** while staying searchable.

> **Web-native equivalent (2026-07-09, track `web-top-who`):** **panoscope `/who`** renders the
> same roster + cross-layer STATE in the browser for viewers without goat-cli. Gate =
> identity-administration groups (`PANOSCOPE_RBAC_WHO_GROUPS`, default `ADMINS,IAM-ADMIN`);
> a `data.view.all` grant deliberately does not admit it. The STATE derivation is mirrored
> server-side from this doc's rules. See `panoscope.md` §"Web-native `top` +
> `who` pages". The terminal `goat who` is unchanged.
>
> **Inbound deep link (2026-08-17):** `/who?identity=<handle>` (alias `?handle=`) — hop 7 of
> [`zenoh-surface-deep-links.md`](zenoh-surface-deep-links.md). It makes `/who` the landing
> place for "who answers for this peer" from any surface that shows peers; the first caller is
> the peerview demand + peers pages ([`peer-demand-surface.md`](peer-demand-surface.md)). The
> param **filters** the roster and selects only when the term picks out exactly one row — the
> caller holds a handle, while a roster row is keyed by its portal anchor, which on a slot-pool
> goatnet is a UUID and not the handle. A term matching nothing says so rather than leaving a
> full roster on screen that looks like the link failed.

## Why a new surface — and why master-detail, not a leaderboard

We already had three identity-adjacent surfaces, none of which is the admin's
"who is here" glance:

- `goat access who <id>` — the *single-identity* scriptable
  query: one id in, its write/read permissions out. No roster, no navigation,
  no live usage.
- `goat presence` — who is *alive* on the bus (liveliness), but
  nothing about permissions or registry membership.
- [`goat top`](goat-top.md) — a *usage leaderboard* keyed by producer, ranked by
  rate. Identities appear only if they are producing, and only as a row of
  numbers.

The missing surface is the **roster as the spine**: every identity (declared in
the registry *or* observed live), each decorated with its capability and its
behavior. The user's framing was explicit — it must lean **searchable**, not
usage-ranked. A `top`-style leaderboard is the wrong default here: an admin
asking "who is in my system" wants a stable, alphabetized, filterable index they
can scan and drill into, not a list that reshuffles as traffic ebbs.

The precedent class is the **resource browser** (k9s, lazygit, `htop` in tree
mode): a filterable list of objects (master) beside a "describe" pane for the
selected object (detail). `goat top` is a dashboard; `goat who` is a browser.
They are deliberately different idioms.

| | `goat top` | `goat who` |
|---|---|---|
| Question | what is flowing? | who is here / what can they do / what are they doing? |
| Spine | producer footprints (live only) | identity roster (declared ⊕ live) |
| Default order | usage rank (msgs/s) | **name** (searchable-first) |
| Detail | one shared class-load + totals | per-identity capability + activity pane |
| Idiom | dashboard / leaderboard | browser / picker |

## The three questions, and where each answer comes from

Everything `goat who` shows is **already built and queryable today** — it is a
new *composition*, not new substrate.

1. **Who is in the system** — the identity universe is the union of:
   - *Declared* — portal `GET /api/vendors` (write-anchored actors) ⊕
     `GET /api/aliases` (the [slot-alias](slot-alias-display-layer.md) human
     names). Loaded once at open.
   - *Observed* — every anchor seen live on the bus this session: a
     `ProducerFootprint`'s `peer` and a presence token's `write_root`. An actor
     producing traffic or holding a liveliness token but **absent from the
     registry** still appears, flagged `live, not in registry` — the same
     declared-⊕-live honesty as [`goat ui`](goat-cli-tui.md).

2. **What permissions they have** — two reads against the
   [Data-Access COP](data-access-cop.md), at two altitudes:
   - **The whole roster, instantly** — portal `GET /api/access/roster` (0.4.9):
     a slim cross-layer summary (W/R grant counts + reconciled `state` +
     on-mesh / last-activity) for *every* identity, assembled server-side in
     ONE call. This is what fills the `W/R` and `STATE` columns the moment the
     roster loads — no per-row round-trip. It is the responsiveness fix: the
     0.4.8 build fetched `access who` lazily *per selected row*, so each scroll
     stop paid a ~9 s portal round-trip on the lossy ZAGROS mesh.
   - **One identity, in full** — portal `GET /api/access/who?id=<anchor>`: the
     per-layer `writes[]` / `reads[]` with the deciding layer + reason, fetched
     **lazily on selection** for the detail pane's reason lists. The roster
     summary already shows that row's counts + status instantly; this enriches
     the *selected* row with the why.

   **The W/R `0/0` fix (0.4.9, portal-side).** Before this, the access graph
   computed an identity's writes only from its *org's* rendered may-write edges
   and never emitted a slot-pool member's own `<anchor>/**` grant — so on a
   slot-pool goatnet (ZAGROS, where nearly every actor is a slot member) the whole
   roster reported zero writes, and reads showed `0` because the flat
   community-read model was a graph *flag* that the forward query never surfaced.
   The graph now emits the `<anchor>/**` namespace write grant for any spine with
   a write anchor (the pre-baked per-uuid grant, [ADR 1025](../adr/1025-zenoh-dynamic-tenant-authz-slot-pool.md))
   and reflects community-read as a `**` read grant. `W/R` is meaningful for the
   whole fleet — and a `0/0` row now genuinely means "onboarded but can write +
   read nothing," the F-177 silent-denial surface, flagged `no-grants` in `STATE`.

3. **How they are using them** — joined live from the bus by anchor:
   - presence (`ops/presence/health/<site>`) — alive / dropped, since, drop
     count;
   - footprint (`ops/fabric/footprint/<peer>`) — msgs/s, bytes/s, per-class
     shares, near-limit, and the per-actor [throughput-envelope](qos-throughput-envelopes.md)
     BAND verdict (the same `EnvelopeRegistry` `goat top` uses);
   - mesh reachability — the bulk roster carries each entity's NetBird
     `connected` + `last_seen` (the mgmt API's, polled server-side); the
     cross-layer `STATE` uses `connected`/`on_mesh` to distinguish an entity
     *connected now* from one enrolled-but-quiet. (Rendering the `last_seen`
     *age* per layer — alongside Ory last-login + SPIRE last-SVID — is the
     roadmap last-activity work; the endpoint already returns `last_seen`.)

The composition is **"what they CAN do" (the access grants) joined with "what
they ARE doing" (live presence + footprint) for every identity** — that join is
what makes `who` the identity COP rather than either a static permission list or
a usage leaderboard.

## Layout

```
┌ goat who — identities ─────────────────────────────────────────────┐
│ 42 identities · 11 live · 7 producing · filter "field"              │ search/facet bar
├─────────────────────────────────────────────────────────────────────┤
│ NAME                 LIVE  MSG/S  BAND  W/R    STATE                  │
│ ▌field-sensor-01      ●     14    ok    3/1    active                 │ roster (master)
│  field-actuator-02    ○      —    —     ·      ·                      │  default sort: name
│  field-camera-c       ●    220    HIGH  2/1    active                 │  / filters · s sorts
│  …                                                                    │
├─────────────────────────────────────────────────────────────────────┤
│ field-camera-c   anchor 9f3c… · kind system · state active · mesh on │ detail (selected)
│ live: ● alive 4m · producing 220 msgs/s 88 KB/s [C2 90% C0 10%] HIGH │
│ WRITES (2)  9f3c…/track/v1  zenoh-acl  slot-pool write anchor   …     │
│ READS  (community)  **      zenoh-acl  flat community-read           │
└─────────────────────────────────────────────────────────────────────┘
 q quit · / filter · s sort · a fetch-all-perms · ↑/↓ select · read-only
```

- **Roster columns** are the cheap, *globally* available signals (name, live,
  usage, band) plus a permissions summary (`W/R`, `STATE`) seeded for the whole
  roster from the one `access roster` call (0.4.9) — instant, not per-row. The
  `STATE` column is a **cross-layer status**, not the old uniform recon "active":
  `online` (a matched mesh peer is connected now), `on-mesh` (enrolled, not
  currently connected), `cut-off` (revoked / disabled — red), `no-grants`
  (onboarded but zero W+R — the silent-denial surface, amber), `stray`
  (live with no registry record), `active` (has grants, no richer signal — rendered
  **dim, not green**, since 2026-09-06: green is for a live signal, and on a
  per-identity slot pool most rows sit here, so painting them green read as
  "everyone is here"), `·`
  (no summary yet — honest, never guessed). The roster **scrolls**: the viewport
  follows the selection (`↑/↓` `j/k`, `PgUp/PgDn`, `g/G`) with a scrollbar when
  it overflows, so a 180-identity roster is navigable past the first screenful.
- **Names** resolve in priority order — slot **alias** (`AliasRegistry`,
  e.g. `jane-doe`) → registry **display name** (`/api/vendors`, e.g.
  `example-vendor`) → truncated **UUID** (the fail-closed floor). A vendor carries
  a display name but no slot alias, so without the middle tier it rendered as a
  bare UUID even when the portal answered.
- **Detail pane** describes the selected identity across **all four identity
  domains**, not just the Zenoh ACL: **IDENTITY** (kind / state / org),
  **MESH** (NetBird on/off), **DATA** (Zenoh-ACL writes/reads), **RELEASE**
  (release-set entries). Write/read entries are grouped by their deciding
  `.layer`; a domain the portal returned nothing for stays silent. A
  **cross-layer summary line** (0.4.9) renders the moment a row is selected,
  sourced from the bulk roster (kind · status · W/R grants · mesh posture · org)
  so the rollup shows instantly even if the full per-layer fetch is slow on the
  mesh. Overflow points at `goat access why <name>` for the full trace.

## Honesty discipline (inherited wholesale from `goat top`)

`goat who` is bound by the same env-health honesty contract — **never show a
state you cannot ground.** Computed once in a pure `view_model`, so the
interactive screen and the `--once` plain renderer can never disagree.

- **Live column:** `●` only when a presence token is *currently alive*; `○` +
  age when the token dropped; dim `?` when no presence beat covers this anchor
  (unmetered — *not* a false "offline"). A stale presence beat (older than
  ~3× its window) collapses to unmetered, never re-asserts the last color.
- **Usage:** absent footprint → `—` (idle), never `0`. Stale footprint ages out
  of the live join.
- **Band:** `—` when no envelope is declared for the actor — an honest
  no-verdict, never a faked green.
- **Permissions:** `·` until `access who` has been fetched for the row;
  `portal unreachable` (not an empty allow-list) when the portal is down. An
  unknown alias renders as the truncated UUID — the alias layer fails closed
  ([ADR 1039](../adr/1039-slot-alias-display-layer.md)); `goat who` never
  invents a name. A *failed* fetch is classified honestly, never one scary red
  string: **slow/unreachable** (the portal didn't answer in time — the common
  lossy-mesh case; amber, `?` in the `W/R` cell, `r` retries) is distinct from
  **unauthorized** (401/403 — the token can't query this id; red) and other
  errors. The 0.4.7 build collapsed all of these into "network or parse error",
  which read as a code bug when it was usually just the mesh.
- **Registry membership:** a live anchor absent from the registry is labeled
  `live, not in registry`, never silently dropped and never silently promoted to
  "declared."
- **No log-bleed:** while the full-screen surface is up, stderr is silenced at
  the fd level so the zenoh transport stack's `tracing`/`log` output (e.g. the
  `Skipping name verification of TLS server` WARN) can't paint over the rendered
  frame. Restored on teardown (incl. panic). The guard lives in `tui/mod.rs`,
  so `goat top` / `goat sub --tui` get it too.

## Command surface

```
goat who [--sort name|live|use] [--filter <substr>] [--site <scope>] [--once] [--uuid]
```

- **interactive** (default, TTY-gated): the master-detail screen.
  - `/` — incremental filter (htop idiom). Free substring matches alias, anchor,
    or display name; the facet prefixes `live:on|off` and `use:on|off` narrow by
    live/producing state (the two signals available globally without a per-row
    fetch).
  - `s` — cycle sort: name → **live** (default, activity-first) → usage
    (msgs/s) → name. The default is `live` as of 0.4.9 — a deliberate,
    documented reversal of the 0.4.8 `name` default after the live review asked
    for a meaningful default. It is *not* a msgs/s leaderboard (that's `use` /
    `goat top`); `/` search is instant in every mode, so the roster stays
    searchable-first — the two framings are reconciled, not flip-flopped.
  - `a` — enqueue every roster row for an `access who` fetch (throttled drip), so
    the `W/R` / `STATE` columns fill in across the whole roster.
  - `↑`/`↓` (`j`/`k`), `PgUp`/`PgDn`, `g`/`G` (top/bottom) — move selection; the
    viewport scrolls to follow it. The selected identity's `access who` is
    fetched on demand and cached.
  - `r` — retry the selected identity's `access who` when a previous fetch
    failed (the common case: a slow-mesh timeout).
  - `q` / `Esc` (clears an active filter first) — quit.
- **`--once`** (the `top -bn1` analog, scriptable / non-TTY): one plain-text
  roster snapshot — name, live, usage, band, and registry-membership flag — to
  stdout, then exit. Permissions are **not** included in `--once` (they are an
  N-call-per-identity fetch); the scriptable single-identity capability path
  stays `goat access who <id>`.

## Session model

Opens a **read-only random-ZID** Zenoh session (like `goat top`): a long-lived
browser must not hold the operator's pinned slot ZID or concurrent `goat`
commands hit `MAX_LINKS` ([F-195]). Attribution rides the cert CN, never the
ZID, so a random-ZID subscriber is correct. The lazy `access who` fetches run on
a small async fetcher task fed by the UI loop over a request channel; results
return on the same channel the bus subscribers use, so the sync ratatui loop
never blocks on the portal.

**Portal timeout at the edge.** The shared `PortalClient` request timeout
defaults to 20 s (`GOAT_PORTAL_TIMEOUT_SECS` overrides) rather than the former
5 s. On a degraded tactical mesh the portal *answers* but a round-trip can take
~10 s, so a 5 s budget
times out on a portal that is up and surfaces it as a false "unreachable" —
which is precisely what made every name and permission read as missing on ZAGROS
in 0.4.7 even though the data was there. A generous ceiling is free on a fast
network (calls return in ms) and is the difference between the roster being
useful at the edge or not. (The change is in `panoscope-core`, so every portal
consumer — panoscope / stethoscope / flashlight — inherits the edge-tolerant
budget.)

## What this is NOT

- **Not an enforcement or mutation surface.** No revoke, no lock, no rotate.
  `goat who` *reveals*; the cut stays `goat admin onboard --revoke` /
  `goat admin peer` (the mesh-first cut), and rotation stays `goat admin rotate`.
  A future "press `x` to revoke the selected identity" is a deliberate
  non-goal for v1 (a read-only COP must not become a foot-gun).
- **Not a replacement for `goat access who`.** That stays the scriptable
  single-identity query and the `--once`-script capability path; `goat who` is
  the navigable many-identity browser that *delegates* to the same
  `/api/access/who` endpoint per selection.
- **Not a new on-bus producer.** It subscribes footprints + presence and queries
  the portal read API. It publishes nothing.
- **Not a central identity authority.** Declared state comes from the portal
  registry; live state is each node self-publishing. `goat who` is a passive
  reader that composes a view, consistent with the brokerless,
  no-central-authority posture.

## Built vs designed

- **Built (0.4.7):** `goat who` interactive screen + `--once` + the
  declared-⊕-live roster fold + lazy `access who` fetcher + the pure view model
  and its unit tests, in `crates/goat-cli` (`cmd/who.rs` + `tui/who.rs`),
  reusing `panoscope-core`'s `AliasRegistry` / `EnvelopeRegistry` / `PortalClient`
  and the shared `health_wire` footprint/presence wire types.
- **0.4.8 quality re-engineering** (this change — the 0.4.7 build was reviewed
  live on ZAGROS and rejected as bug-ridden; every defect below was root-caused
  against the live env, not guessed):
  - **Portal timeout** bumped 5 s → 20 s (env-overridable) in `panoscope-core`
    — the single root cause of "UUID-only names" + "permissions unavailable" on
    ZAGROS (the portal answers in ~9 s on the lossy mesh; the 5 s budget timed
    out). Names + perms now load at the edge.
  - **Name resolution** alias → display name → truncated UUID (a vendor with a
    display name but no slot alias was rendering as a bare UUID).
  - **Permissions across all four domains** (IDENTITY / MESH / DATA / RELEASE),
    grouped by deciding `.layer`, not a flat Zenoh-only dump.
  - **Honest error taxonomy** for a failed `access who` — slow/unreachable
    (amber, retryable with `r`) vs unauthorized vs error — replacing the single
    scary "network or parse error" string.
  - **Stateful scrolling** (`TableState` + scrollbar + `PgUp/PgDn`, `g/G`) so a
    180-row roster is navigable past the first screenful.
  - **Log-bleed eliminated** — an fd-level stderr guard for the alternate
    screen's lifetime (`tui/mod.rs`), so the zenoh stack's WARN lines can't
    corrupt the frame; `goat top` / `goat sub --tui` inherit it.
  - **`--once` W/R column dropped** — batch mode never fetches perms, so the
    column could only show a misleading placeholder.
- **0.4.9 cross-layer COP rework** (this change — after a live-ZAGROS review of
  0.4.8 said "better, but not true to its purpose"; the gaps were in the data
  model and the product concept, not the rendering):
  - **W/R `0/0` fixed (portal-side).** `access.Assemble`/`Who` now emit a
    slot-pool member's `<anchor>/**` namespace write grant and reflect flat
    community-read as a `**` read grant — so `W/R` is meaningful for the whole
    slot-pool roster (ZAGROS), and a real `0/0` now means the silent-denial
    surface, not a rendering gap. (`internal/access/{graph,query}.go`.)
  - **Bulk roster endpoint** `GET /api/access/roster` — one server-side
    assembly returns every identity's W/R + state + on-mesh / last-activity
    summary; the CLI seeds the whole roster from it instead of an N-deep
    per-row `who` fetch. This is the responsiveness fix (the per-row ~9 s
    round-trip on the lossy mesh was the "not responsive" complaint).
  - **Cross-layer `STATE`** (online / on-mesh / cut-off / no-grants / stray /
    active / `·`) replacing the uniform recon "active" — composed from the bulk
    summary so it varies and each value is actionable.
  - **Mesh last-activity** — NetBird `last_seen` + `connected` plumbed from the
    poller through the access graph into the roster summary.
  - **Activity-first default sort** (`live`) reconciling searchable-first with a
    meaningful default; `/` search stays instant.
  - **Instant cross-layer detail line** + person/system/org rollup (the bulk
    roster carries `kind` + `vendor`, and folds Ory persons into the roster).
- **0.4.10 affiliation + drill-down + remediation** (this change):
  - **Affiliation column (`AFF`)** — each identity's identity-grammar
    affiliation (org `kind` = vendor/organic/partner, tinted, + `nation`),
    surfaced in the roster, the `--once` output, and the detail pane. The data
    already lived in `roster.json`'s `orgs` block; the portal previously dropped
    it. `LoadRoster` now reads that block and joins each member to its org's
    kind/nation in **Go**; `Assemble` attaches it to the vendor + person nodes
    via the existing reconciliation key; `/api/access/roster` carries it. **Zero
    Python** — the generator is untouched (it already emitted the orgs block).
    Honest by construction: an anchor with no declared match renders `·`, never
    a guess (clean on per-vendor goatnets where prefix == the org tag). On ZAGROS's
    UUID slot-pool the prefix is opaque, so `Assemble` hops Prefix→Alias→declared
    member handle (`VendorIn.Alias` = the slot's `assigned_to`, joined to the
    roster member's `Handle`) to pick up the member's affiliation — still
    declared-data-only, a `·` remains only when neither key nor alias resolves.
    Display-only attribution (ADR 1043): wire keys / ACL / audit untouched (ADR 1039).
  - **`role` deliberately NOT built — corrected premise.** The original framing
    assumed a per-identity grammar role in the manifests; there is none. The
    only `role:` key is `netbird.role` (uniformly `peer` — a transport role, not
    a capability), and the real capability role (the grammar's
    ADMIN/OBSERVER/DEVELOPER family) lives in the Ory `groups` claim, which the
    generator deliberately **sanitizes out** of `roster.json` and which isn't in
    the Kratos traits the portal reads. A role column today would fabricate or
    cross the sanitization boundary, so affiliation is the real, populated half;
    role waits for a portal path that exposes the capability groups honestly.
  - **Enter drill-down** — Enter expands the selected identity to a full-screen
    detail (roster hides; Esc/q back; ↑/↓ jk still page identities). Same detail
    content as the side pane (shared `build_detail_lines`, no drift).
  - **Stray remediation surface** — the drill-down shows a copy-ready
    `goat admin` command for the two actionable states: `stray` (live, no
    declared record → claim / mesh-first cut) and `cut-off` (re-onboard /
    rotate). `goat who` stays a **pure read-only consumer** (ADR 1047): it
    reveals + proposes; the mutation runs through the audited `goat admin`
    chokepoint. In-TUI execution (terminal-suspend + subprocess + typed
    confirm) is a deliberate, separately-validated non-goal for this pass — it
    would put a mutation path + terminal-handling fragility into a read-only
    surface. (`tui/who.rs`; render test covers both.)
- **Designed-not-built / roadmap:**
  - **The SPIRE/SPIFFE fourth domain** — the access graph already joins NetBird
    + Ory + Zenoh server-side; adding a SPIRE source (registration/entry API,
    zagros-spire-implementation-plan.md,
    ADR 0412) completes the four-domain
    join the North Star names. The roster summary's shape already has room for it.
  - **Per-layer last-activity** beyond mesh — Ory last-login (Kratos) and SPIRE
    last-SVID issuance, joined per entity, so "last activity across them" is
    answered for every layer, not just the mesh.
  - Background auto-enrich (fetch the whole visible window on open) — v1 is
    fetch-on-select + opt-in `a`, to avoid hammering the portal.
  - Operator-action audit folded into the detail pane (how an *operator* used
    their admin powers, decrypting `ops/audit/operator/<actor>`) — the
    [operator-action audit](operator-action-audit.md) stream exists; surfacing
    it here is a clean follow-on.
  - Remaining identity-grammar dimension columns — `role` (the capability
    groups, once a portal path exposes them without crossing the roster
    sanitization boundary), `echelon` / `node-class` / `site` once those land in
    a portal roster endpoint (identity-grammar.md is
    additive-but-not-yet-cutover). `affiliation` is built (0.4.10 above).
  - Live-fire on ZAGROS is operator-gated (the build ships dark, same as
    `goat top` did before [M-067]).

[F-195]: ../findings/F-195-zid-pin-makes-concurrent-same-cert-sessions-collide-max-links.md
[M-067]: ../measurements/M-067-goat-top-live-fire-zagros.md
