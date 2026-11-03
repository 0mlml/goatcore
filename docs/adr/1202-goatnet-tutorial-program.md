# ADR 1202 — The goatnet tutorial program: core in this repository, the worked example in Hello Galaxy

| Field | Value |
|---|---|
| Status | **Accepted** |
| Date | 2026-10-28 |
| Deciders | operator |
| Supersedes | — |
| Related | [`goatnet-tutorials.md`](../design/goatnet-tutorials.md) (the design), [ADR 1201](1201-two-avenues-scripts-documented-cli-supported.md) (both avenues; the CLI supported), [ADR 1198](1198-goatbuilder-config-driven-deploy-release-cli.md) (goatbuilder), [ADR 1084](1084-block-64r-mgmt-repset-and-two-node-federation.md) (founder and joiner sites), [ADR 1132](1132-goat-gym-training-surface.md) (the gym) |

## TL;DR (for a layperson)

There will be three step-by-step tutorials for the people who build goat
environments: build a new one from nothing, attach its first second site, and
later add the node that shares chosen data with allies and partners. Each is
practised first on a small worked example that runs on a laptop and gives the
same answer every time, then done for real. The tutorials live with the tool;
the worked example lives in its own small repository, like the other runnable
examples in the series.

## Decision

The operator's words, 2026-10-28:

> we also need to build out a tutorial program for building out a new goatnet
> from scratch, for adding the first federated node, and eventually (sequence
> this for later but soon) to add a boundary node that is used for ally and
> partner data sharing the cross env federation. and the core of this should
> live in this monorepo (and the forthcoming open source repo) but the
> particular example implementation that is proven e2e will live in a hello
> galaxy repo

1. **Three tutorials, in order.** 1: a new goatnet (the founder site). 2: the
   first federated node (a joiner site attached to the founder). 3: a boundary
   node for ally and partner sharing across environments — designed now, built
   after tutorial 2 is proven on hardware, dated in the design doc.
2. **The core lives here.** Tutorial text in `docs/tutorials/`, served in the
   goatbuilder book and cited from the gym's platform-operator lane; the
   rehearsal semantics in `goatbuilder`; the checks in this repository's CI.
   The open-source repository inherits all of it.
3. **The worked example lives in the Hello Galaxy series**, as
   `hello-galaxy-goatnet-from-scratch`: one goatnet that grows tutorial by
   tutorial, each step's directory starting from the previous step's end state,
   with the recorded output of every rehearsal so a reader diffs their run
   against a known-good one (`USAREUR-AF/hello-galaxy-goatenv`,
   created 2026-10-29). This repository keeps the same directories at
   `ops/goatbuilder/fixtures/goatnet-from-scratch/`, because `goatbuilder tutor`, the
   `goatnet_from_scratch_*` tests and `ops/tutorials/check.sh` read them on every pull
   request here; `ops/tutorials/sync-goatnet-from-scratch.sh` compares the two copies
   and refreshes the Hello Galaxy one.
4. **Every tutorial is rehearsed before it is run.** A section headed
   "Rehearse it on the fixture" runs the real verbs against the worked example
   with `--mock`. Its commands are executed as written on every pull request
   and diffed against the recorded checkpoints; a drift either way fails.
5. **The CLI is taught; the scripts are named beside it** (ADR 1201).

## Consequences

- `goatbuilder standup --mock` gains the two features a joiner's rehearsal
  needs: a phase may name the founder's class as `<site>/<class>` (only the
  site this one joins), and a fixture may list records an earlier standup left.
- Tutorial 3 cannot be written honestly until the boundary node is a
  `goatbuilder` class and tutorial 2 has a hardware run; the design doc dates
  both.
- The Hello Galaxy repository is registered in the estate catalog (goat-hq)
  and granted to platform admins by the console's repository policy. Its CI
  checks the fixtures' shape and, with `GOAT_READ_TOKEN` set, builds
  `goatbuilder` at its `GOATBUILDER_REF` and runs the rehearsals against the
  checkpoints.

## Alternatives

- **The worked example in this repository permanently.** Simplest, but a
  reader must clone the whole platform to run one example, and it breaks the
  series' convention. Rejected by the operator's direction.
- **Tutorials in the Hello Galaxy repository too.** The text would drift from
  the tool it teaches; the text versions with the binary. Rejected.

## Amendment 2026-10-30 — tutorial 3 built against `goatbuilder federate`

The operator approved building tutorial 3 now. It is written and rehearsed:
[`docs/tutorials/03-boundary-node.md`](../tutorials/03-boundary-node.md), the
worked-example fixture `ops/goatbuilder/fixtures/goatnet-from-scratch/03-boundary-node/`,
and the recorded checkpoint
`ops/goatbuilder/fixtures/goatnet-from-scratch/checkpoints/03-boundary-node.txt`. The
`goatbuilder tutor` lesson for it rides the tutor-lesson chain and lands with
that change, not this one.

Two things changed from the plan in decision 1 and the consequences above:

1. **It wraps the `goatbuilder federate` verb, not a new `boundary` machine
   class.** `federate` already stands a sovereign peering up in eleven phases
   (`crates/goatbuilder/src/federate.rs`), subsuming `ops/scripts/peering-standup.sh`.
   A boundary node in [ADR 1208](1208-sovereign-environments-release-namespaces-and-grant-encryption.md)
   and [ADR 1157](1157-cross-sovereign-federation-edge-terminated.md) is the
   sovereign edge router that verb deploys — one dedicated router process per
   partner, trusting only that partner's bundle — so no second standup class was
   needed. The earlier sequencing (a `join-node --class boundary` arm installing
   the octo-pod egress path) is not the path taken.
2. **Its rehearsal is a planned run against a flat fixture, read with `--mock`.**
   A peering's far side is another sovereign's goatnet, so there is no mock
   converger to stand one up; the rehearsal is `federate --dry-run`, which plans
   all eleven phases and executes nothing. To make the worked example
   self-contained like 01 and 02, `federate` now honors `--mock <dir>` — loading
   that directory's flat `goatbuilder.yaml` the way every other verb does —
   where before it was the one verb that ignored it. So `03-boundary-node/` is a
   flat fixture declaring the `hello-ally` peering, and the rehearsal plans
   against that declaration (the "declare it first" C1 check is satisfied by the
   fixture, and an undeclared peering is refused). One runnable fixture contract
   across all three tutorials.

The honest-state caveat in the consequences stands and is recorded in the
tutorial itself: the peering flow is proven between two purpose-built
environments (M-113)
and has not run against a live goatnet. The tutorial teaches the verb and its
acceptance gate; the first live peering is still ahead.

## Amendment 2026-10-29 — each tutorial ends proven by the assay

Each tutorial's done step is `goatbuilder assay <site>`: every acceptance row
that applies to the site, layer by layer, with the goat CLI rows that read the
goatnet as an operator does ([goatnet-acceptance-suite.md](../design/goatnet-acceptance-suite.md)
§"The assay"). The rehearsal runs the assay's plan on each step's fixture value
set (`assay-values.yaml`), so the checkpoints record which rows a founder and a
joiner owe.
