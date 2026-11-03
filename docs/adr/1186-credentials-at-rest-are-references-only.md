# ADR 1186 — Operator files hold references to credentials, never values, and the secret store is replaceable against written requirements

- **Status:** **Proposed.** Nothing changes on any goatnet on merge. On an operator laptop, the `.envrc` loader starts exporting `*.refs` reference files and prints a count of remaining plaintext files; plaintext keeps working until the operator sets `GOAT_REFERENCES_ONLY=1`.
- **Date:** 2026-09-22
- **Deciders:** operator
- **Related:** [ADR 0817](0817-credential-hygiene.md) (credential hygiene) · [`secret-store-requirements.md`](../design/secret-store-requirements.md)

## TL;DR (for a layperson)

The passwords and keys that run goat used to sit in plain text files on each
operator's laptop, hidden from git but copied by every backup, sync tool and
editor. Now those files hold only pointers — "the Postgres password for site X
is in vault Y" — and the real value is fetched from the secret store for one
command at a time and never written down. The default store is 1Password; an
organisation can use a different one if it meets a written list of requirements.

## Context

Keeping credentials out of git is one control. Keeping them out of gitignored
environment files is a separate one: left alone, those files become the largest
unmanaged copy of every credential a team holds.

goat is also adopted by organisations that cannot or will not use 1Password.
Code that hard-wires one vendor would push them back to plaintext files.

## Decision

- **The rule.** No credential value is stored in `.envrc.local` or
  `.envrc.local.d/`. Those locations hold references (`NAME=op://vault/item/field`)
  in `*.refs` files. The two recorded exceptions are the operator age identity
  (it decrypts everything else; replaced by a hardware-held identity per ADR
  0406) and AWS keys, which live in the OS keychain through `aws-vault`.
- **The loader** (`.envrc`) exports each `*.refs` file as references and refuses
  a file containing any non-reference assignment. Legacy plaintext `*.envrc`
  files still load, after the references and with a warning, until the operator
  sets `GOAT_REFERENCES_ONLY=1`.
- **Resolution is per command.** `ops/scripts/with-secrets <cmd>` runs the
  command under the store's injector (`op run --` for 1Password).
  `ansible-preflight.sh` does this itself for an operator in references-only
  mode, so documented commands do not change.
- **The store is a seam.** `GOAT_SECRET_STORE`, `GOAT_SECRET_RUN` and
  `GOAT_SECRET_REF_PREFIX` select it. What a replacement must provide — eleven
  must-haves and five should-haves, each with a test — is
  [`secret-store-requirements.md`](../design/secret-store-requirements.md).
- **Exposure is checked, not assumed.** A separate scanner covers repositories
  (all refs, including closed-PR refs; issue and PR comments) for credential
  shapes and, on an operator laptop, the live values themselves; its `at-rest`
  audit is the check that a laptop meets this rule.

## Alternatives considered

- **Resolve all references into the shell on `cd` (`op inject` from direnv).**
  Rejected: every value then lives in every process the shell starts, and the
  operator is prompted by the store on every directory change.
- **Encrypt the env files at rest (SOPS/age) instead of referencing a store.**
  Rejected as the default: a decrypted copy cannot be revoked and reads are not
  logged. It remains an allowed store where no server can be run, with those
  costs stated in the requirements.
- **Hard-wire 1Password.** Rejected: adopting organisations with a different
  store would fall back to plaintext files.
- **Refuse plaintext files immediately.** Rejected: a loader that stops loading
  plaintext at once strands every operator mid-migration. The opt-in switch lets each laptop prove itself first.

## Consequences

- An operator whose files are references must be signed in to the store to run
  anything that needs a credential. Offline resolution is a should-have (S1) and
  is unmeasured for 1Password.
- `op run` fails the whole command on one unresolvable reference, so a malformed
  reference (an item path with no field) blocks every run until fixed; the
  `at-rest` audit lists them.
- Retiring plaintext needs a check, before any deletion, that no stale copy
  holds an older value still in use.
