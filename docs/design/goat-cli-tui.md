# goat-cli-tui — terminal inspection surface for the goat data fabric

| Field | Value |
|---|---|
| Status | Design (authored 2026-06-02). Phase A planning landed (PR #748). **Phase B (Diagnose lens — `goat inspect --tui`) implemented** 2026-06-02: SSE producer task → channel → synchronous ratatui loop on `spawn_blocking`; TTY-gated; verdict logic stays in stethoscope (renderer over the same `/api/diagnose` SSE the line-oriented `goat inspect` consumes). **Phase C (Tail lens — `goat sub --tui`) implemented** 2026-06-02: producer task (Zenoh subscriber) + decoder task (`DecodeCache`) → unbounded channel → synchronous ratatui loop on `spawn_blocking`; bounded in-memory sample ring + decode-on-select (no `/api/sample`, see Tail section); event table + rate sparkline + decoded/hex payload viewer; TTY-gated. **Phase D (merged picker — `goat ui`) implemented + live-fired** 2026-06-03: declared⊕live roster (portal + panoscope `/api/snapshot`) joined on the topic key with a membership column, peer pivot (Sources/Derivers/Sinks), local search, detail pane, and lens-switch hops preserving the target; per-vendor fetches concurrent. **All three surfaces (both lenses + picker) live-fired on nova-a** (M-048) — the full Phase A–D surface is built and proven on real infrastructure. |
| Scope | An opt-in, TTY-gated interactive terminal surface inside the existing `goat` binary that renders the web inspection surfaces (stethoscope, flashlight, portal, panoscope side-panel) in the terminal. It is a **renderer**, not a new service: it consumes the same stable HTTP/SSE/REST contracts the web surfaces expose, over `panoscope-core`. Line-oriented `goat` stays the default; the TUI is an explicit opt-in that never breaks scriptability. |
| Related | `goat-cli.md` ([ADR 1019](../adr/1019-goat-cli-unified-operator-binary.md) — the binary this extends), [`stethoscope.md`](https://github.com/usareur-af/goatstuff/blob/main/docs/design/stethoscope.md) (the Diagnose lens' source-of-truth service), [`flashlight.md`](https://github.com/usareur-af/goatstuff/blob/main/docs/design/flashlight.md) (the Tail lens' source-of-truth service), `panoscope.md` (the live-fabric snapshot the picker consumes), [`zenoh-surface-deep-links.md`](zenoh-surface-deep-links.md) (Block 94 — the navigation contract the TUI translates into keystrokes), [`minimalism-under-network-constraints.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/minimalism-under-network-constraints.md) (the doctrine goat-cli and flashlight Tier 1 both inherit). |

## Vocabulary (defined before architecture)

- **Surface** — one of the four web services that answer a distinct question about the fabric: **portal** ("what's *declared*?"), **panoscope** ("what does the fabric look like *right now*?"), **flashlight** ("what's happening in *this slice*?"), **stethoscope** ("is *this target* healthy?").
- **Target** — the thing an operator points at: a topic key, a vendor, a peer, or a system. The unit that navigation preserves.
- **Picker** — the TUI surface for choosing a target: a browseable, searchable inventory of everything you could point at.
- **Detail** — the static "what is this thing" view of the selected target: registration, declared schema, live status, who publishes/subscribes.
- **Lens** — a dynamic question asked about the *currently selected* target. Two ship: **Diagnose** (stethoscope verdicts) and **Tail** (flashlight live slice).

## What this is

A single interactive surface inside `goat` that lets an operator browse the fabric and inspect a target without leaving the terminal — picking a target, reading what it is, diagnosing it, and tailing its live traffic, all against the same data the web surfaces show.

It exists because three things are already true in the codebase:

1. **`goat inspect` already proves the pattern.** It opens an SSE connection to stethoscope's `/api/diagnose` and renders the verdict stream in the terminal. The TUI is that pattern made navigable and extended to the other surfaces — not a new capability class.
2. **All four web surfaces sit on `panoscope-core`, which `goat` already imports.** The portal client, Zenoh mTLS session, audit-tail walker, peer registry, and role taxonomy are the same Rust types the web frontends render from. The TUI renders the same data; it does not add a second data layer.
3. **Block 94 already defined the navigation model** ([`zenoh-surface-deep-links.md`](zenoh-surface-deep-links.md)): four questions, six named hops, one URL-param vocabulary (`key=`/`vendor=`/`peer=`/`topic=`/`system=`/`role=`/`site=`). That contract translates directly into TUI keystrokes that preserve the target.

This is the asymmetric advantage goat-cli already trades on (see `goat-cli.md` on `compose` and `--narrate`): the surfaces share substrate *and* a navigation contract, so a terminal renderer can unify them cheaply. `gh`/`kubectl` cannot — their backends are black-box and have no cross-surface contract.

**Line-oriented `goat` stays the default.** Every existing subcommand keeps its line-oriented, scriptable, `--json`-stable behavior. The TUI is opt-in and TTY-gated (it refuses to launch on a non-TTY, exactly like `goat compose`), so pipes, CI, and Ansible never accidentally hang. This surface is an addition to the operator's options, not a change to the default path.

## The organizing model: picker + detail + two lenses (not four panes)

The naive decomposition is "one TUI pane per web surface." That is wrong, and the reason is instructive.

**The web split into four surfaces for deployment isolation** — separate images, separate rollback ledgers, separate CI, separate mesh services (each surface doc says so explicitly). None of that applies to one `goat` binary. The axis that *does* survive into a terminal is **operator posture**, and on that axis the four surfaces are two kinds of thing:

- **Browse / declarative posture** ("pick a target, read what it *is*"): **portal** + the **panoscope side-panel**.
- **Dynamic-question posture** ("ask something about the selected target"): **stethoscope** ("healthy?") + **flashlight** ("what's flowing?").

So the TUI is:

```
┌─ picker ──────────────┬─ detail + lens (selected target) ──────────┐
│ browse / search the   │  detail: what this target IS               │
│ declared ⊕ live        │  ───────────────────────────────────────  │
│ inventory of targets  │  [ Diagnose ]  [ Tail ]   ← lens tabs       │
│ (group by vendor/topic│  Diagnose: stethoscope verdict cards (SSE) │
│  or pivot by peer)    │  Tail:     flashlight live slice + payload │
└───────────────────────┴────────────────────────────────────────────┘
```

One **picker+detail** surface (the two browse/declarative web surfaces coalesced), plus two **lenses** on the selected target (the two dynamic web surfaces, kept separate because their interaction models genuinely differ). The Block 94 "hops preserve the target" contract becomes "switch lens on the current selection" — a purer expression of the contract than the web manages, because the web has to physically change surfaces to do it.

### Web-surface → TUI-element mapping

| Web surface | Posture | TUI element | Notes |
|---|---|---|---|
| **portal** (browse half) | declarative | **picker** | vendor/topic catalog → the declared half of the roster |
| **portal** (detail half) | declarative | **detail** | registration, declared format, schema (`.fds`/`.proto` as a text pane) |
| **panoscope side-panel** (roster half) | live overview | **picker** (peer pivot) | live peers/topics → the live half of the roster |
| **panoscope side-panel** (detail half) | live overview | **detail** | who publishes/subscribes, last-seen, role/site |
| **panoscope force-graph / hulls** | live overview | **— (not ported)** | topology viz does not translate to a TUI; see non-scope |
| **stethoscope** | diagnostic | **Diagnose lens** | verdict cards, findings, remediation, topic-roster, cardinality — all over `/api/diagnose` SSE |
| **flashlight Tier 1** | focused live | **Tail lens** | event table + rate sparkline + on-demand payload viewer |
| **flashlight Tier 2 (WASM)** | focused live | **— (server-side instead)** | browser compute doesn't port; use `POST /api/compute` or the native `flashlight-analytics` crate |

## The picker: declared ⊕ live, merged

The picker coalesces portal's catalog (the **declared** inventory) and the panoscope side-panel's roster (the **live** inventory) because they are two vantage points on the same question — *"what can I point at?"* The same logical entity (a topic, a vendor) appears in both, so the picker **joins them on the entity key and shows both facets as columns**:

```
TOPIC                         DECLARED              LIVE
vendor-a/observations/v1       ✓ protobuf:Obs v3     ✓ 124/min · 3 subs
vendor-a/commands/v1           ✓ protobuf:Command    ✗ idle (no events 7d)      ◀ declared, never runs
vendor-c/radar1/tracks             ✓ protobuf:MyMessage  ✗ —                        ◀ declared, never runs
vendor-c/radar1radar/tracks       ✗ —                   ✓ 12/min · 1 sub           ◀ runs, never declared
```

This is **strictly better than the web**, where declared and live live on two separate surfaces and the operator cross-references them by hand. The high-value information is the *mismatch* between facets — `declared ✓ + live ✗` ("registered something that isn't running"), `live ✓ + declared ✗` ("publishing but never declared" — the undeclared-publisher RCA shape, F-162). Both are invisible in either single surface; the merged roster promotes them to a glanceable column the moment the picker opens.

**Join spine and pivot.** Portal thinks in `vendor`/`topic`; panoscope thinks in `peer`/`topic`. The clean join is along the **topic/vendor spine** (the bulk of both surfaces); `peer` is fabric-only (the portal has no peer concept). So the picker offers two groupings behind one toggle:

- **by vendor/topic** — the declared ⊕ live roster above (default).
- **by peer** — the fabric-only view ("who's online, what's each one publishing"), grouped Sources / Derivers / Sinks.

This is not two panes; it is one inventory shown from whichever vantage the operator wants. Note flashlight's existing `GET /api/directory` already merges portal vendors/topics with topology-derived peers into exactly this Sources/Derivers/Sinks shape — the picker consumes the same kind of merged payload (`panoscope-core::PortalClient` for declared state + the panoscope `/api/snapshot` for live state, the same two reads stethoscope's `subscriber-visibility` check already composes).

**Selection search** reuses the established pattern: stethoscope's `/api/index` (vendors/topics/systems, 30 s cache, breadth-first typeahead) and the auto-detect-kind-from-input-shape rule (no slash → vendor; 1 slash → system; 2+ → topic) are already built; the picker is the terminal expression of the same picker stethoscope ships in its web header.

## The Diagnose lens (stethoscope)

Renders stethoscope's diagnosis of the selected target. This is the interactive form of today's `goat inspect`:

- Opens `GET /api/diagnose?key=<target>` (or `?vendor=`/`?system=`) as SSE; renders one card per check as each verdict streams in (Pass/Warn/Fail/Skipped/N/A), worst-wins roll-up at top.
- Expandable per-card detail, findings (advisories like `declared-but-never-published`), and the remediation block (verbing title + monospace command) — all already structured in stethoscope's response.
- topic-roster and cardinality cards render as-is (lists of child verdicts with their colors).
- Blood-dye (active probes) is a per-session flag; the lens exposes it as a toggle the same way the web does, gated on the `stethoscope` cert being provisioned.

Everything here is structured text over SSE; it ports without loss. The lens runs no checks itself — stethoscope remains the source of truth (delegation guardrail below).

## The Tail lens (flashlight Tier 1)

Renders flashlight's focused live slice for the selected target. This is the grown-up form of the already-planned `goat sub --tui` (implemented Phase C — `crates/goat-cli/src/tui/tail.rs`):

- Subscribes via `panoscope-core`'s Zenoh client narrowed to the target's key expression (the same narrowing flashlight's backend does from its URL filter).
- **Event table** (ratatui list) — `seq`, timestamp (HH:MM:SS UTC, no chrono dep), key, format icon (`{}`/`[]`/`txt`/`0x`/`·` from the first non-whitespace byte, mirroring flashlight), payload length.
- **Rate sparkline** (ratatui sparkline widget) — rolling per-second event rate over the last 60 s.
- **Payload viewer** — on row-select, show the decoded message (protobuf-deserialized against the portal-declared `protobuf:<Name>` via `fetch_fds`) over the raw hexdump, the same two-pane shape flashlight landed in Phase F-3e.

**Direct-subscriber wrinkle (vs flashlight's web Tail).** flashlight's web surface keeps payload bytes *off the wire* — its event stream carries only metadata, and the browser fetches bytes on demand from `GET /api/sample/{seq}`. goat-cli is itself the Zenoh subscriber, so every sample arrives *with its bytes already in hand*. The Tail lens therefore keeps a **bounded in-memory ring** of recent samples (1000, with a per-sample byte cap) instead of an `/api/sample` round-trip, and **decodes lazily on row-select** (only the bytes the operator actually inspects pay the decode cost; the `DecodeCache` runs on its own task so the UI never blocks on it). A selection held on a sample that has since aged out of the ring degrades to an "evicted" note rather than erroring — pausing the tail (space/`p`) freezes the auto-follow so a sample can be held for inspection.

These are intra-lens regions, not separate panes. Heavy analytics (rolling percentiles, z-score, schema-diff) are the native `flashlight-analytics` crate called via `POST /api/compute` (or linked directly) — not browser WASM.

## Navigation = Block 94 hops as keystrokes

The six named hops in [`zenoh-surface-deep-links.md`](zenoh-surface-deep-links.md) become keystrokes that preserve the target:

- From a picker row or a Diagnose card → **Tail this** (switch to the Tail lens, same target).
- From a Tail row → **Diagnose this** (switch to the Diagnose lens, same target).
- From either lens → **show in fabric** (back to the picker, peer pivot, target pre-selected).
- Open the declared schema → portal `schema.html?key=` is the web hop; in the TUI the detail pane already shows it.

The param canon (`key`/`vendor`/`peer`/`topic`/`system`/`role`/`site`) is the internal target representation, so a target selected in any element is expressible to any other — exactly the inbound/outbound contract Block 94 specifies, with keystrokes standing in for `↗` links.

## Delegation guardrail (load-bearing)

goat-cli's founding principle ([ADR 1019](../adr/1019-goat-cli-unified-operator-binary.md)) is **"delegates, never replaces; decision logic stays in the canonical service."** The TUI must hold that line:

- **Diagnose** renders stethoscope's `/api/diagnose` SSE — it never re-runs checks locally.
- **Tail** subscribes via panoscope-core and decodes through the portal's `fetch_fds` — it never forks the decode logic; bulk analytics go to `/api/compute`.
- **Picker** *aggregates* two read sources (portal declared + panoscope live) and *marks membership* (declared/live/both). Aggregation and membership-marking is presentation and is fine. But a **verdict** — "this mismatch is an anomaly," not just "declared ∌ live" — comes from **stethoscope**, not from logic invented in the picker. The picker may surface that a topic is declared-but-not-live; calling that *unhealthy* is stethoscope's job, reachable in one keystroke via the Diagnose lens.

As long as the TUI renders what the services compute, it is a presentation layer, not a parallel substrate — which is exactly what keeps it from re-introducing the fragmentation ADR 1019 set out to close.

## What does NOT translate

- **panoscope's force-directed graph + hull rendering** — the topology visualization is the half of panoscope that doesn't reduce to a TUI. The side-panel (roster + per-target detail) is what ports; the graph is explicitly out of scope. An operator who needs the topology picture uses panoscope in a browser.
- **flashlight Tier 2 (WASM browser compute)** — the in-browser analytics coprocessor has no terminal analogue. The *same* `flashlight-analytics` crate already compiles native and is exposed at `POST /api/compute`, so the Tail lens loses no capability — it just runs the compute server-side (or links the native crate), which is also flashlight's `?compute=server` story for degraded clients.

## Reuse, not new substrate

- **`panoscope-core`** — already a `goat` dependency. PortalClient (declared inventory + `fetch_fds`), Zenoh mTLS session (Tail subscription), audit-tail walker, peer registry + role taxonomy (picker peer pivot).
- **`ratatui` + `crossterm`** — already in `goat` for `compose`. The TUI is the second ratatui surface, not a new dependency.
- **Stable web contracts** — `/api/diagnose` (SSE), `/api/index`, `/api/snapshot`, `/api/directory`, `/api/sample/{seq}`, `/api/compute`, portal REST. The TUI renders against these; it does not reach into service internals. goat-cli's `schema_version`-pinned `--json` discipline guards against drift in the shapes it consumes.
- **Block 94 param canon** — the navigation vocabulary, reused as the internal target representation.

The new code is presentation only — ratatui widgets and an event loop. Presentation in a TUI is cheap relative to a browser frontend; the substrate and the decision logic already exist and stay where they are.

## Invocation

All TTY-gated (refuse to launch on a non-TTY, by construction, like `goat compose`); none change any existing line-oriented or `--json` behavior:

- `goat ui [<target>]` — open the full surface (picker + detail + lenses). With a target, pre-select it.
- `goat inspect <target> --tui` — open directly on the **Diagnose** lens for the target (the interactive form of today's line-oriented `goat inspect`).
- `goat sub <pattern> --tui` — open directly on the **Tail** lens (the already-planned `goat sub --tui`, now a lens of the larger surface).

The bare `goat inspect <target>` and `goat sub <pattern>` stay exactly as they are: line-oriented, scriptable, the workhorse default.

## Phasing

Each phase lands as one or more PRs with the doc/in-flight delta in the same change-set per documentation discipline. Highest-ROI lenses first, because both already have line-oriented foundations to grow from.

| Phase | Scope | Gate |
|---|---|---|
| **A — planning** | This doc + ADR 1019 addendum + in-flight entry + CLAUDE.md pointer. **No code.** | Operator review + approval of this design. |
| **B — Diagnose lens** ✅ *landed 2026-06-02* | `goat inspect --tui`: ratatui shell + the Diagnose lens rendering `/api/diagnose` SSE — live check cards (left list) + selected-card detail (summary + findings + remediation, right pane) + worst-wins roll-up in the header + streaming/done/error status. Reuses the existing `goat inspect` SSE consumer (shared `open_diagnose_response` + `parse_sse_event_block`); SSE producer task feeds a channel drained by a synchronous ratatui loop on `spawn_blocking`. New module `crates/goat-cli/src/tui/` (`mod.rs` TTY-gate + `diagnose.rs` lens). topic-roster/cardinality cards render via the same generic outcome path (no special-casing). | Live-fire on nova-a: operator opens `goat inspect <topic> --tui`, sees the same verdicts the web stethoscope shows; non-TTY refuses. |
| **C — Tail lens** ✅ *landed 2026-06-02* | `goat sub --tui`: event table + rate sparkline + on-demand payload viewer (decoded + hex via `fetch_fds`); heavy analytics via `/api/compute`. New `crates/goat-cli/src/tui/tail.rs`: producer task (Zenoh subscriber) + decoder task (`DecodeCache`) feed an unbounded channel drained by the synchronous ratatui loop on `spawn_blocking`; bounded in-memory sample ring + decode-on-select (direct-subscriber, no `/api/sample`); auto-follow with pause/scroll; TTY-gated before any session open. Reuses `cmd::pubsub::open_zenoh_session` + `cmd::decode::DecodeCache`. | Live-fire: operator tails a live slice, row-selects, sees decoded + raw bytes; degrades cleanly when a sample has evicted. |
| **D — picker + detail** ✅ *landed 2026-06-03; live-fired nova-a* | The declared ⊕ live roster (portal `list_vendors`/`list_topics` + panoscope `/api/snapshot`), joined on the topic key with a glanceable membership column (`✓✓` both · `✓·` declared-idle · `·✓` live-undeclared); **peer pivot** (`p` toggle: peers grouped by `role_class` — Sources/Derivers/Sinks — with pub/sub counts + a peer detail listing published/subscribed topics; lens hops from a peer target its `vendor=`/`vendor/**`); local substring search; detail pane (declared format/schema/vendor over live eps/pubs/subs); lens-switch keystrokes preserving the target (`d`→Diagnose, `t`→Tail) via the `cmd::ui` orchestration loop; `goat ui [<target>]` umbrella entry. New `crates/goat-cli/src/cmd/ui.rs` (snapshot client + pure `merge_roster`/`aggregate_live`/`aggregate_peers`; per-vendor topic fetches run **concurrently** so the picker draws in seconds, not the ~30 s a sequential 90-vendor walk took) + `crates/goat-cli/src/tui/picker.rs`. Snapshot endpoint derived `portal.*`→`panoscope.*` (+ `$GOAT_PANOSCOPE_ENDPOINT` / `--snapshot-endpoint`); degrades to declared-only when the snapshot is unreachable, live-only when the portal is disabled. **Search:** a local substring filter over the *merged* roster is retained by design — it narrows the declared⊕live view, which the declared-only `/api/index` cannot; `/api/index` typeahead is therefore not wired (decision, not deferral). | Live-fire (M-048): operator browses the merged roster + peer pivot, sees declared/live mismatches at a glance (the live `vendor-c/rada*` typo-namespace `·✓ undeclared` vs declared `vendor-c/radar1radar` `✓✓`), hops picker → Diagnose → Tail with the target preserved. |

Phase D (picker) is the largest and is deliberately last: B and C deliver standalone value on day one (they're just the interactive forms of `goat inspect`/`goat sub`), and the picker is gated on those proving useful plus operator demand for the unified browse.

## What this is NOT

- **Not a new service or daemon.** It is a mode of the existing `goat` binary. No deployment, no socket, no background process. Side effects are confined to the terminal session.
- **Not a replacement for the web surfaces.** stethoscope, flashlight, panoscope, and portal remain the canonical, source-of-truth surfaces with their own deployments and rollback ledgers. The TUI is a second renderer for operators who live in a shell; an operator who wants the topology graph or a shareable URL uses the web.
- **Not the default `goat` experience.** Line-oriented stays the workhorse and the only non-TTY path. The TUI is opt-in.
- **Not a re-implementation of check or analytics logic.** Diagnosis stays in stethoscope; analytics stay in `flashlight-analytics` (native via `/api/compute`). The TUI renders results; it does not compute verdicts.
- **Not a mutation surface.** Like the web inspection surfaces, the TUI is read-only. Topic create/delete and other mutations stay on the existing line-oriented `goat topic …` commands and the portal admin surfaces.

## Open questions

Recorded so they're addressed before the phase that needs them lands. Phases A, B, and C have shipped; the remaining items gate Phase D (the picker).

1. **`/api/snapshot` access from goat-cli. — RESOLVED 2026-06-02.** stethoscope reads panoscope's `/api/snapshot` as a cooperating sibling service; the picker (Phase D) needs the same read from an operator laptop. **Decision: derive the panoscope endpoint from the profile's `portal_endpoint` by host substitution (`portal.*` → `panoscope.*`), reusing the existing generic `substitute_host` helper in `cmd::inspect`** (the same mechanism `goat inspect` already uses to derive its stethoscope endpoint, `portal.*` → `stethoscope.*`), with a `$GOAT_PANOSCOPE_ENDPOINT` env override and a `--snapshot-endpoint`-style flag for the non-standard-hostname case. **No new profile/bundle field.** Rationale: (a) consistency — it is the exact pattern the Diagnose lens path already proves, so there is one endpoint-derivation rule across both lenses and the picker; (b) lower complexity budget — no bundle schema change, no new enrollment-time decision; (c) auth needs nothing extra — the operator laptop is already a mesh peer and reaches `panoscope.*` over the same LE-anchored TLS the portal/stethoscope reads use, so mesh membership *is* the authorization (the snapshot is a read of already-mesh-visible live state, not a privileged surface). The bundle-carried-endpoint alternative was rejected as premature: it adds an enrollment-time field for a derivation the hostname already encodes, and the flag/env override covers the rare split-hostname deployment without baking it into every bundle. Phases B and C did not need this; the picker consumes it when built.
2. **Audit-tail flow data in the picker.** Showing live event rate per topic in the roster wants the same audit-tail access that gates standalone `goat narrate` / `goat replay` (the open D.D substrate-shape decision in `goat-cli.md`: MinIO creds in bundle vs portal proxy vs presigned URL). The picker can ship without rate (declared/live membership alone is the high-value signal); rate is an enhancement that rides whatever D.D resolves.
3. **Terminal capability floor.** ratatui + crossterm assume a reasonably capable terminal. The minimalism doctrine's "usable on first byte" is satisfied by the line-oriented commands, which remain; the TUI is the richer opt-in. No degraded-TUI tier is planned — a constrained terminal uses the line-oriented path.
4. **Lens count.** Diagnose and Tail are the two dynamic surfaces. If a future inspection sibling adds a third dynamic question (cf. ADR 1019's revisit trigger on a panoscope-CLI request), whether it becomes a third lens or stays line-only is deferred — the two-lens model is not a hard cap, but adding a lens is a real scope decision, not a free addition.

## Living document

Updates land via PR. Phases A–C have shipped (Diagnose + Tail lenses built); the next material revision target is operator live-fire on nova-a — driving both lenses against real fabric state — which gates the Phase D picker alongside operator demand for the unified browse.
