# ADR 1010 — Operating Ory (Kratos + Hydra) mindfully on Spock multi-master

| Field | Value |
|---|---|
| Status | Accepted (scope corrected 2026-05-04 + D8 amendment 2026-05-14 — see scope note below) |
| Date | 2026-04-25 (scope correction 2026-05-04; D8 amendment 2026-05-14) |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [`0411-ory-idp-mvp.md`](0411-ory-idp-mvp.md), [`0801-postgresql-spock-multi-master.md`](0801-postgresql-spock-multi-master.md), [`1009-ory-shared-network-nid-preseed.md`](1009-ory-shared-network-nid-preseed.md), [`1008-soak-testing-discipline.md`](1008-soak-testing-discipline.md), [`1011-findings-register.md`](1011-findings-register.md), [`0205-intra-site-vrrp-failover.md`](0205-intra-site-vrrp-failover.md), [`0206-island-mode-by-construction.md`](0206-island-mode-by-construction.md), `../design/identity-architecture.md`, `../design/ha-multi-site.md`, `../design/ha-failover.md`, `../project/idp-decision-review.md`. Forward-cross-reference: [`0224-progressive-config-rollout-with-auto-revert.md`](0224-progressive-config-rollout-with-auto-revert.md) §3.7 carves Ory tables (`kratos.*`, `hydra.*`) out of progressive-config-rollout coverage because D4 (admin-API-only mutations) is incompatible with the compensating-UPDATE revert mechanism. |

> **Scope correction (2026-05-04, updated 2026-05-06 for canonical-simple posture).** This ADR was originally framed
> around **intra-site Spock replication** of the `kratos` and `hydra`
> databases between Router-A and Router-B. That framing was wrong:
> per the corrected per-site topology, intra-site A↔B is
> **single-active with Postgres streaming replication and operator-fired
> failover** (only A's Kratos+Hydra are live; B's containers stay
> stopped until the operator-fired failover runbook promotes B's
> postgres + restarts the mgmt-stack on B with the local DSN — see
> `../design/ha-failover.md` Phase B and the
> per-site postgres-failover runbook). **VRRP-on-Orins was removed entirely 2026-05-06** per
> F-062;
> the canonical externally-mediated failover (VRRP on the site switch)
> becomes available once a site has that switch. Until
> then: operator-fired failover. The race-window classes the ADR
> addresses (Kratos browser flows, Hydra `auth_code`, concurrent admin
> edits) **do not arise intra-site** because there is only ever one
> live Ory process at a time; flow rows + auth codes simply move with
> the WAL stream and are visible on B after promote.
>
> The D1–D6 disciplines remain relevant for the **inter-site Spock
> multi-master** case (Phase 3 federation between sites), where each site's
> active node runs its own live Kratos+Hydra and Spock replicates
> between sites:
>
> - **D1 single-active per site** — already true intra-site (each
>   site's writes originate from one node only); applies fleet-wide
>   because each site has at most one live writer per row.
> - **D2 lifespan budgets** — must exceed worst-case **inter-site**
>   replication lag during a partition (cross-WAN, can be much
>   higher than intra-site lag) **AND** worst-case operator-fired
>   intra-site failover window (~minutes from incident detection
>   through promote + restart + DNS/bundle update). Current 10-minute
>   flow / auth_code lifespans cover both with comfortable margin.
> - **D3 service-side bounded retry** — applies on inter-site
>   redirects.
> - **D4 admin-API-only mutations** — applies fleet-wide.
> - **D5 ACE drift-watch** — applies to inter-site Spock subscriptions
>   only; not relevant intra-site (streaming WAL has no logical
>   drift).
> - **D6 soak coverage** — `run-2node-spock.sh` is structurally a
>   two-site Spock test; soak verdict applies to inter-site Spock,
>   not intra-site (which is exercised by streaming-replication
>   failover drills per `ha-failover.md`).
>
> **Operational impact**: nothing changes about how the soak driver
> exercises Spock+Ory. The post-soak verdict (Pass/Fail per
> `idp-decision-review.md`) gates inter-site federation, not
> per-site bringup. Per-site bringup proceeds without
> dependency on this ADR.
>
> **D8 amendment (2026-05-14).** The 2026-05-14 Pattern C audit
> established that Ory tables were not actually replicated in
> production at the time of the audit — the production Postgres init
> script gated the Spock extension load on a "validated" milestone
> that never happened. D8 (added below) defines the inclusion /
> exclusion list that any future Ory-on-Spock activation must use
> from the start, scoping replication to identity-row + signing-key
> tables only. See the
> Pattern C plan and
> schema audit
> for the operational details.

> **Findings addressed** (per ADR 1011): F-003
> Spock 5.x default conflict resolution is LWW per row (D1+D4+D5);
> F-005
> Kratos `secrets.cipher.0` is per-instance (production-deployment
> discipline; not D1–D6 directly, but in scope of Block 21C wire-up).

## TL;DR (for a layperson)

Our identity service (Ory Kratos + Hydra) stores its data in the same
PostgreSQL cluster that everything else uses, and that cluster
replicates writes between two database nodes so either one can take
over if the other fails. This sort of replication is *eventually*
consistent — a write at one node takes a moment to show up at the
other. Three things in identity software are sensitive to that gap:
in-progress login flows, freshly-issued OAuth2 codes, and concurrent
edits to the same user record. We address all three by (a) routing
all writes through one node at a time (today by running Kratos+Hydra
only on the primary; failover is operator-fired per the
postgres-failover runbook),
(b) sizing Kratos's flow lifespan and Hydra's auth-code lifetime
to be much longer than the replication lag plus the failover
window, and (c) requiring all identity edits to go through the
Kratos admin API so the database never sees parallel out-of-band
writes to the same row. The result is
that Spock and Ory coexist cleanly without giving up Spock's
island-mode benefit (each site keeps authenticating its own users
during a partition).

## Context

ADR 0411 deploys Ory Kratos + Ory Hydra per-site, backed by the
existing postgres-spock cluster (ADR 0801). Phase 2+ enables
intra-site Spock replication of the `kratos` and `hydra` databases so
the identity layer survives a Router-A failure via keepalived VIP
failover to Router-B (ADR 0205). ADR 1009 captured one specific
gotcha (the multi-tenant `nid` filter) but did not articulate the
broader pattern.

Three classes of data have race windows narrower than worst-case
Spock replication lag:

1. **Kratos browser flows** (login, registration, recovery, settings).
   A flow row is created on the active node when a user starts the
   flow, and updated on completion. Lifespan today: 60 minutes (raised
   from 10 in the D2 note below).
   Replication lag in our deployment: <1 s steady-state, bounded by
   tens of seconds during transient WAL congestion. If the VIP
   fails over mid-flow and the flow row hasn't replicated, the
   user's submission lands on a node that returns "flow not found."

2. **Hydra OAuth2 authorization codes**. Single-use, written by the
   active node at code-issuance and expected to be consumed within
   the `auth_code` TTL (10 minutes). If the VIP fails over after
   issuance but before the code-exchange POST, the new active node
   needs to have replicated the code row.

3. **Concurrent identity mutations**. Two writes to the same row from
   different sources collide if both reach the Spock cluster within
   the conflict window. Spock's default conflict resolution is
   last-writer-wins, which silently drops the loser's columns. With
   active-passive deployment, the only paths that produce concurrent
   writes are (a) a brief split-brain during VIP failover, (b)
   out-of-band SQL writes by an operator or migration tool that
   bypasses the VIP, or (c) a future move to active-active.

This ADR locks the operating pattern that keeps all three benign.

## Decision

### D1 — Single-active deployment with operator-fired failover

Per-site Ory deployment is **single-active**, not active-active
load-balanced (ADR 0411). Kratos + Hydra run only on the
currently-active mgmt-host node (Orin-A in steady state); B's
`kratos` + `hydra` containers stay stopped. All writes — both browser
flows and admin API calls — terminate on the active node by
construction.

**Failover model (canonical-simple, 2026-05-06).** Per
F-062
and the per-site postgres-failover runbook,
failover is operator-fired:

1. Operator confirms A is dead (not just network-partitioned)
2. `pg_ctl promote` on B's postgres
3. Start `kratos` + `hydra` on B with the local-postgres DSN
4. Update DNS / bundle so peers / consuming services reconnect to B's endpoint

This is the same failover model as netbird-mgmt — both share the same
mgmt-host pair + postgres-spock cluster, both follow the same
canonical-simple posture (no VRRP-on-Orins; canonical externally-mediated
failover via switch VRRP becomes available once a site has that switch and
restores symmetric semantics).

**Practical effect on the three races:**

- **Race 1 (flows):** under steady state, the flow-create and
  flow-complete requests both land on Orin-A. Cross-node hand-off
  only happens at operator-fired failover.
- **Race 2 (auth codes):** same.
- **Race 3 (concurrent mutations):** during steady state there is
  exactly one writer per row. Concurrent mutation requires that the
  operator has fired failover during a transient (postgres on B
  promoted, kratos+hydra restarted there) AND a stale request from
  the prior active-period reaches A, which by then is offline by
  construction (operator only fires after confirming A dead).

This is the cheapest defense at the cost of a longer failover window
(~minutes vs ~seconds for VIP-mediated). D2 lifespan budgets are
sized to cover the operator-fired window comfortably.

### D2 — Flow / code lifespans must exceed worst-case failover-plus-replication-lag

The Spock catch-up window after a failover (replication lag from the
just-failed node's outstanding WAL) is bounded by `wal_keep_size`
plus replay throughput. Empirically <5 s in steady state on the
postgres-spock-test fixture; budgeted to <60 s under transient WAL
congestion in production.

Lifespans set in our config templates:

| Setting | Value | Why |
|---|---|---|
| Kratos `selfservice.flows.login.lifespan` | **60 m** (was 10 m, raised 2026-08-08) | 60 s lag budget × 60 safety factor — see the note below |
| Kratos `selfservice.flows.registration.lifespan` | **60 m** (was 10 m, raised 2026-08-08) | Same |
| Kratos `selfservice.flows.settings.privileged_session_max_age` | 15 m | Same |
| Hydra `ttl.auth_code` | 10 m | Same |
| Hydra `ttl.access_token` | 1 h | Independent — service-side validation |
| Hydra `ttl.refresh_token` | 30 d | Independent — separate rotation discipline |

**2026-08-08 — login/registration flow lifespan raised 10 m → 60 m.**
This constraint is a **floor**, not a ceiling: D2 requires the lifespan to
EXCEED worst-case failover-plus-lag (~60 s budgeted). Raising it widens the
margin from 10× to 60×, so the property this decision protects is not weakened.

What the 10 m value costs in operation: an operator working across
several fenced mesh surfaces loses any sign-in form they opened while reading
something else. Kratos handles this correctly — it re-renders with *"The login
flow expired N minutes ago, please try again"* — so the cost is interruption
and retyping, not an error page.

The reasoning for the change: a **login-flow** lifespan is how long a rendered
form stays submittable. It is not the session (24 h), and an unsubmitted form
grants nothing — so shortening it buys no session security while guaranteeing
that anyone who opens a tab and comes back loses it. This environment fans one
IdP across five or more fenced surfaces, which makes "opened it, came back"
the normal path rather than an edge case.

**Deliberately NOT changed:** Hydra `ttl.auth_code` stays 10 m. An
authorization code is exchanged machine-to-machine within seconds, so its
lifespan is not a usability constraint and lengthening it would be a real
loosening.

**Operational discipline**: any future change to these TTLs goes
through the same review as any other production-config change, and
the ADR-1009 soak driver validates the relationship implicitly (its
30-second tick interval will fail if the lifespan ever drops below
the realized lag).

### D3 — Service-side retry on flow-not-found and code-not-found is mandatory

Services exchanging a Hydra authorization code, and browser frontends
completing a Kratos flow, must implement bounded retry on 404 / 410
responses to absorb the small replication-lag window during a
failover. Retry policy:

- Up to 3 attempts
- 200 ms initial backoff, 2× exponential, ±50 ms jitter
- Total budget capped at 5 s
- After exhaustion, surface the error to the user (not the operator)

Reference implementation: documented in
`../design/identity-architecture.md`
§11.1 (browser-flow walkthrough) and §11.2 (m2m walkthrough). Each
service that consumes Ory ships this retry helper.

### D4 — Identity mutations go through the Kratos admin API only

No direct SQL writes to `identities`, `identity_credentials`,
`identity_recovery_addresses`, `identity_verifiable_addresses`,
`sessions`, `hydra_client`, or any other Ory-owned table — including
from migration scripts, Ansible roles, or operator one-off psql
sessions. The single supported write paths are:

- `kratos identity create / import / update / delete` (admin CLI)
- `POST /admin/identities` and friends (admin API)
- `hydra create client / update client / delete client` (admin CLI)
- `POST /admin/clients` and friends (admin API)

The reason: the Kratos / Hydra processes serialize concurrent writes
to the same row through their own logic *before* the row reaches
postgres-spock. With single-writer-per-row at the application layer,
Spock's default LWW conflict resolution is a non-event because there
are no conflicts to resolve. Out-of-band SQL writes break that
invariant and silently lose data.

Codified in:

- The `ory` Ansible role uses `kratos identity import` for admin
  seeding, never `INSERT INTO identities`
  ([`../../ops/ansible/roles/ory/tasks/main.yml`](../../ops/ansible/roles/ory/tasks/main.yml)).
- The `mgmt-host-bootstrap` Go CLI does **not** include any
  Ory-table writers (ADR 0410); it is scoped to netbird's own tables.
- The identity-architecture doc states the rule explicitly
  (`../design/identity-architecture.md`
  §5).

### D5 — ACE drift-watch covers Ory tables

The pgEdge ACE (Active Consistency Engine) cron job already planned
for netbird tables (per `ha-multi-site.md` §5.8) extends to the
`kratos` + `hydra` databases. Any non-empty `ace table-diff` output
on Ory tables alerts via the same Block 17A.5 notification path as
the rest of the Spock-replicated set. This is the safety net for any
write path we missed (D4) or any conflict Spock did not resolve the
way we expected.

### D6 — Soak coverage validates the pattern, not just the mechanism

The 2-node Spock-replicated soak driver
([`../../ops/ansible/roles/ory/tests/soak/run-2node-spock.sh`](../../ops/ansible/roles/ory/tests/soak/run-2node-spock.sh))
emits split DB-vs-API success fields per tick (per ADR 1009).
Cases implemented today:

- **A14 — login-flow row replication A→B.** Measures the realized
  flow-row lag against the lifespan budget in D2. Probe only — does
  not exercise an actual cross-node flow completion (which the VIP
  prevents in production anyway).
- **A15 — Hydra access-token issued at A, introspected at B.**
  Forward-direction complement to the existing B→A token check.
- **A16 — concurrent same-row UPDATE on A and B (LWW probe).** Fires
  two `UPDATE identities … SET traits.name = …` writes nearly
  simultaneously on both nodes, waits for convergence, and emits
  `concurrent_converged` + `concurrent_data_loss` so a regression
  shows in JSONL rather than as a silent column drop.
- **F3 — concurrent same-email identity-create at A AND B.** Fires
  two `POST /admin/identities` writes with the same email on both
  Kratos instances. Pass = exactly one returns 200 (Kratos's app-
  layer uniqueness held); fail = both return 200 (the discipline now
  relies on Spock's LWW, which silently drops one row). Emits
  `f3_ok_count` + `f3_app_layer_caught`.

Partial F1+F2 coverage via the soak driver's nemesis mode (gated by
`SOAK_NEMESIS=1`, intended for canonical-tier 72 h runs):

- **pause_postgres_b** — `docker pause postgres-b` for `NEMESIS_HOLD_S`
  seconds, then unpause. Stalls B-side writes + Spock replay without
  changing network plumbing. Validates D2 (lifespan budget exceeds
  realized partition window) and D5 (drift surfaces) on the event tick
  and the immediate-next tick.
- **kill_kratos_a** — `docker kill kratos-a`; compose's
  `restart: unless-stopped` brings it back. Validates D3 (service-side
  bounded retry on transient 404/410) and confirms Kratos reconnects
  to its DSN with no operator action.

Each nemesis tick emits `nemesis_event=<tag>`; the immediate-next tick
emits `nemesis_in_recovery=1`. The summarizer reports a recovery
green-rate over recovery ticks separately from the steady-state rate,
so a regression in failover behavior shows in JSONL rather than as an
averaged-out signal.

Still future work (requires real VIP-flip orchestration):

- **F1 — flow created on A, completed on B after VIP transfer.**
  The nemesis-mode `kill_kratos_a` exercises the kratos-side recovery
  shape but does not move the request from one node to the other
  mid-flow. A real F1 needs keepalived + Traefik in the test fixture.
- **F2 — auth code issued on A, exchanged on B after VIP transfer.**
  Same harness shape as F1.

The soak driver is the regression-coverage anchor; pin-regression CI
(ADR 1008) re-runs the smoke tier on every Kratos / Hydra image bump.
F1 + F2 land before Block 21D commits production downstream consumers.

### D7 — Hydra `networks` row pre-seeded at pre-2013 timestamp

ADR 1009's shared-`nid` pre-seed pattern works as written for Kratos
but fails silently for Hydra. The Hydra-side root cause was sleuthed
on 2026-04-26 (see F-002
and the Hydra solo-boot probe T1–T5 on a dev environment that day):

1. `hydra-migrate sql -e -y` inserts a default `networks` row with a
   **random UUID** at a hardcoded **`created_at = 2013-10-07 08:23:19`**.
2. Hydra's `networkx.Manager.Determine()` selects the tenant via
   `c.Q().Order("created_at ASC").First(&p)` — adopts the row with
   the earliest timestamp.
3. A pre-seed using `created_at = NOW()` is later than 2013-10-07,
   so it loses the tie-break and Hydra adopts the migration row.
4. Across multiple Hydra instances (one per site), each migration
   inserts its own random-UUID 2013 row. Postgres's `ORDER BY
   created_at ASC` is non-deterministic on ties, so each Hydra picks
   a different one. Cross-site federation breaks.

**Operating rule:** Pre-seed the shared `networks` row in Hydra's
database (and Kratos's, for symmetry) with a `created_at` earlier
than 2013-10-07. The deploy uses `1970-01-01 00:00:00`. The
deterministic UUID is shared across all sites in
`ory_shared_network_nid` (defaults/main.yml) so every Hydra at
every site adopts the same canonical tenant.

```sql
INSERT INTO networks (id, created_at, updated_at)
VALUES ('{{ ory_shared_network_nid }}',
        '1970-01-01 00:00:00',
        '1970-01-01 00:00:00')
ON CONFLICT (id) DO NOTHING;
```

Run after `hydra-migrate` (so the table exists), before either
Kratos or Hydra `serve` starts. T5 probe on a dev environment 2026-04-26
confirmed: two parallel Hydras on separate postgres instances both
adopt the same pre-seeded nid; OAuth clients written by either are
visible to the other's API.

This subsumes ADR 1009 for the Hydra side; ADR 1009 stands for the
Kratos side (Kratos's migration does not bake a 2013-timestamped
row that competes for earliest, so a `NOW()` pre-seed wins by
default — but for safety the deploy uses pre-1970 for both).

### D8 — Narrow Spock repset scope for Ory tables (Amendment 2026-05-14)

**Context.** D1–D7 assume Ory tables ride on Spock multi-master
replication. The 2026-05-14 audit during the Pattern C track pickup
established two facts:

1. **Production was not actually replicating Ory tables.** The
   production Postgres init script
   ([`ops/postgres/spock/init/02-spock-extension.sql`](../../ops/postgres/spock/init/02-spock-extension.sql))
   explicitly does not load the Spock extension into the `kratos` or
   `hydra` databases (an inline comment gates the change on "when
   intra-site Spock for kratos+hydra is validated"). The Ory Ansible
   role + the operator-idp-postgres role have zero `spock` /
   `repset` / `sub_create` references. `CREATE EXTENSION spock`
   against the kratos/hydra DBs exists only in the soak harness.
   The A14 / A15 measurements were soak-harness numbers; activation
   into production never landed.
2. **When activation lands, the repset scope must be narrow from
   the start.** D1–D7 cover *write-path discipline* (single-active,
   admin-API-only, drift-watch); they do not constrain *which tables
   replicate*. Replicating high-churn transactional tables (flows,
   sessions, auth codes, tokens) under Spock LWW is operationally
   noisy and — in the case of `hydra_oauth2_code` — actively
   security-relevant (single-use semantics under LWW pick one row
   in a concurrent-consume race, breaking the single-use property).

**Operating rule.** When Ory-on-Spock activates at a site (per-site
opt-in via `ory_spock_enabled: true` group_var), the only repset that
gets the kratos / hydra tables is `ory_narrow`, containing exactly
the inclusion list authored in
`../project/ory-spock-narrow-repset-schema-audit.md`:

**Kratos (7):** `identities`, `identity_credentials`,
`identity_credential_identifiers`, `identity_credential_types`,
`identity_recovery_addresses`, `identity_verifiable_addresses`,
`networks`.

**Hydra (3):** `hydra_client`, `hydra_jwk`, `networks`.

All other `kratos.*` and `hydra.*` tables stay site-local — explicitly
excluded by the `ory-narrow-repset` Ansible role's
`spock.repset_add_table` calls + pre-flight assertion that no
exclusion-list table is a repset member. Full classification matrix
and rationale per table is in the schema-audit doc.

**Why this is the narrow choice.** Identity-row tables + signing
keys are low-churn, change at human-action cadence, and have clear
cross-site value (any-site authentication, JWT validation via JWKS).
Flow / session / token / auth-code tables are inherently site-local,
have short TTLs (10 m – 1 h), and LWW conflict resolution is at best
useless (flows are single-user single-flow; replicating doesn't add
anything) and at worst incorrect (auth codes are single-use; LWW on
concurrent-consume breaks security).

**JWT cross-site validity is preserved without `hydra_oauth2_*`
replication.** Per F-078, `access_token_strategy=jwt` makes Hydra
issue JWTs whose validity is established by signature verification
against `hydra_jwk`-loaded keys exposed at every site's JWKS
endpoint. A JWT issued at site A is verified at site B via JWKS,
not via DB lookup. The `hydra_jwk` inclusion in the narrow repset
is the load-bearing piece for cross-site auth; the rest of the
token tables are housekeeping that can be site-local.

**ADR 1009 unchanged in scope; complementary to D8.** Identities
are nid-tagged regardless of how many tables replicate. The
`networks` row pre-seed (1009) makes the multi-tenant filter
return non-empty at every site. D8's `networks` inclusion makes
the row itself replicate so drift-watch can detect tampering.

**Cost saved.**

- ~95% of replication volume eliminated (the transactional tables
  are where the chatter is).
- Auth-code single-use LWW security exposure structurally
  prevented — not a runtime check, the exposure simply doesn't
  exist because the rows don't replicate.
- F3 (concurrent-same-email-create) drops in severity to the
  identity-row write rate, which is orders of magnitude lower than
  flow-row writes.
- Ory upgrades stop being fleet-coupled to Spock schema
  considerations for the high-churn tables (those tables can change
  shape freely on point releases without breaking subscriptions).

**Soak coverage update under D6.** A14 + A15 (flow / token A→B
replication probes) measure properties that are no longer present
under D8 and are correctly so. The Pattern C plan retires them and
adds:

- A14′ — identity row replicates A→B within Spock lag budget.
- A15′ — Hydra signing key replicates A→B (rare event; probe via
  deliberate key rotation).
- A17 — auth code issued at A is NOT consumable at B (single-use
  preserved by exclusion).
- A18 — JWT issued at A validates at B via JWKS (cross-site auth
  via signature, not DB).

**Reversibility.** Adding a previously-excluded table to the repset
is a one-line `spock.repset_add_table` SQL. Removing a previously-
included table is the same shape, in reverse. The role's inclusion
list is a defaults-overridable Ansible variable, not hardcoded;
per-site exception or future expansion is straightforward.

**Cross-references.**

- `../project/ory-spock-narrow-repset-plan.md`
  — the plan that authored this amendment.
- `../project/ory-spock-narrow-repset-schema-audit.md`
  — per-table classification matrix.
- F-078
  — `access_token_strategy=jwt` finding that makes D8 viable.

## Alternatives considered

- **Active-active load-balanced Ory (no VIP affinity).** Doubles read
  capacity but multiplies the conflict surface across all three races.
  Rejected for our scope — at an admin population in the tens, the active-
  passive pattern has plenty of headroom and trivially eliminates the
  conflict window during steady state.

- **Synchronously-replicated Postgres (Patroni-style with
  `synchronous_commit = remote_apply`).** Eliminates replication lag
  but defeats island mode — a partitioned site could not commit
  identity writes locally. Rejected because island mode is a load-
  bearing property of the design (ADR 0206).

- **Single-node Ory per site (no replication of `kratos` + `hydra`
  databases at all).** Trivially eliminates all three races. Rejected
  because identity loss on Router-A failure means a site goes auth-
  dark until pgBackRest restore completes (minutes-to-hours RTO),
  which is unacceptable for the operator-on-call human-auth path.

- **Application-layer sticky session affinity (cookie-pinned
  routing).** Useful for active-active deployments. Not needed for
  active-passive because the VIP already enforces single-node-per-
  active-period writes. Re-open if we ever move to active-active.

- **Tighter conflict policy via per-column resolvers (Spock's
  `spock.set_conflict_resolution`).** Would let us encode "password
  hash always wins from the most-recent timestamp" or similar. Not
  worth the complexity at our scale; D4 + D5 (single writer +
  drift-watch) is operationally simpler and easier to reason about.

## Consequences

**Positive.**

- The full Ory stack runs on the existing Spock-replicated Postgres
  cluster without sacrificing island mode (ADR 0206) or accepting
  silent data loss.
- Operators have an explicit, terse playbook (D1–D6) for "how do I
  not break this" rather than discovering the gotchas the hard way.
- The pattern generalizes: any future Ory-shaped service (Keto,
  Oathkeeper, similar OIDC software with browser-flow + auth-code
  semantics) adopts the same six rules.
- Failure modes are caught by mechanical regression coverage (ADR
  1008 pin-regression smoke + ACE drift-watch) instead of operator
  vigilance.

**Negative.**

- The 10-minute flow / code lifespans are user-visible. If an
  operator stalls mid-login for more than 10 minutes, they restart
  the flow. Acceptable for our scope.
- D4 requires discipline. A future contributor could write a
  one-off SQL-direct migration and silently break the invariant.
  Mitigation: ACE drift-watch (D5) surfaces the resulting divergence;
  CONTRIBUTING.md will pick up a paragraph on this rule once it
  proves stable.
- The three failover-mode soak cases (F1–F3) are not yet implemented
  — the soak driver currently exercises steady-state replication
  only. Tracked as a Phase 2 follow-up in
  `../project/idp-decision-review.md`.

**Residual risk.**

- A future Ory release could change row-write patterns (e.g.,
  introducing a row that's mutated outside the admin API path). The
  pin-regression smoke catches this on the bump PR.
- A future Spock release could change conflict-resolution defaults.
  ACE drift-watch surfaces the resulting divergence; the operator
  decides on the per-table policy at that time.
- Active-active upgrade in the future re-opens all three races. This
  ADR is the gate to revisit before that change ships.

## Assessor view (SCA-R)

This ADR sits in **CM-2** (baseline configuration — the six
operating rules are part of the documented baseline), **CM-7**
(least functionality — restricting writes to the admin API path is
a least-functionality control on the Postgres surface), **AU-12**
(audit generation — every Ory write goes through the admin API and
emits an audit event per `identity-architecture.md` §7), and **CP-2**
(contingency planning — the failover behavior under D2 + D3 is a
documented contingency property that the soak validates). The pattern
is reproducible, version-controlled, and has automated regression
coverage. No CMVP-relevant crypto change.

## Trigger conditions for revisit

- Move from active-passive to active-active for Ory (re-opens all
  three races; D1 no longer holds; D3 retry alone is insufficient
  for the new conflict surface).
- Spock release changes default conflict resolution semantics
  (D5 surfaces it via ACE drift-watch; operator decides per-table
  policy).
- Kratos or Hydra release adds a new write path that does not flow
  through the admin API (D4 invariant breaks; pin-regression smoke
  catches the change).
- Failover-window soak (F1–F3) reveals the lifespan budget in D2 is
  too tight under realistic WAL-congestion conditions; raise the
  TTLs and update the table.
- **D8-specific:** a real use-case for cross-site flow-row or
  session-row visibility materializes (e.g., partial-flow handoff
  across sites mid-authentication). Today no such use-case exists;
  if one does, weigh the value against re-opening the LWW conflict
  surface on the relevant table class and the auth-code single-use
  exposure.
- **D8-specific:** a Kratos or Hydra release adds a new
  identity-tier table not on the schema-audit inclusion list
  (e.g., a new credential type stored in a separate table). The
  Sub-block C reality-check pass should catch this on each
  version-bump audit; add to the inclusion list, amend the
  schema-audit doc, re-run the smoke harness.
- **D8-specific:** the JWT-bearer grant
  (`hydra_oauth2_trusted_jwt_bearer_issuer` table) becomes a real
  deployment pattern (currently excluded as "not used"). Promote
  to the inclusion list at that time.
- Cross-site identity replication is enabled (today: per-site
  sovereign per ADR 0401; cross-site changes the conflict model
  fundamentally).

---

*See `GLOSSARY.md` for terms and acronyms.*
