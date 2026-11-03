# ADR 1060 — Universal deploy-rollback ledger

**Status:** Accepted — deployed + live-fired on ZAGROS 2026-06-25 (M-075).
**Extends / supersedes:** [ADR 0859 — easy rollback as an engineering principle](0859-easy-rollback-engineering-principle.md). 0859's three substrate elements stay valid; this ADR makes the capability universal, background, artifact-type-aware, and fail-closed.
**Anchor design:** [`docs/design/universal-deploy-rollback.md`](../design/universal-deploy-rollback.md).

## Context

ADR 0859 made "roll this service back to its last known-good image with one command" a per-service invariant: an in-repo `deploy-history.yml`, an on-host `deploy-history.jsonl`, and `ops/scripts/rollback.sh`. The pattern works. Its *adoption* does not — it is opt-in and copy-pasted, so coverage has drifted away from the invariant.

Re-verified this session (grep over `ops/ansible/roles/`, evidence not inference):

- **94** roles total; **55** actually deploy a container (the rollback universe — the precise count is the coverage ratchet's detector over `tasks/`+`handlers/`; an initial narrow grep estimated ~47, missing handler-driven `docker compose … up -d --force-recreate` deploys).
- **35** have an in-repo `deploy-history.yml` (what `rollback.sh` reads).
- **22** carry the hand-copied per-host runtime-ledger task (`/var/lib/goat/<svc>/deploy-history.jsonl`).
- **12** group_vars files pin a `*_image:` *outside* the role defaults — invisible to both the CI gate (`check-deploy-history.sh` only scans `roles/*/defaults/main.yml`) and to `rollback.sh` (which reads `${ROLE_DIR}/deploy-history.yml`). The live ZAGROS portal is the worked example: pinned at `group_vars/zagros_hosts/vars.yml:1058`, and `rollback.sh goat-zenoh-portal` cannot target it because the authoritative ref does not live where the tool looks.

Three structural faults underlie the drift:

1. **Opt-in, copy-pasted.** The ~20-line runtime-ledger task is pasted per role, so new roles forget it and coverage decays.
2. **The tool reads the weaker record.** `rollback.sh` reads the in-repo `deploy-history.yml` (role-default `*_image` only, CI-gated) rather than the on-host jsonl, which is the *authoritative record of what actually ran* and covers role-default + group_vars + env-var pins uniformly.
3. **"Rollback" is treated as one operation.** Components are heterogeneous artifacts (image, config, ACL, data, cert) and some changes are deliberately forward-only. A single "roll back everything" verb is unsafe: a pin swap cannot undo a committed Postgres write or a destructive ACL cut.

## Decision

Adopt easy rollback as a **background, universal, fail-closed** capability — every deploying component gets it automatically, in every deployment.

1. **Host-runtime ledger is the source of truth.** Every deploying role appends `/var/lib/goat/<component>/deploy-history.jsonl` recording the ref it converged the host to. This covers role-default, group_vars, and env-var pins uniformly — the one record that always reflects what actually ran.
2. **A shared role writes it, auto-included via meta-dependency.** `roles/rollback-ledger/` (`allow_duplicates: true`) is depended on by every deploying role with zero per-role boilerplate beyond a dependency stanza naming the component, its artifact type, and its ref var. The ~22 hand-rolled tasks migrate to it.
3. **Artifact-type taxonomy is load-bearing.** The ledger tags each component `image | config | acl | data | cert | forward-only`. The tool dispatches by type and **refuses where a swap cannot undo the change**, printing the correct restore pointer instead of pretending.
4. **The tool reads host truth and dispatches by type.** `rollback.sh` (and the eventual `goat admin rollback`) reads the on-host jsonl as authoritative — finally covering group_vars/env-pinned components like the ZAGROS portal — and respects `reversible: false` / `do_not_roll_to`. Dual-sourced with the in-repo `deploy-history.yml` during the transition (additive, back-compat).
5. **Coverage ratchet, fail-closed.** A CI gate refuses a deploying role that lacks the shared ledger dependency. The covered set can only grow.

### Ledger schema

`{component, artifact_type, ref, prior_ref, ts, operator, reversible, restore_pointer?}` — additive over the 0859 jsonl. The 0859 fields (`image`, `action`, `via`) are retained; `ref` aliases `image` for the image type so existing `rollback.sh` reads keep working.

### Artifact-type taxonomy and dispatch

| type | example components | rollback mechanism | tool v1 behavior |
|---|---|---|---|
| `image` | portal, panoscope, relays, flashlight | re-pin + redeploy | **automated** (the 0859 path) |
| `config` | rendered compose/env, Caddyfile, generated zones | git-SHA revert + redeploy | **guided** — prints the revert + redeploy; auto in v2 |
| `acl` | Zenoh static ACL | git-SHA revert + *surgical* re-render | **guided / refuse** — full re-render clobbers portal-appended rules; never blind |
| `data` | Postgres, router rocksdb, MinIO/audit | backup-restore (pgBackRest, Object-Lock) | **refuse** + print restore pointer |
| `cert` | leaf certs | re-mint / rotate | **refuse auto** + print `goat admin rotate` |
| `forward-only` | Kratos schema `additionalProperties:false`, destructive ACL cuts, Object-Lock audit writes | none | **refuse** (`reversible:false` / `do_not_roll_to`) |

"Roll back where safe," never "roll back everything." An image component that also bears data (e.g. `postgres-spock`) records `artifact_type: image` for the rollable container but carries a `restore_pointer` and a data warning so the operator is told the image flip does **not** undo committed rows.

## Alternatives considered

- **Keep per-role opt-in (status quo).** Drift already proved it fails — 35/55 in-repo-ledger coverage and the live portal untargetable. Rejected.
- **Keep reading only the in-repo `deploy-history.yml`.** Cannot see group_vars/env pins; the exact gap that left the ZAGROS portal unrollable. Rejected.
- **One generic "roll back everything" verb.** Unsafe — pretends a pin swap undoes a committed write or a destructive cut. The taxonomy + refusal *is* the fix. Rejected.
- **End-of-role handler (append strictly on success) instead of a meta-dependency at role entry.** A handler needs a `notify` line in every role = the per-role boilerplate we are removing. Chose meta-dependency (records the intended converged ref at role entry); the tool reconciles every target against the live `docker inspect` before acting, so an optimistic row from a failed mid-deploy never drives a wrong rollback. Caveat documented in the design doc.

## Consequences

**Positive:**
- `rollback.sh` can target the ZAGROS portal (and the other 11 group_vars-pinned components) for the first time — host truth, not role-default truth.
- Coverage can only grow; a new deploying role cannot silently skip the ledger.
- Honest refusal on data/cert/forward-only prevents the dangerous illusion that a swap undid a committed change.
- One shared role replaces ~22 divergent copy-pasted tasks — one place to fix, no drift.

**Negative / cost:**
- 55 deploying roles gain a meta-dependency stanza (53 in the Phase-C fan-out after the 2 pilots); 22 hand-rolled tasks migrate; one new shared role + one new CI ratchet to maintain.
- The meta-dependency records intent-at-entry, not strictly on-success (mitigated by live `docker inspect` reconciliation in the tool).
- `allow_duplicates: true` is mandatory on the shared role — without it Ansible de-dupes the meta-dependency and only the first service role in a play gets a ledger row.

## Composition

- [ADR 0859 easy rollback](0859-easy-rollback-engineering-principle.md) — extended/generalized by this ADR.
- [ADR 0224 progressive config rollout with auto-revert](0224-progressive-config-rollout-with-auto-revert.md) — the `config` row's eventual auto path.
- [ADR 1044 credential rotation](1044-credential-rotation-in-place-reissue.md) — the `cert` row's mechanism.
- [ADR 0811 audit log integrity](0811-audit-log-integrity.md) + [`pgbackrest.md`](../design/pgbackrest.md) — the `data` row's restore pointers.
- `instance-variability-model.md` — why group_vars vs role-default pin classes exist (and why the ledger must span both).

## Tracker

[`docs/design/universal-deploy-rollback.md`](../design/universal-deploy-rollback.md). Track: `rollback-universal` — phased (A) ADR + design, (B) shared role + tool + ratchet proven on portal + panoscope pilots, (C) fan-out to all deploying roles.

See `GLOSSARY.md` for terms.
