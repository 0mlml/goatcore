# keepalived-mgmt — ⚠️ OBSOLETE / RESERVED, do not deploy

**Status:** This role is preserved as a forensic
reference but is **NOT** called by any orchestrator playbook and
**MUST NOT** be deployed. The whole VRRP-on-Orins layer was removed
per F-062 architectural reframing.

**The canonical externally-mediated failover path** is VRRP on the
LAN's L3 switch (per `docs/design/canonical-lan.md`
+ `docs/design/ha-failover.md`), not VRRP on the
mgmt hosts. Configure VRRP at the switch tier; do not restore this role.

**On a LAN without such a switch** failover is **operator-fired** per the
site's Postgres failover runbook.

**Why this role exists in the tree:** 22L originally bundled
keepalived-on-Orins as the lab-fallback failover path. F-055 +
F-062 surfaced its sharp edges (split-brain on every deploy /
recovery / pause-resume). The role was retained for forensic
auditability — see the findings + the original README content
below — but operationally it should never run.

If you have a real reason to use VRRP-on-Orins (e.g., explicitly
testing the lab-fallback path against a plain LAN, with
operator-fired postgres promote and FULL awareness of F-055/F-062),
deploy with `keepalived_auto_promote_postgres: false` (default) and
expect deploy-time VRRP races to be possible. Test in an ephemeral
env first.

---

## (Original content preserved for forensic value)

Per-site VRRP active-passive failover between mgmt-host Router-A
(MASTER) and Router-B (BACKUP). Owns a single LAN-internal VIP that
the mgmt-API endpoint resolves to from inside the site.

## Canonical posture (post-F-062)

This role is **VRRP-VIP-only-plus-operator-fired-postgres-promote**
by default. VRRP fails over the VIP in ~3-5s; postgres failover (the
`pg_ctl promote` on the standby's postgres-a) is operator-fired, not
an automatic notify_master script.

**Why this is the canonical default:**

VRRP was designed for stateless VIP failover. Bolting `pg_ctl promote`
onto VRRP's `notify_master` races on every deploy / recovery / pause-
and-resume window because VRRP elects on broadcast-timeout, not on
actual primary health. See F-062
for the full surfaced-incident + reframing.

Canonical postgres-HA stacks:

- **Patroni / Stolon / repmgr** — consensus via etcd / consul / zookeeper.
  Use these when seconds-RTO postgres failover is genuinely required.
- **VRRP-VIP-only-plus-operator-promote** (this default) — VIP fails
  over fast; postgres requires operator action.

The knob `keepalived_auto_promote_postgres` is preserved (operator can
flip true) but the default is the canonical-safe path.

**RTO trade:**

- VIP failover: ~3-5 seconds (VRRP master_down_interval).
- Postgres failover: minutes to hours (operator response time +
  sanity checks + `pg_ctl promote -D /var/lib/pgsql/16/data`).
- **Zero false-promote risk.**

**Patroni upgrade path:** if a future site needs sub-second postgres-
RTO that operator-fired promote can't meet, replace this role with a
Patroni deploy. Don't add sanity-check scripts on top of
`notify_master` — those are patches on a non-canonical pattern.

## What it does

- Installs `keepalived` from apt.
- Installs four scripts under `/usr/local/bin/`:
  - `keepalived-mgmt-check.sh` — health check (postgres-a + traefik
    running + postgres healthy). Drives the failover decision.
  - `keepalived-mgmt-notify-master.sh` — fires when the local node
    BECOMES master. Calls `pg_ctl promote` inside postgres-a if it
    is in recovery (auto-promote behavior, controllable via
    `keepalived_auto_promote_postgres`).
  - `keepalived-mgmt-notify-backup.sh` — fires when local node yields
    the VIP. v1: log only.
  - `keepalived-mgmt-notify-fault.sh` — fires when health-check fails.
    v1: log only.
- Renders `/etc/keepalived/keepalived.conf` with per-host priority
  (A=110, B=100), the shared `auth_pass`, and the configured VIP.
- Enables + starts the service.
- On the MASTER, asserts the VIP is bound on the LAN interface within
  30s.

## Failover behavior (designed)

- **Steady state:** A holds the VIP, B is backup. postgres-a on A is
  primary, postgres-a on B is standby (replicated via the
  `postgres-streaming-replication` role).
- **A failure:** B's keepalived stops receiving VRRP advertisements;
  after `advert_int * fall = 2s`, B promotes itself. notify_master on
  B runs `pg_ctl promote` so postgres-a on B becomes a writable
  primary. management-a on B (which was running read-only against the
  standby) reconnects with write capability.
- **A recovery:** A boots, sees B holding the VIP, becomes BACKUP. To
  swing the VIP back to A, operator action: re-basebackup A from B
  (now the new primary), then either reduce B's priority or `systemctl
  stop keepalived` on B briefly. Auto-failback is intentionally NOT
  enabled to prevent split-brain.

## Required vars

| Var | Where | Example |
|---|---|---|
| `keepalived_vip` | per-site `group_vars/<site>_hosts/vars.yml` | `192.168.1.10` |
| `keepalived_lan_interface` | per-site `group_vars/<site>_hosts/vars.yml` | `eno1` |
| `keepalived_router_id` | per-site `group_vars/<site>_hosts/vars.yml` | `51` (must be unique 1-255 per LAN; collide → broken VRRP) |
| `router_role` | per-host inventory var | `A` or `B` |
| `keepalived_auth_pass` | env (`-e` extra-var) | from `<SITE>_KEEPALIVED_AUTH_PASS` in `.envrc.local` |

## Optional vars (with defaults)

| Var | Default |
|---|---|
| `keepalived_priority_a` | `110` |
| `keepalived_priority_b` | `100` |
| `keepalived_advert_int` | `1` (s) |
| `keepalived_health_check_interval` | `5` (s) |
| `keepalived_health_check_fall` | `2` (consecutive failures → demote) |
| `keepalived_auto_promote_postgres` | `true` |

## Why the health check intentionally omits `management-a`

`management-a` crash-loops on the standby until that node is promoted.
Including it in the health check would make the standby look
"unhealthy" and cause priority storms (master alternates rapidly when
neither is healthy). The check is scoped to "is postgres-a writable
and can traefik front it?" — that's the actual user-visible mgmt-API
SLO.

## Idempotency

Re-runs against an already-deployed pair show no change. The notify
script template is rendered with the auto_promote knob; changing the
knob and re-running updates the script in place.

## F-055 lone-node split-brain guard

**The role refuses to deploy to a single node by default.** Lone-node
deploy is the documented split-brain failure mode: a single keepalived
instance with no peer advertising VRRP self-elects MASTER within
~3-4s and runs `pg_ctl promote`, producing two primaries when the
other node eventually comes online. See F-055
for the surfaced incident + engineering response.

The first task of the role is an `assert` that BOTH `router_role=A`
and `router_role=B` are present in the play's host list. The role
fails with a multi-line diagnostic message before any `apt install`
or config render, so a partial-deploy attempt never changes system
state.

**Override (only when reinstating a single recovered node into an
established pair where the other already has keepalived running):**
`-e keepalived_allow_lone_node=true`. Never set this in CI or
orchestrator paths.

`nopreempt` is also configured by default — once a node is BACKUP, it
does not preempt the current MASTER even if its priority is higher.
Combined with the no-auto-failback stance, this prevents flap-and-
preempt loops when a recovered A returns to a B-as-MASTER pair.

## Recovery from split-brain

If you find yourself with two primaries (most likely from a pre-F-055-
guard deploy or from `keepalived_allow_lone_node=true` plus
operator-error), the recovery is codified in
`ops/ansible/site-split-brain-recovery.yml`.
That playbook stops the loser-side, re-basebackups it from the
canonical primary using the `postgres-streaming-replication` role with
`force_rebuild=true`, and re-deploys keepalived to both nodes. Idempotent;
safe to re-run if interrupted.

## Cross-refs

- `docs/design/ha-failover.md` — design context.
- [`ops/ansible/roles/postgres-streaming-replication/`](../postgres-streaming-replication/) — companion role that wires the WAL stream this VRRP layer fails over.
