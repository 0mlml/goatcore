# goatlite — a first goatnet on one box, from one file

| Field | Value |
|---|---|
| Status | **Run once on hardware, not yet proven for its purpose. A goatlite is never for production: production is the full preset (§7).** Designed 2026-10-31; built 2026-11-02; first real run 2026-11-02 on a fresh cloud VM through `goatbuilder`: every automated phase completes in about 24 minutes of phase time, and the defects that run found are fixed. Proven for its purpose when the cold-read drill of §9 passes and the grow-out of §6 is measured (phases C and D). |
| ADR | [ADR 1213](../adr/1213-goatlite-preset-one-box-one-file.md) |
| Prepared for by | [ADR 1198 D6](../adr/1198-goatbuilder-config-driven-deploy-release-cli.md) ("goatlite is a preset, not a fork"); [goatbuilder.md §"A goatlite profile"](goatbuilder.md#a-goatlite-profile-future) |
| Bar it is measured against | [complexity-budget-and-encapsulation.md §"The bar"](https://github.com/usareur-af/goatdocs/blob/main/docs/design/complexity-budget-and-encapsulation.md#the-bar-definition-of-success): the 5-minute and 30-minute onboarding paths (ADR 1016 D2) |
| Procedure it is an instance of | [goatnet-standup.md](../operations/goatnet-standup.md), LAN profile. goatlite adds no second procedure (doctrine P1). |
| Learning ladder | [§8](#8-the-learning-ladder) and the goat gym lane `learner` |

## 1. What goatlite is

goatlite is the way a person who knows none of the open-source projects under goat stands up a working goatnet and uses it. They clone one repository, build two binaries from it, write one file of four facts, and run one command. What comes up is a real goatnet, not a demo: its own WireGuard mesh, its own identity service, its own data-fabric router, its own certificate authority, on one Ubuntu host or VM on their LAN.

Concretely, goatlite is `preset: lite` in the goatbuilder config ([schema](../../ops/goatbuilder/schema/goatbuilder-v1.schema.json) line 13), run through the existing fourteen-phase standup in its LAN profile. The preset supplies every value the full shape asks the operator for, and a derivation step writes the config surface the standup engines require. The same binary, the same playbooks, the same gates run; only what the learner has to know changes.

goatlite is not a smaller stack. ADR 1016 D3 holds: encapsulation, not removal. The substrates a goatlite runs are the ones a production goatnet runs at tier ceiling 1 (`site_deploy_tier_max: 1`, core and essential services; [service-criticality-tiers.md](service-criticality-tiers.md) line 86).

## 2. What the learner does

The learner's whole first hour, as the quickstart [`docs/tutorials/00-goatlite.md`](../tutorials/00-goatlite.md) states it:

1. Have an Ubuntu 24.04 host or VM on the LAN that they can SSH into with a key. 4 cores and 16 GB fit.
2. Clone this repository and build `goatbuilder` and `goat` on their laptop.
3. Write `goatlite.yaml`:

```yaml
schema: goatbuilder/v1
preset: lite
lite:
  name: pasture            # the goatnet's name; its zone is pasture.lan
  host: 192.168.1.50       # LAN address or hostname of the one box
  ssh_user: ubuntu
  admin: me@example.org    # the first and only administrator
```

4. Run `goatbuilder doctor`, then `goatbuilder standup pasture`.
5. When the driver prints the enrolment line, run it on the laptop, open the dashboard address it prints, and sign in with the one-time password it prints.
6. Publish and subscribe something with `goat`, and read `goat health`.

Nothing in that hour names NetBird, Ory, Zenoh, Traefik, Postgres, LUKS, age, Cloudflare, AWS, or 1Password. Each of those is a substrate the preset has already decided.

## 3. What the preset decides

Each row is a value the full shape asks for and the lite preset supplies. The "today" column names the existing fact the default rests on.

| Decision | goatlite default | Today |
|---|---|---|
| Topology | one environment, one founder site, one class `core`, no obs host, no core pair, no relays | the LAN profile skips phases 7, 8 and 11 and the relay-connect step ([goatnet-standup.md](../operations/goatnet-standup.md) line 276) |
| Converger and transport | `push-ansible`, `direct` | the only converger goat's own config uses |
| Cert mode | internal CA, file-delivered certs: `dashboard_cert_delivery_mode: file`, `mgmt_dynamic_cert_mode: file`, `has_acme: false`, `has_internal_ca: true` | `file` is the playbook default ([dashboard-deploy.yml](../../ops/ansible/dashboard-deploy.yml) line 109) |
| CA | minted by the render step on the first run (`ca-init --mode dev`, the learner's own age key as the single recipient); root committed to the learner's repo at `ops/enrollment/ca/<name>/`, key under `~/.goat/state/<name>-ca/`; `mint-site-certs.sh --mint-ca-if-missing` is the safety net in phase 4 | built 2026-11-02 in `goatlite-render.py` and `mint-site-certs.sh`; `ca-init` is [ops/enrollment/cmd/ca-init](../../ops/enrollment/cmd/ca-init/main.go) |
| Name resolution | `lan-coredns` authoritative for `<name>.lan`, bound to the LAN IP; peers receive it as their NetBird nameserver; enrolment apex `netbird.<name>.lan` | the `lan-dns` tag of `site-services.yml` (line 94) and the nameserver group the account-init play writes ([goat-netbird-account-init.yml](../../ops/ansible/goat-netbird-account-init.yml) line 130) |
| Host on its own mesh | `install_netbird_daemon: true` | an isolated node is not on any other mesh |
| Secrets | `secrets.store: env-file`; `goatbuilder secrets init --env <name>` writes the gitignored 0600 file | ADR 1204 D1, D4, D5 |
| Cloud | none. `GOAT_AWS_MODE=none`; no Cloudflare token; no Terraform tree | `with-aws` supports `none` ([ops/scripts/with-aws](../../ops/scripts/with-aws) line 8) |
| Images | pulled anonymously from the public namespace | ADR 1197 D1 (Proposed): the open core publishes signed OCI images. Until it lands, `GHCR_READ_TOKEN` is the one account goatlite needs, and the quickstart says so in one line |
| Disk encryption | `disk_encryption: true` by default; `false` is a declared exclusion for a VM | doctrine P2; standup decision 1 "Encrypt the data disk" |
| Administrator | `site_admins: [admin]`; the bootstrap identity and the NetBird infra service token are created by the driver | standup decision 2 "`site_admins` is you"; today the LAN path stops before phase 12 until the identity is made in the dashboard (goatnet-standup.md line 558) |
| Service tier | `site_deploy_tier_max: 1` | the tier model's minimal-node ceiling |
| Onboarding more people | the three-stage candidate pipeline with the mail step hand-delivered: the driver prints the links the mail would carry | the pipeline is built; the no-mail variant is designed here, not built |
| Promotion path | one rung, the goatnet itself | schema: a rung requires only `name`, `kind`, `environment` |
| Cert identity (Class A) | the URI-SAN scheme, CN suffix and onboarding filenames derived from `name` | each is per-goatnet machine configuration |

## 4. The derivation step

The learner writes four facts; the standup engines read a file set. The preset bridges them by writing the files, not by reading a different shape.

`goatbuilder standup` under `preset: lite` runs `ops/scripts/goatlite-render.py --site <name>` as the first phase-1 gate (built 2026-11-02; `goat-standup.sh` runs the same line). The renderer, in order and idempotently:

- writes the cert manifest first, then, when both CA halves are absent, mints the goatnet's CA through `mint-site-certs.sh <name> --mint-ca-only` (the learner's age identity created if absent; root at `ops/enrollment/ca/<name>/root-cert.pem`, public key at `ops/enrollment/public-keys/`, key under `~/.goat/state/<name>-ca/`); an existing CA is kept and its id read back;
- writes the inventory group `[<name>_hosts]` with the one host into `ops/ansible/inventory/mgmt-hosts.ini`, between goatlite markers;
- writes `group_vars/<name>_hosts/` (`vars.yml`, `spire.yml`, `grant-keyd.yml`, `minio.yml`, `audit-subscriber.yml`) carrying the §3 values with `name`, `host`, `zone` and `admin` threaded through every Class-A string, and a class waiver for the rest of the reference file set in `.groupvars-parity.yaml`;
- writes the goatnet's row and its per-component deviation blocks into the sidecar pins table `ops/pins/lite-pins.yaml` (never into `fleet-pins.yaml`, §5 row 8), the `direct` door entry in `doors.yaml`, and the declared-absent rows in `cross-env.yaml` and `goatnet-runners.yaml`;
- runs the pins generator for the merged table and signs the goatnet's intent with the CA it minted;
- writes `PLATFORM_ADMIN_SSH_PUBKEYS` from the learner's public key into the secrets posture.

Every whole file it writes carries a first-line header naming the renderer; a file whose header is gone has been taken over by the learner and is reported, never overwritten. `--check` exits non-zero on drift; `--remove` undoes the render, CA material included, which is also the retire path for a lite goatnet.

Then the ordinary engines run unchanged: `standup-config-preflight.py` gates the rendered surface, the fourteen phases run in the LAN profile, `goatbuilder assay` gates the result. The rendered files are real, committed and reviewable. The doctrine's "a config surface is a FILE SET, not a vars.yml" (P3) holds because the file set exists; the learner just did not type it.

## 5. Zero accounts on the first run, and what stands in the way

A goatlite first run should need no account with any outside service. Each row is one place the LAN path still reaches for one, with the build that closes it.

| # | Where an account leaks in today | Evidence | Closes with |
|---|---|---|---|
| 1 | `goatbuilder doctor` requires the `aws-vault` binary and runs `sts get-caller-identity` whatever the profile | [doctor.rs](../../crates/goatbuilder/src/doctor.rs) lines 67–71, 868–884 | doctor reads the profile; under `lan` with `GOAT_AWS_MODE=none` the AWS rows are skipped as declared |
| 2 | doctor reads `GOAT_AWS_VAULT_PROFILE`; the driver reads `GOAT_AWS_PROFILE` | doctor.rs line 868; [goat-standup.sh](../../ops/scripts/goat-standup.sh) line 195 | one variable name, `GOAT_AWS_PROFILE` |
| 3 | Phase 3 writes new secrets only to `1password`, `openbao` or `custom` | [newsite-secret-store.py](../../ops/scripts/newsite-secret-store.py) line 135 | the store-generic generator already dated 2026-11-30 in [pluggable-dependencies.md](pluggable-dependencies.md) row 8 |
| 4 | Phase 12 `netbird-service-identities.py --mint` writes to 1Password; the driver never passes `--no-1password` | [netbird-service-identities.py](../../ops/scripts/netbird-service-identities.py) line 250; goat-standup.sh line 785 | the driver passes the flag whenever `secrets.store` is not `1password` |
| 5 | Phase 4 expects a CA root to exist; the dev blob is "out-of-band from the trunk owner" | goatnet-standup.md line 124; [mint-site-certs.sh](../../ops/scripts/mint-site-certs.sh) line 85 | phase 4 calls `ca-init --mode dev --allow-single-recipient` when the manifest's root does not exist, generating the age key first if absent |
| 6 | The LAN path needs the bootstrap identity made in the dashboard before phase 12, and the infra service-user token made in the dashboard UI | goatnet-standup.md lines 502–520, 558 | the management service seeds `svc-<name>-netbird-infra` and writes its token to a root-only file at first account creation (a change in the forked management binary); the driver reads the file. Until that lands, the driver prints the exact dashboard steps with the one-time password |
| 7 | Images are private; every host needs `GHCR_READ_TOKEN` | goatnet-standup.md line 124 | ADR 1197 public images. Until then the token is the one account, named in the quickstart |
| 8 | Every goatnet's signed pins intent hashes the whole `fleet-pins.yaml`, so a learner's row would invalidate goat's three intents in their checkout, which they cannot re-sign | `intent.py` and `pins.rs` `build_body`, `sources` | a lite goatnet lives in the sidecar table `ops/pins/lite-pins.yaml`; both loaders merge it; goat's intents keep their sources, a lite intent lists both tables |

**Status 2026-11-02.** Rows 1, 2, 3, 4, 5 and 8 are built. Row 6 is built as the printed phase-11 step on every LAN site; the driver-made token (a change in the forked management service) is not. Row 7 is owned by ADR 1197.

## 6. Growing out of lite

A goatlite is a LAN goatnet. The internet-enrollable shape, with relays, a public zone and ACME, is the same fourteen phases with 5-relay, 7, 8 and 11 active. Growing is an edit to the `lite:` block and a re-run, not a new goatnet:

```yaml
lite:
  name: pasture
  host: 192.168.1.50
  ssh_user: ubuntu
  admin: me@example.org
  internet:
    zone: pasture.example.org
    aws_profile: pasture
    cloudflare_token: op://…   # or env-file key, per the declared posture
```

Derivation re-renders the cert mode to `acme`, the relay inventory and the Terraform tree; `standup --from 5` runs the internet-only steps; phase 8 re-mints the router cert against the mesh IP, which is the existing coupled-fact step (standup decision 3). goatnet-standup.md says today that growing a LAN site "is not free" (line 103). Making it free is a stated target of this design, measured in phase D. Until measured, the quickstart says what it costs.

## 7. What goatlite is not

- Not a second procedure. `check-deployment-doctrine.sh` must see the quickstart point back to goatnet-standup.md.
- Not production, now or later. No obs host, no core pair, no Spock, no relays, tier ceiling 1. Those are the full preset, and growing out (§6) adds reach, not those. A production goatnet is stood up with the full preset.
- Not a participant appliance. A learner who wants to join someone else's goatnet runs a moon-pod ([goat-moon-pod.md](https://github.com/usareur-af/goatstuff/blob/main/docs/design/goat-moon-pod.md)); rung 2 of the ladder.
- Not a fork of the playbooks. Any lite-only playbook is a defect against ADR 1198 D6.

## 8. The learning ladder

goatlite is rung 4 of a ten-rung ladder from "I have a welcome package" to "I share sealed data with another sovereign". The ladder is the goat gym lane `learner` ([ops/gym/gym.yaml](https://github.com/usareur-af/goatstuff/blob/main/ops/gym/gym.yaml)). Each rung names the thing it runs on and that thing's status.

| Rung | The learner | Runs on | Status |
|---|---|---|---|
| 1 | uses an existing goatnet: enrols a laptop, reads what is published, uses the CLI | gym newcomer modules | Built |
| 2 | builds a moon-pod and joins with it | the `moon-pod` reference implementation | Reference implementation; design Proposed |
| 3 | rehearses a new goatnet on the fixture | `goatbuilder tutor`, tutorial 01 | Built, CI-checked |
| 4 | stands up a goatlite and uses it | this design, [the quickstart](../tutorials/00-goatlite.md) | Run once on a fresh VM, every automated phase complete, the put/subscribe round trip passing; cold-read drill under way |
| 5 | grows it to an internet-enrollable goatnet | §6; goatnet-standup.md internet profile | Driver built; not turnkey-certified; §6 unmeasured |
| 6 | breaks it, reads `goat explain`, fixes it, tears it down and rebuilds | `goat explain`, `goatbuilder assay`, `goatbuilder teardown` | Verbs built; module not written |
| 7 | federates a first node inside it | tutorial 02, `goatbuilder federate` | Rehearsed on the fixture; no hardware run |
| 8 | adds a boundary node | tutorial 03 | Written and rehearsed |
| 9 | federates across the boundary to another sovereign | ADR 1157 | Built and run between purpose-built environments. Not yet run between two goatnets or with a real counterparty |
| 10 | releases data across the boundary, then seals it to one receiver with grant encryption | [partner-data-exchange.md](partner-data-exchange.md), [grant-crypto-sdk.md](grant-crypto-sdk.md) | Release path measured on a live goatnet; cross-sovereign unmeasured |

Rung 6 is on the ladder because the one-week troubleshooting path is the bar's hardest test (complexity-budget-and-encapsulation.md, "The bar").

## 9. Acceptance

goatlite is done when a person who did not write it, with no AI and only `docs/tutorials/00-goatlite.md`, takes a fresh Ubuntu VM to a green `goatbuilder assay` and a round-trip publish, and every other document they had to open is recorded. That is a cold-read drill applied to the lite preset, and the 5-minute and 30-minute paths of ADR 1016 walked for real.

**The drill, as assigned 2026-11-03:** two people who did not write goatlite each run it, separately, on a fresh Ubuntu 24.04 VM on their own network, from this tutorial alone, and record where they stopped by phase number and every other page they opened. goatlite is proven for its purpose when both reach a green assay and a round-trip publish.

Two numbers are recorded as a measurement: wall-clock from `standup` to green `assay`, and the count of documents opened beyond the quickstart.

## 10. Build phases

| Phase | Delivers | Proof | Date |
|---|---|---|---|
| A | schema `lite:` block; preset-supplied `repo`, `components`, `environments`, `promotion`; derivation (§4); doctor profile-aware (§5 rows 1, 2); fixture `goatnet-from-scratch/00-goatlite` and a `--mock` rehearsal in CI | `goatbuilder standup pasture --mock` converges the fixture from the four-line config | **built 2026-11-02** (dated 2026-11-21) |
| B | zero accounts: the CA minted by the render step, store-generic phase 3, no 1Password write off that posture, the LAN phase-11 step printed, the sidecar pins table (§5 rows 3–6, 8) | a LAN standup on a VM with no `op`, no `aws-vault`, no Cloudflare token in the environment, logged | **built 2026-11-02** except the driver-made token; the VM run is phase C's drill (dated 2026-12-12) |
| C | `docs/tutorials/00-goatlite.md` (**written 2026-11-02**); gym module `goatlite-first-goatnet`; the drill (§9) | the drill's two numbers recorded | 2026-12-19 |
| D | growing out (§6) measured on the same VM | internet profile reached from the lite goatnet without a re-standup; time recorded | 2027-01-16 |

Phase B row 7 (public images) is owned by ADR 1197 and is not on this schedule.
