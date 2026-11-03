# 10. Standing up, proving, joining, tearing down and federating

Five verbs work on whole sites rather than one component. `standup`,
`join-node`, `teardown` and `federate` run numbered, idempotent phases with a
fail-closed gate at each end. Each prints the exact command of every phase with
`--dry-run` and resumes with `--from <phase>`. A phase marked `[MANUAL]` stops
the run and says which `--from` resumes it. `assay` reads only: it proves what
the other four built.

Phase kinds, as every plan prints them:

| Kind | Means |
|---|---|
| `GATE` | a fail-closed check; the run stops if it fails |
| `T` | a tool on the operator's machine |
| `A` | an Ansible play |
| `MANUAL` | a person's step; the run stops and names the resume point |

## `standup`

**Purpose.** Take a site from nothing to verified: the phases of
[goatnet-standup.md](../../operations/goatnet-standup.md) in the doctrine's
order, the observability host as a parallel leg, the rescue nodes and the
cross-goatnet surfaces last
([ADR 1183](../../adr/1183-rescue-nodes-are-part-of-the-site-baseline.md)).

**Use it** to build a new goatnet's founder site, or a new joiner site of an
existing goatnet.



| Phase | Kind | What it does |
|---|---|---|
| 1 | GATE | config preflight, required-input scan, declared-ACL check, env door, host preflight |
| 2 | A | host baseline and disk encryption on every node |
| 3 | T | generate the site's per-instance secrets |
| 4 | T | mint every leaf certificate from the manifest |
| 5 | A | management stack (a first run stops at the NetBird schema wait, which is expected), then the relay connection (internet profile only) |
| 6 | A | identity stack (Ory Kratos and Hydra), then the management stack again: completes phase 5 and prints the bootstrap setup key |
| 7 | A | public TLS through ACME DNS-01 and the relay tier (internet profile only) |
| 8 | MANUAL | enrol the management host in the mesh and re-mint the router certificate |
| 9 | A | the Zenoh router, bound to the mesh address, and the SPIRE server |
| 10 | A | LAN services, or the dashboard and OIDC clients |
| 11 | MANUAL | first browser login, admin promotion, the bootstrap NetBird token (internet profile only) |
| 12 | A + GATE | the declared ACL first, then service identities, then every service |
| 13 | GATE | acceptance: `assay`, every applicable row proven live, layer by layer |
| 14 | A | rescue nodes the site declares, the relay-health box and cross-goatnet surfaces |

Phases are whole numbers 1 to 14 with no gaps; a LAN site skips 7, 8 and 11. A
run started before 2026-10-29 used the older numbering; the old-to-new table is
in goatnet-standup.md §"Phase numbers".

The observability host, when the site declares one, runs as two background
legs: `host` (baseline, disk encryption, Forgejo) alongside phases 2 to 6,
joined before 12, and `stack` (Prometheus, Alertmanager, Grafana) from 9,
joined before 13.

**Phase 12 in waves.** Phase 12 has fourteen steps. Each declares the steps it
needs in `ops/goatbuilder/standup-dag.yaml`. With `--parallel`, the steps whose
needs are met run together, in six waves. A failed step stops the run after its
wave and the message names each failed step and its log.

| Wave | Steps |
|---|---|
| 1 | ACL groups |
| 2 | ACL policies, service-identity mint, account baseline |
| 3 | ACL enforce, service-identity gate, mesh DNS |
| 4 | ACL gate |
| 5 | portal |
| 6 | pub/sub mirror, intake, onboarding runner, grant-keyd, synthetic publishers |

```bash
goatbuilder doctor --site <site>                         # every missing input, before any host
goatbuilder standup <goatnet> --site <site> --dry-run    # every phase and command
goatbuilder standup <goatnet> --site <site> --ack "<your name>" --parallel --yes
goatbuilder standup <goatnet> --site <site> --ack "<your name>" --from 9 --yes    # resume after phase 8
goatbuilder standup <goatnet> --site <site> --obs-only        # the observability legs only
goatbuilder standup --mock ops/goatbuilder/fixtures/standup dev --site alpha --ack "<your name>"   # rehearsal
```

| Flag | Meaning |
|---|---|
| `--profile lan\|internet` | topology profile; default read from the site's config |
| `--from N`, `--to N` | run a range of phases |
| `--skip-obs` | build no observability host even when the site declares one |
| `--obs-only` | run only the observability legs, in the foreground |
| `--parallel` | run phase 12 in waves |

**Success:** phase 13's `assay` reports every applicable row PASS. On a fixture, the run reports
declared-to-verified time.

**Refusals:** GB-060 (a required input is not set), GB-061 (a secret the
standup's plays read is not available), GB-062 and GB-063 (warnings: an input
may be required; a host limit selects no host). A failed phase names its
recovery command. **Script:** `ops/scripts/goat-standup.sh <site>`; its
`--plan` is the dry run. The standup parity gate compares both plans phase by
phase on three sites.

Status: **built**. No site has yet been stood up end to end through the binary
on hardware; the first timed run is the one-hour measure (chapter 15).

## `assay`

**Purpose.** Prove a built goatnet layer by layer: run every row of the
acceptance manifest that applies to the site, against values derived from the
site's own group_vars, and grade each layer. It is standup's phase 13 as a
command of its own, so a goatnet can be re-proven at any time
([ADR 1115](../../adr/1115-goatnet-e2e-acceptance-suite.md),
[goatnet-acceptance-suite.md](../../design/goatnet-acceptance-suite.md)).

**Use it** after a standup, after a change to a site, and whenever you need to
know that a goatnet still works end to end. It reads only.

```bash
goatbuilder assay <site> --plan                  # the rows that apply; probe nothing
goatbuilder assay <site>                         # every row, a table per layer
goatbuilder assay <site> --only <row id>         # one row, after a fix
goatbuilder assay <site> --var goat_via=<user>@<host>   # goat CLI rows run there
goatbuilder assay <site> --rows-out rows.jsonl   # verdicts as JSON lines too
```

| Layer | What its rows prove |
|---|---|
| host | data root on its declared mount, operator SSH per class, unattended boot, the node's connections |
| mgmt-stack | NetBird service identities, the management API with and without a token, signal TLS, the enrolment certificate |
| dns | every declared service resolves; the name snapshot is served; the mesh fallback answers with this goatnet's own relays |
| relays | each relay's certificate is trusted for the enrolment name and the relay port |
| mesh | mesh health green and fresh; the declared ACL assertions hold |
| identity | a login starts for the dashboard and every running client; admin fences enforce; the roster |
| fabric | router identity and storage, a mesh beat, `goat status`, a hello round trip, a put/subscribe round trip, presence, an adminspace write refused |
| time | time health green and fresh |
| observability | scrape targets up, alert rules loaded, Alertmanager reached, the deadman firing |
| deploy | every declared component's ledger names main's ref; consistency health |
| federation | joiner only: replication both ways, seeded from the founder, site-local tables kept local |

Twelve rows run the `goat` CLI's own JSON output (`status`, `hello`,
`presence`, `health` per domain, `who`, `acl test`, `goatbuilder diff`). They
need a goat context for the goatnet on the machine that runs them, or on
`goat_via`. A problem on the probing machine (no profile, off the mesh, an old
`goat`) is graded UNKNOWN, never a goatnet FAIL. Four rows are negatives that
must be refused, each paired with a positive that proves the path carries.

**Success:** exit 0, only when every applicable row is PASS. **Exit 3** while any
row fails, cannot answer, or none passed (GB-048): UNKNOWN is a row nobody
proved. **Exit 1** when it cannot run (GB-049). **Script:**
`python3 ops/validation/generate-values.py <site>` then
`python3 ops/validation/run-acceptance.py --env <site> --values ops/validation/values-<site>.yaml --all-eligible --by-layer`.

## `endstate`

**Purpose.** Check that a goatnet is in its declared end state: every service its
own declarations say it runs is deployed at the declared version, running,
healthy and doing its job, with its zero-trust connections wired, and nothing
runs that it does not declare ([ADR 1206](../../adr/1206-goatnet-declared-end-state.md),
[goatnet-end-state.md](../../design/goatnet-end-state.md)).

**Use it** after a standup, after any deploy or teardown, and whenever a site
changes shape. It reads only.

```bash
python3 ops/goatbuilder/endstate/endstate.py model <site>   # what the site should run; reads no host
goatbuilder endstate <site>                  # read the hosts once, run the assay, grade
goatbuilder endstate <site> --no-assay       # skip the acceptance rows
goatbuilder endstate <site> -o json          # one check per service and per orphan
```

**Declaring the shape.** The end state is read from the site's own
declarations, so a site changes what it runs by changing them:
`grafana_enabled: false` or `zenoh_sandbox_enabled: false` in
`ops/ansible/group_vars/<site>_hosts/vars.yml` declares a service absent;
`has_obs_host: false` runs the observability services on the one management
machine; `goatnet_founder: false` makes the site a joiner, without the founder's
control plane. A service no declaration names is reported undeclared, never
assumed.

**Success:** `<site> is in its declared end state`, exit 0. Grades with no
signal to read (UNOBSERVED) are counted apart and do not fail it unless
`--strict` is given.

**Refusals:** GB-066 (the site is not declared, or the script cannot run),
GB-068 (not in its declared end state; each failing grade and orphan is
printed with its reason). **Script:** `ops/scripts/goatnet-endstate.sh <site>`.

## `teardown`

**Purpose.** Take a site from verified to nothing, in reverse phase order,
keeping every record: a teardown appends rows, it never erases them.

**Use it** to stop a site's services, or to rehearse a burn-down on a fixture.

```bash
goatbuilder teardown <goatnet> --site <site> --dry-run              # the burn-down order
goatbuilder teardown <goatnet> --site <site> --services --yes       # stop services, keep volumes
goatbuilder teardown <goatnet> --site <site> --services --from 12 --yes
goatbuilder teardown --mock ops/goatbuilder/fixtures/standup dev --site alpha
```

`ops/goatbuilder/teardown.yaml` declares how each standup step comes down.
`--services` stops each compose service in reverse order with
`ops/ansible/teardown-compose.yml`, keeps its volumes, proves it stopped with
`docker compose ps -q`, and records it in `teardown.jsonl`. A full teardown
that reaches a step whose teardown is not yet declared exits 3 before touching
anything.

**Status:** built on fixtures; `--services` built on real hosts. A full real
teardown is designed (chapter 16). **Script:** `ops/scripts/goat-teardown.sh`.

## `join-node`

**Purpose.** Add one machine of a class to a live site: declared, converged and
verified, as fifteen numbered phases taken from
node-addition.md.

**Use it** when a site gains a core, observability, break-glass or
network-jump machine.

| Phase | Kind | What it does |
|---|---|---|
| 1 | MANUAL | reach the machine on the site LAN |
| 2 | T | seed the SSH key: key login and passwordless sudo |
| 3 | GATE | repository work first: group, inventory, pins, exclusions |
| 4 | T | quiet the machine's automatic package updates |
| 5 | A | host baseline, narrowed to the machine |
| 6 | A | WireGuard kernel modules, first pass |
| 7 | GATE | the site trusts its own CA, not a shared development root |
| 8 | MANUAL | mint a setup key carrying the site's management group |
| 9 | GATE | enrolment preflight on the machine: clock, stale interface, reach |
| 10 | MANUAL | enrol the machine; the gate is the kernel ACL plane, not "Connected" |
| 11 | A | WireGuard kernel modules again; `changed=0` proves the module took |
| 12 | GATE | the mesh address and mesh inventory filled in one change |
| 13 | A | the class's converge; a joiner core node takes three passes and a founder-side mirror |
| 14 | T | restore automatic package updates |
| 15 | GATE | every applicable acceptance row PASS, never UNKNOWN |

```bash
goatbuilder join-node <goatnet> --site <site> --node <name> --class core --dry-run
goatbuilder join-node <goatnet> --site <site> --node <name> --class core \
  --address <LAN address> --yes
goatbuilder join-node <goatnet> --site <site> --node <name> --class core --from 9 --yes
```

`--pass-var` names the environment variable holding the machine's first-login
password; the default is `<SITE>_NODE_PASS`. **Script:** none; this verb is the
runbook's only driver. Status: built; not yet run on hardware.

## `federate`

**Purpose.** Stand up a sovereign peering with another goatnet: declared,
converged and verified
([ADR 1157](../../adr/1157-cross-sovereign-federation-edge-terminated.md)).

**Use it** when two goatnets under different roots of trust agree to exchange
data.

| Phase | Kind | What it does |
|---|---|---|
| 0 | GATE | peering preflight |
| 1 | T | our half, read from the running system |
| 2 | T | lint our package against the specification |
| 3 | MANUAL | exchange packages and bind the partner's verifying key out of band |
| 4 | T | compose one agreement with one digest |
| 5 | T | verify the agreement from our side |
| 6 | T | mint the peering leaf certificates from the agreement |
| 7 | T | render the access rules and configuration; the only writer |
| 8 | A | deploy the dedicated edge router |
| 9 | GATE | open the transport the agreement chose, or refuse when no play stands it up |
| 10 | GATE | acceptance in four tiers: it carries, it refuses, it round-trips |

```bash
goatbuilder federate <partner> --env <goatnet> --dry-run
goatbuilder federate <partner> --env <goatnet> --yes          # stops at phase 5
goatbuilder federate <partner> --env <goatnet> --from 5 --yes
```

`--as` says which party we are, `--edge-group` names the Ansible group of the
edge (default `<partner>_edge`), and `--work` the working directory (default
`$FED_WORK`, else `/var/lib/goat-federation/<partner>`). **Script:**
`ops/scripts/peering-standup.sh`. Status: built; not yet run on a live peering.
