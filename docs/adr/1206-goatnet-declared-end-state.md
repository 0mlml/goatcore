# ADR 1206 — A goatnet's declared end state: derived, flexible per site, verified end to end

| Field | Value |
|---|---|
| Status | **Accepted** |
| Date | 2026-10-29 |
| Deciders | operator |
| Realises | [ADR 1125](1125-fleet-reconciliation-join-and-graduation-sets.md) for one declared fact, service presence: one derived row per site service, sync, health and coverage graded apart |
| Related | [`goatnet-end-state.md`](../design/goatnet-end-state.md) (design, measurement, catalog), ADR 1201 (both avenues), ADR 1060 (deploy ledger), ADR 1115 (acceptance manifest), ADR 1084 §D3 (single-homed admin surfaces), F-197, F-405, F-592 |

## TL;DR (for a layperson)

goatbuilder now knows what each goatnet is supposed to be running, and checks that it is. The list is not typed out by hand for each goatnet: it is worked out from what each goatnet already says about itself, so a goatnet that runs without Grafana, without the sandbox, or on a single computer simply says so, and the check adjusts. For everything that should be running, the check confirms it was deployed at the right version, is up, is healthy, does its job, and is correctly wired to the identity and security services around it. Anything running that nobody declared is reported as left over.

## Decision

The operator's words are the requirement:

> we need goatbuilder to be aware of what a correct deployed state should be and we need that to be flexible against possible modifications to the set of services run like some goat envs might not host all services might not have sandbox services or might not have grafana or something like that, or might need to do a single computer host not by choice but necessity so it needs to be flexible in defining end state and needs to be able to verify however that whatever is intended is fullly deployed and active and in a healthy and functial state including cross service threading of zta and such things

Applied:

1. **The end state is derived, never a second hand-kept list.** `ops/goatbuilder/endstate/catalog.yaml` says what each service *is*: its containers, its machine class (management or observability), its deploy ledger, the served names it owns, its OIDC clients, its SPIRE registrations and the acceptance rows that exercise it. Nothing in the catalog is per-goatnet. *Whether* a site runs a service is read from that site's own declarations; *where* is read from its declared shape.
2. **Presence has one home per service.** Where a service has its own flag (the service's `<name>_enabled` in the site's group_vars, else the role default), the flag decides. The fleet pin table declares versions: a component it merely renders for a member is a version, not a vote, and only its `absent: true` is a statement of presence, so an `absent: true` against a flag that says on is a **contradiction** ([#2140](https://github.com/usareur-af/goatcore/issues/2140)). A service with no flag takes the pin table and the founder rule (the goatnet's own control plane runs on the founder; a joiner does not run it, ADR 1084 §D3). A service no declaration answers for is **undeclared**; neither case is guessed. Today's gaps are listed in `ops/goatbuilder/endstate/baseline.txt`, which CI lets only shrink (GB-067).
   The flag is also what the deploy obeys: every role or play that deploys a catalog service reads its flag and skips (or refuses) where the site declares it off, so a play run never deploys a declared-absent service. `ops/ci/check-role-honours-presence-flag.py` gates it in CI.
3. **Optional services are declared absent, by the same flag.** A goatnet without Grafana sets `grafana_enabled: false`; without the sandbox, `zenoh_sandbox_enabled: false`. The observability services default to present, so a site opts out rather than in.
4. **A single machine is a declared shape, not a degraded one.** With `has_obs_host: false` the observability class runs on the management host; the model records the shape (`single-host`, `2-host`, …) and grades the same services on the one machine. What a single host gives up is stated in the design (one failure domain, observability shares the host it watches).
5. **Five grades per declared service, and its edges.** *Deployed*: the host's ledger names the declared ref, `action: deploy`. *Active*: every container running. *Healthy*: its docker healthcheck, or isitup's own probe of its declared health path with the row's `expect_codes`. *Functional*: the acceptance rows whose `requires` name it (ADR 1115). *Edges*, the zero-trust threads, each with a paired negative where one exists:
   - **route**: each served name resolves on the site's host, presents a certificate that verifies for that name, and its front door answers;
   - **OIDC client**: registered in Hydra with a redirect on the service's own name;
   - **SPIFFE**: the SPIRE registration entry exists;
   - **zenoh grant**: the router's live access_control grants a fabric client the keys its catalog row declares it publishes or subscribes, its certificate chains to the goatnet CA and its CN names an ACL subject; the operator-audit trail and an undeclared key are refused to it;
   - **NetBird path**: the site's declared matrix admits the groups that must reach the service on its ports (tcp/443 for a front door), the live account carries those rules enabled, and its internal ports are refused to participant groups; the account holds nothing undeclared and no All-group rule (ADR 1185);
   - **secrets**: every secret the service's deploy play templates is available to a deploy from this checkout under the declared posture (ADR 1204), names only; a name supplied only by the legacy plaintext `.envrc.local.d/*.envrc` is named as such.
6. **The negative side is graded.** A container, Hydra client or SPIRE entry that no present service declares is an **orphan**, whether a retired service's leftover or a service the site declares absent. An orphaned client or SVID registration is still a credential path.
7. **Honesty grading.** A grade nothing can read is UNKNOWN (an unreachable host, the assay not run) and keeps the verdict red. A grade nothing *could* read, because no signal is declared (no healthcheck, no acceptance row, no ledger), is **UNOBSERVED**: a coverage gap counted and listed apart, as ADR 1125 separates coverage from sync and health. `--strict` fails it too.
8. **Both avenues, one file.** `goatbuilder endstate <site>` and `ops/scripts/goatnet-endstate.sh <site>` run `endstate.py`; `ops/goatbuilder/parity-endstate.sh` holds them line for line on the fixtures in CI. The assay reads the model too: a row that requires only services the site declares absent reads N/A, not SKIPPED.

## Consequences

- Each goatnet's intended service set is visible and checkable from the repository alone (`endstate.py model <site>`), and the gaps in it are a countable list.
- The design records each measured result (`docs/design/goatnet-end-state.md`, §Measured).
- A role that does not yet read its `<name>_enabled` flag still deploys when its play is run. The end state reports the result truthfully (an undeclared container is an orphan; a declared one that is missing fails), and gating each role on its flag is a dated plan in the design.

## Alternatives rejected

- **A hand-kept per-goatnet service list.** It would be a second home for facts the group_vars and pin table already hold, and would drift exactly as the repo's other hand-synced copies did.
- **Treating the pin table as the presence source alone.** It renders all 31 components for every founder member: it declares versions, and absence only for joiners.
- **Keeping the pin table and the flag as two equal presence votes.** It made every founder contradict itself for each pinned service whose role defaults off (#2140) and gave presence two homes; the flag is the one the deploy reads.
- **Counting a missing signal as a pass.** It would report a goatnet as healthy because nothing was looking.
