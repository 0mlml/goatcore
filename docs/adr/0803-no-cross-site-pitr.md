# ADR 0803 — Cross-site site-loss durability via Spock; no cross-site PITR

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0801-postgresql-spock-multi-master.md`](0801-postgresql-spock-multi-master.md), [`0802-pgbackrest-paired-stanzas.md`](0802-pgbackrest-paired-stanzas.md) |

## TL;DR (for a layperson)

If a whole site is lost, the data still exists at every other
site (Spock replication keeps every site in sync). What we
*don't* offer is cross-site point-in-time recovery — the
ability to roll the entire fleet back to a specific moment in
time. Each site has its own local PITR within a 7-day window;
that's enough for almost every recovery scenario, and the
operational cost of cross-site PITR would be very high.

## Context

Two failure scenarios for the management-plane database:

1. **Single-site loss.** A site's hardware is destroyed or
   compromised. Recovery: rebuild the site, sync from the
   other Spock peers.
2. **Logical corruption needing rollback.** A bad change is
   made and replicates everywhere; we want to roll *all*
   sites back to before the change.

Scenario 1 is handled by Spock + bootstrap-bundle
([`../design/bootstrap-bundle.md`](../design/bootstrap-bundle.md))
with no cross-site PITR needed.

Scenario 2 *would* require cross-site PITR — orchestrated
rollback across every site to the same point in time. The
operational cost (a synchronized backup discipline across
sites, partitioned-recovery procedures, conflict re-resolution
on rejoin) is high; the probability of needing it is low.

## Decision

goat offers:

- **Site-local PITR**: 7-day window per site, paired pgBackRest
  stanzas ([`0802`](0802-pgbackrest-paired-stanzas.md)).
- **Site-loss durability**: Spock multi-master replication
  ([`0801`](0801-postgresql-spock-multi-master.md)) — data
  exists at every site.
- **Total-loss recovery**: bootstrap bundle with cloud-held
  stateless artifacts (IaC + container images + offline-CA
  public cert) — see
  [`../design/bootstrap-bundle.md`](../design/bootstrap-bundle.md).

goat does *not* offer:

- **Cross-site PITR**: orchestrated rollback across all sites
  to the same point in time. This is an explicit, accepted
  trade-off.

If a logical-corruption rollback is required across the
fleet, the procedure is:

1. Stop replication.
2. Per-site rollback to the desired PITR point (using local
   stanza).
3. Re-establish Spock peering with conflict re-resolution.
4. Validate state.

This is operationally heavy and not automated. The risk is
accepted because the use case is rare and the alternative
(cross-site coordinated PITR) is much heavier always-on.

## Alternatives considered

- **Cross-site PITR via shared backup repo.** Violates
  sovereignty / Island Mode. Rejected.
- **Cross-site PITR via coordinated per-site stanzas.**
  Possible but operationally heavy; we accept the
  manual-rollback procedure instead.
- **No PITR at all.** Loses site-local recovery. Rejected.

## Consequences

**Positive.** Backup design is operationally simple. Site
loss is absorbed without manual intervention. Sovereign
per-site backups.

**Negative.** Cross-site logical-corruption rollback is a
manual procedure. We accept; the procedure is documented
([`0818`](0818-incident-response-runbook.md)).

## Assessor view (SCA-R)

The trade-off is documented for CP-9 (backup) / CP-10
(recovery) review. Site-loss durability is met by Spock;
site-local PITR is met by paired stanzas; total-loss
recovery is met by bootstrap bundle. The accepted gap
(cross-site PITR) is named explicitly with the manual
procedure as the compensating control.

NIST SP 800-34 contingency-planning guidance is consistent
with the trade-off as documented.

## Trigger conditions for revisit

- A specific authorization requires cross-site PITR.
  Re-evaluate.
- A logical-corruption incident demonstrates the manual
  procedure is too slow. Investigate automation; do not
  silently downgrade Island Mode.

---

*See `GLOSSARY.md` for terms and acronyms.*
