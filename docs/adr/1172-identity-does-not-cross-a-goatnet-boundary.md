# ADR 1172 — Identity does not cross a goatnet boundary, in either direction

- **Status:** Accepted — **rule decided; the split is built and committed, not yet deployed.** No identity information moves between goatnets, up or down. Onboarding fresh through the three gates is the only way into an environment. There is deliberately no identity graduation ladder, unlike images and pins, which have one.
- **Date:** 2026-09-08
- **Deciders:** operator
- **Related:** [ADR 1062](1062-three-stage-delegated-candidate-onboarding.md) (the three gates, which are the only door) · [ADR 1120](1120-release-graduation-ladder.md) (the ladder that exists for artifacts and deliberately does not exist for people) · [ADR 1171](1171-person-nationality-and-org-ownership-are-two-facts.md) (`identity.nationality`, the field that made the shared artifact stop being inert) · [ADR 1031](1031-credential-revocation-mesh-first-offboarding.md) (revocation is already per-goatnet) · [`data-access-cop.md`](../design/data-access-cop.md) (the declared half)

## Decision

**A person admitted to one goatnet is not thereby known to another.** They are admitted separately, through the same three gates, and their record in each environment is established by that environment.

This is bidirectional and the downward direction is the sharper one:

| Direction | Ruling |
|---|---|
| preprod → prod | No promotion. A person on a preprod goatnet has no standing in production until production admits them. |
| prod → preprod, or either → dev | **No backflow.** Production identity information must not land in an environment with weaker controls, looser access and a larger set of people who can read the host. |

**Permissions do not travel either.** Holding a tier on one goatnet implies nothing about the same person's tier on another. Each environment's approval gate decides its own, subject to its own ceilings.

**The live identity in an environment is authoritative for that environment.** Where a fact about a person is needed — their nationality, their tier, their organisation — the answer is the one established where they were actually admitted, not one inherited from another environment's record. Being Canadian on a preprod goatnet says nothing about who someone is in production until production has admitted them.

## Why fresh onboarding rather than a promotion path

The operator's reasoning, recorded because it is the load-bearing part: **it is easier to onboard someone fresh than to avoid accidentally moving the wrong things between environments.** A promotion mechanism has to decide, correctly and every time, which of a person's facts travel and which do not — tier, organisation, keys, machine quota, group membership, collaboration accounts. Every field is a chance to carry across something that should have stopped. Fresh onboarding has no such surface: the gates are the only door, and they already work.

This is the opposite of the choice made for artifacts. Images and pins have a deliberate graduation ladder ([ADR 1120](1120-release-graduation-ladder.md)) because an image is the *same* artifact in every environment and the value is in proving it once. A person is not the same principal in every environment, and the value is in deciding separately.

## Why a shared roster is the hazard

`docs/operators/roster.json`, the declared half of the access picture, was a **single artifact** that the portal role copied from one hardcoded path (`ops/ansible/roles/goat-zenoh-portal/tasks/main.yml`) to every goatnet.

**It grants nothing, and that is the trap.** A declared row with no live identity on that goatnet produces no access: nobody appears in one environment because they onboarded to another, and that part of the system is correct and is not what this ADR changes. But because sharing costs nothing visible, the file accumulates the union of every environment and no gate objects. What it costs is truth: each goatnet reconciles its live identities against a declared list dominated by people it never admitted.

It stops being inert once [ADR 1171](1171-person-nationality-and-org-ownership-are-two-facts.md) adds `identity.nationality` to it. Shipping a union roster would then place citizenship onto hosts where most of those people have no presence.

## Consequences

**The declared registry is per-goatnet.** `gen-roster.py --env zagros` reads `ops/operators/zagros/` and `ops/identity/orgs/zagros.yaml` and writes `docs/operators/zagros/roster.{md,json}`. The flat `ops/operators/*.yml` set is excluded from every goatnet's artifact, because it names no goatnet. The org-catalog union added on 2026-09-07 applies only to the controller-side union artifact, which was its actual justification.

**The portal role names the goatnet or refuses.** `portal_roster_env` has no default; a host that has not named its goatnet fails the play rather than falling back to the union, because every wrong value in that variable ships one environment's people into another.

**The controller-side union survives, deployed nowhere.** `docs/operators/roster.json` still exists as the operator's index across every environment, read by repo-side tooling on their own laptop. The repo legitimately holds every environment's record; a goatnet host does not.

**A per-goatnet roster that declares nobody is refused in CI.** Deploying one does not fail — the portal loads it, finds no declared half, and every live identity reconciles to `out_of_band`, which reads as a fleet of strays rather than as a missing file. A goatnet with no declared half sets `portal_roster_path: ""` instead, and the COP reports the gap.

**Reconciliation gets louder before it gets quieter, and that is the point.** A goatnet's own record may account for only some of its active identities; the rest predate the console's proposal store and will reconcile `out_of_band` until that goatnet's own record accounts for them. That is the leakage becoming visible rather than a regression. A goatnet with no declared half at all is served none.

**Nationality is recorded in every roster where the person actually resolves**, established per environment. The field belongs wherever a roster exists; what it must not do is arrive from somewhere else.

**Some live identities predate the console.** Under the primacy rule above, the live identity plane is the authority and the proposal store is one input to it; those people are established from what is actually live on that goatnet, never from another goatnet's manifest.

## What this does not change

Revocation and offboarding are already per-goatnet. The three gates are unchanged. Nothing here alters what a person may do once admitted — only where their admission counts.

## Not decided here

Whether a person's *organisation* facts may be shared, as distinct from their identity. An organisation is not a person, its registry is already per-goatnet, and the catalogs are separate files; that question is open and deliberately left so.
