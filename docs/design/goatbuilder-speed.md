# goatbuilder speed — a goatnet in an hour, a node in fifteen minutes, a burn-down in fifteen

| Field | Value |
|---|---|
| Status | **Measured and partly built** (2026-10-29). Built: SSH multiplexing for direct hosts, per-phase timing, a static required-input scan as a phase-1 gate, phase 12 in dependency waves (`--parallel`), the bot-token pause removed, and a services teardown in both avenues. Designed with dates: phase-8 enrolment as a scripted step, host install per node class, a site-local image mirror, and the rest of the teardown |
| Design | [goatbuilder.md](goatbuilder.md) §"Standing up a goatnet in an hour" · [phoenix-architecture.md](phoenix-architecture.md) · [ADR 1201](../adr/1201-two-avenues-scripts-documented-cli-supported.md) (both avenues) |
| Targets | founder site declared → verified ≤ 60 min · one new node ≤ 15 min · burn-down to nothing, records kept ≤ 15 min |

## Where the day goes

Past buildouts took a day or more. Most of that time was not Ansible running.
It was a run stopping on a defect, the defect being fixed on main, and the run
resuming. Each defect cost a stop, a diagnosis, a pull
request and a resume. The runtime itself is the second cost.

Ranked by the minutes each costs a founder standup:

| # | Time sink | Cost before | Lever and status | Saving |
|---|---|---|---|---|
| 1 | Defects found during the run, one stop each | hours to days; the inchworm skill's case: 16 runs of a 5-min play to find 12 missing inputs, 80 min | **built**: `goatbuilder doctor --site <s>` and the phase-1 gate in both avenues scan every play the plan reaches for inputs it requires and secrets it reads, in one pass | **measured**: one scan takes 6–13 s per site. **Modelled**: each input it names is one stopped run avoided |
| 2 | A fresh SSH connection per Ansible task | 62–133 min | **built**: multiplexing for direct hosts | **modelled** from M-120's measured per-call cost: 14–30 min remain |
| 3 | Human pauses | each one waits on a person; any absence adds hours | **built**: the bot step reads its token and continues. **Designed**: phase 8 as a scripted step (2026-11-13). Phase 11 stays a person: the first NetBird token needs a browser login | one stop removed; phase 8 saves one more |
| 4 | Serial phase-12 steps | 14 steps one after another | **built**: every step declares `needs:`; `--parallel` runs them in 6 waves | **modelled**: 14 step-durations become 6, about 8 min at a minute a step |
| 5 | Host baseline + LUKS per host (215 tasks) | 10–20 min per host (**modelled**) | **designed**: an installer seed per node class (2026-11-27) | **modelled**: 3–5 min per host |
| 6 | Image pulls per role | unmeasured | **designed**: a site-local mirror on the obs host, plus a pre-pull of every pinned digest (2026-11-20) | warm mirror only: rebuilds and joiners, not the first standup |
| 7 | Fact gathering per play | small | `gathering = smart` with a one-hour cache | none needed |

Hosts in one play already run together: `ansible.cfg` sets `forks = 10`, and the
obs leg runs beside the core host. Sink 5's saving is per host, not per site.

## The budget

| Phase | Minutes | Basis |
|---|---|---|
| 1 gates, preflight, required-input scan, door | 3 | **measured**: the scan takes 6–13 s |
| 2 host baseline + LUKS (hosts together; installer seed per class) | 10 | **modelled**, sink 5 |
| 3–4 secrets, leaf certs, name snapshot | 2 | **modelled** |
| 5–6 mgmt stack, relay connect, identity | 15 | **modelled** |
| 7 public TLS and relay | 5 | **modelled** |
| 8 enrolment (scripted, 2026-11-13) | 3 | **modelled** |
| 9 router and workload identity | 5 | **modelled** |
| 10–11 dashboard, OIDC clients, first admin and token | 4 | a person, phase 11 |
| 12 services in 6 waves | 8 | **modelled**, sink 4 |
| 13 acceptance | 3 | **modelled** |
| 14 rescue classes and cross-goatnet surfaces | 2 | **modelled** |
| **Total** | **60** | |

The first timed run on hardware replaces the modelled rows. A new node is the
phase-1 and class-converge subset: ≤ 15 min. A burn-down of the services is
phase 12 in reverse: ≤ 5 min (**modelled**). The rest of a burn-down waits on the
designed teardown steps below.

## What is built

### The required-input scan (sink 1)

`ops/scripts/standup-required-inputs.py <site>` reads the plan that
`goat-standup.sh <site> --plan` prints. It follows every play, role, included
task file and meta dependency the plan reaches. It collects the variables each
one requires (`assert`, `| mandatory`) and the environment each one reads. Each is
checked against the site's group_vars, host_vars, role defaults and role
parameters. Secrets are checked through the secrets posture (ADR 1204), the same
way `goatbuilder doctor` checks them. A requirement inside a `when:` it cannot
evaluate is reported as possible, not as missing. It runs as the phase-1 gate in
both avenues and as `goatbuilder doctor --site <s>`.

| Code | Meaning |
|---|---|
| GB-060 | an input the run will reach is required and not set |
| GB-061 | a secret the run reads is one the secrets posture cannot supply |
| GB-062 | an input may be required: its condition cannot be decided before the run |
| GB-063 | a play's `--limit` selects no host, so the play does nothing |

Measured on 2026-10-29, with the secrets check off:

| Site | Requirements checked | Missing | Time |
|---|---|---|---|
| zagros-dev | 91 | 0 | 5.9 s |
| zagros | 101 | 0 | 8.4 s |
| zagros-prod | 132 | 0 | 13.1 s |
| fixture `ops/goatbuilder/fixtures/required-inputs` | 6 | 3, plus 1 secret | 0.1 s |

The scan found a real defect before it found zero. The relay-connect step of phase 5 on zagros runs
`zagros-rathole-client.yml`, whose hosts line requires `rathole_client_target`.
Neither avenue passed it, so every zagros standup stopped there. Both avenues now
pass the site's group.

### Phase 12 in waves (sink 4)

`ops/goatbuilder/standup-dag.yaml` names each phase-12 step, the steps it needs
and why. A step's wave is one more than the latest wave it needs. A need on a
step the run skips counts as met.

| Wave | Steps |
|---|---|
| 1 | ACL groups |
| 2 | ACL policies, service-identity mint, account baseline |
| 3 | ACL enforce, service-identity gate, mesh DNS |
| 4 | ACL gate |
| 5 | portal |
| 6 | pub/sub mirror, intake, runner, grant-keyd, synthetic publishers |

The dry run prints each step's `needs:` in both avenues, and the standup parity
gate compares them. Without `--parallel` the run is serial as before. With it,
each wave's steps run together with one log each. A failed step stops the run
after its wave, and the message names every failed step and its log.
`standup-dag.py --check` refuses a cycle or an unknown need.

### Pauses that became inputs (sink 3)

The bot step in phase 12 used to stop the run so a person could export
`FORGEJO_PUBSUB_TOKEN` and resume. It now reads the token from the operator's
envrc with `ops/scripts/envrc-var.sh` and continues. `goatbuilder` does the same.

### Teardown of the services

`ops/goatbuilder/teardown.yaml` declares how each standup step comes down. Both
`goatbuilder teardown` and `ops/scripts/goat-teardown.sh` read it, and the
teardown parity gate compares their plans.

| Action | Meaning | Steps |
|---|---|---|
| compose-down | stop the compose projects, volumes kept, proved by `docker compose ps -q` | synthetic publishers, grant-keyd, runner, intake, portal |
| keep | nothing to reverse | gates, the pub/sub mirror, the TLS certificate, the host baseline, the rescue nodes |
| refused | reversing it destroys data | the NetBird account rows, service identities, ACL, OIDC clients, Ory, the mgmt stack, the relay tier |
| designed | defined, not built | see the dated plan below |

A full teardown that reaches a refused or designed step refuses with exit 3
before touching anything. `--services` stops the compose-down steps in reverse
order and lists the rest as left in place. Each stop appends a row to
`/var/lib/goat/<component>/teardown.jsonl`. `deploy-history.jsonl` is never
touched, so rollback's history is unchanged.

Planned on zagros-dev on 2026-10-29, dry run only:

| Command | Result |
|---|---|
| `goatbuilder teardown zagros_dev --site zagros-dev --dry-run` | refused: 15 steps hold data or are not built |
| `… --services --from 12 --dry-run` | 5 services come down, 6 steps left in place |

The fixture rehearsal (`goatbuilder --mock`) verifies 5 components on 4 machines,
appends 8 teardown records, and finds all 8 already torn down on a second run.

## Designed, with dates

### Phase 8 enrolment as a scripted step — 2026-11-13

Phase 8 is a person today: run the `netbird up` line that
`ops/preflight-enrollment.sh` prints, check the peer connected, fill the mesh IP
into inventory and re-mint the router certificate. The scripted step:

1. Mints a single-use setup key on the host with
   `mgmt-host-bootstrap create-setup-key`, as `site-bootstrap.yml` already does.
   The key never leaves the host.
2. Enrols the host against a declared enrolment URL in group_vars, so the port
   rule from F-188 is a fact of the site and not a choice made at the keyboard.
   The rosenpass flags come from `rosenpass_enabled`.
3. Waits for `netbird status` to show the management connection and a mesh IP,
   writes the IP into the site's inventory and cert manifest, and re-mints the
   router pair.

Phase 11 stays a person. The first NetBird token needs a browser login by the
first admin. The zero-SaaS quickstart removes the other console steps.

### Host install per node class (sink 5) — 2026-11-27

A cloned disk image is ruled out. A cloned LUKS header gives every host the same
volume key, and host-site-node-disk-encryption.md
starts from LUKS created by each host's own installer. Ubuntu Pro attachment and
FIPS enablement are per machine with a token and a reboot.

The pre-bake is therefore an Ubuntu autoinstall seed per node class, not an image:

| Part | Where it runs |
|---|---|
| LUKS on the root disk, a passphrase per host from the secrets store | the installer |
| the baseline's package set, from the site's apt cache | the installer's late commands |
| Pro attach, FIPS enable, the TPM2 or Tang unlock slot | the existing plays, unchanged |

The baseline play then converges a host that is already most of the way there, so
most of its 132 tasks report no change. Acceptance is a timed baseline on a
seeded host against a stock one, recorded as a measurement.

### Site-local image mirror (sink 6) — 2026-11-20

The `docker-registry-mirror` and `apt-cacher-ng` roles exist. Only retired
plays deploy them, and `utility-host-bootstrap.yml` names them as steps still to
add. oci-goat-substrate.md §4 places the mirror on a site
cache host. The plan:

1. `utility-host-bootstrap.yml`, which the obs leg already runs, gains both roles
   on the obs host beside Forgejo, before the core host's phase 5.
2. The core host's Docker daemon lists the obs host as a registry mirror.
3. A phase-1 step pre-pulls every pinned digest the plan's roles will use, in
   parallel with the laptop-side phases 3 and 4.

The first standup of a site still pulls from upstream. A rebuild after a
burn-down, and every node that joins, pulls from the warm mirror.

### The rest of the teardown — 2026-11-20

| Step | What lands |
|---|---|
| mesh CoreDNS | the systemd service stop and the nameserver-group removal, with the account teardown |
| dashboard, SPIRE | a per-service stop, after the mgmt-host compose project is split |
| zenoh router | the teardown play reads the rendered per-site compose path |
| relay connect | the rathole client stop, with the relay teardown |
| obs leg | the obs host's stacks come down with the obs leg |
| cross-goatnet surfaces | the site leaves `cross-env.yaml` and every relay-health box is redeployed |

The refused steps stay refused. Destroying a NetBird account, an identity store
or the database is a decision a person makes by hand, not a teardown flag.

## Both avenues

Every lever lives in both drivers: the scan gate, the `needs:` lines and waves,
the bot-token read, the rathole target and the teardown. The shared facts live in
data files both read: `standup-dag.yaml` and `teardown.yaml`. The parity gates in
`goatbuilder-pr.yml` compare the standup plans, the
teardown plans, and the federate and deploy plans.

## The laptop stays awake

A run from a Mac holds an idle-sleep assertion for its own lifetime: the long
goatbuilder verbs spawn `caffeinate -i -w <pid>` (`crates/goatbuilder/src/awake.rs`)
and the drivers source `ops/scripts/lib/keep-awake.sh`, which does the same.
Without it a Mac that idles into sleep drops the mesh and stalls the play; a herd
deploy on zagros-dev took 1095 s instead of 145 s that way (#2122). One assertion
per run: `GOAT_KEEP_AWAKE` passes the holder's pid down, so a script that calls
the binary, or the reverse, does not start a second. `GOAT_NO_CAFFEINATE=1` opts
out; off macOS nothing changes.

## Measuring the hour

The first timed run on hardware is the number that matters:

```bash
goatbuilder standup <env> --site <site> --parallel --yes
cat ~/.goat/state/standup-<site>-timings.tsv
```

Record the total and the per-phase rows as a measurement. The slowest three
phases are the next three levers.
