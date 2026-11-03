# ADR 1070 — Portal service-key store: machine credentials off the deploy env

**Status:** Accepted
**Date:** 2026-07-08
**Design:** [`portal-service-keystore.md`](../design/portal-service-keystore.md)

## Context

The portal renders its whole operator-key set (`PORTAL_OPERATOR_API_KEYS`) from the deploying
laptop's gitignored per-laptop direnv, so any portal deploy overwrites the set with that
laptop's version. A machine credential caught this: the onboard-runner's key was silently
dropped by a portal upgrade deployed from a laptop that didn't carry it, and the runner 401ed
(failing silently in a log). A machine credential should not depend on a human's personal
laptop secret file.

## Decision

Add an **additive, on-host service-key store** the portal reads at startup and merges with the
env keys:

- New config `PORTAL_OPERATOR_API_KEYS_DIR` — a directory of `handle:key` files, merged into
  `OperatorAPIKeys`. **Env wins on conflict.** Absent/empty = no-op; a malformed line is a
  hard error.
- The store lives under `portal_data_dir` (persistent + host-mounted), so a portal redeploy
  from any laptop keeps keys written there.
- Each **service's own ansible role** generates its key once, persists it on the mgmt host, and
  writes it into the store. The onboard-runner role does this and seeds from the existing
  direnv value on first deploy (seamless migration), then reuses it forever.

Human per-person keys stay env-managed (removable); only **machine/service** keys go in the
store.

## Alternatives

- **Sync the direnv across laptops** — band-aid; doesn't decouple the machine credential from
  human secrets, and stays manual/lagging.
- **SPIRE/mTLS auth for the runner** — cleanest long-term (the runner is on the mgmt host where
  SPIRE is live), but the portal is not yet a SPIRE consumer; a larger change for later.
- **Age-encrypted committed keys file for the whole set** — the right fix for the *human*-key
  divergence (Move 1), but it swaps how the live portal sources *all* operator auth (an
  all-operator-lockout risk if botched); sequenced as its own ADR, additive-with-fallback.

## Consequences

- The onboard-runner key survives any portal deploy from any laptop — the fragility is gone
  for machine credentials.
- Additive + fail-open-for-humans: removing the store reverts to prior behavior exactly; the
  env key set is unchanged and authoritative.
- Human-key divergence (Move 1) is explicitly **not** solved here — a separate, deliberate
  follow-up.
