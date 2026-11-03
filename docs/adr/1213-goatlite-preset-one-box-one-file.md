# ADR 1213 — goatlite is the lite preset: a first goatnet on one box, from one file, with no outside account

| Field | Value |
|---|---|
| Status | Proposed; decisions 1–3 and most of 4 built 2026-11-02 (unproven on hardware until the drill of decision 6 passes; a goatlite is a learning and first-goatnet shape, never a production one) |
| Date | 2026-10-31 |
| Deciders | operator |
| Repo scope | goat core (`goatbuilder`, the standup path, the gym) |
| Supersedes | — |
| Superseded by | — |
| Related | [ADR 1198](1198-goatbuilder-config-driven-deploy-release-cli.md) D6 (the preset layer), [ADR 1016](1016-complexity-budget-substrate-encapsulation.md) (the bar), [ADR 1204](1204-declared-secrets-posture-and-zero-saas-quickstart.md) (env-file posture), [ADR 1197](1197-public-core-versioned-dependency-private-overlay-repo.md) (public images), [ADR 1201](1201-two-avenues-scripts-documented-cli-supported.md), [ADR 1202](1202-goatnet-tutorial-program.md); design [goatlite.md](../design/goatlite.md) |

## TL;DR (for a layperson)

Today, standing up a goatnet means knowing about a dozen open-source projects and holding accounts with three outside services before the first command runs. goatlite is the version for someone who knows none of that: one repository, one binary, a four-line file naming the goatnet, the box, the SSH user and the administrator, and one command. What comes up is a real goatnet on their own LAN. It is the same software with every decision pre-made, not a cut-down copy, and it is rung 4 of a ten-rung learning ladder that ends with sharing sealed data across sovereign environments.

## Context

ADR 1198 reserved `preset: lite` in the goatbuilder schema and said goatlite "must not require a second tool". ADR 1016 set the bar: a 5-minute path (`git clone`, one command, a running substrate on a laptop) and a 30-minute path (one operator, one host, no substrate decisions surfaced), each from one document. Neither path has ever been walked; the leak register holds two entries and was never seeded.

The pieces a LAN goatnet needs mostly exist. The standup driver's LAN profile runs the fourteen phases with no operator pause (`goat-standup.sh` lines 707 and 743 make both pauses internet-only). `ca-init` mints a CA root from nothing. `with-aws` has a `none` mode. The `env-file` secrets posture is accepted (ADR 1204). The hobby goatnet proved the cloudless value set (file certs, LAN CoreDNS, internal CA, tier ceiling 1) on a 4-core box in July 2026.

What does not exist is anything that lets a beginner reach those pieces. The config surface is about thirty group_vars files plus a cert manifest, a pins row and a door entry, written by hand from a template. `goatbuilder doctor` demands `aws-vault` on every profile. Phase 3 writes secrets only to 1Password or OpenBao. Phase 12 writes service identities to 1Password unconditionally. No phase mints the CA. The LAN path stops before phase 12 until an administrator is created in the dashboard. Images need a GHCR token. The full inventory is in [goatlite.md §5](../design/goatlite.md#5-the-first-run-should-need-no-outside-accounts-these-are-the-places-that-still-do).

## Decision

1. **goatlite is `preset: lite`, nothing else.** Same binary, same playbooks, same fourteen phases in the LAN profile, same gates. A lite-only playbook or script is a defect. (Restates ADR 1198 D6 as the governing rule of this effort.)
2. **The learner's input is one `lite:` block of four facts**: goatnet name, host, SSH user, administrator. Every other value the full shape requires is supplied by the preset; the defaults and their sources are the table in goatlite.md §3.
3. **The preset derives the file set; it does not replace it.** `standup` under `lite` runs `ops/scripts/goatlite-render.py`, which mints the goatnet's CA on the first run and renders the inventory group, the group_vars file inventory, the cert manifest, the door entry and the goatnet's pins row into the learner's checkout, then runs the unchanged engines over them. The pins row lives in the sidecar table `ops/pins/lite-pins.yaml`, merged by both loaders, so a lite goatnet never changes the signed intents of the goatnets declared in `fleet-pins.yaml`. Doctrine P3 holds; a learner-edited file is never overwritten.
4. **A first run needs no account with any outside service.** The CA is minted by the render step (`ca-init`, the learner's age key as the single recipient). Secrets go to the gitignored `env-file`, the generator and the service-identity mint write through that posture, and the learner's own SSH public key becomes the administrator key. AWS mode is `none` and the doctor skips the AWS rows as declared. The administrator and the NetBird infra service token are made by the driver, not in a dashboard; until the forked management service seeds that token at first start, the driver prints the exact dashboard steps as a phase-11 manual step on every LAN site. The one exception until ADR 1197's public images land is `GHCR_READ_TOKEN`, named in the quickstart as the one account.
5. **A goatlite grows into an internet-enrollable goatnet by editing its `lite:` block and re-running**, never by standing up again. Whether that is free is measured (phase D), and until measured the quickstart states the cost.
6. **Done is the cold-read drill on the lite preset**: a non-author, no AI, one document, fresh VM, green `goatbuilder assay` and a round-trip publish; every other document opened becomes a leak-register row; time and document count recorded as a measurement.
7. **goatlite is rung 4 of the ten-rung learning ladder**, kept as the goat gym lane `learner`: use a goatnet, join with a moon-pod, rehearse on the fixture, goatlite, grow it, break and rebuild it, federate a node, add a boundary node, federate across to another sovereign, release and seal data across it.

## Alternatives considered

- **A separate lightweight stack** (fewer services, a simpler identity provider, no fabric router). Rejected by ADR 1016 D3 and ADR 1198 D6: it would be a fork the learner later has to leave, and every substrate it drops is one the ladder's later rungs need.
- **A single big quickstart document over the full shape.** Rejected by ADR 1016 A2: the failure is the amount a beginner must read and decide, and one long document does not shrink it.
- **Docker Compose on a laptop with no mesh.** Rejected: it is not a goatnet, so nothing learned on it carries to rungs 5 through 10, and it would be the second procedure doctrine P1 forbids.
- **Keep the dashboard steps for the administrator and token, documented well.** Rejected for the first run: creating a NetBird service user and token is a substrate the 30-minute path says must not surface. Kept as the printed fallback until the driver-made token lands.

## Consequences

**Positive.** The 5-minute and 30-minute paths get a concrete artefact to walk, and the leak register gets its seeding from real walks. The seven account leaks in goatlite.md §5 are closed for every LAN goatnet, not only lite. The hobby value set becomes a maintained template instead of an archived instance. The ladder gives the gym a spine from newcomer to cross-sovereign sharing.

**Negative.** The schema gains a `lite:` block and preset-conditional requirements, which the conformance suite must cover. The forked management binary gains a first-start seeding path (goatlite.md §5 row 6). Derivation adds generated files to a learner's repository that they must learn to treat as generated until they choose to own them.

## Trigger conditions for revisit

- ADR 1197 lands public images: drop the `GHCR_READ_TOKEN` line from the quickstart and decision 4's exception.
- The phase-D measurement shows growing out of lite costs a re-standup: decision 5 is reopened.
- The drill (decision 6) surfaces a substrate that cannot be defaulted: it goes to the leak register and this ADR's §3 table, not to the quickstart.
