# Admin group rename — `ADMINS` → `PLATFORM-ADMIN` (zero-lockout cutover)

**Goal.** Rename the platform-admin authorization group so it reads clearly beside `IAM-ADMIN` (`ADMINS` was asymmetric), **without locking any admin out of any gated surface at any point.** Rides the ADR-1043 §8 gated-rename discipline (sandbox-first, per-goatnet, with rollback).

**Why this needs a runbook and not a find-replace.** `ADMINS` is a load-bearing *enforcement* string, not a label. It lives in **two independent namespaces** that must stay consistent, and any step done out of order is a fleet-wide 403:

- **Ory / L7 RBAC** (the confusing-to-read one): the group name in [`ops/permissions/group-permissions.yml`](../../ops/permissions/group-permissions.yml) + `data-access-roles.yaml`, the `--allowed-group=ADMINS` on ~12 service gates, and the JWT `groups` claim the consent UI emits ([ADR 0857](../adr/0857-patched-selfservice-ui-for-groups-claim.md)).
- **NetBird / L4 mesh**: the group `<SITE>-ADMIN` (taxonomy suffix `ADMIN`). For the role vocabulary to be consistent (a role rides in *both* the NetBird group and the Ory claim, per identity-grammar.md §1.1), this renames to `<SITE>-PLATFORM-ADMIN` in the same cutover.

Gate roles carrying `ADMINS` (all must move together): `id-admin-console`, `oauth2-proxy-id-console`(+`-oidc-client`), `oauth2-proxy-dashboard`(+`-oidc-client`), `netbird-dashboard`, `minio`, `panoscope`, `stethoscope`, `relay-health`, `onboard-intake-collector`, `ory`. A CI parity fence ([ADR 1073](../adr/1073-rbac-adherence-principle.md), `check-group-permissions-parity.sh`) requires the matrix and the gates agree — a partial rename fails CI, which is the safety net.

## The zero-lockout principle: expand → verify → contract

Never remove the old grant before the new one is proven to work. At every moment during the cutover, **every admin is authorized via the old name OR the new name (or both)** — so no one is ever cut off.

## Pre-req (hard gate)

- **Control plane is stable.** Ory/Hydra/Kratos healthy on the management host; the mesh not mid-storm; ansible runs completing cleanly. Do NOT run the deploy steps through a flapping management host — a half-applied gate config is the exact lockout this runbook exists to prevent.
- A second admin session reachable, so if the operator's own token breaks mid-cutover there's an independent way back in.

## Phase E — Expand (additive; zero lockout — every step only *adds* access)

1. **Create the new Ory group `PLATFORM-ADMIN`** (Kratos/Hydra) — empty, references nothing yet. No effect.
2. **Dual-membership:** add every current `ADMINS` member identity to `PLATFORM-ADMIN` as well. Everyone is now in both. No effect (nothing checks the new group yet).
3. **Dual-accept the gates:** change every gate's `--allowed-group=ADMINS` → `--allowed-group=ADMINS,PLATFORM-ADMIN` (both accepted). Update `group-permissions.yml` to define `PLATFORM-ADMIN` with the `ADMINS` permission rows (keep `ADMINS` too, for now) so the parity fence passes. Deploy the ~12 roles. A member passes via *either* group → still no lockout.
4. **Claim carries the new group:** confirm the JWT `groups` claim emits `PLATFORM-ADMIN` (it will, once membership is set + the consent-UI groups-claim flow is in place — ADR 0857). Re-login once to mint a token carrying the new group.
5. **NetBird side (additive):** create `<SITE>-PLATFORM-ADMIN`, add every `<SITE>-ADMIN` peer to it, and (post-L4-cutover) ensure the matrix rule sources include both during the window.

## Phase V — Verify (the go/no-go gate before any removal)

- Every admin can reach **every** gated surface authenticating **via `PLATFORM-ADMIN`** (test with a token that carries only the new group, or confirm the gate logs show the new-group match). Enumerate the ~12 surfaces; each must return 200 for an admin.
- `check-group-permissions-parity.sh` green.
- If any surface fails on the new group, **stop** — you are still fully covered by the old `ADMINS` grant; fix forward, do not contract.

## Phase C — Contract (remove the old name; only after V is 100%)

6. Gates: `--allowed-group=ADMINS,PLATFORM-ADMIN` → `--allowed-group=PLATFORM-ADMIN`. Deploy.
7. `group-permissions.yml`: remove the `ADMINS` rows (now only `PLATFORM-ADMIN`). Parity fence green.
8. Ory: remove the now-unused `ADMINS` group (membership already mirrored to `PLATFORM-ADMIN`).
9. NetBird: retire `<SITE>-ADMIN` once the matrix + peers reference only `<SITE>-PLATFORM-ADMIN`.
10. Repo sweep: `grep -rn '\bADMINS\b'` returns only historical/ADR references, none load-bearing.

## Rollback

- **During Expand/Verify:** nothing to roll back — the old `ADMINS` grant was never removed; just revert the additive commits if abandoning.
- **After a bad Contract step:** re-add `ADMINS` to the affected gate(s) (`--allowed-group=ADMINS,PLATFORM-ADMIN`) + re-create the Ory group with membership; you are covered again within one deploy. This is why membership is mirrored (Phase E2) before any removal.

## Note — this is the template for the whole ADR-1043 role rename

The same expand→verify→contract shape retires the `OPERATOR`→`MONITOR` and `VENDOR`→`DEVELOPER` aliases. Do the admin rename first (highest read-confusion, smallest membership), prove the pattern, then batch the rest in one coordinated cutover so the role vocabulary lands consistent across NetBird + Ory + cert in a single deliberate move.
