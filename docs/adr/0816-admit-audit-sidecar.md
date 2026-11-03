# ADR 0816 — Admit-audit sidecar — Postgres-polling peer-admit detector

| Field | Value |
|---|---|
| Status | Accepted (corrected 2026-04-25 after audit found the original Decision section described a different architecture than the one actually shipped) |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0307-setup-key-rotation.md`](0307-setup-key-rotation.md), [`0811-audit-log-integrity.md`](0811-audit-log-integrity.md), [`../design/mgmt-plane-threat-mitigations.md`](../design/mgmt-plane-threat-mitigations.md) |

## TL;DR (for a layperson)

`admit-audit` is a small read-only sidecar that watches our local
NetBird management database for newly-admitted peers and emits a
log line + Prometheus metrics for each one. Its specific purpose
is to surface coincident-infiltration during partition: an
attacker who compromises a site and severs its uplink can admit
themselves silently during the partition; the new peer row
replicates cluster-wide on reconnect. The sidecar makes that
attack signature visible.

## Context

The threat we're hedging against (per
[`../design/mgmt-plane-threat-mitigations.md`](../design/mgmt-plane-threat-mitigations.md)):
an adversary with site compromise + uplink severance can self-admit
during the partition window. The peer row replicates via Spock on
reconnect. Without a detector, the only signature is "a cluster of
new peer rows from a single source-user / fresh WG keys appearing
within the partition window."

NetBird's management process produces internal admission logs, but
they share fate with the management process. An independent
detector that watches the database directly produces a separate
signal that can be cross-referenced.

## Decision

The actual implementation, per [`ops/admit-audit/main.go`](../../ops/admit-audit/main.go):

- **Pattern**: read-only Postgres polling sidecar. Polls the
  NetBird management database (`peers` table) every 30s for new
  rows. NOT an HTTP reverse proxy.
- **Database role**: dedicated `admit_audit` role with `SELECT`-
  only on the `peers` table. Created by Ansible role
  `admit-audit` before the sidecar runs.
- **Records**: per detected admit — `source_user`, `wg_key`,
  ip address, `meta_hostname` if present, detection timestamp.
- **State**: in-memory only. Process restart re-emits "detection"
  for any peer row already in the table within the retention
  window — this is the same failure mode as any tail-based audit
  and is acceptable because admit-audit is a *signal generator*,
  not a tamper-proof ledger.
- **Output**:
  - One JSON log line per detected admit, shipped via the
    container's stdout / Docker log driver.
  - Prometheus metrics on `:9124` with a rolling-window view of
    admit density, per-source-user burst, and distinct-key counts.
- **Partner control**: `ops/compose/mgmt-host/monitoring/rules/admit-ioc.yml`
  consumes the metrics and fires IOC alert rules (per-site Prometheus → Alertmanager).
- **Deployment**: Ansible role `ops/ansible/roles/admit-audit/`.
- **Schema gap acknowledgement**: NetBird's `peers` table has
  `peer_status_last_seen` (per-heartbeat) but **no row-insert
  timestamp**. Admit-audit uses its own detection time as a
  best-effort proxy. Cold-start undercounts real admit→detection
  delay.

This is C1 + C3 of the mgmt-plane-threat-mitigations design (admit
audit log + IOC alert rules).

## Alternatives considered

- **HTTP reverse proxy in front of mgmt endpoint.** Originally
  drafted in this ADR. Would capture more (denials, ACL evaluation
  outcomes, raw API calls) but requires standing in the data path,
  which raises a different set of operational risks (latency,
  failure isolation, certificate handling). The polling sidecar
  was shipped instead because it's strictly read-only and stays
  out of the request path. **See "Potential consideration" below.**
- **NetBird internal logs only.** Co-located with the process;
  shares fate. Rejected as the only mechanism.
- **Manual log shipment.** Loses real-time signal. Rejected.
- **Audit at the relay.** Misses LAN-only enrollments. Rejected.

## Consequences

**Positive.** Sidecar is read-only, stays out of the request
path, and produces a signal independent of the management
process's own logs. Postgres-role separation (`admit_audit`
SELECT-only) means a sidecar compromise cannot tamper with peer
state.

**Negative.** Polling-based detection means a 30s detection
window. Schema gap (no `created_at` on `peers`) means cold-start
detection latency is undercounted. Detector misses admission
*denials* and *ACL-evaluation events* — only sees rows that made
it into the table.

## Assessor view (SCA-R)

Admit-audit serves AC-2 (account management — peer enrollment
is account creation), AU-2 (audit), AU-12 (audit generation), and
AU-9 (protection of audit information — paired with the integrity
tier per [`0811`](0811-audit-log-integrity.md)). The Postgres
role separation (SELECT-only) is reviewable evidence of CM-7
(least functionality).

## Potential consideration (forward-looking)

The original draft of this ADR described admit-audit as an HTTP
reverse proxy in front of the management endpoint, recording
setup-key consumption + peer registration outcomes + ACL-evaluation
outcomes. That description was wrong — admit-audit ships as a
polling sidecar and the proxy was never built. **But the broader
audit surface that the proxy would provide is genuinely valuable:**

- **Denial events** — failed enrollments today produce no audit
  trail at all. A proxy or NetBird-side hook would surface them.
- **ACL evaluation outcomes** — admit-audit only sees what got
  admitted, not what got rejected by ACL.
- **Setup-key consumption events** — currently inferred from
  the `peers` row, not directly observed.

A future ADR could either (a) extend admit-audit's scope by
adding a NetBird upstream patch for an audit-event hook, or (b)
add a complementary HTTP-layer auditor. Tracked as a forward
consideration; not a blocker for the current sidecar's role.

## Trigger conditions for revisit

The polling-sidecar-only posture is the *current* answer. The
broader-audit-surface decision (HTTP-proxy or NetBird-side hook
to capture denials + ACL-evaluation + direct setup-key
consumption — see Potential consideration above) is **deferred,
not declined.** It re-opens when any of the following fire:

- **NetBird upstream gains a structured admission-event hook.**
  Adopt it; the broader surface becomes essentially free, and
  the polling pattern can be retired or kept as a complementary
  signal.
- **An authorization assessor flags the audit-coverage gap as
  a finding.** Decide based on the assessor's specific scope —
  may be partial (denial events only) rather than full proxy.
- **An incident reveals a denial-event pattern that would have
  been caught by HTTP-proxy logging.** Use the incident's
  evidence to scope what the broader surface needs to cover;
  don't over-build past what the incident showed.
- **Phase 3 production rollout to forward sites.** Re-evaluate
  against the operational audit needs that emerge in actual
  deployed environments — the threat model may shift from
  C1+C3 (coincident-infiltration during partition) to a
  broader posture per site.
- **Detection latency (30s) proves too long for a specific
  threat scenario.** Lower the poll interval, implement
  LISTEN/NOTIFY (requires NetBird-side change), or move to
  the proxy approach.

Each trigger fires *re-evaluation*, not automatic adoption of
the proxy approach. The cost of the broader surface (in-band
position, latency budget, certificate handling, restart-cascade
concerns, "read-only sidecar" → "in-band proxy" risk-class
shift) is real and must be weighed against the value the
specific trigger surfaced.

---

*See `GLOSSARY.md` for terms and acronyms.*
