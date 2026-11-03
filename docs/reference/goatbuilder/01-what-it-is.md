# 1. What goatbuilder is

`goatbuilder` is one command-line program that deploys, releases, rolls back and
stands up goat environments. It reads what a repository declares in git, makes
the hosts match it, and calls the work done only when each host's own record
says so.

| Field | Value |
|---|---|
| Binary | `goatbuilder`, built from `crates/goatbuilder` in the goat source repository |
| Status | **Built.** Every verb in this book runs on `main`. Each verb that has a script equivalent is held to it by a CI parity gate |
| Config | `ops/goatbuilder/goatbuilder.yaml`, validated against `ops/goatbuilder/schema/goatbuilder-v1.schema.json` |
| Decisions | [ADR 1198](../../adr/1198-goatbuilder-config-driven-deploy-release-cli.md) (the CLI), [ADR 1201](../../adr/1201-two-avenues-scripts-documented-cli-supported.md) (two avenues), [ADR 1204](../../adr/1204-declared-secrets-posture-and-zero-saas-quickstart.md) (secrets posture), [ADR 1200](../../adr/1200-deployment-e2e-three-tiers.md) (deployment e2e) |
| Design | [goatbuilder.md](../../design/goatbuilder.md) |

## The model: declare, converge, verify

Every verb follows the same three steps.



- **Declare.** A person states, in git, which digest each component should run on
  each environment. The declaration lives in `ops/pins/fleet-pins.yaml` and is
  rendered into per-group `pins.yml` files and one signed intent per goatnet.
- **Converge.** A converger makes the hosts match the declaration. Today the
  converger is `push-ansible`: goatbuilder runs the component's Ansible play
  against each member group of the environment.
- **Verify.** goatbuilder reads each host's deploy ledger,
  `/var/lib/goat/<component>/deploy-history.jsonl`. A host is converged only when
  its newest row names the declared digest with `action: deploy`. A host whose
  ledger cannot be read is *unknown*. A host that has not yet changed is
  *pending*. Neither is ever counted as done.

The ledger is the only proof. The play's exit status, a container being up, or a
metric are not proof that the declared version is running.

## The two avenues

goat ships two ways to do the same work. goatbuilder is the supported avenue.
The shell and Python drivers it mirrors stay in the repository, documented and
working, so a team that prefers a script, or needs one when the binary is
unavailable, still has one.



| Task | goatbuilder | Script |
|---|---|---|
| Deploy one component to one environment | `goatbuilder deploy` | `ops/scripts/deploy-component.sh` |
| Release a candidate up the ladder | `goatbuilder release` | `ops/scripts/rc.sh` |
| Roll back | `goatbuilder rollback` | `ops/scripts/rollback.sh` |
| Ship to every goatnet now | `goatbuilder fastlane` | `ops/scripts/fastlane.sh` |
| Graduate between rungs | `goatbuilder promote` | `ops/release/graduate.py` |
| Regenerate pins and intents | `goatbuilder pins` | `ops/pins/generate-pins.py` |
| Stand up a site | `goatbuilder standup` | `ops/scripts/goat-standup.sh` |
| Take a site down | `goatbuilder teardown` | `ops/scripts/goat-teardown.sh` |
| Stand up a sovereign peering | `goatbuilder federate` | `ops/scripts/peering-standup.sh` |

Both avenues read the same files and write the same records. A CI parity gate
per verb (`ops/goatbuilder/parity-*.sh`, run by `.github/workflows/goatbuilder-pr.yml`)
runs both sides in dry run and fails the pull request when they differ. The
gate also runs when only the script changes, so neither side can drift alone.
The full equivalence table, with the verbs that have no script, is
[Appendix B](appendix-b-script-equivalents.md).

## What goatbuilder is not

- **Not a second source of truth.** It holds no state of its own. Declarations
  live in git; facts live in each host's ledger.
- **Not a fork of Ansible or a new converger.** It drives the repository's
  existing plays through `ops/scripts/ansible-preflight.sh`.
- **Not tied to goat's own environments.** Everything repository-specific is in
  the config file. Another team points the same binary at its own config.

## Who this book is for

Platform administrators and engineers who deploy or release goat components, on
the goat team or on a team adopting goat. Read chapters 2 to 6 once. Chapters 7
to 10 are task chapters, one per group of verbs. Chapter 11 strings them into
end-to-end workflows. Chapter 12 is the place to start when something refuses.

## How to read a command in this book

- `<goatnet>` is an environment key from your config, such as `dev`.
- `<site>` is a host site id from your inventory.
- `<component>` is a key in `ops/pins/fleet-pins.yaml`.
- Examples marked *fixture* run against `ops/goatbuilder/fixtures/`, touch no
  host, and can be repeated freely. Their output in this book is real output.
