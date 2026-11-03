# Orphan reaper — batch enumeration + classification for half-created accounts

**Status:** Built + live-fired on ZAGROS (2026-07-30). Closes backlog item 1 of the
user-management/deprovisioning handoff (PR #1911): the *cut* was already a solved
one-command operation ([ADR 1031](../adr/1031-credential-revocation-mesh-first-offboarding.md));
what was missing was authoritative **enumeration** and repair-vs-purge **classification**.
**Tool:** [`ops/scripts/reap-orphans.py`](../../ops/scripts/reap-orphans.py) (+ `test_reap_orphans.py`).
**Related:** F-266 ·
credential-revocation-and-offboarding.md ·
[anomaly-probation-lifecycle.md](anomaly-probation-lifecycle.md) (ADR 1068) ·
[onboard-rotate-revoke-streamlining.md](onboard-rotate-revoke-streamlining.md)

## BLUF

A native-onboarded account spans five systems with **no shared key** — the only
row carrying *both* the handle and the email plus setup-key/bundle back-references
is the id-console `onboard_proposals` row. The reaper joins that row against the
portal slot registry, live Kratos, and NetBird, and buckets everything it finds.
It is **read-only and never cuts** (the same posture as `goat access strays`):
the cut stays the audited `goat admin onboard <handle> --revoke [--purge]`, and
the reaper prints those exact commands per finding.

## Buckets

| Bucket | Meaning | Disposition |
|---|---|---|
| `ORPHAN` | slot assigned + unrevoked, proposal exists, **no Kratos identity** (the F-266 crash-orphan shape) — or a `failed` proposal with live NetBird artifacts | test-domain email → **purge**; real email → **repair** (revoke → re-onboard, the proven F-266 recovery) |
| `INERT RESIDUE` | `failed` proposal, zero live artifacts on any plane | none — the row is audit trail, leave it |
| `TEST IDENTITIES` | active Kratos identities on test domains (`drill-*@zagros.test`, …) | operator decision; may be deliberate RBAC-drill fixtures — never auto-classified as cut |
| `UNDECLARED` | live, legitimate identity whose `email_hash` is absent from the declared roster | **not an orphan** — fix is the declared-roster sync (`sync-onboarded-manifests.py` + `gen-roster.py` regen from the full manifest store) |
| `PRE-CONSOLE SLOTS` | slots with no proposal row at all | out of the reaper's lane — triage via `goat access strays` + the probation list (ADR 1068) |

Separating `UNDECLARED` from `ORPHAN` is the load-bearing distinction: a stale
declared roster makes healthy accounts *look* stray, and the two
wrong answers — cutting a healthy partner vs leaving a real orphan — are both bad.
The orphan test is **Kratos-presence keyed by email from the proposal row**, never
a handle/email fuzzy join (a fuzzy diff over the same live data produced
candidates that were almost all false).

## Usage

Run on the mgmt host (all stores local):

```bash
export ZAGROS_NB_API_TOKEN=…   # NetBird admin token (live-artifact check)
sudo -E python3 ops/scripts/reap-orphans.py \
    --onboard-db /var/lib/goat-id-console/onboard.db \
    --registry-db /opt/goat-zenoh-portal/registry.db \
    --kratos-admin-url http://<kratos-container-ip>:4434 \
    --nb-url https://netbird.<goatnet-domain> \
    --roster docs/operators/zagros/roster.json --env zagros [--json] [--cohort purge.txt]
```

`--cohort FILE` writes the purge handles for `goat admin onboard --revoke --purge
--from FILE`. Degraded sources (Kratos/NetBird/roster unavailable) drop their
buckets **loudly** (`!! DEGRADED` header), never silently — F-177 discipline.

## Live-fire gotchas baked into the tool

- **Kratos legacy pagination is a footgun**: `?page=1` returned an *empty* list
  on ZAGROS's Kratos, which silently classified every healthy slot-holder as an
  orphan. The tool now makes one un-paged call at the size cap and hard-fails at
  the cap rather than silently under-covering.
- **Peer-name matching must mirror `revoke_peer_matches` anchoring** (`peer-<handle>-<env>-…`
  / `peer-<handle>-<tag>-<env>-…`): a loose `peer-<handle>-` prefix falsely
  attributed `peer-<handle>-<surname>-…` to the shorter handle `<handle>`.
- `slots.enrolled_at` is never populated — it is **not** an orphan signal; Kratos
  presence is.

## Follow-ups (unchanged from the handoff backlog)

- Portal idempotent-reclaim on 409 (the crash-between-mint-and-rollback residual;
  note the revoke→re-onboard cycle re-issues the *same* freed slot, proven live).
- `revoke --uuid` subset selector (ADR 1031, designed-not-built).
- Issuance registry as the revoke SoT (ADR 1031 Stage 2) — retires the stitched
  enumeration this tool performs.
