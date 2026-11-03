# ADR 1009 — Ory Kratos / Hydra shared-`nid` pre-seed for Spock multi-master replication

| Field | Value |
|---|---|
| Status | Accepted (scope corrected 2026-05-04; ADR 1010 D8 cross-reference added 2026-05-14 — see scope note below + relationship section) |
| Date | 2026-04-25 (scope correction 2026-05-04) |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [`0411-ory-idp-mvp.md`](0411-ory-idp-mvp.md), [`0401-per-site-dex-oidc.md`](0401-per-site-dex-oidc.md), [`1008-soak-testing-discipline.md`](1008-soak-testing-discipline.md), [`1010-ory-on-spock-mindful-operation.md`](1010-ory-on-spock-mindful-operation.md), `../design/identity-architecture.md`, `../project/idp-decision-review.md`, `../design/ha-failover.md` |

> **Scope correction (2026-05-04).** This ADR's premise — running two
> parallel Kratos instances against a Spock-replicated database — was
> originally framed as the **intra-site A↔B** scenario (Router-A and
> Router-B both running live Kratos). That framing was wrong: per the
> corrected per-site topology, intra-site A↔B is **active-passive
> behind a keepalived VIP with Postgres streaming replication** (only
> A's Kratos process is live; B's stays stopped until `notify_master`
> promotes), so two parallel Kratos instances never exist intra-site
> and the multi-tenant `nid` mismatch problem does not arise.
>
> The discipline in this ADR remains valid and necessary for the
> **inter-site Spock multi-master** case (Phase 3 federation, e.g.
> zagros alpha ↔ zagros beta), where each site's active node runs its own
> Kratos+Hydra and Spock replicates between sites. There the two
> parallel `serve` processes ARE live concurrently, generate their
> own `networks`/`hydra_client_blob` rows independently, and need
> the deterministic shared-`nid` pre-seed described below.
>
> **Action items**: (a) deployment must pre-seed the shared `nid`
> only when inter-site Spock is being wired up, NOT at first boot of
> a single site; (b) the soak driver `run-2node-spock.sh` and the
> nid pre-seed Ansible task models the inter-site scenario, not
> intra-site (the two-postgres harness is structurally a two-site
> test); (c) intra-site bringup runbooks must NOT pre-seed shared
> `nid` since only one Kratos is live.

> **Scope vs ADR 1010.** This ADR covers the **one-time wiring fix**
> (pre-seed shared `nid` row before either Kratos `serve` boots).
> [ADR 1010](1010-ory-on-spock-mindful-operation.md) covers the
> **steady-state operating pattern** (D1–D6: active-passive VIP,
> lifespan budgets, retry policy, admin-API-only writes, ACE drift-
> watch, soak coverage). Both must hold for the identity layer to be
> safe under **inter-site** Spock replication.

> **Findings addressed** (per ADR 1011): F-001
> Kratos `networks` row + `nid` multi-tenancy under Spock multi-master;
> F-002 Hydra
> `hydra_client.nid` analog.

## TL;DR (for a layperson)

Ory's two services (Kratos for identities, Hydra for OAuth2 clients)
each tag every row they store with a "tenant" identifier so a single
deployment can hold multiple isolated organizations without leakage.
When we run two parallel copies of these services across a replicated
database, each copy generates its own tenant tag at first boot — and
even though the data flows freely between the database nodes, each
service then refuses to show the other's data because of the tag
mismatch. The fix is to insert one shared, deterministic tenant tag in
the database before either service starts, so both copies pick it up
and use it. We discovered this by running a structured 2-node soak
test, and we keep this discovery permanently in code (the soak driver
inserts the row automatically) and in this ADR (so a future operator
who hits this without the soak doesn't lose the day to relearning it).

## Context

Block 21C runs Ory Kratos + Hydra against the existing per-site
postgres-spock multi-master cluster (`ADR 0411`). Phase 2+ adds
intra-site Spock replication of the `kratos` and `hydra` databases for
HA failover between Router-A and Router-B.

The 2-node Spock-replicated soak (`run-2node-spock.sh`) immediately
surfaced a non-obvious failure mode:

- **Spock replication of identity rows worked correctly.** A row created
  via kratos-a's API ended up on postgres-b's `identities` table within
  a second.
- **But kratos-b's API returned 404 for that same row.** Even though the
  row was sitting right there.

Diagnosis was linear:

1. `SELECT count(*) FROM identities` on postgres-a == postgres-b → Spock
   is fine.
2. `SELECT id, traits->>'email' FROM identities WHERE id = '<id-from-A>'`
   on postgres-b returned the row → row really is there.
3. `curl http://kratos-b/admin/identities/<id-from-A>` returned 404 →
   API filter is rejecting the row.
4. `SELECT id, nid, ... FROM identities WHERE id = '<id-from-A>'` showed
   `nid = <Network-A-UUID>`.
5. A new identity created via kratos-b's API got `nid = <Network-B-UUID>`.
6. Both networks are present on both postgres nodes (Spock replicated the
   `networks` table too) — but each kratos cached one network at boot
   and only filters by *its* nid.

So: each kratos instance creates its own `networks` row at first boot of
`serve` and uses that row's UUID as the `nid` (tenant filter) for every
subsequent operation. With two parallel postgres-spock instances bringing
up kratos-a and kratos-b in parallel, each kratos generated its own
networks row independently. Spock then replicated both rows everywhere —
a perfect by-the-book outcome from the replication layer's perspective.
But the kratos API layer's "single-tenant" filter excluded everything
not in *its* network.

Hydra has the analogous structure (`hydra_client` + friends carry an
`nid` column tied to the hydra instance's bootstrap network row), so
the same pattern recurs symmetrically.

Without intervention, the only data path between two kratos instances
sharing a Spock-replicated database is "rows present, API blind." Every
production scenario (Router-A keepalived failover to Router-B, mid-
session API calls, OIDC issuer continuity) would silently break unless
we resolve the multi-tenancy mismatch.

## Decision

**Pre-seed a deterministic shared network UUID at both postgres-spock
nodes BEFORE either Kratos `serve` process starts.** Both Kratos
instances then see exactly one row in `networks`, pick it as their tenant
identifier, and produce identities tagged with the shared `nid`.
Replicated rows resolve through the API on either side.

Same pattern for Hydra: pre-seed the equivalent root tenant row in
`hydra_client_blob` / equivalent (Hydra's nid origin) before either
Hydra `serve` process starts.

The deterministic UUID:

- Comes from a fixed-per-site convention. Sample value used in the soak
  fixture: `00000000-0000-4000-8000-000000000001` (RFC 4122 v4 shape,
  unambiguously synthetic).
- Production deployment generates one UUID per site at bootstrap time,
  stores it in the per-site Ansible group_vars (e.g.
  `ops/ansible/group_vars/<site>_hosts/vars.yml` as
  `ory_shared_nid:`), and uses it for both kratos and hydra pre-seeds.
- New sites get a different UUID; replication is intra-site only (per
  ADR 0411 + ADR 0401), so cross-site collisions are impossible by
  topology.

**Implementation point.** The pre-seed runs after schema migrations
(both kratos-migrate and hydra-migrate complete) but BEFORE the
long-running kratos+hydra services start. Two equivalent insertion
patterns:

1. **Insert at one node, wait for Spock to replicate.** Requires Spock
   subscriptions to be configured first. Slower (one-second poll for
   the row to land at the other node).
2. **Insert at BOTH nodes with `ON CONFLICT (id) DO NOTHING`.** Order-
   independent. Spock's later subscription sees the row exists at both
   sides; the duplicate-INSERT is a harmless no-op. This is the pattern
   the soak driver uses (`run-2node-spock.sh`).

Pattern (2) is preferred for production because it's resilient to the
Spock setup ordering question.

**Verification.** Soak drivers (`run-2node-spock.sh` per ADR 1008) emit
two distinct fields per tick:

- `repl_a_to_b_db` — postgres-b returned the row by primary key
  (Spock-layer evidence, independent of API tenancy).
- `repl_a_to_b_api` — kratos-b's `/admin/identities/<id>` returned 200
  (kratos-tenancy + Spock both work; pre-seed is in place).

Splitting these fields is essential — without the split, a `repl_a_to_b
== 0` outcome can't distinguish "Spock didn't replicate" from "kratos
filtered the row out by nid mismatch," and the post-soak decision
review can't act on the right cause.

## Alternatives considered

- **Run a single Kratos instance per site (active-passive only at the
  process level, not active-active).** Lower complexity but loses HA
  capacity scaling and adds VIP-failover latency to every kratos
  request when A goes down. Rejected — the cost of pre-seeding one
  row at bootstrap is minimal compared to giving up active-active.
- **Patch Kratos to share network selection across instances at boot.**
  Upstream change with unclear acceptance pace. Rejected — the
  pre-seed is purely operational and requires no upstream change.
- **Don't replicate kratos+hydra at all (leave per-Router-A only,
  failover via DB restore).** Restore-on-failover RTO is minutes,
  not seconds. Rejected — replication is the whole point of HA at the
  identity layer.
- **Use Kratos's `--read-from` flag or similar config switch.** No such
  flag exists for tenant override at the time of writing (Kratos
  v1.3.1). Even if it did, pre-seeding is more robust than relying on
  config alignment between two instances.

## Consequences

**Positive.**

- 2-node Spock-replicated kratos+hydra is now operationally viable for
  Phase 2+ HA failover.
- The discovery is captured permanently — in this ADR, in the soak
  driver, in the decision review — so a future operator who runs
  these services on a replicated database doesn't relearn it.
- The split DB / API verification in the soak driver becomes a
  reusable pattern for any future component built on a replicated
  multi-tenant store.
- Each new site gets a per-site shared-nid by convention, reinforcing
  the per-site sovereignty model (ADR 0401).

**Negative.**

- Adds a bootstrap-time step (pre-seed the row at both nodes) to the
  sovereign site bringup runbook and any future site bringup. Cost is
  one INSERT per database. Documented; not silent.
- Silently fails if a future Kratos version changes the bootstrap-time
  network selection logic. Mitigation: the pin-regression CI smoke
  tier (per ADR 1008) re-runs the 2-node soak on every kratos version
  bump, surfacing any regression before it reaches production.

**Residual risk.** The shared-nid model assumes Kratos won't add
post-bootstrap network creation paths in future versions (e.g. an
admin API that creates a second tenant). If it does, our pre-seeded
single-tenant model breaks down differently than the original problem.
Compensating control: the smoke-tier CI catches it on the bump PR.

## Assessor view (SCA-R)

This decision sits in SC-12 (cryptographic key establishment — the
shared-nid is functionally a tenant key for the identity store) and
CM-2 (baseline configuration — the per-site shared-nid is part of the
documented baseline). The pre-seed is reproducible, version-controlled
in `group_vars`, and has automated regression coverage via ADR 1008's
soak discipline. Auditors can reproduce the failure mode (drop the
pre-seed, run the 2-node soak, observe the API-layer failure) and the
fix (add pre-seed, observe the API-layer pass) in under an hour.

## Trigger conditions for revisit

- Kratos changes its network selection logic in a way the pre-seed no
  longer covers (smoke-tier CI catches this).
- We move to a multi-tenant Kratos deployment (one Kratos serving more
  than one organization). At that point per-site shared-nid is wrong;
  we'd switch to per-tenant nids managed by the application boundary.
- A future Spock version supports tenant-aware filtering at the
  replication layer, eliminating the need for application-layer
  tenancy alignment. (Speculative.)

## Relationship to ADR 1010 D8 (added 2026-05-14)

ADR 1010's D8 amendment defines the narrow Spock repset scope for
Ory tables; the `networks` table (the nid scaffolding this ADR is
about) is on D8's inclusion list. The two decisions are
complementary:

- This ADR (1009) ensures the **same nid row exists** at every site
  before either Kratos or Hydra `serve` starts. Without it, each
  site's Ory bootstraps with its own random nid and the API filter
  hides cross-site data even when the underlying tables replicate.
- ADR 1010 D8 ensures that **when Ory-on-Spock activates**, the
  nid row (and the identity-row tables that depend on it) actually
  replicates. Without D8, the table-membership decision is implicit
  / accidental; with D8, it's explicit and bounded.

Activating the Spock extension on the kratos / hydra databases is a
separate step from the pre-seed: the nid row can be in place (Ansible
role applied; the row sits with the canonical UUID at every site)
before the replication half is wired. D8 closes the wiring decision; this ADR's
pre-seed is the prerequisite that makes the wired state correct.

---

*See `GLOSSARY.md` for terms and acronyms.*
