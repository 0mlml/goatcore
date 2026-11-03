# Pub/sub state-as-code (Block 86)

**Status:** design-of-record, v1 substrate landing.
**Companion ADR:** TBD (queued post-Phase-A live-fire).
**Composes with:** [Block 85 portal-driven schema publishing](portal-schema-publishing.md), Block 75 OCI image distribution, [ADR 0859 easy rollback](../adr/0859-easy-rollback-engineering-principle.md), [ADR 0858 zenoh upstream pivot](../adr/0858-zenoh-image-upstream-pivot.md).

## Problem

`goat-zenoh-portal` is the live registry: vendors register, topics get created, schema `.fds` bodies land, code examples get authored. All of it persists in the portal's SQLite DB plus the schemastore filesystem. None of it is mirrored anywhere.

This produces three concrete failure modes:

1. **Site rebuild stops at "empty fabric."** Even after the rapid-bringup runbook (Block 68) reconstitutes goatnet, the portal table is empty — every vendor must re-register, every topic must be re-created, every schema must be re-uploaded, every code-example override must be re-typed. Live data connections do not return on their own.
2. **Operator + UI changes drift.** A vendor uploads a schema, an operator PATCHes a description, an Ansible script idempotently bootstraps a topic — three call paths, three different times, no audit trail beyond the portal's per-row `audit_events`. The DB is the only canonical state; lose it, lose everything.
3. **Island-mode rebuild has nothing to fall back to.** A site brought up while disconnected from any external git remote has no way to recover the pub/sub state. Per ADR 0827 and the resilience model (CLAUDE.md §Resilience + continuity), island-mode capability is first-class. GitHub-as-source-of-truth fails this bar.

## Goals

- **Durable:** every vendor/topic/schema/code-example mutation is captured outside the portal DB within seconds.
- **Idempotent:** rebuilding a fabric from the captured state is a single command, re-runnable, current-state-only (removals propagate).
- **Turnkey:** zero hand-holding on rebuild — the operator runs one command and the live fabric returns. No ID re-mapping, no manual stitching.
- **Island-mode-capable:** the state-of-record is *primarily on-site*, not in any external git host. External git is optional uplink, not load-bearing.
- **Composable:** plays with rapid-bringup (Block 68), easy-rollback ([ADR 0859](../adr/0859-easy-rollback-engineering-principle.md)), and the existing Block 85 schema-publishing substrate without bypassing them.

## State-of-record hierarchy

Three tiers in strict precedence order. Each tier is fully self-sufficient — a site can rebuild from any single tier without consulting the others.

1. **Primary — per-site Forgejo repo `pubsub-state`.** Already provisioned on every Orin-C (ADR 0826). Always reachable in island mode; survives any external-network outage. Holds the canonical filesystem layout + git history of every change. This is what `pubsub-apply` reads on rebuild by default.
2. **Backup — MinIO `pubsub-state-snapshots/` bucket.** Already-provisioned per-site MinIO (Block 82B-storage). Holds a `latest.tar.gz` overwrite-on-every-snapshot plus per-mutation timestamped objects. Cold-start cover when Forgejo itself was lost (Orin-C reflash before the per-site repos were restored). Read by `pubsub-apply --from minio://...` fallback.
3. **Optional uplink — GitHub mirror (`goat-maintainer/goat-pubsub-state-<site>`).** Forgejo's built-in [push-mirror feature](https://forgejo.org/docs/latest/user/repo-mirror/) syncs each commit upstream when network reaches GitHub. *Never load-bearing.* Useful for off-site review and cross-org visibility; never queried on rebuild.

The rebuild order: try Forgejo (loop until clone succeeds, with a short timeout); on failure, fall back to MinIO snapshot; on both empty, manual seed from an offline tarball (rapid bringup S1 carries one).

## Mission-data boundary

Schema definitions, topic names, vendor display names, and code examples are **not mission data**.

Justification:
- They describe *the shape of data*, not the data itself. Payload bytes are mission data; the descriptor that lets a subscriber decode those bytes is not.
- They are the same kinds of artifacts the `goat-maintainer/goat-open-standards` repo already publishes upstream by design ([ADR 0217](../adr/0217-shim-repo-containment.md)). Topic dictionaries are explicitly catalog metadata in [ADR 0216](../adr/0216-goat-net-federation-and-site-planes.md)'s federation-plane definition.
- Vendor display names + handles are the same strings already in operator-onboarding manifests at `ops/operators/<handle>.yml`, which are committed to this repo today.

What *is* deliberately excluded from the state-of-record (and stays portal-DB-only):
- `api_key_hash`, `bundle_token`, `bundle_path` — per-vendor secrets / one-time download tokens.
- `audit_events` — runtime audit log; flows separately into the MinIO `audit-integrity` bucket per [ADR 0811](../adr/0811-audit-log-integrity.md).
- `last_seen_at`, `publish_count`, `created_at` — runtime telemetry; reconstructed from real publishes after rebuild.
- Payload bytes — mission data; not the portal's domain (audit subscriber handles those per [Block 82S](zenoh-audit-external-subscriber.md)).

## Repo layout

The portal mirrors its registry to a filesystem tree under `pubsub-state/`:

```
pubsub-state/
├── MANIFEST.yaml                 # generated_at, portal_version, content_sha256
├── README.md                     # tells humans how to read this repo
└── vendors/
    └── <vendor-prefix>/
        ├── vendor.yaml           # prefix, display_name, synthetic, approved_at, approver
        └── topics/
            └── <key-segments>/   # mirrors the Zenoh key_pattern exactly
                ├── topic.yaml    # key_pattern, format, schema_ref, description, publish_to_storage, retired_at
                ├── schemas/
                │   ├── v1.fds    # protobuf FileDescriptorSet bytes
                │   ├── v1.meta.yaml  # version, compatibility, format, sha256, uploaded_at, uploaded_by
                │   └── v2.fds
                │   └── v2.meta.yaml
                └── examples/
                    ├── python.py    # one file per language (.py / .rs / .go / .ts / ...)
                    └── rust.rs
```

For example, the live notional vendor's `notional/tarsier/alpha-fob/cam/v1` topic lands at:

```
pubsub-state/vendors/notional/topics/tarsier/alpha-fob/cam/v1/topic.yaml
pubsub-state/vendors/notional/topics/tarsier/alpha-fob/cam/v1/schemas/v1.fds
pubsub-state/vendors/notional/topics/tarsier/alpha-fob/cam/v1/schemas/v1.meta.yaml
```

Rationale:
- **Path mirrors key:** the directory hierarchy *is* the Zenoh key expression (after the leading `vendors/<v>/topics/` prefix). A human reading the repo can see the namespace shape at a glance, and there are no string-escape questions.
- **Schemas live with their topic, not in a central index.** Reading a topic's full descriptor is `ls schemas/`. Removing a topic removes its schemas.
- **`.proto` source is optional + sibling.** When a vendor uploads only the `.fds` (the only v1-accepted format per `registry.ValidSchemaFormat`), only the `.fds` lands. When the vendor ships canonical `.proto` source separately (the synthetic-vendor pattern at `ops/zenoh-trial-peers/synth-vendor-schemas/`), CI can re-render `.fds` from the `.proto` and a sibling `.proto` lands alongside the `.fds` for human readability. Either tier is the source of truth depending on what the vendor uploaded — the portal records which one in the meta.
- **`MANIFEST.yaml` is one canonical bytes-hash** of the whole tree so `pubsub-apply` can short-circuit "already current" without diffing every file.

## Reconciler semantics

`pubsub-apply` is **current-state-only and idempotent**. It reads the repo, queries the portal API for current live state, computes a three-way diff (create / update / delete), and applies. Re-running with no repo changes is a no-op.

Order of operations matters because of foreign-key + side-effect dependencies:

1. **Create missing vendors first** (POST `/api/vendors`) — side-effect: mints fresh cert + bundle, renders ACL, SIGHUPs zenohd. Re-runs against an existing vendor return 409, which `pubsub-apply` treats as "already present, OK." On a true rebuild every vendor is missing, so every cert gets re-minted. On a routine reconcile no vendor mints fire.
2. **Update vendor metadata** (PATCH `/api/vendors/<v>`) — display_name drift correction. Idempotent at the handler.
3. **Create missing topics** (POST `/api/vendors/<v>/topics`). 409-on-exists handled like vendors.
4. **Update existing topics** (PATCH `/api/vendors/<v>/topics/<id>`) — format / schema_ref / description / publish_to_storage. Topic ID is portal-internal; the reconciler looks up the existing ID by (vendor, key_pattern) before PATCHing.
5. **Upload missing schemas** (POST `/api/vendors/<v>/topics/<id>/schemas`). Schemas are versioned (v1/v2/...); existing versions are immutable. New versions present in the repo but not in the portal land as fresh uploads.
6. **Upsert code examples** (PUT `/api/vendors/<v>/topics/<id>/examples/<lang>`). The portal's UPSERT semantics make this trivially idempotent.
7. **Retire topics no longer in repo** (DELETE `/api/vendors/<v>/topics/<id>` → flips `retired_at`). Non-destructive at the DB level; the schema FK CASCADE only fires when the vendor row itself is deleted.
8. **Delete vendors no longer in repo** (DELETE `/api/vendors/<v>`). Refused by the handler if the vendor still owns non-retired topics — which is the correct behavior, since step 7 would have retired them first within the same apply run.

`--dry-run` prints the computed plan and exits non-zero if anything would change.

`--from <ref>` selects the source: `--from <dir>` (default; reads filesystem), `--from minio://<bucket>/<key>` (downloads tarball + applies), `--from forgejo://<host>/<org>/<repo>` (clones + applies).

`--scope <vendor>` narrows to one vendor (for partial rebuilds).

## Write path

**v1 (this PR):** operator-triggered. The portal grows a `POST /api/admin/snapshot` endpoint that:

1. Runs `pubsub-export` in-process against the live DB + schemastore.
2. Tars + gzips the result.
3. Uploads to MinIO at `pubsub-state-snapshots/<timestamp>.tar.gz` and overwrites `pubsub-state-snapshots/latest.tar.gz`.
4. Returns `{size, key, sha256, generated_at}`.

A cron + the Ansible role together run this every 5 min during the trial and commit the export to the per-site Forgejo repo. Day-1 latency between mutation and durable mirror is on the order of minutes — acceptable for the trial fabric; portal-side auto-commit closes the gap to seconds in v2.

**v2 (follow-on PR, captured in implementation-plan Block 86.2):** in-portal auto-commit. Every successful mutation handler (`createVendor`, `updateVendor`, `deleteVendor`, `createTopic`, `updateTopic`, `retireTopic`, `uploadSchema`, `putCodeExample`, `deleteCodeExample`) fires an async export-+-commit-+-snapshot. Failures of the mirror are logged + alerted but do not fail the API call — the DB write succeeded; the mirror is a best-effort durability layer with operator-visible Prometheus metric `goat_pubsub_state_mirror_lag_seconds`.

## Read path

Site-rebuild precedence (encoded in `ops/scripts/pubsub-rebuild.sh` per the [runbook](../operations/pubsub-state-rebuild.md)):

1. `git clone https://<local-forgejo>/<org>/pubsub-state.git` — succeeds in island mode, runs in seconds.
2. On clone failure: `mc cp minio/pubsub-state-snapshots/latest.tar.gz . && tar xzf latest.tar.gz`.
3. On both empty: extract the offline tarball shipped in the rapid-bringup bundle (single trusted bytes the operator hand-carried).
4. `pubsub-apply --from ./pubsub-state` — reconciler does the rest.

After `pubsub-apply` returns 0, a `--diff` re-run prints empty.

## Trade-offs

- **Auto-commit produces noisy git history.** Every UI button-press is a commit. That's the cost of UI-as-source-of-truth ergonomics; the alternative — every change goes through PR — kills the portal's "vendor uploads a schema" affordance. The trial-scope verdict: noise is fine, the log doubles as an audit trail.
- **Vendor api_keys are not in the repo.** On rebuild, every vendor gets a brand-new api_key. This is by construction — secrets don't ride in mirrors. Vendors re-download their bundle from the freshly-minted one-time URL the rebuild emits, same flow as initial onboarding (83E).
- **Forgejo on Orin-C is a single point of failure per site.** Acceptable: it's already the per-site code mirror under ADR 0838, and the MinIO snapshot is the redundant backup.
- **Schema bodies are binary in git.** `.fds` files diff opaquely. Mitigated by the sibling `.proto` source-of-truth when the vendor provided one; otherwise `pubsub-apply --dry-run` is the operator's diff view.

## Alternatives rejected

- **GitHub as primary state-of-record.** Fails island-mode by definition. Rejected on first contact.
- **Database replication (Spock).** The portal's SQLite isn't on Spock — moving it would be a massive substrate shift, and a streaming DB replica doesn't give you the "current-state-only, removals propagate" property a config-repo gives you for free. Rejected.
- **Repo as source-of-truth (GitOps full)**: vendors push PRs to add a topic, CI reconciles to live. Kills the UI affordance vendors actually use. Rejected for trial-scope; can revisit when the partner onboarding model matures.
- **Per-vendor sub-repos.** Adds discovery + auth complexity, gains very little (vendors don't peer-review each others' topic shapes). Rejected; one repo per site, one tree by vendor inside it.

## Long-term evolution

This design is trial-scope expedient. The long-term shape (post-Block 23 + Block 30 sanitization) replaces the portal's role as registrar:
- Vendors publish their own schemas directly on the fabric at `attest/schema/<vendor>/...` ([per the Block 85 design memo](portal-schema-publishing.md)'s "Long-term evolution" §).
- The state-of-record repo becomes a *derived* artifact, generated by a fabric-walker rather than a portal mirror.
- Federation-plane catalog ([ADR 0216](../adr/0216-goat-net-federation-and-site-planes.md)) consumes the repo as input for cross-site topic discovery.

The substrate landed here forward-compatibly composes: `pubsub-export` is replaced (DB-walker → fabric-walker), `pubsub-apply` is unchanged, the repo layout is unchanged. The reconciler's idempotency guarantee survives.

## Deployment topologies

Two shapes are in service. They deploy the same substrate; they differ only in
how many hosts carry the pieces.

**Single-host (nova-a, the trial fabric).** Portal + MinIO + Forgejo all on one
peer. The portal's 8090 is host-published, so the mirror reaches it at
`127.0.0.1:8090`. Playbook: `nova-pubsub-state-repo-bootstrap.yml`.

**Three-Orin (zagros-prod, production).** Split across the ADR-0826 topology:

| Host | router_role | Carries |
|---|---|---|
| Orin-A | A (active) | portal + MinIO; the `pubsub-state-repo` **mirror** timer |
| Orin-B | B (passive) | the `pubsub-state-checkout` **warm-checkout** timer + staged `pubsub-apply` |
| Orin-C | — (obs+storage) | **Forgejo** (primary tier) |

Two deviations the single-host shape does not have, both handled without forking
the portal or the shared role:

1. **Portal not host-published.** On the three-Orin topology the portal's 8090
   is reachable only by container name on the `mgmt` docker network (no Traefik
   front), and its container IP is not stable across a redeploy. The
   `pubsub-state-repo` role gained an opt-in `pubsub_state_portal_container` mode
   (default off — nova-a unchanged): when set, the mirror script re-resolves the
   portal container's IP on the named network at the top of every cycle. See
   `group_vars/zagros_prod_hosts/pubsub-state.yml`.
2. **Standby recovery is its own host.** nova-a rebuilds in place; zagros-prod's
   Orin-B is a distinct passive node, so the recovery-side warm checkout is a
   dedicated role, [`pubsub-state-checkout`](../../ops/ansible/roles/pubsub-state-checkout/),
   that keeps `/opt/pubsub-state/repo` current and stages a cross-compiled
   `pubsub-apply` binary (the portal image is distroless — no in-image binary to
   borrow). It never reconciles; the failover one-liner lives in the `RECOVERY.md`
   it drops. Playbooks: `zagros-prod-utility-bootstrap.yml`
   (Forgejo on Orin-C), [`pubsub-state-mirror-deploy.yml`](../../ops/ansible/pubsub-state-mirror-deploy.yml)
   (mirror on Orin-A), [`pubsub-state-checkout-deploy.yml`](../../ops/ansible/pubsub-state-checkout-deploy.yml)
   (checkout on Orin-B).

The mirror authenticates to the snapshot endpoint with a dedicated
`pubsub-state-mirror` **service operator key** dropped into Orin-A's
`operator-keys.d` (reloaded every 30s, survives portal redeploys) — not a
deploy-env credential.

## Turnkey wiring — every new goatnet, no fork

The deploy plays are **site-agnostic** (`-e target=<group>`, like `ory-deploy.yml`
/ `zenoh-deploy.yml`); a goatnet's specifics live in its
`group_vars/<group>/pubsub-state.yml` (the `zagros_prod_hosts` one is the
reference). Repl rides the two enforced standup paths, it is not a bespoke
zagros-prod job:

- **Any goatnet the driver builds** (`goat-standup.sh`): when the site declares
  an obs host, the driver's obs leg runs `utility-host-bootstrap.yml -e
  target=<site>_obs_hosts` (docker data-root pinned on the NVMe, *then*
  Forgejo) in parallel with the core phases from phase 2, joins it before
  phase 12, mints the mirror bot there if `FORGEJO_PUBSUB_TOKEN` is unset, and
  then runs `pubsub-state-mirror-deploy.yml -e target=<group>` when the goatnet
  declares a `pubsub-state.yml` group_var (skips cleanly otherwise). Flat
  goatnets with no obs host get the mirror against Forgejo + MinIO on the mgmt
  host; with no passive node there is no checkout to deploy.
- **Core A/B pair** (`<env>-bootstrap.yml`): the same utility play brings
  Forgejo up on the obs Orin (the `zagros-prod-utility-bootstrap.yml` name is a
  wrapper that pins the target); then `pubsub-state-mirror-deploy.yml`
  (Router-A) and `pubsub-state-checkout-deploy.yml` (Router-B).
- **After any Docker data-root rebuild on the obs host, re-run the utility
  play** (`goat-standup.sh <site> --obs-only`, or the play by hand). Forgejo's
  containers live in that root and nothing else re-creates them.
  The acceptance row `pubsub-state-forgejo-primary-up` now fails the gate when
  the primary does not answer.

The two one-time-per-goatnet secrets:

- **Portal service key** — generated by `newsite-secrets.sh` (phase 3) as
  `<SITE>_PUBSUB_MIRROR_PORTAL_KEY`; the goatnet's group_var points
  `pubsub_state_portal_api_key` at it.
- **Forgejo mirror-bot PAT** — minted *after* Forgejo is up by
  [`ops/scripts/forgejo-mint-pubsub-bot.sh <forgejo-ssh-target>`](../../ops/scripts/forgejo-mint-pubsub-bot.sh),
  which creates the bot **as an admin** (a non-admin user cannot create the
  `goat-pubsub-state` org via the API even with a full-scope token — the
  org-create right is a user attribute Forgejo 7.x has no CLI to toggle) and
  writes the token to the operator envrc.

## Wire-up index

- **Reconciler binary:** [`ops/goat-zenoh-portal/cmd/pubsub-apply/`](../../ops/goat-zenoh-portal/cmd/pubsub-apply/)
- **Exporter binary:** [`ops/goat-zenoh-portal/cmd/pubsub-export/`](../../ops/goat-zenoh-portal/cmd/pubsub-export/)
- **Snapshot endpoint:** `POST /api/admin/snapshot` in [`ops/goat-zenoh-portal/internal/api/snapshot.go`](../../ops/goat-zenoh-portal/internal/api/snapshot.go)
- **Ansible role (mirror):** [`ops/ansible/roles/pubsub-state-repo/`](../../ops/ansible/roles/pubsub-state-repo/)
- **Ansible role (recovery checkout):** [`ops/ansible/roles/pubsub-state-checkout/`](../../ops/ansible/roles/pubsub-state-checkout/)
- **Apply playbook (nova-a single-host):** `ops/ansible/nova-pubsub-state-repo-bootstrap.yml`
- **Apply plays (site-agnostic, `-e target=<group>`):** [`pubsub-state-mirror-deploy.yml`](../../ops/ansible/pubsub-state-mirror-deploy.yml) · [`pubsub-state-checkout-deploy.yml`](../../ops/ansible/pubsub-state-checkout-deploy.yml). Forgejo on the obs host via `utility-host-bootstrap.yml` (`-e target=<site>_obs_hosts`; `zagros-prod-utility-bootstrap.yml` is its wrapper) — run by the driver's obs leg.
- **Mirror-bot mint:** [`ops/scripts/forgejo-mint-pubsub-bot.sh`](../../ops/scripts/forgejo-mint-pubsub-bot.sh) · **portal key:** `newsite-secrets.sh` (`<SITE>_PUBSUB_MIRROR_PORTAL_KEY`) · **standup phase:** `goat-standup.sh` obs leg (Forgejo, from phase 2) + phase 12 (bot mint, mirror deploy).
- **Site-rebuild runbook:** [`docs/operations/pubsub-state-rebuild.md`](../operations/pubsub-state-rebuild.md)
- **Implementation tracking:** `docs/project/implementation-plan.md` § Block 86
- **In-flight tracker:** `docs/project/in-flight.md` § block-86
