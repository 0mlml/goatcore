# ADR 1001 — Bootstrap (SSM) vs refresh (SSH-over-mesh) playbook pattern

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`1002-two-phase-enrollment.md`](1002-two-phase-enrollment.md) |

## TL;DR (for a layperson)

Every new management-host environment ships with two
deployment playbooks: a bootstrap that connects via AWS SSM
(used once when the host isn't on the mesh yet) and a refresh
that connects via SSH over the mesh (used routinely after the
host is enrolled). The bootstrap is slow but doesn't require
mesh membership; the refresh is fast and runs the drift-prone
phases only.

## Diagram



Source: [`diagrams/1001-bootstrap-vs-refresh.mmd`](diagrams/1001-bootstrap-vs-refresh.mmd).

## Context

A new mgmt-host can't be reached over the mesh until it is
enrolled into the mesh. But enrollment requires deploying
NetBird, which requires deployment automation. The chicken-
and-egg is solved with a two-phase pattern.

The pattern is durable enough to be the default for every
new management-host environment.

## Decision

Every new mgmt-host env ships with both playbooks:

| Playbook | Connection | Frequency | Phases |
|---|---|---|---|
| **`<env>-bootstrap.yml`** | AWS SSM (no mesh yet) | Once per env | All phases — NTP, mgmt-host compose, account seed, observability, pgBackRest, admit-audit, Zenoh, smoke. ~30-40 min. |
| **`<env>-refresh.yml`** | SSH over Netbird mesh IP | Routine | Drift-prone phases only — observability configs, admit-audit, Zenoh. Skips one-time installs. ~3-5 min. |

Inventory split:

- `inventory/mgmt-hosts.ini` — SSM connection, instance-ID
  hosts. Used by bootstrap.
- `inventory/mgmt-hosts-mesh.ini` — SSH connection override,
  mesh-IP hosts. Loaded AFTER the first file so its
  `ansible_host` + `ansible_connection` win. Used by
  refresh.

Promotion path after first bootstrap:

1. Enroll each host: `sudo netbird up --management-url
   https://<env>.legacy.example --setup-key $KEY`
2. Grab each host's mesh IP from `netbird status` or the
   mgmt DB.
3. Fill real IPs into `inventory/mgmt-hosts-mesh.ini`.
4. From then on, use the refresh playbook for routine
   updates; only fall back to bootstrap for full rebuilds
   or one-time-phase changes.

This is documented in CLAUDE.md as the default pattern.
Existing refresh playbooks: `integdev-refresh.yml`. Create
`<env>-refresh.yml` when routine drift starts accumulating.

## Alternatives considered

- **Bootstrap-only.** Slow on every routine deploy.
  Rejected.
- **Refresh-only.** Doesn't work for first deploy.
  Rejected.
- **Single playbook with branches.** Complicates the
  Ansible logic. Rejected; two playbooks is clearer.

## Consequences

**Positive.** First deploy works without mesh dependency.
Routine deploys are fast. Pattern is reusable for every new
env.

**Negative.** Two playbooks per env. Acceptable.

## Assessor view (SCA-R)

The pattern serves CM-3 (change control — both deployment
paths produce auditable Ansible runs), CM-2 (baseline
configuration — same baseline applied via either path), and
SC-7 (boundary protection — SSM vs mesh-IP connection paths
have distinct security postures, both reviewable).

## Trigger conditions for revisit

- A specific env class doesn't fit the two-playbook pattern.
  Document the exception.

---

*See `GLOSSARY.md` for terms and acronyms.*
