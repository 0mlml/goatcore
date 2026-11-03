# ADR 1111 — Cutover-gated service class for standby nodes

| Field | Value |
|---|---|
| Status | Proposed (2026-08-04, design) |
| Date | 2026-08-04 |
| Deciders | maintainers |
| Repo scope | goat core (every goatnet with a standby node) |
| Supersedes / amends | **Generalizes [ADR 1084](1084-block-64r-mgmt-repset-and-two-node-federation.md) D3** ("single-active mutation surfaces") from an identity-plane special case into a named, enforced class. **Concretizes** the two-node design's §8.5 P3/P4 sketch ("dormant serving surfaces" / "dormant single-active engines"). Consumes [ADR 1060](1060-universal-deploy-rollback-ledger.md) posture. |
| Related | [`standby-cutover-gated-services.md`](../design/standby-cutover-gated-services.md) (the design) · `ops/standby/service-classes.yaml` (the SoT) · [`node-topologies-and-replication-mechanisms.md`](../design/node-topologies-and-replication-mechanisms.md) (#2) · `ha-multi-site.md` |

## TL;DR (for a layperson)

A backup node is only a *safe* backup if the things that would collide with the
live node stay switched off until the moment we cut over. This decision names
that set of "off-until-cutover" services, writes down a one-line test for whether
a service belongs to it, and adds a CI guard that stops anyone from quietly
turning one on on a backup. The plain-data list is what the promotion procedure
reads, so nothing gets forgotten at the switch.

## Context

Building out a dormant cross-site full standby (topology #2) surfaced a real,
unnamed distinction. Some services are **standby-safe** — they
run continuously on the backup with zero risk: the listen-only Zenoh router, the
environment-health self-publishers (each writes only its OWN router under a
node-scoped key), and the passive replication receivers (Spock, MinIO async).
Others are **not**: relay-health, for example, POSTs a Hydra OIDC client into the
Spock-replicated identity plane, serves an authenticated route, and holds the
single relay-fleet "vantage" — running it on the backup would corrupt the shared
plane, route clients to a node that must serve none, and double-probe.

Today this line is drawn only by hand: a prose "standby DOES NOT RUN" comment,
scattered `*_enabled: false` flags, and `absent` pin deviations. Nothing defines
the criterion, nothing enforces it, and the promotion procedure that must start
these services at cutover is unwritten — so the set can drift and a cutover can
forget a member. ADR 1084 D3 already carved out the identity-plane subset; this
is the same idea, generalized and made first-class.

## Decision

**D1 — A node declares itself a dormant standby** with `node_standby: true` in
its group_vars. On such a node, the cutover-gated class applies.

**D2 — The cutover-gated criterion (one test, four triggers).** A service is
**cutover-gated** — it must NOT run active on a `node_standby` and is switched on
only at promotion — if it trips any of:
1. **shared-write** — writes a Spock-replicated plane (identity via Hydra/Kratos
   `ory_narrow`; account/policy via the netbird DB) or DNS (isitup/CoreDNS zone).
2. **route** — serves clients on a route/DNS/ingress the fleet points at.
3. **side-effect** — fires outbound effects (email/SES, onboarding, cert mint,
   notifications).
4. **singleton** — holds a one-per-goatnet "vantage"/rollup/ingest role.
Everything else is **standby-safe** and runs continuously. The class is per-MODE
where a service splits (node_net_health *publisher* is safe, its *rollup* is
gated; MinIO *storage* is safe, its *console* is gated).

**D3 — The labels live as data, in one SoT:** `ops/standby/service-classes.yaml`
lists every service's class, its `signal` (the group_vars key whose truthy value
means "active", or `pin:<component>` when a fleet-pins `absent` deviation already
governs it), its triggers, its reason, and its dormancy sub-mode (`absent` vs
`deployed-dormant`).

**D4 — A CI guard enforces it fail-closed:**
`ops/ci/check-standby-cutover-gated.sh` fails if any boolean-`signal` cutover-
gated service is set active in a `node_standby` scope. (Image-pin-governed ones
ride the existing fleet-pins `absent` enforcement.)

**D5 — Promotion is the single activation point.** The promotion runbook /
`promote-<node>.sh` DERIVES its "start these" list from `cutover_gated` in the
manifest, so the switch-on set cannot silently diverge from the guard's off set.
Start-on-exactly-one-node is enforced by the promotion split-brain gate
(ha-multi-site.md), not by any per-service framework.

## Consequences

- The first standby build-out is retroactively validated: P1 router + P2 env-health
  publishers are exactly the standby-safe set; relay-health is correctly gated.
- Adding a new service to a standby forces a conscious class decision (the guard
  fails otherwise) — the drift that made this implicit is now caught.
- The promotion runbook has an authoritative, non-forgettable start-list.
- Cost: one manifest + one guard to maintain; the manifest is the same shape as
  `parity-capabilities.yaml`, so the pattern is familiar.

## Alternatives considered

- **Leave it implicit (status quo)** — rejected: no criterion, no enforcement, a
  new service can be turned on active on a standby (the mistake this build nearly
  made), and the cutover set can drift from reality.
- **A heavy `standby_mode` framework in every role** — rejected: over-built. The
  hazards are real only at the *moment of promotion* (start-on-exactly-one-node);
  a dormant node fires nothing. A label + a guard + a derived promotion list is
  sufficient (this is the two-node design's §8.5 "no heavy framework" finding, now
  formalized).
