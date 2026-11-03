# Collab onboarding provisioner

The provisioning leg that turns the captured `collab.enabled` decision into a real
collaboration-environment account, folded into the standard onboarding engine as a
**best-effort, swappable, provider-agnostic** step.

Companion to collab-onboarding-field.md (the
decision-capture half). Read that first — this doc covers only delivery.

## Shape

```
sponsor opts in (collab.enabled) ──▶ manifest ──▶ onboard-runner
                                                       │  mesh identity minted (unchanged)
                                                       ▼
                                          provisionCollab  (best-effort, non-fatal)
                                                       │
                                                CollabProvider (swappable)
                                                       │  today: goat-collab (Dex+Kanidm)
                                                       ▼
                              create FLAT person  ──▶  mint 24h one-time setup link
                                                       │
                                                       ▼
                              ops/operators/out/<handle>-collab.md   (0600; welcome package sends it)
```

The leg runs in `ops/onboard-runner` **after** the mesh writeback, so it can never
delay or fail the primary onboarding — the mesh identity is already minted and
recorded when it runs.

## Decisions

**Best-effort, non-fatal.** A collab failure is logged and dropped, never propagated.
The mesh onboarding succeeds regardless. Nothing in an enforcement path depends on the
collab account (the standing rule that the collab plane and the mesh Ory plane stay
separate).

**Swappable provider.** `CollabProvider` (`ops/onboard-runner/collab.go`) is the seam.
Today the only implementation is `goat-collab` — our own Dex+Kanidm plane. When the
collab env is offloaded to an environment we do not fully control, a new implementation
performs a **handoff** into that environment's own flow instead of minting an account;
the manifest and the runner leg do not change, only the provider does.

**Flat encoding — the answer to "encoding of users/roles/attributes."** A collab
account is a **flat Kanidm person** carrying only identity attributes: `name` (=handle,
the sign-in id), `displayname`, `mail`. It carries **no mesh roles or groups.** We
deliberately do **not** derive Kanidm groups from the mesh identity grammar
(role/affiliation/echelon/node-class/site, which the manifest encodes as
`netbird.auto_groups` / `ory_site.groups`). Reasons:

- The `collab` block is loosely coupled by contract — deriving groups from it would
  couple the two identity planes the schema deliberately separates.
- The collab surfaces (Outline / filedrop / Zulip) JIT-provision on first SSO and do
  not consume mesh roles; there is no closed collab-role vocabulary to map onto.
- Collab-side administration (Outline/Zulip moderation) is an operational concern of
  the collab plane, not a mesh grammar dimension — it is managed there
  (`idm_admin` / a collab-admin group), not stamped from a mesh manifest.

So no new user/role/attribute encoding is introduced on the mesh side; the collab
account is intentionally minimal. If collab RBAC is ever needed, it is added inside the
collab plane, not by projecting mesh groups across the boundary.

**Auth — least privilege preferred.** The provider authenticates to Kanidm with a
**scoped service-account token** (`COLLAB_KANIDM_API_TOKEN`) when set — a service
account whose rights are limited to person-create + credential-reset. It falls back to
the `idm_admin` password 3-step flow (`COLLAB_KANIDM_IDM_ADMIN_PW`) only when no token
is configured; that path places a full-admin credential on the runner host, so the
token is the recommended production path. Creating the scoped Kanidm service account +
its ACLs is a standup step (see Follow-ons).

**Delivery via the console welcome email (built).** The person gets their collab
username + one-time setup link in the SAME welcome email the console already sends on the
onboarded result — no separate manual send. To make this work the collab leg runs
**before** the writeback (the console emails synchronously on `/result`): the runner
provisions collab, then carries the account + one-time link in the result payload
(`collab_account` / `collab_setup_link` / `collab_activated`); the console's
`welcome.Render` adds a "Collaboration environment" section (separate sign-in plane, MFA
required) when a link is present. The link is credential-bearing + one-time, so it is
never logged and never persisted — used once for the email. The runner also still stages
`ops/operators/out/<handle>-collab.md` (mode 0600) as crash-insurance / an operator
fallback. Best-effort throughout: a collab failure returns empty, the writeback proceeds
without collab fields, and the email simply omits the section.

**Idempotent — and "already there" is ACHIEVED, not failed.** Re-provisioning a person
who kept their collab access (the re-onboard case) returns the account id with **no
setup link** and no error; the runner records `collab_account` + `collab_activated` and
the welcome email renders the account-only collab block — username, sign-in address, and
the console's `/setup-code` page for a fresh code — rather than the set-up walkthrough. The
console's welcome download and the resend path render the same account-only block, since
the link is never stored (#1061).

Two details are load-bearing, both learned the hard way:

- **The live conflict shape.** Kanidm answers a repeat create with **HTTP 409** and
  `{"conflicting_attributes":[…],"error":"Attribute uniqueness error"}` — verified
  against the live collab IdP 2026-08-01. The original tolerance test matched on the
  strings `"already exists"` / `"duplicate"`, which this server never emits, so **every
  re-onboard of a collab-retaining person was recorded as a collab failure**: the email
  dropped the collab section and the proposal recorded `collab_activated: false` for
  someone who demonstrably had access — inventory drift, on exactly the surface F-274 is
  about. `isAlreadyExists()` now keys on the 409 status (string forms kept as
  belt-and-braces for other providers behind the seam).
- **No fresh link for a live account.** The already-provisioned path deliberately does
  **not** mint a credential-setup intent. That link is a credential *reset* path: minting
  one for an account whose credentials already work hands out an account-takeover link,
  emailed, valid 24h, for a person who never asked to reset anything. Whether minting an
  intent invalidates an existing credential is **unverified** (the credential-update
  payload shape defeated a live probe on 2026-08-01) — the design sidesteps the question
  rather than resting on it.

## Revocation is a separate decision

A mesh revoke does **not** imply a collab revoke, and that is deliberate: a person can
leave the goatnet while remaining a legitimate participant in the collaboration space
(and the reverse). The provider therefore exposes `Deprovision()`, but **nothing in the
onboarding runner ever calls it** — cutting collab access is an explicit operator act:

```
onboard-runner -collab list                       # the full inventory
onboard-runner -collab status -handle <handle>    # does this person have collab access?
onboard-runner -collab revoke -handle <handle> -yes
onboard-runner -collab reconcile --repo-root <repo>   # provider accounts vs the identity stores
onboard-runner -collab reconcile --repo-root <repo> -emit-backfill <patch.yaml>
onboard-runner -collab backfill -file <patch.yaml> --repo-root <repo> [-yes]
```

`revoke` refuses without `-yes` (it disables a real person's access), pre-checks
existence, and is idempotent — an absent account is success. The subcommands reuse the
provisioning leg's provider, credentials and config: no second client, no second
credential path.

**The decision is recorded where the revoke is (built 2026-08-02).**
`goat admin onboard <handle> --revoke --collab keep|revoke` carries the operator's choice
into the revoke receipt line (`collab=keep|revoke-requested`) and the
`admin.revoke-native` audit event's args digest. It is a RECORD, not a cut: goat-cli
holds no collab credentials, so the execution stays `onboard-runner -collab revoke` on
the host that does. Omitting the flag for someone the manifest shows collab for records
`collab=undecided` and prints the follow-up command — an unanswered question stays
visible instead of resolving silently to "keep". `n/a` when the person has nothing on
that plane.

**`reconcile` is the inventory arm.** It joins the provider's account list against the
operator manifests (authoritative) plus `docs/operators/roster.json` (the committed
projection, for hosts without the gitignored manifests) and prints one row per
discrepancy, loudest first, carrying handles and never email addresses:
`revoked-identity-live-account` (the F-274 headline — an action candidate it reports and
never acts on) · `account-no-identity` · `account-unrecorded` · `recorded-no-account` ·
`declared-no-account`. It exits 0 even when it finds discrepancies — it is a report, and
turning "found something" into "failed" would make the cron that runs it lie; exit 2 is a
usage/config error (including an empty identity side, so a wrong `--repo-root` cannot read
as "every account is an orphan"), exit 3 an operational failure. `-emit-backfill` writes
the observed accounts as a patch document in the manifest schema's own `collab`
vocabulary (`activated_at` deliberately omitted — the provider's list carries no creation
date and inventing one would assert something unobserved); `backfill` applies it, editing
only those keys so comments and unrelated blocks survive, dry-run until `-yes`, and
refusing any path outside `ops/operators/`.

## Config

Default OFF (role default `onboard_runner_collab_enabled: false`) — most goatnets have
no collab env. ZAGROS turns it on (`group_vars/zagros_hosts/vars.yml`), reaching
`idm.collab.example`. The `.env` rendered by the `onboard-runner` role carries:

| env | meaning |
|---|---|
| `COLLAB_PROVISION_ENABLED` | master switch (`1` ⇒ leg active) |
| `COLLAB_KANIDM_URL` | collab Kanidm base (default `https://idm.collab.example`) |
| `COLLAB_KANIDM_API_TOKEN` | scoped service-account bearer (preferred) |
| `COLLAB_KANIDM_IDM_ADMIN_PW` | `idm_admin` password (fallback; resolved from `60-collab-services.envrc` at deploy) |

Fail-safe: if the flag is on but no credential is present, the leg stays disabled (a
warning is logged) rather than crashing.

## Mesh awareness — read-only

Two records, both read-only (the mesh **tracks** collab access, **never governs** it):

- The **manifest** carries `collab.enabled` (always stamped at mint).
- The **onboarding proposal** now records `collab_account` + `collab_activated` (built) —
  written back on the onboarded result, `collab_activated` latches (once set, stays), and
  the one-time link is never stored. Any console-side read of the proposal surfaces it.

An absent activation (e.g. an external provider with no callback) is normal, not an error.

## Follow-ons

- **`goat who` awareness line** — the Rust `OnboardManifest` **now carries** the `collab`
  block, so it round-trips instead of being dropped (`crates/goat-cli/src/cmd/admin.rs`
  `MfCollab`). What remains is the display: a TUI line showing decision + activation needs
  a collab source in the portal registry (`goat who` reads the registry, not disk), so that
  is a thin follow-on on top of the registry plumbing.
- **Backfill** — the dogfooding cohort was minted as a manual batch ahead of
  this leg, so their `collab` state populates only for onboards that flow through the
  runner after this change. The **remedy is built** (`-collab reconcile -emit-backfill` →
  `-collab backfill`); running it over the live accounts is the open item, and it
  belongs to the arc's deploy pass.
- **Scheduled reconcile** — `-collab reconcile` is a command, not yet a job on the
  identity-reconcile cron; and the roster projection (`gen-roster.py` → `roster.json`)
  carries no collab field, so a host without the manifests can see *that* a person exists
  but not what their collab record says.

### Built (this iteration)

- **Welcome-email integration** — collab username + one-time link now ride in the console
  welcome email (see "Delivery" above).
- **Activation writeback** — `collab_account` / `collab_activated` persisted on the
  onboarding proposal for mesh-plane awareness (see "Mesh awareness" above).
