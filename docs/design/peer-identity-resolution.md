# Peer ↔ person identity resolution

**Status:** Option B **deployed + live-verified on ZAGROS 2026-07-08** — the portal name→person resolver (`GET /api/access/peer` + batch `POST /api/access/peers`) resolves a real peer to its owning Ory identity, and the Thor relay-health dashboard bakes the id into each card and renders an `identity & access →` deep-link to the id-admin-console person page. Peer cards whose names are not `peer-<handle>` (infra/synth names) stay unresolved, correctly. The Thor resolver reaches the portal app by docker-inspecting the container IP on the `mgmt` network (port `8090`, operator Bearer key) — the portal mesh hostname doesn't resolve from the Thor itself and the host-published `:8088` is the oauth2/ADMINS gate, not the app. **Option A (NetBird `user_id` stamp) — attempted, then found a dead-end by live-fire (F-237).** The fork patch is built + validated + deployed on ZAGROS, and it correctly stamps `peer.UserID` — but NetBird **validates `peer.UserID` against its own `users` table**, and ZAGROS has zero person-scoped NetBird users (all `issued=api` service accounts), so a peer bound to a Kratos identity id fails to sync (`user … not found`). Binding an external identity id therefore *breaks* enrollment. The onboarding wiring was reverted before merge; the merged patch (#1437) stays as a harmless, default-off capability usable only with a real NetBird user id. **Option B is the resolution mechanism** — it needs no NetBird user and covers all peers. Companion to [data-access-cop.md](data-access-cop.md) §"Entry from the relay-health baseball card" and relay-observability.md B.4.

**Second consumer, 2026-08-14: netvis.** The reachability surface now carries an
ownership lens on its peer and pair views — see netvis.md §"Who is
responsible for this machine". It calls the same batch endpoint through
`panoscope_core::peerowner`, the **Rust** client for this resolver (the Thor
side is Go). Two things that module adds, and that any future consumer should
take rather than re-derive:

- **The four-state attribution is a type, not a convention.** `registry` (the
  recorded linkage) / `name` (handle parsed, no live identity) / `unmatched`
  (the name carries no principal) / `unread` (we could not ask). The resolver's
  wire format distinguishes the middle two only in prose — `resolved: false`
  covers both, separated by whether `handle` is present — so every consumer
  that reads this API raw has to rediscover that. It also has no `unread` at
  all, because a transport failure is the caller's to name; collapsing it into
  "not resolved" reports a failure to measure as a measurement.
- **A resolver failure may never take the page down.** `resolve()` never returns
  `Err`: it returns a row for every name asked about, with the transport error
  carried as that row's reason. Ownership is an enrichment on a surface whose
  actual job is reachability.

**Third consumer, 2026-08-17: the peerview surfaces**, and with them the
**Python client**, [`ops/identity/peerowner.py`](../../ops/identity/peerowner.py).
The endpoint was already shared; the *reading* of it was not — the Thor
resolver carried its own inline POST and its own partial reading, panoscope_core
carried the typed one, and peerview was about to be a fourth. There is now one
client per language (Go on the card, `panoscope_core::peerowner` in Rust, this in
Python) and the Thor resolver has been moved onto it, so the four-state
attribution is derived once per language rather than once per consumer. The
Python client additionally **drops the person's email and display name by
default**: the endpoint is operator-scoped and returns both, and a surface that
renders handles must not leak a roster of email addresses into a served json by
forgetting to strip a field.

### Two gaps this resolver has, found by the third consumer

Both are cases where the resolver's answer is correct and cannot be acted on.
peerview supplies them locally today and says which source it used; they belong
here, so that every consumer gets them.

1. **A machine resolves to the machine.** A peer minted for a system resolves to
   that system's handle, which can never answer for anything. The person who can
   is its **custodian** — the person→system custody edge ([ADR 1096](../adr/1096-delegated-machine-enrollment.md)).

   **For machines the portal enrolled, the resolver answers this itself** (code
   written 2026-09-11; live once the portal is redeployed). A
   `machine-<handle>[-<hostname>]` name is matched against the registry's
   enrolled machines (`access.MatchMachineHandle`, longest handle wins), `handle`
   names the custodian, and a `machine` block (`system_handle`, `slot_id`,
   `node_class`) marks it as a custodian rather than an owner. Custodian → roster
   member uses the recorded linkage first (the custodian slot's `identity_id` →
   Ory identity → email-hash), then the slot alias, then a handle-valued
   custodian column. With no member found the row carries `machine` and a
   `reason` but no `handle`.

   Two parts stay open. Systems declared in operator manifests rather than
   enrolled through the portal still resolve to the machine; peerview re-points
   those locally. And `panoscope_core::peerowner` and the Go relay-health card
   read `handle` without `machine`, so they show a machine's custodian with no
   custodian label until they read the block.
2. **Substrate has no answer at all, forever.** The mgmt host, the relays, a
   kiosk box: no onboarding name, so `unmatched` is correct and permanent. The
   activity log does record the enrolment, but for a setup-key enrolment **the
   initiator is the key** (`opEvent.InitiatorID = sk.Id`, netbird
   `management/server/peer.go`), so a shared key names nobody — a lead, not an
   owner. peerview takes a declared `<peer-glob>: <handle>` map from config as a
   fallback that never overrides a resolved answer. A declared-owner tier
   belongs in the registry rather than in one surface's config.

## The problem

Surfaces that hold a **NetBird peer** (the relay-health baseball card, `goat who`, panoscope) want to resolve it to the **person** who owns it — their Ory identity, credentialing, roles, affiliations, permissions. There was no obvious link, and the natural key is empty.

## The finding — `user_id` is empty *by NetBird design*, and the link lives in the name

- **`peer.user_id` is blank for every setup-key-enrolled peer.** This is not a bug: NetBird only stamps `peer.UserID` when a peer is added by an authenticated *user* over JWT (the `addedByUser` gate, `netbird management/server/peer.go:701` + `:562`). Setup-key (non-interactive) enrollment never sets it. A setup-key-enrolled peer therefore has `user_id=""`.
- **The real link is the deterministic peer NAME.** Onboarding names peers `peer-<handle>[-<short_tag>]-<site>-<date>` from `manifest.identity.handle` at mint time (`operator-onboard.yml`, `auto_peer_name`); the handle is schema-validated kebab-case (`operator-manifest.schema.json`). The manifest (`ops/operators/<env>/<handle>.yml`) carries `handle → email → name → org`. So **peer → person = parse the handle from the name → registry lookup → email → Ory identity**. Deterministic, not fuzzy.
- **peer → org is already resolvable in the portal** (`matchPeerVendor`, `access.go`; `who?id=<peer-name>` already accepts a peer name). What is *missing* is **peer → the person's Ory identity id** (needed to deep-link the id-admin-console person page) and a **recorded, queryable** index so this doesn't depend on re-deriving the join each time.

## Option B (now) — formalize the name → registry resolution

Make the person↔peer link **explicit, recorded, and queryable** without a NetBird fork. Works for existing peers (their names already encode the handle) and all forward ones.

1. **Record the link at onboard.** Manifest schema gains `netbird.setup_key_ids: {<site>: "<key-id>"}` and `netbird.auto_peer_names: {<site>: "peer-…"}`; the orchestrator writes the minted key id + computed peer name back into the manifest after Phase 1. This turns the fire-and-forget key mint into a recorded edge (audit + resolution).
2. **A resolver.** `GET /api/access/peer?netbird_name=<name>` (or `?netbird_id=`) → `{handle, org, person: {email, name, ory_identity_id}, deep_links: {id_console, org_access}}`. It parses the handle from the name, looks the person up in the registry/roster (by handle/email), resolves the Ory id via the existing `kratosadmin` reader (email→id), and returns the deep-link targets. Operator-scoped, reuses the readers the access graph already assembles.
3. **The deep-link consumes it.** The relay-health baseball card's "identity & access →" link (mesh-only, admin-gated) targets the id-admin-console person page (`/users/<ory_identity_id>`) when the person resolves, else the org access view (always resolvable). Resolution is baked server-side (the Thor resolver), so the browser never needs a cross-service authed call.

**Invariants:** the name→handle convention is the contract (validated at onboard); a peer name that doesn't parse fails loud (no silent mis-attribution); the resolver is read-only + operator-scoped; nothing here touches enforcement.

**The access graph consumes the same link (2026-07).** `access.MatchPeerHandle` (the resolver's longest-handle-prefix match, moved into the pure graph package) now also runs inside `Assemble`: every mesh peer whose name matches a declared member handle gets an `is_on_mesh` edge to that person's reconciled node, and the person's `on_mesh`/`connected`/`last_seen` roll up from their own devices — org membership no longer required, which closes the "internal person with no org shows `on mesh: no` while demonstrably connected" gap. `who` responses carry `mesh_attribution` (`peer`/`org`/`none`) so renderers can be honest about can't-know vs known-absent (see [data-access-cop.md](data-access-cop.md) §"The person↔peer custody edge"). Note the manifest write-back fields from step 1 (`netbird.auto_peer_names`) are still absent from the live manifest set — the graph join deliberately derives the prefix from the handle (the convention) rather than requiring the recorded field.

**The convention is single-sourced + fenced (2026-08).** Because the name IS the link, its grammar became load-bearing — and by the 2026-08-01 identity fragility audit §1.2 it had five independent implementations across Go, Rust and the onboard playbook with no fence between them. There is now one machine source, [`ops/identity/peer-name-grammar.yaml`](../../ops/identity/peer-name-grammar.yaml), generated by `ops/identity/generate-peer-name.py` into `internal/peername` (Go) and `panoscope_core::peername` (Rust). `ops/ci/check-peer-name-grammar-sync.sh` fails the build when a generated consumer drifts, and asserts the Ansible playbook still templates the fields in the SoT's order; the SoT's conformance vectors are generated into a table-driven test in **both** languages, so a behavioural divergence between the minter (goat-cli) and the parsers (the portal) fails a test, not only a byte compare.

Two rules with different strictness come out of that SoT, and which one a caller uses is a safety decision:

- `match_longest_key` — the loose join behind the person and org attributions. Its stated limit is that the field separator also occurs *inside* handles, so `alex` does match `peer-alex-smith-…` when `alex-smith` is not itself among the candidates; pass the full candidate set and let longest-wins disambiguate.
- `belongs_to` — anchored on handle + optional org tag + site. This is the one a **cut** may use: goat-cli's native revoke selects peers with it, which is what stopped the 2026-07-14 org-mate mis-attribution.

## Option A (attempted — dead-end, see F-237) — NetBird `user_id` stamp for the DB-layer link

The idea was to close the DB-layer gap for **forward** peers so `SELECT … WHERE user_id = ?` works without name parsing, by minting **user-bound** setup keys that stamp `peer.UserID` = the owner's Ory identity id.

**What was built + deployed:**

1. **NetBird fork — built + deployed (`ops/netbird/patches/setupkey-user-id-stamp.patch`, #1437).** Adds optional `UserID` on the `SetupKey` type; a user-bound key stamps its owner on `peer.UserID` at `AddPeer`. Mirrors the AutoPeerName wiring exactly (struct field + `Copy` + `GenerateSetupKey`/`CreateSetupKey` param + `api.CreateSetupKeyRequest.user_id` + `handleSetupKeyAddedPeer`→`peerAddConfig.SetupKeyUserID`→`AddPeer`); does NOT set `addedByUser`. Empty `user_id` = upstream behavior (additive/default-off). Applied 4th in `ops/netbird/Dockerfile`; deployed on ZAGROS (`:latest`, `setup_keys.user_id` column added by AutoMigrate). Verified live: a `POST /api/setup-keys` with `user_id` persists it, and enrollment stamps `peer.UserID`. **The patch is correct.**

**Why it's a dead-end (live-fire, F-237):** NetBird **validates `peer.UserID` against its own `users` table** for the network-map/sync path. ZAGROS has zero person-scoped NetBird users (all `issued=api` service accounts); setup-key participants have no NetBird user at all. So a peer bound to a Kratos identity id fails to sync (`rpc … user: <id> not found`) — binding an external id *breaks* enrollment, worse than a no-op. The onboarding wiring that would have set it (resolve-by-email at mint) was **reverted before merge**. The merged patch stays as a harmless capability usable **only** with a real NetBird user id — it must never be fed an external identity id.

**Resolution: Option B is the mechanism.** It needs no NetBird user, covers all peers by name, and is live. Making `peer.UserID` viable would require *creating* a NetBird user per person purely to populate an attribution column — not worth it when B resolves cleanly. Existing peers (and forward ones) stay name+registry-linked via B.
