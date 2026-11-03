# ADR 1203 — A self-hosted OpenBao credential store, outside every goatnet, selectable beside 1Password


| Field | Value |
|---|---|
| Status | **Superseded** (2026-10-31, operator) — the server-side-decrypting store was stopped and emptied; a zero-knowledge store replaces it (redesign ADR to follow). The sign-in decisions in ADRs 1209 and 1210 stand. |
| Date | 2026-10-29 |
| Deciders | operator |
| Repo scope | goat core (portable role and client tools); the team's instance lives in a private infra repo |
| Amends | [ADR 1186](1186-credentials-at-rest-are-references-only.md): adds a named second store behind the same seam |
| Related | `secret-store-openbao.md` (design), [`secret-store-requirements.md`](../design/secret-store-requirements.md) (the bar), ADR 1197 (public core, private overlay), ADR 1179 (cloud host bar) |

## TL;DR (for a layperson)

The team's passwords and keys live in 1Password, a hosted service. We now also run our own
store, OpenBao, on a machine we control. Either store can serve any laptop: one setting picks
which. The new store is not part of any of the networks it helps build, so it keeps working when
one of them is down. Nobody on the internet can reach it; team members reach it through the team
VPN and sign in with their team account.

## Context

ADR 1186 made operator files hold references and made the store replaceable against written
requirements. The team wanted a store that is open source, self-hosted, as safe as 1Password, the
same for everyone, with roles that segregate access, and able to run anywhere.

Three placements were evaluated: a distributed signed ledger on every laptop, a plain encrypted
ledger (SOPS with age), and OpenBao on a server the team runs. An independent re-evaluation, given
only the requirements and the designs, reached the same answer. The decisive facts:

- A rule enforced on a laptop is a rule its owner can decline. A chip-held key is bound to the
  machine, not to a program, so erase-on-removal in a client is cooperative.
- A ledger's git history keeps old partition keys wrapped to removed devices, so a removed person
  can open every value written before the re-key, including the classes that cannot be rotated
  without an outage (backup key, disk passphrases, database passwords).
- OpenBao enforces removal and records every read on a machine the team controls, and needs no
  security protocol written by the team.

Among self-hosted stores, OpenBao is the one whose free, open-source product carries every
requirement: Infisical gates groups, approvals and air-gapped use behind its Enterprise plan;
HashiCorp Vault is no longer open source; Bitwarden Secrets Manager self-hosting needs an
enterprise plan.

## Decision

- **OpenBao is the team's self-hosted store.** `GOAT_SECRET_STORE=openbao` selects it; references
  read `bao://<mount>/<path>/<field>`; `ops/scripts/with-secrets`, the `.envrc` loader,
  `ansible-preflight.sh` and the site-secret generator honour it. 1Password stays the default and
  keeps running in parallel until the team decides otherwise.
- **The store lives outside every goatnet.** It never runs on a goatnet host, never binds a
  goatnet mesh address (the role refuses `100.64.0.0/10`) and never signs people in through a
  goatnet's identity plane.
- **No public listener.** It binds a private address; people reach it through the team VPN, admins
  also through a Session Manager port forward. TLS always, with a CA made on the host.
- **Portable core, private instance.** The role (`ops/ansible/roles/openbao`, any Linux host with
  Docker) and the client tools (`ops/secrets/openbao/`) are in this repository. The team's
  instance (host, network, partitions, identity provider wiring) is in a private infra repo.
  Moving the store is a
  snapshot restore onto another host running the same role.
- **Four ways to sign in, one identity per person:** SSO through the team's Kanidm,
  a YubiKey certificate from the store's own user CA, an OpenBao username and password, and the
  one-time code (TOTP) that the password always requires. Only SSO depends on another service. Groups map to partition
  policies: `goat-secrets-user369x` reads and writes all five, `goat-secrets-software` reads
  the software partition; each exists as an SSO group and a local group with the same policies.
  Machines use AppRole, one partition each.
- **Unlock with a key on the host's encrypted volume** (OpenBao's static seal): it restarts without
  people and without any cloud service. Init output (root token, recovery keys) is age-encrypted to
  at least two admins.

## Alternatives considered

- **Signed ledger on every laptop, with an enforcing client.** Fully offline and serverless, but
  removal and read attribution depend on the leaver's own machine, git history defeats removal for
  values that cannot be rotated, and the team would own a security protocol. Right only if the
  team worked weeks at a time with no route to any server.
- **Plain encrypted ledger (SOPS with age).** Least to build; every departure becomes a rotation and
  reads are not recorded.
- **Infisical, Vault, Bitwarden Secrets Manager.** Paid tiers or non-open licences for features the
  requirements need.
- **AWS KMS auto-unseal.** Works only on AWS and needs a separately installed plugin in OpenBao 2.7.

## Consequences

- One small server to run: patching, watching the audit log's disk, a restore drill each quarter.
- A 2-of-3 recovery split is real only when each share is encrypted to a separate holder; one age
  envelope readable by every recipient makes it nominal.
- A laptop needs the team VPN (or the admin tunnel) to resolve values; the opt-in age-encrypted
  cache covers short offline periods and holds only values already read.
- Offboarding gains one plane: `ops/secrets/openbao/revoke-person` ends a person's sessions;
  removing them from the Kanidm group stops the next sign-in.
