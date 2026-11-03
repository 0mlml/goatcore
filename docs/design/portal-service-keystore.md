# Portal service-key store — decoupling machine credentials from the deploy env

**Status:** built + **deployed + proven live on ZAGROS 2026-07-08 (M-078): the runner key survived a portal redeploy carrying no runner key in the env** ([ADR 1070](../adr/1070-portal-service-key-store.md)). Move 1 (human-key
shared source) is a separate, deliberately-sequenced follow-up — see §"Not this change".

## The problem

The portal renders its **entire** operator-key set from the deploy environment:
`portal_operator_api_keys: "{{ lookup('env', 'PORTAL_OPERATOR_API_KEYS') }}"`. That env
comes from `.envrc.local.d/30-site-services.envrc` — gitignored and **per-laptop**. So a
portal deploy doesn't *add* keys, it **overwrites the whole set** with whatever the deploying
laptop's direnv holds.

That is a structural fragility for every operator key, but it became acute for a **machine
credential**: the onboard-runner (the onboarding auto-executor) authenticates to the portal
with a dedicated key `onboard-runner-<site>`. On 2026-07-08 a portal upgrade deployed from a
different operator laptop — whose direnv lacked that key — silently **dropped it**, and the
runner started 401ing (a WARN in a container log; nothing a human notices the way they notice
their own key vanishing). The deeper defect: **a machine credential should not live in a
human's personal laptop secret file at all.**

## The change

The portal gains an **additional, additive** operator-key source: an on-host directory read
at startup and **merged** into the env-derived key map.

- Portal config: `PORTAL_OPERATOR_API_KEYS_DIR` (a dir of `handle:key` line files). Merged
  into `OperatorAPIKeys`; **the env set wins on a handle conflict** (env is the live deploy
  intent; the dir supplies keys the env doesn't carry). Absent/empty dir is a **no-op** — a
  portal with a bad or missing keystore behaves exactly as one configured with env keys only,
  so the blast radius is contained. A malformed line is a hard config error (a typo can't
  silently drop a service's auth).
- The keystore lives under `portal_data_dir` (`/opt/goat-zenoh-portal/operator-keys.d`),
  which is already host-mounted + persistent — so **no portal deploy from any laptop can drop
  a key written there**, because it isn't sourced from the deploy env.
- Each **service owns its own key**: the service's ansible role generates the key once,
  persists it on the host, and writes `<service>.key` into the portal keystore. The
  onboard-runner role does this (`onboard_runner_key_file` on the host →
  `onboard-runner.key` in the keystore + the runner's own env), seeding from the existing
  direnv value on first deploy (seamless migration) then never regenerating.

Net effect: the runner's key is a **per-site machine secret living with the infrastructure it
belongs to**, not in any human's direnv. Human per-person keys are unchanged.

## Invariants

- **Additive + fail-safe-open-for-humans:** the env key set is still authoritative for human
  keys and wins every conflict. Removing the keystore reverts to today's behavior exactly.
- **Machine keys only.** Never put human per-person keys in the keystore — those stay
  env-managed so they remain *removable* (a keystore entry is add/accumulate; you revoke by
  deleting the file + redeploying, which is right for a service, wrong for a person).
- **One writer per key.** A service's own role is the sole writer of its keystore file.

## Not this change (Move 1, sequenced follow-up)

Human per-person keys still diverge across operator laptops (a deploy from a laptop missing
someone's key drops it). That is lower-severity — a human notices, and it fails closed — and
its clean fix (an **age-encrypted committed keys file**, mirroring the existing
`portal_ca_bundle_dir/ca.key.age`, decrypted at deploy so every laptop renders an identical
set) is a deliberate change to how the live portal sources *all* human auth. It gets its own
ADR + additive-with-env-fallback rollout, not folded in here.
