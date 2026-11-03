# Fabric credential collection — mint on collect

A person's fabric credential is minted when they come and collect it, not when
an operator onboards them. The welcome email then carries only what someone
needs to reach the mesh and set a login; the credential itself is issued inside
the private mesh, to someone who has proved who they are.

Direction approved 2026-08-07. Plan and rationale:
fabric-credentials-mint-on-collect.md.

## Status

| Step | State |
|---|---|
| 0. Console door declared on the production goatnet | **DONE** |
| 1. Portal: mint-for-identity endpoint | **BUILT** — `POST /api/orgs/collect` |
| 2. Console: the collection page | **BUILT** — `GET/POST /fabric-credential` |
| 3. Onboarding: stop minting the people leg; email drops to mesh + login | **BUILT** — `--defer-credential`, off by default |
| 4. Paired pin bump + deploy | **Default-off everywhere** — the var has a role home (below); a goatnet defers only when it sets it |
| 5. Deferred create erases pool-era cert stamps | **BUILT** — deploy to deferral goatnets pending |

Deferral is opt-in at every layer (a CLI flag the runner passes only when
`ONBOARD_DEFER_CREDENTIAL` is set for that goatnet); a goatnet without the
env var keeps minting at onboard.

## The mint-at-onboard path has its own silent hole

A goatnet that keeps minting at onboard — which today is all of them — still
has to put the **goat-cli profile** in the tarball, and that is gated
separately. `certmint.go` mints the `.cbor` only when all three of
`BundleCreatePath`, `PortalURL` and `RouterEndpoint` are non-empty; the
`default:` branch drops it and says nothing. The mint reports success, the
tarball assembles, and the person is handed cert+key with no profile — while
the welcome email tells them to run `goat profile init <the .cbor>`.

One goatnet shipped that way from standup, missing
`portal_bundle_create_bin` and `portal_public_url`
(F-404).
Two operational facts worth keeping:

- **`bundle-create` is a host binary bind-mounted into the container, not part
  of the image.** Two goatnets ran the identical portal image digest and
  behaved differently for exactly this reason. Build it `-tags nopiv` for the
  target arch and restart the portal — a running container pins the old inode
  through the mount.
- **Current builds enforce a fail-closed gear-class taxonomy check**, and the
  portal passes no `--taxonomy`, so the binary resolves the repo-relative
  default by walking up from its working directory — `/` inside the container.
  The portal role now stages the taxonomy and mounts it at
  `/ops/ansible/netbird_acl_taxonomy.yml` whenever bundle-create is enabled.
  Goatnets that enabled this earlier run older binaries predating the check.

Unfixed: the omission is still silent. A mint that knows it is dropping the
profile should say so, and nothing asserts that a bundle contains what the
welcome email tells the reader to look for.

People onboarded BEFORE the switch keep the credential they were sent; the
collection page answers them `already_minted` rather than replacing it.

The first live cohort was ALSO answered `already_minted`, for a
credential nobody held — the pool-stamp conflict below
(F-313).

## The finding that decides the design

A portal API key **cannot be shown after it is minted**. The registry keeps a
bcrypt hash and returns the plaintext exactly once — `rotateVendor` in
`ops/goat-zenoh-portal/internal/api/handlers.go` says outright that the old
plaintext is unrecoverable.

That leaves two ways to put the key behind a post-enrollment step:

1. Persist the plaintext at mint, reveal it later. **Rejected** — a live bearer
   token at rest in the database purely so it can be re-read, for no functional
   gain.
2. Do not mint until the person collects. **Chosen.**

## Scope fence: people only

A machine cannot complete an Ory login, so it can never collect. Machine
enrollment ([ADR 1096](../adr/1096-delegated-machine-enrollment.md)) keeps
minting at enrollment and delivering to a human custodian. Nothing in the
collection path is reachable from the machine path.

## The endpoint

`POST /api/orgs/collect` — operator-gated, org-only (no `/api/vendors` twin, per
[ADR 1052](../adr/1052-vendor-to-org-machine-rename.md)).
Implementation: `ops/goat-zenoh-portal/internal/api/collect.go`.

```
POST /api/orgs/collect        Authorization: Bearer <operator key>
{"identity_id": "<kratos identity uuid>"}
```

**Trust boundary.** The collector authenticates to goat-id-console (Ory, through
oauth2-proxy); the console calls the portal with its operator bearer and names
the authenticated person. Holding an operator key *is* the trust boundary — the
same delegation `X-Goat-Operator-Actor` already rests on.

**The caller names a person, never a slot.** The request carries an identity and
nothing else; the portal resolves identity → slot itself via
`slots.identity_id`, the linkage stamped at onboard. There is no anchor field to
steer the mint with, and the request decoder refuses unknown fields, so an
attempt to collect against someone else's anchor fails at the door.

**Collection issues the whole fabric identity** — leaf cert (CN = the slot
anchor) *and* the portal api_key. They are coupled deliberately, for the same
reason rotation couples them: the plaintext is unrecoverable, so one cannot be
re-issued without the other. For a first collection that is ideal — nobody is
using the cert yet, so the identity is created when its owner asks for it.
Collection is the same mint onboarding does today, moved later and authenticated
differently. No new decoupled portal operation was required.

### Refusals

| Condition | Response |
|---|---|
| Credential already collected | `409 already_collected` |
| Credential already minted at onboard (every pre-change slot) | `409 already_minted` |
| No live slot recorded for the identity | `404 no_slot` |
| More than one live slot for the identity | `409 ambiguous_identity` |
| Slot assigned but no org row | `409 no_org_row` |
| Goatnet runs per-vendor identities, not the slot pool | `501 not_slot_mode` |

Both 409s exist for one reason: re-minting would silently invalidate the copy
the person is already holding. That is the rotation trap this design closes —
admins reached for `goat admin rotate` to hand someone a working key, which
re-issues in place and kills the copy in that person's own bundle. The refusal names the deliberate operator path,
`POST /api/orgs/{anchor}/rotate`
([ADR 1044](../adr/1044-credential-rotation-in-place-reissue.md),
credential-rotation.md), rather than quietly doing it.

### Ordering

The api_key hash lands before the mint, because the plaintext is baked into the
tarball's goat-cli `.cbor` — a tarball minted against a key the registry never
accepted authenticates to nothing. `collected_at` is stamped **last**, after
every artefact exists: a failure anywhere above leaves it NULL and the person
can simply collect again, where stamping first would burn their one collection
on a mint that produced no bundle. Same shape as the operator-key flow's
write-then-report ordering ([operator-action-audit.md](operator-action-audit.md)),
which is prior art in a different feature — a worked example, not shared code.

Lazy per-slot storage ([ADR 1101](../adr/1101-zenoh-lazy-per-slot-storage.md))
is added here too, since on this path the slot's cert first exists at
collection rather than at onboard. Best-effort and non-fatal, as at onboard.

## The collection page

`GET /fabric-credential` explains and offers; `POST /fabric-credential` mints and
shows the result once. Implementation:
`ops/goat-id-console/internal/handlers/fabriccred_page.go`.

Self-service, alongside `/ssh-key` and `/operator-key`, and wrapped in
`selfServiceAuth` — identification without authorization. That wrapper is not
optional decoration: a route registered bare reads the zero `Actor` and refuses
every visitor, which is exactly what F-309 was.

**The collector's identity comes from Kratos, resolved by the session's email —
never from a form field and never from a request header.** A form-supplied
identity would let anyone with a console session collect somebody else's
credential. Resolving through Kratos rather than reading
`X-Forwarded-Preferred-Username` also means the page does not depend on which
claim a given IdP maps into that header.

**There is deliberately no "can I collect?" pre-check.** The portal has no read
side for collectability, and a page that guessed would be asserting something it
cannot stand behind. The answer to "can I collect" is the attempt, and each
refusal names what is true and who can change it — notably `already_minted`,
which every pre-existing member will hit and which must read as "you already
have one, check your welcome email", not as a bare 409.

The POST carries `X-Goat-Operator-Actor` naming the collector, so the portal's
audit row attributes the mint to the person rather than to the shared console
credential.

## Deferring the mint at onboard

`POST /api/orgs` takes `defer_credential`: the slot is allocated, the person
linkage stamped and the org row created — everything that makes the identity
exist — and no api_key or cert is minted. The org row carries an empty
`api_key_hash`, which `resolvePrincipal` already skips, so a deferred identity is
a real registry entity that authenticates to nothing until collection. The lazy
per-slot store is not added either; it is created when the cert first exists,
which on this path is at collection.

`goat admin onboard --defer-credential` sets it, and the onboard-runner passes
that flag only when `ONBOARD_DEFER_CREDENTIAL` is set. **Off by default at every
layer, deliberately:** a goatnet whose console does not yet serve
`/fabric-credential` would otherwise leave every new enrollee with no credential
and no way to obtain one.

**The env var's home is the role, not the container.** It had no
line in `ops/ansible/roles/onboard-runner`, so one goatnet's deferral was hand-set on
the running container — and a routine runner redeploy replaced that container
without it, flipping the goatnet back to immediate-mint with nothing in any
config to show the change had happened. It is now `onboard_runner_defer_credential` (role
default **false**), rendered into the runner's compose env. A goatnet turns
deferral on by setting that group_var, and a redeploy can no longer silently
undo it. **Do not set it true until the F-313 fix below is deployed on that
goatnet** — enabling first re-breaks every collect on a pre-stamped pool slot.

### The deferred create erases pool-era cert stamps

`slots.cert_minted_at` is not clean history on every goatnet: the pre-cut pool
(ADR 1025) was batch pre-minted on one goatnet (most slots stamped
before anyone owned them), and the old flow masked that by overwriting the
stamps at every mint. A deferred onboard mints nothing, so without
intervention it inherits the pool's stamps and the collection guard refuses
its owner with `already_minted` — which stranded the first deferred cohort
the morning the feature went live
(F-313).

The deferred branch of `POST /api/orgs` therefore calls
`registry.ClearSlotCert` after allocation — fatal on failure, same rationale
as the linkage stamp: a deferred slot that keeps a stale stamp is a person
who can never collect, and nothing is minted yet so the rollback is clean.
Unassigned pre-stamped slots are left alone; they self-heal at allocation.
Repair for someone already stranded is in the finding.

### The bundle URL is absolute

The collection response's `bundle_url` is absolutized against
`PortalPublicURL` when set — it is rendered on the *console's* page, where a
bare `/api/org-bundle/…` path reads as a fragment or resolves against the
wrong host. Scoped to collection: the other bundle-URL producers answer the
goat CLI, which joins paths onto its own portal base. The console page
hyperlinks the URL only when it is absolute.

### Two things that must be stated, never inferred

Both were live traps, not hypotheticals:

- **The slot anchor.** The person→slot linkage was resolved by regexing the
  bundle tarball path, and a missing bundle read as "no fabric leg, nothing to
  link". A deferred onboard has a real slot and no bundle, so the linkage would
  have been skipped in silence and collection would answer `no_slot` forever.
  The mint states the anchor; the runner carries it as `slot_anchor`; the path
  derivation survives only as a fallback for older writebacks.
- **The deferral itself.** "Has fabric access but no key" is *also* what a failed
  key-scrape looks like (`parseMintResult` is best-effort). Inferring deferral
  there would tell someone whose key is in their bundle to go and collect one,
  sending them to a page that refuses them. The runner states
  `credential_deferred` and the email believes only that.

### A returning person reuses their slot (#1576)

A deferred onboard for a handle that already holds a live slot gets that slot
back (HTTP 200, `reused: true`) instead of `alias_taken`. This is the restore of
an archived person and the retry of a mint that failed after the slot leg: the
slot from the first onboarding is still theirs. Nothing is minted, so a
credential they already collected keeps working and an uncollected one is
still theirs to collect. The portal never relinks: a slot already linked to a
different identity is refused with `identity_mismatch`, and the CLI makes the
same check once the login leg has resolved the identity. A reused slot is never
rolled back on a later failure, because it existed before the call. A
non-deferred onboard and a partner-lane request keep the 409. When the login
identity already exists, the native onboard moves its name and org to the new
manifest's values (credentials, groups and state untouched).

### A failed mint revokes the setup keys it minted (#1587)

The mesh leg runs first and mints the person's setup key and handset key. When a
later leg fails the run (a portal error, the login leg on a slot-holding tier, an
identity mismatch, an unwritable bundle), the native onboard revokes exactly the
keys that run minted before it exits, and says so; the run's own error is still
the error reported. Nobody is sent those keys, and a retry mints fresh ones, so
without the revoke every failed-then-retried mint left a valid, unheld enrolment
credential behind. A key the run did not mint is never touched. The browse-tier
case where only the login leg fails does not fail the run and keeps its keys.

## `slots.collected_at`

New nullable column, additive migration in `internal/registry/registry.go`.

It is **not** a duplicate of `cert_minted_at`. `cert_minted_at` says a cert
exists; it does not say who asked for it. A slot minted for someone at onboard
has `cert_minted_at` set and `collected_at` NULL, and that difference is the
point: it separates *onboarded* from *holds a fabric credential they asked
for*.

## Consequences to accept deliberately

- **"Onboarded" stops implying "holds a fabric credential."** A person can be
  fully onboarded and hold nothing until they collect. Roster views and
  `goat who` will read differently, and anything inferring capability from
  onboarding state needs checking.
- **Someone who never collects never gets fabric access.** Intended —
  uncollected identities become a visible signal rather than a pile of unused
  live credentials.
- **The guide page must not carry any of this.** Its invariant — no credential
  values, identical per tier, therefore no login needed — is what lets it be an
  unauthenticated mesh page. Collection is a different, authenticated surface.
