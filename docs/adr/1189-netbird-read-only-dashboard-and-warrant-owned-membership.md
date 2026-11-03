# ADR 1189 — The NetBird dashboard is read-only by default, and warrant owns peer membership

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-09-23 |
| Repo scope | goat core (`ops/warrant/`, goat-cli `warrant` + `admin members`, `ops/ansible` roles `netbird-admin-reconcile`, `warrant-membership`, `oauth2-proxy-dashboard`) |
| Supersedes | The live-write path of ADR 1088 and the hold records of ADR 1089 / ADR 1091, **on each scope warrant governs** |
| Superseded by | — |
| Related | ADR 1129 (warrant — extended here), ADR 1088 (transfer engine — now the converger's mechanism), ADR 1091 (quarantine), ADR 1182 (platform admin), ADR 1178 (external services), ADR 1109 (pulled convergence), ADR 1110 (consistency foundation); F-565, F-566; design [`mesh-change-control.md`](../design/mesh-change-control.md) |

## TL;DR (for a layperson)

Every platform and identity admin could change anyone's network access from the
NetBird web dashboard, and did, hundreds of times, with no review and no name
attached. From now on the dashboard is for looking. Three named platform
admins have a separate address where they can make emergency edits. Every
real change is written down in the repository first and applied by a
machine account, the same way the access rules already are.

## Context

Measured on a production goatnet over three months (NetBird event log, Kratos,
the dashboard's oauth2-proxy):

- The dashboard admits Ory `PLATFORM-ADMIN` and `IAM-ADMIN`.
  `netbird-admin-reconcile` makes every `PLATFORM-ADMIN` a NetBird `admin`, which
  can change anything.
- People made hundreds of peer-membership, policy and group edits by hand. The NetBird event log names none of them; only an account id is kept.
- Access rules were already warrant's (ADR 1129): git → `generate.py` → cutover →
  `reconcile.py`. Peer membership was not — warrant.md §9 names it as the change
  that "writes no file and so escapes every write-time gate". The only record was
  an exceptions file read by a daily report that nobody acted on, while drifted
  peers accumulated.
- A quarantine was entered with no reason and no hold record, then half-released
  in the dashboard into a state the hold-record rules call inconsistent
  (F-565).

## Decision

1. **Read-only by default.** `netbird-admin-reconcile` maps Ory groups to NetBird
   roles through an ordered `netbird_mgmt_role_map`. On a governed goatnet
   `PLATFORM-ADMIN` and `IAM-ADMIN` map to `auditor` (read everything, change
   nothing). The reconciler still only raises roles; lowering is an explicit,
   audited play.
2. **One write door, three holders.** A new Ory group `NETBIRD-ADMIN` maps to
   NetBird `admin` and is held by three named platform admins, changed only with
   `goat admin role`. They write from `admin-dashboard.<site>`,
   a second dashboard behind its own oauth2-proxy. The everyday
   `dashboard.<site>` talks to `mgmt-read.<site>`, which forwards only
   `GET`/`HEAD`/`OPTIONS`, so the three are read-only there too.
3. **Warrant owns membership.** `ops/warrant/membership/<scope>/` (see the amendment below) declares the
   exceptions to each peer's enrollment-key groups (`grant`/`absent`, `exact`,
   `hold: quarantine`) with an owner, reason and expiry. The file existing is the
   switch that puts a scope under warrant. `goat warrant apply --membership` runs on
   the management host every five minutes as `svc-<site>-warrant`, in `report` or
   `enforce` mode, and refuses a run that would change more than `--max-changes`.
   Warrant's existing approval rules apply unchanged: CI proves a narrowing, and
   one owner approves it; widening waits for review.
4. **Events keep their commands.** Enrollment keys, peer removal and roles stay
   with the onboarding pipeline, `offboard.sh`, `peer cut` and `platform-admin`.
   Any of them that changes membership on a governed scope writes a warrant entry
   instead of the live account.
5. **Quarantine is an entry; release reverts its commit.** The hold replaces the
   peer's entry, reverting restores it, and the desired set is recomputed from the
   declaration, so no prior-groups record is kept.

## Alternatives

- **Hostname split alone.** Rejected: NetBird authorizes by the token's role, so a
  second address changes nothing without the role map.
- **Role map alone.** Rejected: the three writers would then write from the
  everyday address; read-only would not be the default for them.
- **Alerting on dashboard edits.** Rejected as the primary control: it detects the
  change after the fact on a path that should not exist.
- **Put enrollment keys and peer removal in git.** Rejected: they are events; a pull
  request cannot hand a one-time key to a person.
- **Keep hold records for quarantine.** Rejected: a second store for what the
  declaration already says.

## Consequences

- **Positive:** every standing network change has a reviewed diff, an owner and
  a named author; hand edits revert within one converger pass; quarantine is one entry and release is one revert.
- **Negative:** a membership change takes a pull request, a deploy of the file
  (until the signed-intent increment) and up to five minutes;
  `IAM-ADMIN` holders gain read visibility of every peer; the converger is a new
  timer on each governed management host.
- **Rollout:** a development goatnet first (2026-09-23). Each production goatnet
  needs the operator's authorization, and starts in `converge: report`.
- **Not yet:** the declaration reaches the host from the role, not as a subject of
  the signed `IntentV1` from `get.<site>` (ADR 1110); that is the next increment.

## Amendment — 2026-09-25: a directory per goatnet, and defaults from the manifest

**Status:** Accepted.

- **One directory per goatnet, split by owner.** `ops/warrant/membership/<scope>/`
  holds `_scope.yaml` (the `converge:` switch), `_baseline.generated.yaml`, and
  fragments — `substrate.yaml`, `services.yaml`, `holds.yaml`, `people.yaml`,
  `orgs/<org>.yaml` — merged into one declaration. Each fragment has its own
  `CODEOWNERS` reviewer, each tool writes its own fragment, and an organisation can
  own its exceptions. Cross-file rules (one entry per device) are checked on the
  merged view. The single file per goatnet is retired.
- **A device's default groups come from its owner's operator manifest,** falling
  back to its enrollment key only for machines and devices not named after a
  person. Keys are deleted, sometimes minted with no groups, and do not follow a
  person's re-tier; the manifest does. The directory then holds only real
  exceptions.
- **Rejected: selector rules** ("every device of org X gets group Y"): they widen
  what one line can grant and defeat the per-device narrowing check. Revisit only
  if per-device entries still reach the thousands after the manifest baseline.
- Design: [`mesh-change-control.md`](../design/mesh-change-control.md) §4.1–§4.2.
