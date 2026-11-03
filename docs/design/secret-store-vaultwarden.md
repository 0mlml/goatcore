# Team credential store (Vaultwarden) — the zero-knowledge option

**Status: WIP — not for production use.** Built and measured on synthetic values on 2026-10-31; a live smoke (`ops/secrets/vaultwarden/smoke.sh`) last passed on the date in `ops/secrets/vaultwarden/STATUS.yaml`, which `posture.py doctor` prints as the `secrets:vaultwarden-status` row. The tag lifts when the operator's choice between this and the OpenBao option is recorded in ADR 1212 and one collection has been migrated from 1Password with byte-identical values on the admins' laptops. Until then no real team value enters the store.

| Field | Value |
|---|---|
| Status | Built 2026-10-31 for the goat team, on synthetic values only; gates measured in M-122; proved end to end in CI by `tests/test_vaultwarden_e2e.py` (a real server container and the real CLI). One of the two shapes of ADR 1212, built beside the OpenBao option; the operator chooses between them with both measured. 1Password stays the store until then |
| Canon | [ADR 1212](../adr/1212-team-secrets-never-decryptable-on-the-server.md) (R0 and the two shapes), [ADR 1186](../adr/1186-credentials-at-rest-are-references-only.md) (references only on disk), [ADR 1204](../adr/1204-declared-secrets-posture-and-zero-saas-quickstart.md) (one reference grammar) |
| Code | `ops/ansible/roles/vaultwarden` + `ops/ansible/vaultwarden.yml` (the server, any host), `ops/secrets/vaultwarden/` (the adapter, the gate scripts, `smoke.sh`, `STATUS.yaml`), `tests/test_vaultwarden_adapter.py` (mock CLI), `tests/test_vaultwarden_e2e.py` (real server + CLI, `.github/workflows/vaultwarden-e2e.yml`) |
| Instance | PRIVATE `usareur-af-infra`: `modules/aws/vaultwarden`, `environments/aws/prod/main_vaultwarden.tf`, `vaultwarden/` (inventory, settings, deploy driver), runbook `docs/deployment/VAULTWARDEN.md`. The instance's name, addresses, identifiers and the names of its secret items live there, not here (ADR 1197: the public core carries no deployment-specific addresses) |
| Measurement | M-122 |
| Related | [secret-store-requirements.md](secret-store-requirements.md) (the bar), secret-store-openbao.md (the other shape) |

## What it is

A Bitwarden-compatible server the team runs itself (its name is in the private instance
runbook), used through the Bitwarden clients: the web vault, the browser extension, the phone app and the `bw` command
line. Every value is encrypted on the person's device with keys derived from their master
password before it is sent; the server stores, serves and syncs ciphertext and never holds a key
that opens any of it. That is R0 of the requirements, and it is why the server stands on the
public internet with no VPN.

A laptop's files hold references such as `bw://<collection>/<item>/password`;
`ops/scripts/with-secrets` fetches each value through the `bw` CLI for one command and nothing
keeps it afterwards. It runs beside 1Password: each person chooses with one setting, and
unsetting it goes back.

```bash
export GOAT_SECRET_STORE=vaultwarden     # unset = 1Password (op://)
```

## Where it runs

| Piece | Where |
|---|---|
| Server | One small arm64 EC2 host (Ubuntu 24.04) in the team's cloud account, region 1, public subnet, fixed public address; Terraform `modules/aws/vaultwarden` in the private instance repo |
| Name | One DNS A record, not proxied; the certificate the host proves is the one clients see |
| Door for people | TLS on 443, from anywhere. Nothing else is open |
| Door for admins | Session Manager only; no SSH key, no port 22 |
| Disk | Encrypted root volume for the operating system and Docker; a separate encrypted data volume mounted at `/var/lib/vaultwarden` for everything the store writes. The disk-image gate images that device |
| Certificate | Let's Encrypt by DNS-01 with `lego` on the host; `_acme-challenge.<name>` is a CNAME into a small challenge-only Route53 zone, which the instance role may write TXT records in and nothing else. Renewed by a daily cron; Caddy restarts on a new chain |
| Edge on the host | Caddy 2.11.7 terminates TLS and proxies to Vaultwarden 1.37.4 on the compose network; both pinned by digest in the role defaults |
| Name resolution on the host | The server must reach the identity provider where the browser does. `vaultwarden_container_dns` can point the container at public resolvers for a network whose private zone shadows the provider's public name; the team instance needed it for one day and dropped it once its private zone answered the public edge (discovery document 200 from inside the container both ways) |
| Backups | Nightly: an online copy of the SQLite database plus attachments, sends, the JWT signing key and `config.json`, age-encrypted on the host to the platform admins and the break-glass identity, copied to a versioned bucket in the team's account |
| Mail | SES, from an address on the team's verified domain, through a send-only IAM user whose SMTP credential sits in Secrets Manager and is read by the deploy driver for one run |

The role knows nothing about AWS. Moving the store is: run `ops/ansible/vaultwarden.yml`
against the new host, restore the latest backup into `/var/lib/vaultwarden/data`, point the
DNS record at it.

## Signing in

Two things identify a person, and both are needed: the team's identity provider says who they
are, and their master password opens their vault. The identity provider never sees the master
password and the server never sees a key.

| Step | What happens |
|---|---|
| Single sign-on | The web vault and the apps send the person to the team's own Kanidm (OAuth2 client `vaultwarden`, scopes `openid profile email`). Only members of the Kanidm group the client's scope map names may sign in: it grants nothing to anyone else. `SSO_ONLY` is on, so the e-mail + master-password sign-in is refused |
| Master password | Set by the person on their first SSO sign-in, 16 characters at least (`SSO_MASTER_PASSWORD_POLICY`). It derives the key that opens their vault on the device. The server stores a hash for authentication and nothing that reconstructs the key |
| Second factor | A FIDO2 key, enrolled by the person in the web vault; the organisation policy "Require two-step login" makes a second factor mandatory for every member and is switched on by the first owner once their own key is enrolled (a member without a second factor is revoked when it is enabled). Kanidm's own sign-in is passkey-based as well |
| Command line | `bw config server https://<the store's name>` once; `bw login --apikey` once with the personal API key from Settings → Security → Keys (the API-key grant is the one grant `SSO_ONLY` leaves open, and it opens nothing by itself); `export BW_SESSION="$(bw unlock --raw)"` per shell, typing the master password to `bw` |

Accounts exist only by invitation from an organisation owner (registration is off); nobody but
the platform admins may create an organisation (`ORG_CREATION_USERS`). The admin page
`/admin` is gated by a token held in the team password manager (the item is named in the
private runbook); only its Argon2id hash is on the host.

## Who can read what

One organisation with a small number of collections, each sized by what a leak would cost. A person is granted whole collections by the organisation's owners; nobody is granted items one at a time.

Owners are the platform admins. Sharing is done on the device: an organisation key is
wrapped to each member's public key when they are confirmed, and every collection item is
encrypted under that key. Removing a member deletes their wrapped copy; what they already
synced stays readable to them, which is why removal is paired with rotation of what they could
have cached, as under 1Password.

Machines have no place in this shape yet: a `bw` login is a user account with a master
password. Machine secrets stay where ADR 1212 puts them for this shape, SOPS-encrypted files
with age, each host decrypting with its own key.

## The adapter

`ops/secrets/vaultwarden/run` is the injector `with-secrets` hands `bw://` references to.
It replaces every environment variable whose value is a `bw://` reference with the value at
that reference, in the child's environment only, then execs the command. One unresolvable
reference stops the run.

| Rule | How it is kept |
|---|---|
| Grammar | `bw://<collection>/<item>/<field>`; `<field>` is `username`, `password`, `totp`, `uri`, `notes`, or the name of a custom field. Three parts exactly; an item name is unique within its collection or the run refuses |
| Two calls, not N | One `bw sync`, then `bw list collections` and `bw list items`; every reference is resolved from that one decrypted listing in the adapter's memory. `GOAT_BW_SYNC=0` skips the sync and the CLI answers from its encrypted local copy |
| No master password | The adapter needs `BW_SESSION` from the person's own `bw unlock` and refuses without it. It never prompts for, reads or passes a master password |
| No value on disk or argv | Values travel from `bw`'s stdout into the adapter's memory and into the child's environment. `BW_SESSION` travels in the environment of each `bw` call, never in its arguments |
| One grammar | `op://`, `bao://` and `bw://` references may share one `.refs` file; `with-secrets` chains one injector per scheme present (ADR 1204). `GOAT_SECRET_STORE=vaultwarden` reads `.envrc.local.d/vaultwarden/*.refs` |
| Doctor | `posture.py doctor` reports the CLI, the unlocked session and malformed references for the store |

`tests/test_vaultwarden_adapter.py` runs 15 tests against a mock `bw` on PATH: the grammar,
byte identity, two listing calls for ten references, no value in any `bw` argv, collection
scoping, every refusal, no session means no run, a revoked session resolves nothing, and the
`with-secrets` dispatch.

## What the server can and cannot see

| The server holds | The server cannot |
|---|---|
| Ciphertext of every item, encrypted under the organisation key, which is itself wrapped to each member's public key | Decrypt any item, collection name or attachment: no key on the host opens them |
| Each person's e-mail, name, master-password hash (for authentication), their encrypted private key and encrypted user key | Reconstruct a master password or a user key from what it stores |
| The organisation's members, their roles and collection grants, as metadata | — |
| The event log: who signed in, who created, edited or shared which cipher id, and which cipher ids a client reported viewing, from which address, when (`ORG_EVENTS_ENABLED`, 365 days) | Know which reference a run used: the CLI reports a view of every organisation item it lists, so a run of the adapter is twelve "viewed" rows, not one. The row is the client's report; a client built to stay silent leaves none |
| The web vault's static code, served to browsers | — |

A root attacker on the host obtains: the encrypted database and attachments, the TLS private
key for the store's name, the JWT signing key (`rsa_key.pem`, which mints sessions but opens no
vault), the Argon2id hash of the admin token, the SSO client secret and the SMTP credential (both
in the root-only environment file), and the ability to serve altered web-vault code to the next
browser that visits. The last is the standing weakness of every server-served client, and it is
why the `bw` CLI, installed from a signed release on the device, is the adapter's client.

## Gates

Each gate was run on 2026-10-31 against the live server with synthetic values; the commands and
their output are in the measurement.

| Gate | Result |
|---|---|
| (a) plant one value, image the data volume as root, search the image | `dd` of the whole 10 GiB device to a file; 0 raw matches, 0 base64 matches for the value; the control string (an account e-mail, which the server stores in plaintext) found 74 times |
| (b) the planted value through `with-secrets` is the bytes `bw get` shows | identical sha256 (`ea77e866…`) |
| (c) a removed member cannot sync after removal | before: 11 collection items; removed from the organisation: next sync shows 0 items, 0 organisations, local store emptied; account deleted by an admin: next sync is 401 and the CLI is signed out |
| (d) ten references in one `with-secrets` call | 10 resolved, 10 distinct, 11.1 s and 11.6 s wall on a mobile uplink (one `bw sync`, two listings) |
| (e) what the event log records for a read | one `Cipher_ClientViewed` (type 1107) row per organisation item the CLI returned, with the acting user, the cipher id, the address and the second; 12 rows per adapter run whatever the number of references; `bw get` adds one row for its item |

## Using it

```bash
source <usareur-af-infra>/vaultwarden/client.env       # GOAT_SECRET_STORE=vaultwarden
bw config server https://<the store's name>             # once
bw login --apikey                                       # once; personal API key
export BW_SESSION="$(bw unlock --raw)"                  # per shell
ops/scripts/with-secrets terraform plan                 # values resolved for this one command
```

| Task | Command |
|---|---|
| Put the server on its host, or change its settings | `GOAT_DIR=<goat checkout> vaultwarden/deploy.sh` in the instance repo (secrets resolve for that one run) |
| Invite a person | an organisation owner, in the web vault; the person must already be in the Kanidm group the OAuth2 client's scope map names |
| Remove a person | remove them from the organisation and from the Kanidm group; delete the account under `/admin`; rotate what they could have cached |
| Restore | `age -d` the newest `vaultwarden-*.tar.age` from the bucket, stop the containers, unpack into `/var/lib/vaultwarden/data`, start |
| Export | `bw export --organizationid <id> --format json` by an owner (the exit format of R11) |
| Prove the server holds nothing readable | `ops/secrets/vaultwarden/gates/disk-image.sh <planted value>` as root on the host |
| Prove the adapter and the event log | `ops/secrets/vaultwarden/gates/client-gates.sh <collection> <item> <field>` from a laptop |
| Prove it still functions, and record the date | `ops/secrets/vaultwarden/smoke.sh bw://<collection>/<synthetic item>/<field> --record` from an unlocked CLI; six checks, writes `last_smoke` into `STATUS.yaml` |
| Prove the whole shape without a live server | `VW_E2E=1 python3 -m unittest tests/test_vaultwarden_e2e.py` with Docker and `bw` on PATH (what `.github/workflows/vaultwarden-e2e.yml` runs on every change and weekly) |

## Not in this build

Machine identities (R8), a server-side read log naming the value (R6 is client-reported
events, measured in gate (e)), and government-card sign-in; these are the points ADR 1212
names as what decides between the two shapes. No real team secret has entered the store.
