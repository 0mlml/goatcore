# ADR 1205 — The highest-echelon site of a goatnet overrides every other site and can sever one; identity edits run at the identity's owner site

- **Status:** **Proposed.** Nothing in a live goatnet changes on merge. Each part ships behind the ADR 1165 partition harness and turns on with ADR 1165 step 6.
- **Date:** 2026-10-29
- **Deciders:** operator
- **Evidence:** F-590 — two Kratos sites that both write one identity while cut off never converge, because every admin write replaces the identity's credential rows with fresh ids.
- **Amends:** [ADR 1165](1165-island-mode-multi-writer-every-site-admits.md) D3 class 2 (who owns an identity, and how ownership moves) and class 5 (which site is the hub).
- **Related:** [ADR 1193](1193-revocations-are-absorbing-on-replicated-mesh-and-identity-tables.md) (revocation ledger) · ADR 1043 (echelon) · [ADR 1031](1031-credential-revocation-mesh-first-offboarding.md) · [`island-mode-multi-writer.md`](../design/island-mode-multi-writer.md) §3 · [`island-mode-write-inventory.md`](../design/island-mode-write-inventory.md) C41–C46, K1–K12

## TL;DR (for a layperson)

Every site of a goatnet keeps working when cut off. One site outranks the others: the
one at the highest command, the most secure and best-resourced location. Its changes win
over any other site's, and in an emergency it can cut a compromised site out of the
goatnet. No other site can do either. Otherwise, a person's record is changed only by
the site the person is registered at, so two sites never edit the same person at the same
time.

## The operator's decision

> "yes except there is a higher authority that supercedes others and that is the highest
> echelon node for an evnironment. the highest echelon would reside at the higher command
> at the most secure and well resourced location. and it's actions should take precedence
> over any other node including an emergency act to sever a site if it is compromised.
> only the higher echelon site would have those two asymetric overrides, other wise yes
> the priority should go to the site to which an identity is register and active last"
> (2026-10-29, accepting the F-590 fix with this amendment)

## Hubs, as declared

| Goatnet | Hub | `goatnet_hub_site` |
|---|---|---|
| zagros-dev (dev) | site alpha | `zagros_dev` |
| zagros-preprod (preprod) | site alpha | `zagros_preprod` |
| zagros (prod) | site alpha | `zagros_a` |

## Decision

**D1 — Each goatnet declares one hub, its highest-echelon site.** The declaration is
`goatnet_hub_site`, the Spock node name of that site, set once in the goatnet's
group_vars. ADR 1165 class 5 work (creates and deletes of global derived state after
heal) runs at this site. The `spock-hub-directives` role refuses to run on a site with
no `goatnet_hub_site`, so no site carries out directives until the operator has named
the hub.

**D2 — Hub directives are the hub's only channel for acting on another site.** A
directive is a row in `goat_hub_directives` in the `netbird` database (repset
`netbird_mgmt`, present at every Spock site): `id` uuid, `kind`, `subject`, `body` jsonb,
`at`, `actor`. A trigger refuses UPDATE and DELETE. Each site runs
`spock-hub-directives`, a 30-second systemd timer that applies directives it has not yet
applied and records each in a local table that is in no repset. The timer accepts a
directive only when its commit came from the hub: a local commit at the hub itself, or,
elsewhere, a commit whose replication origin (`pg_xact_commit_timestamp_origin(xmin)`) is
the hub's Spock `node_id`, which Spock's apply records on every commit it applies.
`goatnet_hub_site` is read from the site's own configuration, never from the database.
A directive written at any other site is refused at every site and raised as an alert,
so the two overrides stay with the hub even when another site's database is
compromised.

**D3 — Override 1: the hub's actions take precedence.** The hub may change any record.
When the record is owned by another site, the hub writes a directive, and the owner site
carries the change out with its own local write. While the owner is cut off, the
directive waits at the hub. After heal it applies after the owner's own cut-off edits
have arrived, so the hub's version is the last write and is the one both sites keep.
There is still only one writer per record, so the F-590 divergence cannot occur. A hub
revocation needs no directive: the hub writes to the ADR 1193 ledger like any site, and
a reinstatement from another site cancels only the revoke events it names. The hub
therefore undoes a hostile reinstatement by revoking again.

**D4 — Override 2: the hub can sever a site.** `sever_site` names a Spock node. On
applying it, every site disables its own subscriptions from that node in every Spock
database, so nothing the severed site writes afterwards reaches anyone. The hub's admin
then revokes the severed site's management-host mesh peers through the ADR 1193 ledger
(`goat_revoke('netbird.peer.deleted', …)` at the hub), so other sites' NetBird drops them
from every network map; the applier does not do this, because nothing declares which
peers belong to a site. Writes the severed site made before the directive arrived stay
applied, and are what `spock.exception_log` and the drift watch show from then on. A
severed site comes back only by being re-seeded from the hub (the existing
`*-seed-from-peer.yml` playbooks) and re-subscribed, followed by a `restore_site`
directive. Its queued changes are never replayed.

A directive whose commit origin a site cannot yet read (its commit timestamp is not
available) is left pending and counted in `goat_hub_directives_pending`; a site records
`refused_origin` only for a commit it read as coming from a node other than the hub.

**D5 — An identity's owner is the site it was registered at or last made active at.**
The owner site is held in the identity's `metadata_admin.owner_site`, written by the
creating site at registration. It moves only by a `transfer_owner` directive from the
hub, which covers re-registration and reactivation at another site. An identity with no
`owner_site` (every identity created before this ADR) is owned by the hub.

**D6 — Who writes a person's identity.**

| Act | Owner site | Hub, not owner | Any other site |
|---|---|---|---|
| Edit traits, metadata, credentials (C42, C43, C45, C46, K2, K12) | writes through local Kratos | directive, carried out by the owner | refused, naming the owner |
| Revoke: deactivate, delete, session revoke (C44, K4, K11) | ADR 1193 ledger | ADR 1193 ledger | ADR 1193 ledger |
| Reinstate | ledger, naming the revokes it cancels | ledger | ledger |
| Create (C41, K1, K9) | — | — | allowed; the creating site is the owner |

A non-owner revocation is written through the ledger functions, never as a Kratos
`PATCH /state`, because that `PATCH` rewrites the credential rows.

**D7 — Two sites creating the same email is a collision, resolved by the hub.** Spock
logs the second site's credential identifier as a `23505` in `spock.exception_log`. The
console lists it in the ADR 1165 D4 resolution queue. The hub's admin picks the identity
that survives. Two directives resolve it, each carried out by an owner site:
`identity_delete` for the losing identity, then `identity_resave` for the surviving one,
which re-issues its credential rows so they reach the site that discarded them.

## Alternatives considered

| Alternative | Why not |
|---|---|
| Every site equal, last write wins on identities (ADR 1165 as written) | Two writers to one identity never converge |
| The hub writes other sites' identities directly | Two writers again whenever the hub and the owner both act while cut off |
| A hub-precedence conflict handler inside Spock | Spock resolves per row by timestamp only; a custom handler is code on the replication apply path |
| Hub authority checked by a `site` column on the row | Any site can write any value into a column; only the replication origin shows where a commit came from |
| Signed directives | No signature verification in Postgres without an extension; the replication origin gives the same guarantee for free |
| Ownership moving on every login | Logins happen at every site, so ownership would change while cut off, with two sites each believing they are the owner |
