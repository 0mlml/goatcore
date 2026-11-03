# Platform-admin self-service onboarding — native, guardrailed, operator-run

**Status:** proposed 2026-07-09. Problem statement + target design.

## The problem (concretely)

Onboarding a **participant** is native: `goat admin onboard <handle>` runs the
native fast path (`onboard_native`, `crates/goat-cli/src/cmd/admin.rs`) —
provisions the portal (Zenoh cert/slot), NetBird, and Ory Kratos surfaces
**directly in Rust, no Ansible, seconds not minutes**.

Onboarding an **operator / platform-admin** did NOT follow suit.
`goat admin operator onboard <handle>` exists but **shells out to
`ops/ansible/operator-tier-onboard.yml`** (admin.rs `operator_onboard` →
`ansible-preflight.sh operator-tier-onboard.yml`). And that playbook was a
first-cut that had never been live-fired, printed two "apply by hand" steps, and
left several things to the operator's judgement.

What that path costs:
- **Manual SSH-key install** — the operator's `ssh_pubkey` was delivered out of band and
  hand-pasted into the manifest before the play could run.
- **Manual portal operator key** — the play *prints* `ZAGROS_PORTAL_OPERATOR_API_KEYS="...,handle:op_..."` for the operator to add + redeploy the portal by hand.
- **Manual age-recipient roster edit** — same; a printed "add this to
  `40-dev-ca-operators.envrc`" step.
- **Undelivered login credential** — the Ory identity existed but had no
  credential, so the admin couldn't log in; a recovery link had to be minted
  separately (now tracked as **F-241 — undelivered Ory credentials**).
- **Uncommitted roster** — the operator manifest was a locally-generated file not
  in the repo; the ceremony's edits lived only on one laptop.
- **No verification** — nothing confirmed, end to end, that the operator could
  actually SSH + log in + act; you found out by trying.

Net: completing a platform-admin required **an interactive session with admin
privileges** running SSH, sqlite, ansible, and secret-file edits by hand. That is
not something another platform-admin can reliably do, and it is the explicit thing
we need to eliminate: **a platform-admin must be able to onboard another
platform-admin, end to end, correctly, from their own operator laptop — with no
privileged assistant in the loop.**

## Target

Make `goat admin operator onboard <handle> --env <env>` **native**, the operator
analog of the participant native path — one command, run by any authorized
platform-admin with *their own* operator credentials, that provisions every
surface, delivers the artifacts, records the roster, and prints a **green
end-to-end verification**. Retire the Ansible wrapper (keep it available behind
`--ansible` only as a fallback during the transition, then remove it).

### What "native" means here (extend `onboard_native`, don't wrap Ansible)

The operator tier is the participant tier **plus** a small, well-bounded set of
additions. Each is doable in Rust against an API or a local key — no playbook:

| Surface | Participant native today | Operator addition | Native mechanism |
|---|---|---|---|
| Ory Kratos | creates identity + groups | ADMINS group + a **delivered credential** | Kratos admin API (already used) + mint-and-deliver a recovery link (closes F-241) |
| Portal | mints Zenoh cert/slot | **portal operator key** | a portal endpoint to mint an attributed operator key + write it to the management host's keystore dir (the runner-key pattern), returning the plaintext once — no direnv hand-edit, no full redeploy |
| Zenoh cert | portal-minted publisher leaf | **operator-trial-peer leaf** (CA-signed) | Rust cert-sign using the age-decrypted CA key the operator already holds (`~/.goat/state/.../ca.key.age` + `~/.config/age/keys.txt`) |
| Host access | n/a | **SSH key → mgmt-host `authorized_keys`** | native SSH append (idempotent), or a small host-side authorized-keys reconciler the operator command drives; the one genuinely host-level step, bounded + verified |
| Roster | manifest is the input | **committed, spillage-safe roster row** | write/commit the manifest through the identity-spillage-safe path; `ssh_pubkey`/`age_recipient` handled per the F-180 convention |

### Guardrails (the "guiderails")

1. **Operator-run, least-privilege** — runs with the invoking operator's creds
   (their portal operator key + their age identity + their SSH access), not root
   and not a shared admin session. Authorization to onboard a platform-admin is
   itself checked (only a platform-admin may mint a platform-admin).
2. **Validate before touching anything** — manifest lints, `ssh_pubkey` parses,
   tier is within the inviter's authority, env is real, target reachable.
3. **Idempotent + fail-closed** — re-runnable; a half-completed operator converges,
   never double-issues (compose with ADR 1048 collision-safety); any surface that
   can't be provisioned aborts with a precise pointer, never a silent skip.
4. **Deliver, don't print-and-hope** — the bundle + the login credential are
   produced and handed off as first-class outputs (secure OOB), not left as
   "now go do these two things by hand."
5. **Green verification** — the command ends by *checking* each surface (SSH
   reachable, Ory login credential present, portal key resolves, cert validates)
   and prints a single honest pass/fail per surface. The operator knows it worked.
6. **Audited** — the operator-action-audit chokepoint records who
   onboarded whom, at what tier.

## Boundaries / open questions

- **The SSH host step is the only non-API surface.** Options: (a) native SSH
  append to `authorized_keys`; (b) a host-side reconciler that reads the declared
  operator roster and converges `authorized_keys` (more like the rest of the
  substrate). Decide during build; (b) is more idempotent + auditable.
- **Portal operator-key minting needs a portal endpoint** (mint + keystore-write,
  return-once). Additive; mirrors the existing per-handle key model.
- **Credential delivery** — the recovery link is mesh-internal today; for a
  remote new operator, delivery channel must be decided (mesh-reachable, or a
  short-lived out-of-band hand-off). Not unique to operators; solve once.

## Build sequence

1. Portal: add the operator-key mint endpoint (mint + keystore-write + return-once).
2. goat-cli: `operator_onboard_native` extending `onboard_native` — Ory ADMINS +
   credential mint, portal operator key, operator cert (Rust cert-sign), roster.
3. The SSH host step (native append or host reconciler) + verification pass.
4. Flip `goat admin operator onboard` default to native; keep `--ansible` fallback.
5. Live-fire on a throwaway operator (→ M-NNN), then remove the Ansible path.

## References

- Participant native path: `onboard_native` (`crates/goat-cli/src/cmd/admin.rs`).
- Legacy operator ceremony (retiring): `ops/ansible/operator-tier-onboard.yml`.
- operator-onboard-utilities.md — the operator-onboard tooling arc.
- [admin-operator-guide.md](../operations/admin-operator-guide.md) — operator getting-started.
- F-241 (undelivered Ory credentials), F-207 (operator-action audit), ADR 1048 (collision-safety), F-180 (identity spillage in committed files).
