# ADR 1193 — Revocations are absorbing on the replicated NetBird and Kratos tables

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-09-28 |
| Repo scope | goat core (`ops/postgres/spock/`, roles `spock-mesh-peering` + `ory-narrow-repset`, the `*-seed-from-peer.yml` playbooks, `crates/goat-cli` native revoke, `ops/compose/mgmt-host/monitoring/`) |
| Supersedes | — |
| Superseded by | — |
| Evidence | F-573, F-574; CI job `postgres-spock.yml` → `partition-revoke` |
| Related | ADR 1084 (the 20-table `netbird_mgmt` repset, single-homed admin writes), ADR 1010 (Ory on Spock, narrow repset), ADR 1094 point 5 + `grant-keyed-encryption.md` §6 (revoke-wins for grants), ADR 1031 (revocation is mesh-first and reversible); design `ha-multi-site.md` §5.1, §8; issue #1505 |

## TL;DR (for a layperson)

A goatnet's host sites keep copies of who is on the network and who can sign
in. After a network cut between sites, the copies merge row by row and the
latest write wins. A routine write at the cut-off site, such as a device
reconnecting, therefore undid revocations made at the other site. This was
measured, not hypothetical. From now on a revocation is recorded in a
replicated list that only grows, and no later write can undo it; undoing one
takes a deliberate reinstate.

## Context

Spock replicates the NetBird management tables (ADR 1084) and the Kratos and
Hydra identity tables (ADR 1010 D8) between a goatnet's sites, and settles a
conflict per row by the later commit timestamp. Revocations in these tables are
ordinary column values or deletes: `setup_keys.revoked`, `users.blocked`,
`peers.peer_status_login_expired`, `identities.state = 'inactive'`, and row
deletes of peers, keys and identities. NetBird and Kratos rewrite those same
rows in normal use: `IncrementSetupKeyUsage` on every enrolment,
`SaveUserLastLogin` (a full-row save) on every login, `SavePeerStatus` on every
client connect, and Kratos on a self-service settings change.

The induced-partition test `ops/postgres/spock/tests/partition-revoke/run.sh`
measured on Spock 5.0.11: each of those writes, made at the cut-off site
after the revocation, undid it at **both** sites. Both sites then agree on the
wrong value, so the drift watch, which compares sites, cannot see it.

Administrative writes are single-homed (ADR 1084 D3), but the writes that undo a
revocation are the services' own runtime writes, which every site makes.

## Decision

**1. A revocation is recorded in an insert-only, replicated ledger.** Each of
the `netbird` and `kratos` databases gets a `goat_revocations` table
(`kind`, `subject_id`, `action` = `revoke` | `reinstate`, `at`, `actor`, `site`,
`cancels`), a member of that database's repset (`netbird_mgmt`, `ory_narrow`).
Rows are never updated or deleted (a trigger refuses both). Inserts carry fresh
uuid keys, so they never conflict and every site ends up holding every
revocation.

A subject is revoked while any `revoke` event for it is not named in the
`cancels` set of a `reinstate` event for the same subject (`goat_revoked()`).
The decision is set membership: `at` and arrival order never enter it, so a
site whose clock runs behind cannot order an older reinstatement after a newer
revocation made elsewhere during a partition. `cancels` (a `uuid[]`, NULL on
revoke rows, enforced by a check constraint) lives on the reinstate row itself,
so one replicated insert carries the whole decision. Historical baseline events
written at install get an id derived from `(kind, subject_id)` and no `site`,
so the same pre-install fact backfilled at two sites is one event, and a
reinstatement at either site cancels both copies.

| `kind` | Recorded when |
|---|---|
| `netbird.setup_key` | `setup_keys.revoked` goes false → true |
| `netbird.setup_key.deleted` | a `setup_keys` row is deleted |
| `netbird.user` | `users.blocked` goes false → true |
| `netbird.peer.deleted` | a `peers` row is deleted |
| `kratos.identity` | `identities.state` becomes `inactive` |
| `kratos.identity.deleted` | an `identities` row is deleted |

**2. Revocations are captured from the services' own writes.** Origin-only
triggers on those tables append the ledger row, so every revoke path (goat-cli,
the portal, the console, the NetBird dashboard, the Kratos admin API) is covered
without change. They do not fire for Spock apply, so a replicated revocation is
not recorded twice.

**3. No later write can undo a recorded revocation.** `ENABLE ALWAYS` triggers,
which fire for local writes and for Spock apply alike, force the revoked value
back (`revoked`, `blocked`, `peer_status_login_expired` for the user's peers,
`state = 'inactive'`) and drop any write that would bring a deleted peer, key
or identity back.

**4. A ledger row landing at a site re-applies the revocation there.** This
corrects a stale row that won the merge at that site, and deletes that site's
Kratos `sessions` for the identity. Sessions are not replicated, so this is what
ends a revoked person's sessions at every site. `goat_revocation_sweep()`
re-applies every standing revocation and returns the rows it changed (0 on a
converged site).

**5. Undoing a revocation is deliberate, and undoes only what it names.**
`goat_reinstate(kind, subject, actor)` appends a `reinstate` row whose
`cancels` holds every revoke event for that subject visible at the reinstating
site at that moment. After that the normal unblock works. A revoke made at
another site and not yet received there is not cancelled: when it arrives the
subject is revoked again at every site, and a second reinstatement, which now
sees it, is needed. A revoke after a reinstatement revokes again. An unblock
through the NetBird API or dashboard alone leaves the user blocked. A reinstate
row with no cancel set (one written before the column existed) cancels nothing.

**6. Spock skips only the failing change, and records conflicts.** The
`postgres-spock` image sets `spock.exception_behaviour = discard` (was the
default `transdiscard`), `spock.exception_logging = all` and
`spock.save_resolutions = on`. postgres-exporter counts
`spock.resolutions` and `spock.exception_log`; `SpockConflictLogged` reads the
first and the new `SpockApplyExceptionLogged` the second.

**7. The native revoke ends sessions and blocks the NetBird user.**
`goat admin onboard <handle> --revoke --native` now deletes the identity's Kratos
sessions after deactivating it (R2) and blocks the NetBird account whose id is
the Kratos identity id (R2.5, new receipt field `netbird_user`).

## Where it is installed

| Surface | Change |
|---|---|
| `ops/postgres/spock/revocation-guard/{netbird,kratos}.sql` | The guard. Idempotent; needs the service's tables to exist |
| `spock-mesh-peering` | Installs `netbird.sql` before curating the repset (`spock_mesh_revocation_guard`, default true); `goat_revocations` added to `spock_mesh_netbird_mgmt_tables` |
| `ory-narrow-repset` | Installs `kratos.sql` before the repset step (`ory_spock_revocation_guard`, default true); `public.goat_revocations` added to the kratos inclusion list |
| the `*-seed-from-peer.yml` playbooks | Install the guard on both ends before the dump and seed the ledger |
| `ops/postgres/spock/conf/postgresql.conf` | Decision 6 settings |
| `postgres-spock.yml` CI | `partition-revoke` job, matrix `GUARD=0` (pins the defect) and `GUARD=1` (proves the guard) |

Nothing changes on a live goatnet at merge. The guard lands on the next
`spock-mesh-peering` / `ory-narrow-repset` run for a goatnet with inter-site
Spock; the image settings on its next `postgres-spock` roll through the release
ladder. A single-site goatnet is unaffected.

## Consequences

- The ledger grows by one row per revocation, including NetBird's own deletes of
  ephemeral peers. Rows are small and never removed; a revocation is a fact
  worth keeping.
- A NetBird or Kratos upgrade that renames `revoked`, `blocked`,
  `peer_status_login_expired`, `user_id`, `state` or `sessions.identity_id` breaks
  the guard's install (it fails loudly, `ON_ERROR_STOP`). The `partition-revoke`
  job uses cut-down tables, so it does not catch that; the schema audit for a
  version bump must.
- Running services learn of a correction the same way they learn of any
  replicated change: NetBird management reads the database, with the 30–60 s
  cross-site delay `ha-multi-site.md` already accepts.
- Revocation still reaches a cut-off site only when the link returns. This
  decision makes it stick once it arrives; it does not make it arrive sooner.
- With `discard`, a transaction can be applied partly at another site (the
  failing change skipped, the rest applied). Before, the whole transaction was
  dropped. Each skipped change is in `spock.exception_log` and alerts.

## Alternatives rejected

- **A per-column or custom Spock conflict resolver.** ADR 1010 already rejected
  per-column resolution as not worth the complexity, and a resolver sees only an
  update against an update: it cannot stop a deleted row from being written back.
- **A post-rejoin job that re-applies revocations through the admin APIs.**
  Leaves a window after each rejoin, depends on a job running at every site, and
  still needs a replicated record of what was revoked, which is the ledger.
- **Latest event by `at` decides.** A partitioned site's clock orders its
  events against the other site's, so an older reinstatement from a site whose
  clock runs ahead undoes a newer revocation.
- **Cancel sets in a separate `(reinstate_id, revoke_id)` table.** Replicates
  as several rows. With `spock.exception_behaviour = discard` (Decision 6) a
  reinstatement can land without some of its cancel rows, or the reverse, and
  the table adds a repset member and a second merge path.
- **Relying on single-homed admin writes (ADR 1084 D3).** The writes that undo a
  revocation are runtime writes every site makes, not admin writes.
