# onboard-runner

Pull-model auto-executor for the three-stage candidate onboarding flow. It makes
the UI flow's **"admin approves → it just happens"** real without putting any mint
capability on the web tier.

Design + credential-surface review: [`docs/design/operator-onboard-intake.md`](../../docs/design/operator-onboard-intake.md)
§"Fast-follow: fully-automated onboarding". The portal side (the atomic
`claim` + `result` endpoints) lives in `ops/goat-zenoh-portal`.

## What it does

Runs on a **trusted sovereign host** (the site Thor), on a poll interval:

1. `GET /api/onboard/proposals?status=approved` — the operator-approved queue.
2. For each (optionally filtered to `--only-env`): atomically **`claim`** it
   (`approved → onboarding`; a 409 means another runner won — skip).
3. **Dedup by email** against already-onboarded proposals (the native mint is also
   idempotent; this is the cheap first guard).
4. Materialize `manifest_json` → `ops/operators/<env>/<handle>.yml`.
5. Run **`goat admin onboard <handle> --env <env>`** — the native fast path (~3s,
   all three surfaces), stamping `GOAT_OPERATOR_HANDLE=<runner-id>` so the audit
   event reads *runner-executed / operator-approved*.
6. Write back **`result`** (`onboarded` | `failed`).

The portal never holds a credential and never initiates a privileged call — the
runner reaches *in*.

## Credentials (process env, never flags)

- `ONBOARD_RUNNER_PORTAL_KEY` — the operator-tier bearer for the portal contract.
- Whatever `goat admin onboard` itself needs, inherited from the env: the per-env
  NetBird API token, SSH to the site Thor's Kratos admin (the Ory leg), and the
  portal operator key (the Zenoh cert mint).

**This binary is a mint-anything capability.** Run it only on a tightly-controlled
host with least-privilege per-env tokens and its own attested identity; its
compromise model is an operator-laptop compromise, not a web-service one.

## Running

Start **safe** — prove connectivity + auth + the eligible set with no state change:

```
ONBOARD_RUNNER_PORTAL_KEY=… onboard-runner \
  --portal https://portal.zagros.netbird.zagros.example \
  --only-env zagros --once --dry-run
```

`--dry-run` polls + logs what it *would* onboard but never claims, mints, or writes
back. Drop `--dry-run` (and `--once`, for the daemon) to go live:

```
ONBOARD_RUNNER_PORTAL_KEY=… onboard-runner \
  --portal https://portal.zagros.netbird.zagros.example \
  --repo-root /opt/goat/repo --goat-bin /usr/local/bin/goat \
  --runner-id onboard-runner-zagros --only-env zagros --interval 30s
```

| Flag | Default | |
|---|---|---|
| `--portal` (`PORTAL_URL`) | — | portal base URL (required) |
| `--repo-root` | `.` | holds `ops/operators/` + the manifest schema |
| `--goat-bin` | `goat` | path to the goat binary |
| `--runner-id` | `onboard-runner` | `GOAT_OPERATOR_HANDLE` for the mint audit |
| `--only-env` (`ONLY_ENV`) | `` (all) | process only this env's proposals |
| `--interval` | `30s` | poll cadence |
| `--http-timeout` | `25s` | per-request portal timeout (lossy-mesh tolerant) |
| `--once` | `false` | one cycle then exit (cron/testing) |
| `--dry-run` | `false` | poll + log only |

## Collab account one-shots (`-collab`)

The cloud collaboration environment (Outline / files / chat behind Dex+Kanidm) is
a **separate identity plane**: a mesh revoke deliberately does not cut it
([collab-onboarding-provisioner.md](../../docs/design/collab-onboarding-provisioner.md)
§"Revocation is a separate decision"). These one-shots are the operator surface
for it — same provider, same credentials, no second client. They exit before any
portal/console wiring, so they need only `COLLAB_*` config.

```
onboard-runner -collab status -handle jane-doe        # is there an account?
onboard-runner -collab list                            # every account
onboard-runner -collab revoke -handle jane-doe -yes    # cut one (idempotent)
onboard-runner -collab reconcile --repo-root /opt/goat/repo
onboard-runner -collab reconcile --repo-root /opt/goat/repo \
  -emit-backfill /tmp/collab-backfill.yaml
onboard-runner -collab backfill -file /tmp/collab-backfill.yaml \
  --repo-root /opt/goat/repo            # dry run
onboard-runner -collab backfill -file /tmp/collab-backfill.yaml \
  --repo-root /opt/goat/repo -yes       # write
```

`reconcile` joins the provider's account list against the identity stores —
operator manifests (`ops/operators/**/*.yml`, authoritative) plus
`docs/operators/roster.json` (the committed projection, used when manifests are
absent — they are gitignored). It prints one row per discrepancy, loudest first,
and **carries handles, never email addresses**:

| Verdict | Meaning |
|---|---|
| `revoked-identity-live-account` | the F-274 headline: a revoked/inactive person still holds collab. **Action candidate** — cutting it stays a deliberate `-collab revoke`, never automatic |
| `account-no-identity` | an account matching no manifest and no roster member |
| `account-unrecorded` | live account, active person, nothing records it — the backfill set |
| `recorded-no-account` | a manifest names an account the provider lacks (reported, never auto-cleared) |
| `declared-no-account` | `collab.enabled` with no account — a provisioning gap |

A reconcile that finds discrepancies still exits **0**: it is a report, and
turning "found something" into "failed" would make the cron that runs it lie.
Exit 2 is a usage/config error (including an empty identity side — a wrong
`--repo-root` must not read as "every account is an orphan"), exit 3 an
operational failure.

`-emit-backfill` writes the durable record as a patch document — the observed
account expressed in the manifest schema's own `collab` vocabulary
(`enabled` / `provider` / `account` / `activated`; `activated_at` is deliberately
omitted, since the provider's list carries no creation date and inventing one
would assert something unobserved). `backfill` applies it, editing only those
keys in the parsed YAML so comments and unrelated blocks survive; it is a dry run
until `-yes`, idempotent, and refuses any patch path outside `ops/operators/`.

## Deployment

Deployed on the site Thor as a **container** (`ghcr.io/usareur-af/onboard-runner`,
built by the `onboard-runner-image` workflow) via the `onboard-runner` ansible
role — with its **own scoped portal identity**, not the operator's login. The
container joins the shared `mgmt` Docker network so it reaches the portal and
Kratos by container name, `sh -c curl`s the sibling Kratos admin API locally
(`GOAT_ONBOARD_KRATOS_LOCAL=1`), and bind-mounts the host `goat` binary. The
minimal goat profile (portal endpoint + the runner's own token) is materialized
at startup by `docker-entrypoint.sh` from the deploy env — no identity material
in the image.

`make build` / `make test` remain for local dev; the deploy artifact is the image.
