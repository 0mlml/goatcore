# 9. Releasing

A release takes a component from a tag on `main` to production by way of the
release ladder: the dev rung, live validation, the preprod rung, a soak, then
graduation into the fleet-wide pin. `release` drives the whole ladder.
`promote` is the graduation tool underneath it. `pins` regenerates the files
the pins render into. `fastlane` is the deliberate exception that skips the
ladder.

## The ladder



| Term | Meaning |
|---|---|
| **Candidate** | a release-candidate tag `<component>/vX.Y.Z-rc.N` on `main`, and the deviation on the preprod environment in `ops/pins/fleet-pins.yaml` that pins it there |
| **Record** | one YAML per promotion at `ops/release/records/<component>/<date>-<ref12>.yaml`: candidate ref, prior ref, reason, evidence, state, ack |
| **Soak** | the time the candidate runs on preprod before prod; the ladder's `min_days` |
| **Graduation** | the candidate's ref becomes the component's `fleet:` value and the preprod deviation is deleted. The ref that graduates is byte-identical to the ref that soaked |
| **Hotfix** | a prod deviation plus a hotfix record with a named approver. It owes a backfill until the same ref graduates normally |
| **Graduation set** | components that must graduate together, declared in `ops/release/sets/` |

Graduation is never automated
([ADR 1177](../../adr/1177-graduation-is-never-automated-the-daily-review-is.md)).
A person runs it, and the start-of-day review says what is ready. Design:
[release-graduation.md](../../design/release-graduation.md),
[release-candidate-validation.md](../../design/release-candidate-validation.md),
[ADR 1120](../../adr/1120-release-graduation-ladder.md),
[ADR 1173](../../adr/1173-release-candidate-tags-trigger-affected-flow-validation.md).

## `release`

**Purpose.** Take a candidate from a tag on `main` through the dev goatnet's
live validation to preprod. After the soak, with `--from prod`, graduate it,
fold it into `fleet:`, deploy prod and re-verify every rung.

**Use it** for every normal release of a component.

| Step | `--from` | What happens |
|---|---|---|
| Preflight | | deps, a current `main` checkout, the nominator is a repository admin, every secret the deploys need |
| Tag | `tag` | cut `<component>/vX.Y.Z-rc.N`, wait for the image workflow, read the digest from the registry |
| Dev | `dev` | write the dev deviation, regenerate pins, re-sign intents, run the gates, land the pin by pull request, deploy dev and verify |
| Rows | `rows` | run the live validation rows mapped to the component in `ops/validation/flow-map.yaml` |
| Preprod | `preprod` | open the record, write the preprod deviation, land it, deploy preprod, run its rows, record convergence evidence, print the soak line |
| Preprod rows | `preprod-rows` | preprod already runs the tag; redo only its deploy check, rows and evidence |
| Prod | `prod` | after the soak: graduate, fold into `fleet:`, close a superseded hotfix record, deploy prod, re-verify every rung |

```bash
goatbuilder release <component> --version 0.1.0 --ack "<your name>" --dry-run
goatbuilder release <component> --version 0.1.0 --ack "<your name>" --yes
goatbuilder release <component> --tag <component>/v0.1.0-rc.2 --from dev --yes
goatbuilder release <component> --tag <component>/v0.1.0-rc.2 --from preprod-rows --yes
# after the soak
goatbuilder release <component> --tag <component>/v0.1.0-rc.2 --from prod --yes
```

`--rc N` chooses the rc number; the default is the next free one on origin.
`--tag` validates an existing tag instead of cutting one.

**Success:** the final rows show every rung converged on the candidate's digest
and the record's state. Nothing lands when the pins on `main` are already
current: "pins already current on main; nothing to land".

**Refusals.**

| Code | Meaning |
|---|---|
| GB-011 | only a repository admin may nominate a candidate |
| GB-012 | the candidate's image build did not succeed |
| GB-013 | the image is not in the registry |
| GB-014 | a live validation row failed |
| GB-015 | the checkout could not fast-forward after the pin landed |
| GB-016 | no runner settings for the goatnet in `goatnet-runners.yaml` |
| GB-020 | `promote` did not produce the expected record |
| GB-022 | intent signing failed |
| GB-032 | the record is open but the goatnet runs a different ref |
| GB-037 | the ladder is missing a live rung |
| GB-038 | `--from preprod-rows` cannot resume this candidate |
| GB-042 | the live rows cannot run from this machine. The candidate is untested, not failed |
| GB-044 | a secret is not set; refused before the tag |

**Script:** `ops/scripts/rc.sh`. The release parity gate compares both dry runs,
every step and every command.

**Where candidates are validated.** A pushed rc tag starts
`.github/workflows/rc-validate.yml`, which runs the flows the change affects.
The live rows run from the machine named for each goatnet in
`ops/validation/goatnet-runners.yaml` (`goat_via: door:<goatnet>` runs them
through the door). `ops/validation/run-acceptance.py` runs them by hand.

## `promote`

**Purpose.** Guide and observe graduation across the ladder. A mutation runs
only when invoked, prints what it did, and needs `--yes` without a terminal.

| Subcommand | Does |
|---|---|
| `status [--json]` | per component, the effective value on each rung, graded `aligned`, `candidate`, `hotfix-backfill-owed`, `env-local`, `legacy-declared` or `UNDECLARED-SKEW` |
| `open <c> --ref R --reason TEXT` | write the candidate record; print the preprod deviation edit |
| `graduate <c> --ack NAME` | verify every hop gate, mark the record graduated, print the fold-to-fleet edit |
| `graduate-set <set> --ack NAME` | graduate every member of a set, or refuse naming every member not ready |
| `hotfix <c> --ref R --reason TEXT --ack NAME [--previous-ref R]` | write a hotfix record and print the prod deviation edit |
| `review [<c>] [--detail] [--json] [--ack NAME]` | every open record sorted by what a person must do; read-only |
| `readiness [--out FILE]` | the `release-readiness-v1` document; read-only |
| `abandon <record> --reason TEXT [--superseded-by R]` | close a record with a machine-readable reason |
| `check` | the ladder's CI invariants; exit 0 or 1 |

```bash
goatbuilder promote review --detail
goatbuilder promote status
goatbuilder promote graduate <component> --ack "<your name>" --yes
```

`promote check` enforces the ladder's own invariants, numbered C1 to C9 in
[release-graduation.md](../../design/release-graduation.md) §6. They are not
the deploy contract's C1 to C10: they check that the ladder is well-formed,
that records parse and name digest-pinned refs, that every record has its pin
edit and every edit its record, and that no hotfix record waits more than
three days without validation evidence.

**Refusals:** GB-033 (a graduation step refused; the row carries the check's
own lines). **Script:** `ops/release/graduate.py`, subcommand for subcommand.

## `pins`

**Purpose.** One resolution pass with two renderings: every member group's
`group_vars/<group>/pins.yml` and every goatnet's signed intent, from
`ops/pins/fleet-pins.yaml`.

**Use it** after editing `fleet-pins.yaml` by hand, and when CI says the pins
are out of date.

```bash
goatbuilder pins --check    # validate and diff; exit 1 on drift
goatbuilder pins            # regenerate
```

The pin for a group resolves by precedence: a member deviation, then a goatnet
deviation, then `fleet:`. `--check` refuses drift, an unsigned or tampered
intent, and a governed pin defined outside `pins.yml`. A re-run on a current
tree changes no byte and re-signs only an intent whose body changed.

**Refusals:** GB-034 (the generated pins or intents differ from
`fleet-pins.yaml`), GB-035 (the goatnet's intent names no usable CA).
**Script:** `ops/pins/generate-pins.py`, byte for byte.

## `fastlane`

**Purpose.** One command from a merged commit to every goatnet, skipping the
soak and the hand-kept records
([ADR 1127](../../adr/1127-fastlane-urgent-fleet-wide-delivery.md)).

**Use it** only when waiting costs more than the risk, and the change's
rollback is a pin revert. **Do not use it** for schema migrations, identity or
ACL changes, or any artifact that is not an `image`.

```bash
goatbuilder fastlane <component> --ref sha-<short> --reason "<why it cannot wait>" --ack "<your name>" --dry-run
goatbuilder fastlane <component> --ref sha-<short> --reason "<why it cannot wait>" --ack "<your name>" --yes
goatbuilder fastlane <component> --ref sha-<short> ... --no-deploy   # land the pin only
```

It resolves the digest, moves `fleet:` (keeping `absent: true` deviations),
regenerates and re-signs, writes a hotfix-class record with the previous ref,
closes what the move overtook, lands the pin by pull request, and deploys every
goatnet through `deploy`. The hotfix record owes a backfill: `promote
graduate` closes it once the same ref has passed the ladder's gates.

**Refusals:** GB-021 (fastlane refused: an unpinned digest, an open graduation
set, or a declared rollback that is not a pin revert). **Script:** `ops/scripts/fastlane.sh`.
