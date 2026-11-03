# Operator + vendor manifest directory

> ## Read this before you create a file here
>
> **You almost certainly should not be authoring a manifest.** Under the live
> onboarding flow these files are **OUTPUTS**: `ops/scripts/sync-onboarded-manifests.py`
> writes them **from** the portal/console DB *after* `onboard-runner` has minted,
> so the declared roster agrees with what actually happened. Each generated file
> says so in its own first line.
>
> **To onboard somebody**, use the three-stage candidate flow (ADR 1062):
>
> ```bash
> goat admin invite --env <goatnet> …          # goat-id-console mints the invite
> # candidate accepts in the console; onboard-runner claims it and mints
> ops/scripts/sync-onboarded-manifests.py --env <goatnet> …   # roster catches up
> ```
>
> **`operator-onboard.yml`'s ENROLL leg is retired** (July 2026) and refuses at
> `Phase 0.2b`. It hand-minted every surface and emitted an out-of-band bundle
> holding the person's **private key** for an operator to carry. Its **revoke**
> leg (`-e action=revoke`) is still live and unaffected.
>
> The `example*.yml.example` templates below exist for the **bootstrap** and
> **schema-reference** cases only — they are not an invitation to enroll.
>
> Canonical: [three-stage-candidate-onboarding.md](../../docs/design/three-stage-candidate-onboarding.md)
> · [identity-access-and-onboarding-reference.md](../../docs/design/identity-access-and-onboarding-reference.md) §3

Per-operator YAML manifests consumed/produced around the onboarding flow. Each
file declares one person's access across the four identity surfaces — NetBird
mesh credential, Ory site Kratos identity + groups, Ory operator-IdP Kratos
identity + groups, and (optional) Zenoh wire-layer cert. The
[`operator-onboard.yml`](../ansible/operator-onboard.yml) orchestrator still
reads them for its **revoke** path.

Design rationale + resolved decisions: `docs/design/operator-onboard-utilities.md`.

## Layout

```
ops/operators/
├── README.md                                   # this file
├── .gitignore                                  # ignores actual manifests
├── example.yml.example                         # canonical OPERATOR template (internal)
├── example-vendor.yml.example                  # canonical VENDOR template (3rd party)
├── schema/
│   └── operator-manifest.schema.json           # jsonschema (draft-07)
├── validate.py                                 # ./validate.py <manifest.yml>  |  --report
├── alice-smith.yml                             # flat / legacy nova-a operator (gitignored)
└── <env>/                                       # per-env (per-goatnet) manifests
    ├── README.md                                #   committed env note (no identity material)
    └── jane-partner.yml                         #   actual operator (gitignored)
```

Actual manifests (`*.yml`, except the two `.yml.example` templates) are
**gitignored** because they carry identity material (email, name) — flat
*and* per-env (`*.yml` matches recursively). Back them up in your team's password store.

## Environments (per-goatnet folders)

Manifests are organized **one folder per goatnet** so envs don't muddle:

```
ops/operators/<handle>.yml          # flat / legacy — treated as nova (nova-a)
ops/operators/zagros/<handle>.yml     # ZAGROS sandbox goatnet
ops/operators/<env>/<handle>.yml    # any future goatnet
```

The env (folder name) is the **site/goatnet id** and selects the per-site
maps in the orchestrator (NetBird mgmt URL, Ory Kratos admin host, Zenoh
portal, ACL taxonomy stanza). Drive it with `goat admin onboard`:

```bash
goat admin onboard jane-partner --env zagros      # → ops/operators/zagros/jane-partner.yml
goat admin onboard alice-smith                  # → ops/operators/alice-smith.yml (flat = nova)
```

`--env <env>` passes `-e operator_env=<env>` to `operator-onboard.yml`,
which resolves the subfolder manifest and derives `zenoh_trial_site=<env>`
so the Zenoh mint targets *that* goatnet's portal (not always nova-a).
Existing flat manifests are unaffected (no env ⇒ nova default). When
adding a new goatnet, extend the per-site maps in `operator-onboard.yml`
and add a stanza to `ops/ansible/netbird_acl_taxonomy.yml`.

**A goatnet's folder is that goatnet's whole population.** `ops/operators/zagros-prod/`
holds the people zagros-prod admitted, and `gen-roster.py --env zagros-prod` turns
exactly that folder into `docs/operators/zagros-prod/roster.json`, the artifact
zagros-prod's portal is served. No other environment's folder reaches it, and the
flat `ops/operators/*.yml` set — the nova-a cohort, which names no goatnet —
reaches none of them. Identity does not cross a goatnet boundary in either
direction ([ADR 1172](../../docs/adr/1172-identity-does-not-cross-a-goatnet-boundary.md)),
so somebody working on two goatnets has a manifest in each folder, admitted
separately through the same three gates. That is expected duplication, not
drift, and the two records are permitted to differ.

Sanitized cohort summary lives at `docs/operators/<goatnet>/roster.md`
— one per goatnet, plus a union at `docs/operators/roster.md`
that spans every environment for repo-side tooling and is deployed nowhere. All
**generated** from the manifests by [`gen-roster.py`](gen-roster.py) (do not
hand-edit). Columns: kebab handle + org + type + state + added-date + custody
(enroller handle); no email / name / group membership. It is the **declared
half** of the [Data-Access COP](../../docs/design/data-access-cop.md) — the
basic tracking index for the principal graph (org → person/system siblings;
`enrolled_by` is the custody edge, not ownership) defined in
credential-revocation-and-offboarding.md.
That file IS committed; `git log docs/operators/roster.md` answers "when did
Alice get added / revoked?" without exposing identity material. The
orchestrator regenerates it (Phase 6.1b) on every enroll/revoke.

**Principal type.** `identity.type` is `person` (default, omittable) or
`system` (a headless publisher — a uuid / Zenoh slot — that is an org member
in its own right). Persons and systems are siblings under the org
(`vendor.short_tag`); a system records `enrolled_by` (the person who brought
it online). `identity.status` is `active | inactive | revoked`.

## Canonical shapes

There are **three** canonical manifest shapes for new onboardings; every
new manifest must match exactly one. The schema enforces structural
requirements; the classifier in `validate.py --report` labels each
existing manifest and flags cross-manifest drift (duplicate
URI-SAN prefixes, orphan siblings from legacy manifests).

| Shape | When to use | Auto-groups | `vendor:` block | `zenoh.enabled` | Template |
|---|---|---|---|---|---|
| **vendor-primary** | Any person at a 3rd-party vendor / partner org (every person is their own primary — distinct `zenoh.trial.vendor` prefix, distinct cert + api_key + bundle, no OOB credential coordination between teammates). The ZAGROS **full-participation** tier. | `VENDOR` + `OBSERVER` | required (`display_name` MAY be shared across teammates at the same org; `short_tag` SHOULD be person-distinguishing) | `true` (mints cert) | `example-vendor.yml.example` |
| **browse** | Browse-only participant — web surfaces (panoscope / portal / stethoscope / flashlight / brief / isitup) via Ory OIDC, **no** fabric publish, no admin. The ZAGROS **browse-only** tier. An optional read-only `observer` cert gives the participant their own fabric identity (subscribe only). Optional `vendor.display_name` records org affiliation. | `OBSERVER` (no `VENDOR`) | optional (`display_name` only, for org attribution) | `false` (no cert), or `enabled` + `observer: true` (read-only cert) | `example-vendor.yml.example` (drop `VENDOR` + `zenoh`) |
| **operator** | Internal operator (NetBird ADMIN / OPERATOR / MGMT-HOST role; optional obs/utility via Orin-C operator-IdP) | `ADMIN`, `OPERATOR`, or `MGMT-HOST` | absent | `false` | `example.yml.example` |

**ZAGROS access tiers** map onto these shapes: *full participation* = `vendor-primary`
(`VENDOR`+`OBSERVER`, Zenoh per-uuid slot; not admin, no netbird-dashboard, no compliance
dash); *browse only* = `browse` (`OBSERVER` only, web surfaces). See
`docs/design/operator-onboard-utilities.md`
§"ZAGROS tiers".

A legacy **vendor-sibling** shape (`zenoh.enabled: false`, shares the
primary's cert OOB) is still structurally valid in the schema for
backward-compatibility with existing manifests, but is **deprecated**
for new onboardings. The classifier still labels it; the cross-manifest
report flags orphan siblings.

**Invariants enforced by the schema (Phase 0 pre-flight halt):**

- `vendor.display_name` required whenever `netbird.auto_groups` contains `VENDOR`.
- `ory_site.credentials` required (both `password` and `passkey` explicit, no defaulting) when `ory_site.enabled: true`.
- `sites` and `groups` arrays MUST be empty when the corresponding `enabled` is `false` (no stub content).
- Filename stem must equal `identity.handle`.
- Site IDs match the per-site PAT envvar naming (`nova` → `NOVA_NB_API_TOKEN`, etc.).

**Invariants surfaced by `--report` (advisory, fix on next touch):**

- Each `zenoh.trial.vendor` prefix (URI-SAN authority) is owned by exactly one vendor-primary manifest. Two manifests with the same prefix → portal returns 409 on every mint after the first.
- Every (legacy) vendor-sibling has a vendor-primary with the same `display_name` in the same set.

### Identity grammar — role vs affiliation (ADR 1043)

A manifest encodes two **independent** things; do not conflate them
(the design canon is `docs/design/identity-grammar.md`,
decision ADR 1043):

- **Role** = *what the person may do*, via `netbird.auto_groups` (+ `ory_site.groups`). The capability family is `ADMIN` / `MONITOR` (platform observability) / `DEVELOPER` (fabric dev — Zenoh pub/sub + dev web) / `OPERATOR` (tactical actor) / `OBSERVER`. **`DEVELOPER` is the affiliation-neutral fabric-dev role** — use it for organic/partner developers so they aren't mislabeled vendors. `VENDOR`/`OPERATOR`/`OFFSITE` remain valid as **legacy aliases** until each goatnet's gated cutover.
- **Affiliation** = *who they belong to*, via the optional `affiliation:` block: `{ id, kind: vendor|organic|partner, nation?, display_name, parent? }`. Hierarchical, identity-only — never a NetBird group, never the Zenoh write anchor. `nation` is load-bearing for `partner` orgs. The legacy top-level `vendor:` block maps to `kind: vendor`; both may coexist during migration (`affiliation` is the forward SoT). So a commercial dev = role `DEVELOPER` + affiliation `vendor`; an organic dev = role `DEVELOPER` + affiliation `organic`; a partner-nation dev = role `DEVELOPER` + affiliation `partner` (+ `nation`).

The canonical-shapes table above is the *role+access* shape; affiliation is orthogonal and applies to all three shapes.

## Add a new operator or vendor (quickstart)

```bash
# 1. Choose the right template based on the canonical shape table above.
# 2. Copy + edit into the env folder. Filename stem MUST match identity.handle.
cp ops/operators/example-vendor.yml.example ops/operators/zagros/jane-acme.yml   # vendor (per-env)
cp ops/operators/example.yml.example        ops/operators/alice-smith.yml      # operator (flat=nova-a)

# 3. Validate against the schema.
ops/operators/validate.py ops/operators/zagros/jane-acme.yml

# 4. Re-classify the whole roster, recursing env folders (catches drift).
ops/operators/validate.py --report ops/operators

# 5. Onboard via the goat CLI (wraps operator-onboard.yml). --env picks the
#    per-env manifest + the goatnet's per-site maps; omit for flat=nova.
goat admin onboard jane-acme --env zagros --dry-run   # preview the ansible command
goat admin onboard jane-acme --env zagros             # real run

# …or call the playbook directly (what the CLI wraps):
cd ops/ansible
OBJC_DISABLE_INITIALIZE_FORK_SAFETY=YES ansible-playbook \
  -i localhost, --connection local \
  operator-onboard.yml -e operator=jane-acme -e operator_env=zagros --check   # dry-run

# 7. The orchestrator regenerated docs/operators/roster.md from the
#    manifests (Phase 6.1b). Review + commit it.
git add docs/operators/roster.md
git commit -m "docs(operators): roster — add jane-acme"
```

## Revoke an operator

```bash
# Symmetric: the same playbook walks the surfaces backward (Phases R0-R5).
# Default scope=person, reversible (Ory deactivated). Options:
#   -e revoke_purge=true   hard-delete the Ory identity (terminal)
#   -e revoke_scope=org    also delete the shared Zenoh vendor (cuts every
#                          publisher under its CN; otherwise last-one-out)
cd ops/ansible
OBJC_DISABLE_INITIALIZE_FORK_SAFETY=YES ansible-playbook \
  -i localhost, --connection local \
  operator-onboard.yml -e operator=alice-smith -e action=revoke

# The playbook flipped the manifest status active -> revoked (Phase R5.3)
# and regenerated the roster (Phase 6.1b). The manifest is gitignored;
# commit the roster:
git add docs/operators/roster.md
git commit -m "docs(operators): roster — revoke alice-smith"
```

See credential-revocation-and-offboarding.md
([ADR 1031](../../docs/adr/1031-credential-revocation-mesh-first-offboarding.md))
for the full revoke model (enumerate-and-cut, principal selector, the
`uuid`/`org` scopes still to land).

## Schema validation

The manifest is validated against [`schema/operator-manifest.schema.json`](schema/operator-manifest.schema.json)
on every orchestrator invocation (Phase 0 pre-flight). Failures halt
before any mint hits a backend. The schema enforces:

- `netbird.role` ∈ `{bootstrap, admin, operator, peer}` ([per setup-key-rotation.md](../../docs/operations/setup-key-rotation.md))
- `netbird.auto_groups[]` ⊆ mesh-ACL taxonomy suffixes (per mesh-acl.md §"Group taxonomy")
- `ory_site.groups[]` ⊆ same set as above
- `ory_idp.groups[]` ⊆ operator-IdP 13-value enum ([per ops/ansible/roles/operator-idp-kratos/templates/identity-schema.json.j2](../ansible/roles/operator-idp-kratos/templates/identity-schema.json.j2))
- `zenoh.trial.vendor` matches `^[a-z0-9-]{1,32}$` ([per add-vendor.sh regex](../ansible/roles/goat-zenoh/files/add-vendor.sh))
- `zenoh.prod.role` ∈ the role enum in the schema ([per ADR 0844](../../docs/adr/0844-zenoh-deployment-topology-and-acl-v1.md))
- Plus the structural invariants listed in §"Canonical shapes" above.

Typos in group names fail at Phase 0 with a clear pointer to the
offending field, NOT silently downstream.

## `--report` mode

```bash
ops/operators/validate.py --report                       # walks the sibling directory
ops/operators/validate.py --report /path/to/manifests    # walks the given directory
```

Validates every `*.yml` in the directory, classifies each into one of
the three canonical shapes, and flags cross-manifest drift (duplicate
primaries, orphan siblings, unclassifiable shapes). Exit code:
0 = clean, 1 = at least one failure or unclassifiable manifest.

Run this after adding any new manifest. Add to muscle memory between
step 3 (validate single file) and step 5 (orchestrator dry-run) in the
quickstart above.

## Cross-references

- Design: `docs/design/operator-onboard-utilities.md`
- Implementation tracking: `docs/project/implementation-plan.md` Block 83
- Local secrets pattern (per-site PAT env vars): `docs/operations/local-secrets-pattern.md`
- Setup-key TTL governance: [`docs/operations/setup-key-rotation.md`](../../docs/operations/setup-key-rotation.md)
- Mesh-ACL group taxonomy: `docs/design/mesh-acl.md`
- Sanitized cohort summary: `docs/operators/roster.md`
