# Onboard / rotate / revoke — fast & seamless

Status: proposed (design). Anchors a multi-phase track to make operator credential
lifecycle operations fast (seconds, not minutes) and seamless (no silent partials,
no manual server-side cleanup, idempotent and repairable).

Supersedes nothing; sharpens the implementation of [ADR 1031](../adr/1031-credential-revocation-mesh-first-offboarding.md)
(revocation), [ADR 1044](../adr/1044-credential-rotation-in-place-reissue.md) (rotation),
[ADR 1019](../adr/1019-goat-cli-unified-operator-binary.md) (`goat` CLI). Related design:
`operator-onboard-utilities.md`,
`credential-revocation-and-offboarding.md`,
`credential-rotation.md`.

## BLUF

Today `goat admin onboard|rotate|--revoke` is a thin wrapper over a ~90-task Ansible
playbook (`ops/ansible/operator-onboard.yml`) that imperatively pushes to four identity
surfaces (NetBird API, Ory Kratos via SSH-delegate to the site Thor, the Zenoh portal,
the offline CA) and renders a bundle. That shape is slow (minutes), failure-prone, and —
worst — **fails silently into half-provisioned state**. This track fixes it in three phases:
reliability hardening (P1, ship first), a native fast path (P2), and declarative
reconcilers (P3).

## Motivation — what actually broke

A routine onboard took about an hour. Every failure was a real,
reproducible defect in the pipeline, not operator error:

1. **Silent partial bundles.** When operator env tokens weren't loaded (direnv does not
   populate a non-interactive shell) or a surface's `when:` guard evaluated false, the
   Ory and portal phases **skipped without error**. The run reported `failed=0` and wrote
   a bundle with `Provisioning gap` / `(not in this bundle)` in place of the login password
   and portal api_key. A silent partial is the worst failure mode — it looks done.

2. **All-or-nothing bundle render.** Each surface only renders into the bundle when its
   phase *freshly creates* it (e.g. the Ory password renders only on `needs_create`; the
   portal key only on a `201`). So a partial **cannot be repaired by re-running** — a
   re-run skips the already-existing surfaces and re-emits the gap. The only recovery is to
   tear down server-side state and re-mint every surface in one clean run.

3. **Revoke doesn't actually clear server state.** `Phase R4.4` does
   `DELETE /api/vendors/{handle}`, but the portal keys deletes on the **uuid anchor**, so
   the delete `404`s (accepted as success) and the vendor row survives — then the next
   onboard `409`s ("already registered, api_key not regenerable"). `Phase R2.3` (Kratos
   identity delete) **skipped entirely** because its lookup came back empty during a Thor
   mesh flap. Net: revoke reported success while leaving both Ory and portal state behind.

4. **No timeouts; Thor-delegated tasks hang.** Ory and revoke phases `delegate_to` the
   site Thor over SSH (observed 475–1200 ms, flapping) with no timeout. A revoke sat >2 min
   with no output; piping through `tail` (which buffers to EOF) made it look hung and it
   was killed prematurely. Slow + opaque + hang-prone.

5. **Affiliation-trait cutover wedge.** `Phase 2.5` injects an `affiliation` trait into the
   Kratos identity-create whenever `manifest.affiliation` is defined. The live ZAGROS Kratos
   schema does not yet accept that trait, so the create returns **HTTP 400** — blocking the
   Ory login half of **every** post-cutover ZAGROS onboard. Worked around per-mint by
   suppressing the injection (`GOAT_GRAMMAR_CUTOVER` override + commenting the manifest
   block); the durable fix is to deploy the ZAGROS Kratos schema migration or make the
   injection schema-conditional. (A bcrypt/passlib warning under Python 3.14 was a
   red herring — `password_hash('bcrypt')` still produces a valid hash.)

6. **The CLI didn't streamline anything.** `goat admin onboard` shells out to the same
   90-task playbook, so it inherited all of the above. The expected win from ADR 1019
   ("unified operator binary") is unrealized for the lifecycle operations.

These are the acceptance tests for this track: each must become impossible.

## Phase 1 — Reliability hardening (ship first, low-risk, in the existing playbook)

Goal: the existing pipeline never silent-partials, never hangs, never orphans state.

- **P1.1 Bundle-completeness gate. [BUILT]** A final phase (5.5) that asserts every
  *enabled* surface actually provisioned (no `Provisioning gap` / `not in this bundle` /
  surface-specific `not provisioned` marker in the rendered bundle) and **fails loudly**
  otherwise. Converts silent partials (#1) into hard failures.
- **P1.2 Fail-fast preflight. [BUILT]** The pre-flight already asserts the portal key +
  `/api/health`; a new pre-flight step TCP-probes each enabled Ory-site Thor's SSH port (15s) and
  refuses early with a `check netbird status` message before any surface is touched.
- **P1.3 Revoke clears state by anchor. [BUILT]** *P1.3b (portal, Go):* `deleteVendor`
  resolves a live handle→anchor (`Registry.AnchorForHandle`) so DELETE accepts the same
  identifier the onboard issued — fixing the orphaned-org-row 409 (#3); test
  `TestSlotOnboard_DeleteByHandleResolvesAnchor`. *P1.3a (playbook):* new Phase R4.6
  GETs `/api/aliases` after the delete and **fails loudly** if the revoked handle's alias
  survived (the slot wasn't freed).
- **P1.4 Timeouts on delegated tasks. [BUILT]** `ansible_ssh_common_args`
  (ConnectTimeout=15 + ServerAlive keepalives + BatchMode) on both the enroll and revoke
  Kratos-delegate `add_host`, so a flapping Thor fails fast instead of hanging (#4).
- **P1.5 `--repair`. [BUILT]** `goat admin onboard <handle> --repair` re-mints a partial
  bundle by sequencing the two already-hardened legs: a **revoke leg** that frees every
  surface (clears the orphaned org row via P1.3b, cuts the peer, deletes the identity) but
  is passed `-e onboard_repair=true` so it leaves the manifest `status: active` (gates off
  the R5.3 flip), then an **enroll leg** that re-mints and renders every surface into one
  complete bundle (verified by the P1.1 gate). This is the supported answer to the
  all-or-nothing render (#2) and is exactly the recovery procedure: a clean free + a fresh mint. There is **no** dangerous mass-delete of *live*
  credentials — a partial bundle was never delivered, so the freed secrets were never in
  anyone's hands. Verified by dry-run (two legs with the right args) + `cargo
  check`/`clippy`/admin tests; ZAGROS live-fire operator-gated.

  *Refinement (Phase 2, not yet built):* the **in-place** variant that preserves the
  anchor/identity — `POST /api/vendors/{anchor}/rotate` for the portal key (rotate endpoint
  already exists, ADR 1044) and `PUT /admin/identities/{id}` to set a fresh Ory credential
  on the existing identity (standard Kratos admin). Repairs the bundle with **zero** slot/
  NetBird churn. Deferred because the sequenced revoke→enroll above already delivers a
  correct repair and the in-place variant belongs in the Phase 2 reconciler restructure
  (the playbook's imperative create-vs-skip can't yet express "exists-but-incomplete →
  reconcile" cleanly). This corrects an earlier wrong framing that repair *requires* a risky
  live-credential teardown — it does not.
- **P1.6 Auto-load operator env. [BUILT]** `goat admin onboard` runs the playbook under
  `direnv exec <repo_root>` when `DIRENV_DIR` is unset and direnv is on PATH, so a
  non-interactive invocation loads the operator tokens instead of silently skipping
  token-gated surfaces (#1 root). Interactive operators (direnv already active) are
  unchanged.
- **P1.7 Affiliation injection schema-guard. [BUILT]** The Kratos `aff` trait injection is
  gated behind `ory_site_affiliation_trait_enabled` (default **off**) — affiliation is
  authoritatively carried by the cert URI-SAN (ADR 1051) + registry, so the Ory trait is
  redundant, and the unmigrated ZAGROS schema rejected it with HTTP 400 (#5). Flip true
  per-env once that env's identity schema includes `aff`; the ZAGROS Kratos schema migration
  is the durable close.

## Phase 2 — Native fast path (the "fast")

Move orchestration out of the 90-task Ansible into native Rust in `goat admin
onboard|rotate|revoke`: direct API calls + offline cert mint + bundle render, with per-call
timeouts and real error handling. Target: the ~4–6 calls that actually change state,
completing in seconds. The CLI becomes the streamlined path ADR 1019 intended.

**The model is already proven.** `goat admin rotate` (`cmd/admin.rs::rotate_one`) is fully
native today — it calls `panoscope_core::portal::PortalClient::rotate_vendor()` → `POST
/api/orgs/{anchor}/rotate` with **no Ansible**, mapping `PortalError` to clean operator
messages. Phase 2 generalizes that pattern across the surfaces.

**Key-only reissue — `rotate --key-only` (goat#1193, BUILT).** `rotate` re-mints the
leaf cert *and* the portal key together. But the portal key and the mTLS cert are
independent credentials, and in practice the key is the one that breaks (lost, mistyped,
stale-pasted) while the cert is fine and the peer is still publishing. For that recurring
case there is a lighter sibling: `goat admin rotate <anchor> --key-only` reissues **only**
the portal key. It calls `PortalClient::rekey_vendor()` → `POST /api/orgs/{anchor}/rekey`
(operator-gated, org-only — a new machine surface, no `/api/vendors` twin, ADR 1052), which
swaps the stored key hash via the existing `UpdateVendorAPIKey` and skips `Mint` entirely. The cert, anchor,
alias, write namespace, and ACL are all untouched, so there is **no bundle to download and
no peer restart** — the output is just the new key to hand over. One consequence to hand over
with it: `certmint` bakes the portal key into the `.cbor` at mint time
(`--goat-cli-portal-token`), and rekey does not re-mint, so the peer's existing fabric bundle —
and any tarball URL still inside its 24 h window — keeps the OLD key. The new key is pasted
into the profile; re-running `goat profile init` from that bundle installs a dead one.
Anchor door only for now
(single peer; not the `--org` cascade, which `--key-only` refuses); an identity-keyed
console twin (`/api/orgs/rekey`) can follow the `rotateByIdentity` pattern if the console
needs it.

**Surface-by-surface native-ability (verified against the code):**

| Surface | Native path | Status |
|---|---|---|
| **Zenoh slot + cert + key** | `PortalClient` → `POST /api/orgs` (slot-onboard: allocate + mint + return `api_key` + `bundle_url`) — the same client `rotate` already uses | **BUILT (P2.1)** |
| **NetBird key + groups** | `NetbirdClient` → `GET /api/groups` (suffix→UUID) + `POST /api/setup-keys` — plain HTTP, reachable off-mesh | **BUILT (P2.2)** |
| **Read-model / status** | `GET /api/access/roster` + `/api/aliases` already assemble per-identity W/R + state + on-mesh in one call (goat-cli 0.4.9) | **already native** |
| **Ory identity + credential** | Kratos admin API is bound to `127.0.0.1:4434` on the site Thor, reached over a thin SSH shell-out (`ssh <thor> curl … --data-binary @-`, body piped via stdin) | **BUILT (P2.4)** |
| **Bundle render** | the `.md` + `.cbor` assembly the playbook does in Jinja | **BUILT (P2.3, by construction)** |

**First increment (P2.1) — BUILT in code.** A native Zenoh-surface onboard,
`goat admin onboard <handle> [--env <env>] --native`, that mints the slot+cert+key via
`PortalClient` and writes the bundle with no Ansible — the lowest-risk slice (mirrors the
proven `rotate` path) and the most common "add a publisher" case. Mechanics: the client gains
`PortalClient::onboard_org()` (the bundle-returning sibling of `create_vendor`, which now
delegates to it) + `download_bundle()`; the CLI reads the manifest's `zenoh.trial.org`
(legacy alias `zenoh.trial.vendor`), `POST`s `/api/orgs`, writes
`ops/operators/out/<handle>-bundle.md` (one-time credentials first, crash-insurance) then
fetches the cert tarball, and emits the `admin.onboard-native` operator-action audit.
**Zenoh surface only** — it warns (doesn't silently skip) when the manifest also enables
NetBird / Ory, pointing the operator at the Ansible path for those; `409 already-onboarded`
points at `rotate` / `--revoke`; a `200` partial-success fails loud (no silent partial).
`--native` is **opt-in**: the default `goat admin onboard` stays the hardened Ansible path
until ZAGROS live-fire proves parity. Unit-tested (manifest projection, bundle render, response
decode, disabled-client short-circuit). **Live-fired + verified on ZAGROS 2026-06-29** (see the
combined P2.1+P2.2 live-fire note below).

The native surface this introduces is **org-neutral** (`onboard_org`, `OnboardedOrg`, "publisher"
language — not "vendor"). The onboard *input* it reads was vendor-shaped; that is the catalogued
ADR 1052 manifest-input item (`vendor-to-org-machine-rename.md`
§2/§3/§6), which this track also closes with an additive `org` alias: the manifest schema accepts
`org.display_name` / `zenoh.trial.org` alongside the legacy `vendor:` keys (`#[serde(alias="vendor")]`
dual-read, `gen-roster.py` coordinated), so new manifests aren't authored vendor-shaped. The cert
URI-SAN scheme and the `affiliation.vendor` taxonomy enum are deliberately untouched.

**Second increment (P2.2) — BUILT in code.** `--native` now also mints the **NetBird mesh
setup key**: `panoscope_core::netbird::NetbirdClient` gains `list_groups()` + `create_setup_key()`
+ a pure `resolve_group_ids()` (expands the manifest's `auto_groups` *suffixes* to
`<site_prefix>-<suffix>` and looks up UUIDs, failing loud on any missing — mirroring the
playbook's Phase 1.3 assert). The CLI runs the **NetBird leg first** (so a mesh failure leaves
nothing Zenoh-side consumed and the onboard is cleanly retryable), then the Zenoh leg, then
writes one bundle with both a NetBird section (setup key + `netbird up` command) and the Zenoh
section. Per-goatnet NetBird config (mgmt URL / token env var / ACL `site_prefix`) is mirrored
from the playbook maps for nova + zagros; an env without native config defers the mesh leg to
Ansible with a clear note (never a silent gap). Now only **Ory** remains on the Ansible path —
the `--native` warn dropped NetBird. Unit-tested (group resolution, setup-key body shape, bundle
NetBird section, role-TTL + per-env config, manifest projection).

**P2.1 + P2.2 live-fire — proven end-to-end on ZAGROS 2026-06-29 (goat-cli v0.4.11).** A real
`goat admin onboard <synthetic> --native --env zagros` from the operator laptop minted, in one
command with no Ansible: a NetBird setup key (`valid`, one-off, group `ZAGROS-VENDOR` resolved
suffix→UUID), a Zenoh slot (anchor `989d0250…`, alias resolvable in the portal, URI-SAN
`urn:goat:zagros:org=…`, portal api_key), and a complete bundle — the cert tarball carrying the
leaf cert + key + CA root + the `.cbor` goat-cli profile + NOTES.md + example pub/sub. Both
surfaces were independently verified live (portal `/api/aliases`, NetBird `/api/setup-keys`),
then the synthetic state was torn down (slot freed via portal `DELETE /api/orgs/{anchor}` → 204;
setup-key revoke pending an NB-token refresh). This is the parity gate — `--native` can now be
promoted toward the default for the Zenoh + NetBird surfaces.

**P2.3 native bundle render — done by construction (confirmed in the live-fire).** The native
path already renders its own bundle `.md` (Zenoh + NetBird sections) and the portal mints the
`.cbor` profile + cert into the one-time tarball the CLI downloads (the CLI can't mint certs —
it holds no CA key — so portal-minting the tarball is correct, not a gap). The live-fire tarball
contained the full set, so there is no separate renderer left to build.

**Fourth increment (P2.4) — BUILT in code.** `--native` now also creates the **Ory Kratos login
identity**, closing the last surface. The Kratos *admin* API binds `127.0.0.1:4434` inside the
container on the site Thor — deliberately off the mesh/public because it edits any identity with
no auth of its own — so unlike the NetBird/Zenoh network token-APIs it must be reached *from* the
Thor. The native path owns a thin **SSH shell-out** (option 3, not a mesh proxy in front of the
most dangerous API): `ssh <thor> "curl … http://127.0.0.1:4434/… --data-binary @-"` with the
JSON body **piped via stdin**, so the email/password never touch an argv or process listing. It
mirrors the playbook's Phase 2: idempotent `GET /admin/identities?credentials_identifier=<email>`
(skip-create if present), `POST /admin/identities` (built with `serde_json`, which emits non-ASCII
as literal UTF-8 — the SQLSTATE-22P05 gotcha the Ansible path hand-serialized around is handled
for free), and an optional passkey `POST /admin/recovery/link` (one-shot 15-min). The password is
cleartext-in-the-body (Kratos hashes it; acceptable over SSH+localhost, avoids a bcrypt dep) and
renders into the bundle for OOB sharing. `aff` is **never** set (default-off; the ZAGROS schema
rejects it). Per-goatnet config (Thor SSH endpoint / Kratos admin URL / public `auth_url`) mirrors
`ory_site_kratos_admin_host_map` for nova + zagros; an env without native config defers the Ory
leg to Ansible with a clear note. **Ordering: Ory runs LAST** — a failure leaves the Zenoh/NetBird
bundle usable, surfaced loudly and marked `partial` in the operator-action audit, never silently
dropped. The `--native` skipped-surfaces warn now names only the Ory *operator-IdP* (still
Ansible-only). Unit-tested (identity-body JSON shape + non-ASCII-not-escaped, SSH argv assembly
carries no secrets, curl-status split, bundle Ory section, per-env config). All three surfaces
(Zenoh + NetBird + Ory) are now native. **Live-fired end-to-end on ZAGROS 2026-07-01 (goat-cli
v0.4.12):** the shipped binary from `get.zagros` ran a full three-surface notional onboard (NetBird
setup key + Zenoh slot with a real signed leaf cert + Ory Kratos identity with the UTF-8 name
preserved literally), each verified live, then revoked + removed clean.

**Fifth increment (0.4.13) — native is now the DEFAULT, groups written verbatim + enum-validated.**
Three changes after the P2.4 live-fire + a review of how identities are structured:
- **`--native` is the default for a plain enroll.** `goat admin onboard <handle>` takes the native
  fast path (~2.8 s measured, all three surfaces) unless the manifest isn't native-serviceable — a
  `native_eligibility()` peek falls back to the Ansible playbook, with a printed reason, for
  operator-IdP manifests / observer/prod Zenoh / an env with no native config. `--ansible` forces
  the legacy playbook; `--native` still forces native (hard-errors if ineligible); revoke/repair
  always use the playbook. This retires the 90-task / 6-SSH-hop / offline-CA-signing / grammar-wedged
  Ansible path (~20 min in practice) as the default for the common case.
- **Ory `groups` are written VERBATIM — no site-prefix.** The P2.4 draft prefixed `ZAGROS-<group>`
  (mirroring the Ansible Phase 2.5 `<SITE>-` map), but the Ory service-permission groups are
  self-describing names (`PLATFORM-ADMIN`, `IAM-ADMIN`, `OBS-ADMIN`, … — SoT
  `ops/permissions/group-permissions.yml`) that the dashboards / consoles / MinIO gate on directly;
  a prefixed `ZAGROS-IAM-ADMIN` matches no policy. This is the SERVICE-PERMISSION axis
  (`docs/design/onboard-tier-role-model.md`), distinct from NetBird `auto_groups` which ARE
  site-prefixed (the mesh-role axis). (The Ansible path has the same latent prefix bug — never
  exercised because admins were hand-seeded.)
- **`ory_site.groups` is enum-validated before minting.** Native was lenient (any string); it now
  reads the valid set from the manifest schema's `ory_site.groups` enum (CI-locked to the SoT by
  `check-ory-groups-enum.sh`) and fails fast on an unknown group, so a typo can't mint an identity
  whose claim matches no policy (an F-177-adjacent silent authz failure).

**Sixth increment (P2.5) — native REVOKE + batch, closing the last verb.** Enroll went native
in P2.1–P2.4, but **revoke stayed 100% Ansible** — every revoke fell to the same ~90-task
playbook (minutes per handle). A dormant-identity sweep hit this head-on: a large batch of handles
through the per-handle Ansible revoke projected to **~4–5 hours**, so it was hand-scripted in
Python instead (M-085). That
hand-script *was* the missing native path; this increment makes it a durable, tested CLI verb so
the fallback is never the only way again. `goat admin onboard <handle> --revoke` now takes the
native path by default (same eligibility peek + `--ansible` / `--native` overrides as enroll),
mirroring the playbook's revoke leg (Phases R1–R5) against the same surfaces with the same guards:
- **R1 NetBird peer cut** — `NetbirdClient::list_peers` (fetched once) → `delete_peer` for every
  peer named `peer-<handle>[-<tag>]-<env>-…`. The match is **handle-anchored** (`revoke_peer_matches`),
  so an org-mate's peer carrying a SHARED org tag mid-name is never falsely cut — the exact
  false-attribution bug the identity-keyed sweep first hit and had to correct.
- **R1.5 setup-key hygiene** — `list_setup_keys` + `revoke_setup_key` (new), best-effort (the peer
  DELETE is the authoritative disconnect; a revoked key only blocks new joins), so failure is
  tolerated. NetBird serializes an ungrouped key's `auto_groups` as explicit `null`; the decode
  null-tolerates it (else one such key failed the whole list).
- **R2 Ory identity** — `revoke_ory_identity` over the same SSH-to-Thor Kratos shell-out: GET by
  email → PATCH `state=inactive` (reversible default) or `--purge` DELETE (terminal). Absent = idempotent.
- **R4 Zenoh slot** — `PortalClient::delete_vendor`, **teammate-guarded** (R4.3): kept when another
  active manifest in the SAME env dir shares the `short_tag` (excluding self by handle — env-scoped
  + non-recursive, so a person's legacy flat manifest can't masquerade as a teammate), unless the
  org cascade forces deletion.
- **R5 manifest** — flip `status: active` → `revoked` in place (idempotent).

Each leg past the peer cut is best-effort and its outcome is **recorded + printed**
(`peers_cut=N ory=… zenoh=… manifest=…`), so a partial revoke that leaves a surface up is VISIBLE,
never silent. The operator-action audit emits `admin.revoke-native`
started→ok/partial. Two **batch** entry points generalize it, both fetching peers/keys ONCE:
`--org <id> --revoke` runs the native cascade over an affiliation's active members (force-deletes
the shared anchor); `--revoke --from <file>` revokes an arbitrary cohort (one handle per line) —
the generalized sweep tool, a per-handle failure reported not fatal. Unit-tested (handle-anchored
peer match incl. the org-mate + prefix-collision cases, env-scoped teammate census, idempotent
manifest flip, handle-list parse, null-tolerant setup-key decode). **Live-fired on ZAGROS 2026-07-14:**
an idempotent native re-revoke of an already-revoked handle ran green end-to-end in **~5 s**
(peers already gone → tolerated, Ory re-deactivated, Zenoh 404 → already-absent, manifest
already-revoked) — vs the ~2–3 min the Ansible path took for the same single revoke.

This is a large, multi-increment build (a critical-path rewrite); each increment ships +
tests independently, and the Ansible path stays the fallback (forced with `--ansible`, or
auto-selected when native doesn't apply).

## Phase 3 — Declarative reconcilers (the "seamless")

Roster (`ops/operators/<env>/*.yml` + the issuance registry) is the single source of truth;
adding/removing a person is one committed file. Per-domain reconcilers converge live state
toward the roster:

- **Zenoh** already works this way — the pre-cut slot pool ([ADR 1025](../adr/1025-zenoh-dynamic-tenant-authz-slot-pool.md))
  means the router picks up nothing per person; the cert *is* the authorization. This is the
  proof-of-pattern for the highest-value domain.
- **NetBird / Ory / portal** get reconcilers (or use native group-sync / SCIM-style
  provisioning) that diff declared-vs-actual and converge. This turns the declared-vs-actual
  read-models (Data-Access COP [ADR 1026](../adr/1026-data-access-common-operating-picture.md),
  denial observability [ADR 1022](../adr/1022-zenoh-denial-observability.md)) from monitors
  into control loops — "Reconciliation, not interception" (ADR 1025 first-principle) applied
  to issuance, not just enforcement.

## Built-vs-designed

- **Built (Phase 1 — complete):** P1.1 completeness gate, P1.2 Thor preflight, P1.3a/b
  revoke-by-anchor + post-revoke verify, P1.4 delegated-task timeouts, P1.5 `--repair`
  (sequenced revoke→enroll), P1.6 env auto-load, P1.7 affiliation schema-guard. Playbook
  syntax-check + portal `go build`/`vet`/tests + goat-cli `check`/`clippy`/admin tests +
  `--repair` dry-run all green; **live-fire on ZAGROS is operator-gated** (the next onboard
  exercises them end-to-end).
- **Remaining:** the in-place `--repair` refinement (anchor-preserving rotate/PUT — a Phase 2
  reconciler item); the ZAGROS Kratos schema migration (durable close for #5, after which
  P1.7's flag flips true on ZAGROS).
- **Built + live-fired (Phase 2 — P2.1):** native Zenoh-surface onboard (`goat admin onboard
  --native`): `PortalClient::onboard_org` + `download_bundle`, manifest-read + `POST /api/orgs` +
  bundle render + tarball fetch + operator-action audit. `cargo test`/`clippy -D warnings` green.
  **Live-fired + verified on ZAGROS 2026-06-29 (v0.4.11).**
- **Built + live-fired (Phase 2 — P2.2):** native NetBird mesh leg folded into `--native`:
  `NetbirdClient::{list_groups,create_setup_key}` + pure `resolve_group_ids`, NetBird-first
  ordering (retry-safe), bundle NetBird section, per-env config (nova/zagros). `--native` now
  provisions Zenoh + NetBird; only Ory stays on Ansible. `cargo test`/`clippy -D warnings` green.
  **Live-fired + verified on ZAGROS 2026-06-29 (v0.4.11)** — one `--native` command minted the
  NetBird key + Zenoh slot + full bundle, both surfaces verified live, synthetic state torn down.
- **Done by construction (Phase 2 — P2.3):** native bundle render — the CLI renders its own
  bundle `.md`; the portal mints the `.cbor`/cert into the downloaded tarball (correct: the CLI
  holds no CA key). Confirmed complete in the P2.1+P2.2 live-fire tarball. No separate build.
- **Built + live-fired (Phase 2 — P2.4):** native Ory Kratos login identity over the SSH-to-Thor
  shell-out; all three enroll surfaces native. `--native` is the default enroll. **Live-fired on
  ZAGROS 2026-07-01 (v0.4.12).**
- **Built + live-fired (Phase 2 — P2.5):** native REVOKE + batch — `--revoke` takes the native
  path by default (R1 peer cut / R1.5 key hygiene / R2 Ory deactivate-or-`--purge` / R4 guarded
  Zenoh delete / R5 manifest flip), plus `--org` native cascade + `--revoke --from <file>` batch.
  `NetbirdClient::{list_setup_keys,revoke_setup_key}` added; handle-anchored peer match + env-scoped
  teammate guard. `cargo test`/`clippy -D warnings` green. **Live-fired on ZAGROS 2026-07-14 — an
  idempotent re-revoke ran green in ~5 s** vs ~2–3 min for the Ansible single-handle revoke;
  motivated by M-085.
- **Designed, not built:** Phase 3 (declarative reconcilers).
- **Worked-around live:** the affiliation cutover wedge (per-mint override);
  P1.7 makes that the default so the override is no longer needed.
- **Already reconciler-shaped:** the Zenoh slot pool (P3's proof-of-pattern).

## Open questions

- Portal handle↔anchor mapping: is there a lookup-by-handle endpoint, or must the reconciler
  enumerate `/api/orgs` and match? (Drives P1.3.) The portal rename to `/api/orgs`
  (ADR 1052) is live; the delete route still takes the anchor.
- P2 scope boundary: which host-side steps genuinely need Ansible vs. can move to native Rust.
- ZAGROS Kratos schema migration owner + timing (durable close for #5).
