# Extracting onboarding out of the data-fabric portal

**Status:** planned 2026-07-08; the ZAGROS runner + front-desk cutover is **LIVE and
verified 2026-07-12** (see below). Supersedes the "console-UI + portal-engine" split in
[`onboarding-console-consolidation.md`](onboarding-console-consolidation.md) (that was
step 1; this pulls the *engine* into the identity plane).

## Live status on ZAGROS (verified 2026-07-12)

The candidate-facing cutover is **done and live** — the console owns the onboarding engine
on ZAGROS. Verified against the running fleet (not inferred):

- **Runner** (`onboard-runner`, image `sha-50b7f68`) runs with
  `--onboard=http://goat-id-console:8080` (the approved-queue / claim / result contract loop)
  and `--portal=http://goat-zenoh-portal:8090` kept **only** for the cert-mint leg
  (`POST /api/orgs`). `PORTAL_URL` in its env is that cert-mint leg + the goat profile
  endpoint — **not** the onboarding-engine target.
- **Front-desk collector** (`onboard-intake-collector`, `intake.zagros.example`) forwards
  the candidate paths to the console's `/public/onboard/*`; the portal is out of the candidate
  path.
- **Deployed console** `sha-a8644ac` = the commit of #1539 — it carries the
  coalition/sector/github capture + repo-grant hook. Env has the matching runner key
  (`ONBOARD_OPERATOR_API_KEYS`), `GITHUB_ONBOARD_TOKEN`, and `PORTAL_INTERNAL_URL` for the
  ADR 1071/1072 read-back.
- **Proven end-to-end:** a live submit through the public front desk
  (`POST intake.zagros.example/api/onboard/submit`) produced a console-store proposal whose
  manifest carried `identity.github`, `affiliation.coalition`, and `affiliation.sector` — the
  exact fields a portal-path submit drops. Test proposal rejected + single-use invite consumed
  (no identity minted, no collaborator added). Repo-collaborator grant leg is wired
  (`GITHUB_ONBOARD_TOKEN` + mounted `repo-access.yaml` + unit-tested); a full live-fire of the
  provision-time grant is deferred (needs a real mint + real collaborator add).

**Two follow-ons remain** (both already sequenced under "Sequenced follow-ons" below); neither
is the candidate cutover, which is done:

1. **`goat admin invite` / `queue` CLI re-point** — the CLI's onboard client now supports a
   distinct onboard endpoint (goat-cli **0.4.21**): the `PortalClient` splits `/api/onboard/*`
   onto an `onboard_base_url` (console) while `/api/orgs` cert-mint + whoami + access stay on
   the portal `base_url`. Resolution order: `GOAT_ONBOARD_ENDPOINT` env → profile.toml
   `onboard_endpoint` → (absent) the portal endpoint (back-compat; old profiles unchanged).
   **The code is landed; the reachability route is BUILT (2026-07-13); the release is the
   remaining deploy step:**
   - **Reachability route — BUILT.** The console's bearer-gated machine realm (`/api/onboard/*`)
     is not reachable from an operator laptop through `admin.auth.<zone>` (oauth2-proxy
     302-redirects a bearer to login). Added a **mesh-only Traefik router**
     `id-admin-console-onboard` (`ops/ansible/templates/traefik-acme/mgmt-dynamic.yml.j2`):
     `Host(onboard.<site>.<zone>) && PathPrefix(/api/onboard/)` → straight to
     `goat-id-console:8080` (no oauth2 sidecar; the console's `onboardAPIGate` is the auth
     boundary), mesh-only-allowlist, its own LE cert (mirrors `admin.auth`). The `PathPrefix`
     confines this host to the machine realm; the human web surfaces stay only behind
     `admin.auth.*`. DNS: `onboard.<site>` added to `dns_extra_hosts` (zagros group_vars) →
     regenerated mesh-hosts (drift-check green). **`onboard_endpoint` = `https://onboard.<site>.<zone>`**
     (e.g. `https://onboard.zagros.netbird.zagros.example`).
   - **Deploy + release — DONE + LIVE-VERIFIED (2026-07-13, ZAGROS).** The Traefik router +
     CoreDNS record are deployed (`traefik-acme.yml -e target=zagros_hosts
     -e public_mgmt_fqdn=netbird.zagros.example` for the router; `goat-coredns.yml
     -l <mgmt-host>` for the record — the play-var-shadow gotcha requires the fqdn
     override). `goat-cli-v0.4.21` is released; `get.zagros` serves 0.4.21. **Proven end-to-end:**
     `goat --context zagros admin invite` (0.4.21, with `onboard_endpoint` in the profile) minted
     an invite that landed in the **console** store (sponsor recorded, absent from the portal
     store) and revoked cleanly. The runner is unaffected (it already reaches the console
     container-to-container).
   - **Operator provisioning — turnkey for new bundles (goat-cli 0.4.22).** The bundle now
     carries `onboard_endpoint` end-to-end: the Go enrollment `GoatCLIExtras` +
     `bundle-create --goat-cli-onboard-endpoint` (sourced from the portal's
     `--onboard-public-url` / `PORTAL_ONBOARD_URL`, set per-site from `portal_onboard_url`),
     the Rust `bundle.rs` (optional field) + `cmd/profile.rs` (writes the line when present).
     So `goat profile init` from a freshly-minted bundle writes
     `onboard_endpoint = "https://onboard.<site>.<zone>"` automatically; a goatnet without the
     onboard route simply omits it (portal-fallback, back-compat). **Existing** profiles
     (minted before 0.4.22) still add the one line once, or `GOAT_ONBOARD_ENDPOINT`.
2. **Portal strip (slice 5)** — delete the portal's now-inert onboarding code.

## Decision

Onboarding is an **identity-plane** workflow (it creates people/participants). It has
lived in **`goat-zenoh-portal`** (the **data-fabric portal** — topics, schemas, Zenoh/org
registration; *not* "the vendor portal", we're org-centric now per ADR 1052) only for
historical reasons. Move the **onboarding workflow** into the identity plane; leave the
**mint** where it correctly is (the cross-plane runner); the data-fabric portal keeps only
its **Zenoh-cert/slot leg**, called by the runner as one mint step.

The tell that the workflow is mis-homed: the `X-Goat-Operator-Actor` attribution header. It
only exists because the *authenticator* (console) and the *decision-recorder* (portal) are
different services. When the console owns the store, it records "jane approved this"
natively — the header + shared-key trust-boundary go away.

## Target shape

- **`goat-id-console` owns the onboarding workflow**: the proposal store, the invite
  lifecycle (mint / one-time token / consume→proposal / expire / revoke), the review + decide
  API, and **attribution (native** — from its oauth2-proxy identity, no header). It needs a
  **persistent store** (SQLite, mirroring the portal's `onboard_proposals`/`onboard_invites`;
  the console's current invite store is in-memory).
- **The runner** owns the mint orchestration — unchanged, but re-pointed at the console's
  `/api/onboard/*` (claim/result contract identical).
- **The data-fabric portal** sheds all onboarding — its `internal/api/onboard_*`, the
  `onboard_proposals`/`onboard_invites` tables, `sponsorOf`, the assembler — keeping only
  `POST /api/orgs` (the Zenoh cert/slot mint) that the runner already calls.

## The wrinkle: two surfaces, one store

Onboarding has an **admin** surface (mint invite, review, approve — ADMINS-gated) *and* a
**public candidate** surface (preview + submit the form — unauthenticated, one-time-token-
gated, on the public internet via the `intake.<zone>` front desk). Both hit the same store.
The console is ADMINS-gated by oauth2-proxy, so the public endpoints need a **carve-out**:

- Console serves `POST /public/onboard/preview` + `/public/onboard/submit` **without** the
  oauth2-proxy ADMINS gate (token-gated in-app, exactly as the portal does today).
- The **front-desk collector** (`intake.zagros.example`) re-points from the portal to the
  console's public paths (still only those two paths allow-listed; everything else 404s).
- Traefik routes the console's public paths **around** `oauth2-proxy-id-console`; the admin
  paths stay behind it. (Same "one service, two fences" pattern the portal + collector use.)

## Slices

1. **Console persistent store** — a SQLite `store` package in the console mirroring the
   portal's `onboard_invites` + `onboard_proposals` (schema, `ConsumeInvite` callback,
   proposal lifecycle). Replaces the in-memory invite store.
2. **Move the invite mint + candidate preview/submit + review/decide** into the console,
   recording attribution natively (drop the header path on the console side).
3. **Public surface + collector re-point** — console public endpoints + oauth2-proxy
   carve-out + point `onboard-intake-collector` at the console.
4. **Re-point the runner** at the console's `/api/onboard/*` (env change).
5. **Strip onboarding from the portal** — **DONE in code (2026-07-12).** Deleted the
   `onboard_*.go` handlers + assembler, the `internal/onboardlint` copy, the registry
   `invites.go`/`proposals.go` store (+ the `onboard_invites`/`onboard_proposals` tables and
   their column migrations), the `/api/onboard/*` route registrations, and the portal's
   onboarding web pages (the still-live ones became "moved to the console" redirect stubs,
   matching the ones already migrated). Kept `POST /api/orgs` + the ADR 1071 sponsorship
   annotation (`setVendorSponsorship`) + `GET /api/whoami`. The shared tier-ordering helpers
   (`intakeHints`/`tierRank`) + `parseIntakeHints` + the `slug` helper (used by trashgoat)
   moved to a surviving `internal/api/intake_hints.go`; shared test helpers to
   `helpers_test.go`. `go build`/`go vet`/`go test ./...` all green. **Deploy ordering:** ship
   the CLI re-point (goat-cli 0.4.21 + its console onboard route + `get.<env>` pin) FIRST, then
   deploy the stripped portal — a pre-0.4.21 CLI mint against the portal now 404s (fails loud)
   instead of silently orphaning, but operators on the old CLI should be moved first. The
   handlers were already inert (collector + runner target the console), so there was no
   dual-write drift — only dead code, now removed.
6. **Cutover** — sandbox ZAGROS has a handful of throwaway proposals; **no data migration**
   (re-mint if needed). Big-bang: deploy console (store + engine + volume) → re-point
   collector + runner → verify onboarding end-to-end console-owned → then slice 5 (strip
   the portal's now-dead onboarding code).
   **Live-cutover correction (2026-07-09):** the no-migration assumption did not survive
   contact — a real sponsor pipeline existed pre-cutover (submitted candidates, proposed
   proposals, still-open invite links in candidates' hands). The portal store's
   `onboard_proposals` + `onboard_invites` were copied wholesale into the console store
   on-host (schemas are identical; `INSERT OR IGNORE` keyed on id). Any future goatnet cutover should treat this copy as a standard cutover step,
   not an exception.

### Sequenced follow-ons (recorded, not dropped)

- **`goat admin invite` / `goat admin queue` CLI re-point** — these go through
  `panoscope-core`'s portal client at the profile's portal endpoint. They need a new
  onboard endpoint (separate from the portal endpoint, back-compat-defaulting to it),
  a goat-cli release, and a `get.*` pin — its own release cycle. Until it ships,
  operators mint/review via the **web console** (native, works today); the runner +
  public candidate form + web admin cover the functional cutover. A CLI mint against
  the *old* portal endpoint would orphan the proposal in the portal store (nothing
  reads it), so the CLI re-point is the first follow-on after the cutover.
- **Portal strip (slice 5)** — the second follow-on, per the note above.

## Resolved design (2026-07-08): auth realms + the ADR 1071 coupling

Reading the portal handlers during slice 2b surfaced two things the plan above
under-specified. The portal's onboard routes span **three auth realms** and the
mint path couples to the portal's **vendors registry** — neither survives a naive
"move the handlers" cutover, so they are resolved here before the build.

**The three realms (portal today → console):**

| Portal routes | Portal auth | Console realm |
| --- | --- | --- |
| `preview`, `submit` | none (invite-token-gated in-app) | **public** — no oauth2-proxy gate, token-gated in-app; collector re-points here |
| `proposals` list/get, `approve`, `reject`, `onboarded`, `claim`, `result`, `invites` list/get/revoke | `requireOperator` (operator key) | **machine token** — a single bearer `ONBOARD_API_TOKEN` (the runner + `goat admin invite` CLI carry it), operator-equivalent |
| `invite` mint, `sponsor-approve/reject`, `intake`, `lint` | `AuthMiddleware(true)` (any portal key) | folded into the two realms above: the **admin web** surface (oauth2-proxy ADMINS, operator-equivalent) for humans, the machine-token for the CLI |

So the console grows a **portal-key auth path** alongside its oauth2-proxy human
realm — the full model, not a shortcut. The runner and `goat admin invite`
present their **per-handle** portal keys (F-207 attribution preserved, not a
single shared token); humans present oauth2-proxy ADMINS. The console verifies a
presented key + resolves its principal (operator vs org, and the org's ADR 1071
sponsorship) by a read-only call to the portal — the identity/key registry stays
the SoT in the portal; the console reads it, it does not fork it.

**ADR 1071 sponsorship (the vendors-registry coupling):** `handleMintInvite`
resolves the minting sponsor's `sponsor_authorized` / `sponsor_max_tier` from the
portal's `vendors` row and caps the invite's tier. That registry **stays in the
portal** (it's an identity attribute of the org/slot registry, not onboarding
state). Resolution:

- An **operator-equivalent** minter (oauth2-proxy admin, or an operator-tier
  portal key) is sponsorship-authorized by definition with no tier cap — exactly
  ADR 1071's "operators sponsor by default". No portal call needed.
- The **participant self-service mint** — a non-admin org authenticating with its
  *own* portal key and minting up to its annotated cap — is **fully ported**, not
  deferred: the console verifies the presented key + reads the org's
  `sponsor_authorized` / `sponsor_max_tier` from the portal
  (`GET /api/orgs/{prefix}`, read-only) and caps the invite's tier by the same
  ceiling the portal did. ADR 1071 (live on ZAGROS since 2026-07-08) does not
  regress. The vendors registry stays the sponsorship SoT in the portal; the
  console reads it.

The read-only console→portal lookup is the *only* residual coupling, and it is
correct: it reflects the real bounded contexts (identity/key registry = portal,
onboarding workflow = console), not leftover debt.

**Extended by ADR 1072 (sponsor-scoped console access):** the same coupling
grew a second read-only resolve — `GET /api/orgs/sponsorship?email=` (operator
bearer or the console↔portal service secret) — so a non-ADMINS human session
can be resolved to its sponsorship annotation, and whoami now carries the
identity's `alias` + bound `sponsor_email`. With those, the console scopes
non-escalated sponsors (both realms) to their OWN invitees at their tier cap,
and the machine realm's admin-stage actions refuse participant keys. See
[`three-stage-candidate-onboarding.md`](three-stage-candidate-onboarding.md)
§"Sponsor-scoped console access".

**Sponsor display name.** A participant sponsor's machine identity is its UUID
org anchor (the whoami `vendor` field) — correct for provenance and ADR 1072
sponsor-scoping, unreadable on a review card ("sponsored by 0ff404dd…"). The
whoami response therefore also carries the org row's `display_name` (in
slot-pool mode the onboarded system handle); the console
resolves it once at invite mint (falling back to the live slot `alias` when
the row has no display name) and carries it invite → proposal as a
display-only `sponsor_name` column beside the unchanged machine `sponsor`
value. Review cards render the name with the truncated anchor subdued; the
candidate page and `identity.added_by` prefer it too. Rows minted before the
column existed have `sponsor_name=''` and keep rendering the raw anchor
(annotated) — no backfill.

**Approve is now fully automated in the console.** The portal's `approve` was
semi-automated (returned YAML + a `goat admin onboard` command for the operator
to run) because the distroless portal has no mint credentials. The console's
approve just records `approved`; the **runner** (which already auto-mints on
approve) claims and mints. The emitted-command/YAML response is dropped.

## Naming cleanup (fold in)

`goat-zenoh-portal` is the **data-fabric portal**, not "vendor portal". Sweep the docs +
UI strings touched here; the `/api/vendors`→`/api/orgs` machine rename is the separate
ADR 1052 track (don't rename the cert URI-SAN / taxonomy enum).
