# ADR 1089 — Probation isolation lifecycle for dormant identities

| Field | Value |
|---|---|
| Status | Accepted; explainer and petition implemented in code, deployment pending |
| Date | 2026-07-17 |
| Related | [membership-lifecycle-and-transfer.md](../design/membership-lifecycle-and-transfer.md), [ADR 1088](1088-membership-transfer-engine.md), [ADR 1068](1068-anomaly-probation-lifecycle.md), [ADR 1031](1031-credential-revocation-mesh-first-offboarding.md), identity-reconcile (obs) |

## Context

Inactive identities retain full mesh reach: an account dormant for weeks can
still address every participant and every service. That is a standing lateral
surface, and it costs NetBird signalling to advertise addressing between peers
that will never use it. We want dormant identities parked in an explicit,
isolated, self-service-recoverable state — without deleting anyone or losing
their role (offboarding is a separate, deliberate act — ADR 1031). This is the
*mesh-reach* expression of the ADR 1068 hold/verify/repair-or-purge stance.

## Decision

A **probation** holding state, driven by an inactivity policy on top of the
membership transfer engine (ADR 1088):

1. **Dormancy signal**: `last_active = max(NetBird peer last_seen, Kratos last
   session)`. `last_active > threshold` (default 14 days, per-goatnet) ⇒ dormant.
   Detected by a cron (sibling of identity-reconcile), which surfaces candidates;
   the shed is the engine acting on that list.
2. **Shed (automatic)**: move the identity's peers from `ZAGROS-<role>` into
   `ZAGROS-PROBATION-<role>`. The `<role>` suffix is retained *only* to record what
   to restore to. The identity keeps its Ory role record. The mesh move is the
   enforcement and the v1 hold record is the server-trusted explanation and
   restore source; no self-asserted Ory trait participates in classification.
3. **Isolation posture**: a `PROBATION-*` peer reaches **no** peer and **no**
   application, save the minimum to be *told* and the identity endpoints needed
   to maintain its account: one rule `PROBATION-* → INFRA` on `:443` + DNS.
   Traefik ForwardAuth on mesh application routers sends browser
   document requests to goat-id-console. The console maps Traefik's
   connection-derived source IP to exactly one live NetBird peer, then requires
   exactly one same-site `dormancy` record whose peer id, hold group and live
   group state agree. Only that state returns the single **probation explainer
   page** for any hostname requested — a message explaining the state + a
   **petition** control. `trustForwardHeader` is false; forwarded chains,
   duplicate IPs, unreadable inputs, stale/releasing/orphan/inconsistent records
   and security records all receive the same generic denial, with no hold kind
   or petition disclosed. A classified held peer receives only the HTML
   explainer and the separately revalidated petition endpoint; asset, API and
   non-document requests are denied rather than forwarded to the original
   service. The peer registry is held as an immutable IP-indexed snapshot:
   requests do not scan or copy the fleet, and refresh happens asynchronously
   after 30 seconds. Ordinary allow and deny decisions keep using the
   last-known-good snapshot during a management-API outage. Showing the
   explainer or accepting a petition requires an IP-to-peer binding refreshed
   within 60 seconds; a would-be explainer synchronously refreshes the snapshot,
   failed refreshes retry after 5 seconds, and disclosure fails closed until a
   fresh binding is available. A newly enrolled address fails closed until the
   next snapshot completes.
   The local hold record is still re-read for every decision, so a newly written
   dormancy or security record fails closed even while that snapshot is stale.
   NetBird management, signal and Ory identity routers use the source allowlist
   directly and never depend on the console, preserving control-plane and
   identity-plane connectivity. This does not restore access to a requested
   application: application routers still run the probation gate. A held peer
   may reach Ory's login, session and recovery endpoints, but cannot use that
   identity session to cross the application gate or change its mesh hold.
4. **Petition → separate lane**: the petition posts into a dedicated
   reactivation queue in goat-id-console, triaged apart from the new-join
   onboarding lane. Its token is authenticated over the peer id and exact hold
   timestamp, the POST re-runs source-IP classification, and SQLite admits one
   row per `(peer_id, hold_entered_at)`. A repeated click is therefore harmless;
   a later hold is a new reviewable event.
5. **Reactivate (admin)**: any `IAM-ADMIN` or `PLATFORM-ADMIN` approves; the
   engine absorbs `ZAGROS-PROBATION-<role>` → `ZAGROS-<role>` and the peer is a
   full participant again. **Auto in, human out; never auto-delete.**

`PROBATION-<role>` groups are declared in the taxonomy (fleet-standard, created
once). The isolation rule set is part of the standard matrix.

## Alternatives

- **Just disable the Kratos identity** (existing capability): kills login but
  leaves the peer on the mesh (still addressable at L4) and gives the person no
  self-service path back. Rejected — doesn't shrink the mesh or explain itself.
- **Delete/offboard dormant peers**: destructive, non-reversible, loses the role;
  that is ADR 1031's deliberate path, not an inactivity default. Rejected.
- **A single flat `PROBATION` group** (no `<role>` suffix): simpler, but loses
  the exact-restore target. Rejected — the suffix is cheap and makes reactivation
  unambiguous.
- **Block at L7 only** (leave L4 reach): doesn't reduce signalling and still lets
  a dormant peer address others below the web layer. Rejected — the L4 shed is
  half the point.

## Consequences

Adds engineering: a dormancy cron, the `PROBATION-*` taxonomy + isolation rules,
the route-override middleware + explainer page, and the console reactivation
lane. In exchange: dormant identities become isolated and
self-recoverable, the participant mesh shrinks to the active set (less signalling),
and the whole thing is reversible and audited via the ADR 1088 engine. Risk: a
false-positive dormancy shed briefly isolates an active user — mitigated by the
generous threshold, the self-service petition (immediate path back), and any
admin being able to reactivate. Threshold + the "active" definition are
per-goatnet config, not hardcoded.

## Implementation status (2026-10-05)

The route override, self-contained light/dark explainer, hold-bound idempotent
petition intake, and read-only `/reactivation-requests` admin queue are built in
goat-id-console and wired conditionally into the Traefik ACME path behind the
separate, default-off `probation_explainer_enabled` switch (#1038). NetBird
supplies only peer id, mesh IP, last-seen and groups to an asynchronously
refreshed, IP-indexed classifier snapshot; the
hold-record reader keeps a site-and-peer index and checks the file identity,
size and modification time on every request, rebuilding the index synchronously
when an append or atomic replacement changes it. The explainer shows the hold
start date and age from the record's `entered_at`, never the live last-seen of
the member loading the page. A
refresh failure retains the last-known-good peer binding for ordinary allow and
deny decisions rather than coupling all mesh HTTPS availability to the
management API. After 60 seconds, only a would-be explainer or petition forces a
synchronous refresh and fails closed until a successful refresh establishes the
current IP-to-peer binding. During a management outage, a stale binding can
still pass a reassigned address only if a peer is deleted, a new peer is enrolled
at the same address, and a hold changes through the unavailable management API;
this exposure lasts only for the outage.
An empty peer list fails readiness and never replaces a last-known-good
snapshot.
The queue deliberately has no release control or mesh write capability. The
guarded release action remains #1044 and is not a prerequisite for deploying
this intake path. This increment has no close path; a hold can also end through
the sweep's release or revoke, so the queue may list requests whose hold has
ended. The admin confirms the current state through the row's **View mesh hold**
link; closing those rows belongs to #1044.

This increment consumes legacy v1 hold records only. The Traefik play refuses
to enable it where the active warrant membership declaration says
`converge: enforce`; ADR 1189 makes warrant entries authoritative there. A
`report` declaration is still ungoverned and may use the legacy record. Deploy
the compatible console and converge its record mount and NetBird read token
before enabling the switch. The Traefik preflight calls the running console's
probation readiness endpoint, which uses that process's actual URL, token and
record path to verify both dependencies before the gate is installed. Roll back
by disabling the switch and applying Traefik first, then roll back the console
image. Later console converges repeat the same readiness check after container
recreation while the gate is enabled.
