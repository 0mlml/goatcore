# Setup-key rotation plan

Setup keys are the primary enrollment credential for a Netbird peer. A leaked
key is an open door to the mesh for however long it remains unrevoked, with
attacker-chosen peer names and WireGuard keys — the blast radius equals
every peer the key can enroll before revocation. This plan keeps the blast
radius bounded and auditable without demanding constant operator attention.

## Properties we want

1. **Bounded TTL** — every key has an `expires_at`, no forever keys.
2. **Role-scoped** — keys declare their role at creation; TTL + usage-limit
   defaults come from the role.
3. **Auditable** — every `create`, `use`, `revoke`, and `auto-expire` is
   a log entry with who, when, where, why, and outcome.
4. **Auto-revocation** — expired keys stop working on their own, even if
   no operator touches them.
5. **Emergency kill switch** — a single SQL statement revokes every
   unrevoked key in a site.
6. **Observable** — Prometheus counter for admits-per-key + an alert if a
   single-use key is redeemed more than once (attempted re-use of a
   compromised key).
7. **Integration with offline-enrollment bundles** — when Block 2C lands,
   the rotation plan for CA-signed bundles lives alongside this one. See
   [`offline-enrollment.md`](../design/offline-enrollment.md).

## Role taxonomy

Four roles with distinct TTLs + usage-limits. All defined in
`ops/ansible/group_vars/vega_hosts/vars.yml`.

| Role | TTL default | Usage limit | Typical use |
| --- | --- | --- | --- |
| `bootstrap` | 1 day | 1 peer | First peer at a fresh site (the admin's own workstation). Revoked the moment the first peer enrolls. |
| `admin` | 7 days | 3 peers | Operator enrolling the mgmt-host itself + immediate neighbors during site bring-up. Rotated weekly during shakedown; monthly once stable. |
| `operator` | 30 days | 25 peers | Regional-ops engineer bringing up peers over a rollout window. Rotated monthly. |
| `peer` | 7 days | 1 peer | One-time single-device key baked into an offline enrollment bundle OR handed out for a single device enrollment. Revoked on first use. |

**Why `peer` is 7 days, and how to ask for longer.** This default was 90 days
until 2026-08-18. A welcome package is written to be acted on the day it lands —
the credential is emailed alongside the instructions for using it — so a
three-month window bought nothing a one-week window does not, while leaving a
live mesh credential sitting in a mailbox for the other eleven weeks. It also
made the key the one artifact in the package a reader could safely defer, which
is exactly backwards: deferral is how a credential ends up unredeemed and
forgotten.

The exception path already exists and is per-person, not a global loosening: set
`netbird.ttl_days` in the operator manifest and it overrides the role default.
Use it where the credential genuinely must be minted before it can be redeemed —
kit provisioned ahead of a move, where people and systems are issued at one
place and enrolled at another. That case is real but narrow; it does not
describe an ordinary onboarding.

**A longer window must say why, and the mint enforces it** (2026-08-18). A
`ttl_days` above the role default is refused unless the manifest also carries
`netbird.ttl_reason`:

```yaml
netbird:
  role: peer
  ttl_days: 90
  ttl_reason: "issued ahead of the <name> movement; enrolled on arrival"
```

Asking for the same as the default, or *less*, needs no justification —
tightening a credential is never something anyone should have to argue for — and
omitting `ttl_days` takes the default silently.

This is checked at mint time rather than in CI, and that placement is forced:
operator manifests are gitignored because they carry names and addresses, so no
job in the repo ever sees one. `goat admin onboard` is the only thing that reads
the file, which makes it the only place the rule can live. Before this, widening
the window was a convention that cost nothing to ignore — any manifest could set
a bigger number and nothing would ask. The requirement is one sentence, which is
the whole point: the person widening the window is the person who knows why, and
the reason belongs next to the number rather than in somebody's memory.

Keys are identified by a compound `(site, role, sequence)` name like
`vega-admin-000012` so an operator can tell at a glance what a key is for
and which instance it is.

## Lifecycle

### Creation

```sql
-- Operator-side SQL via docker exec psql, or via the create-key helper
-- ops/ansible/scripts/create-setup-key.sh. The helper creates a new key, emits
-- the audit line, and returns the opaque token to stdout for the operator
-- to copy once.
INSERT INTO setup_keys (
  id, account_id, key, key_secret, name, type,
  created_at, expires_at, updated_at, revoked,
  used_times, usage_limit, ephemeral, auto_groups
) VALUES (
  :id, :account_id, :key_hash, :key_display,
  :name,       -- 'vega-admin-000012'
  'reusable',  -- or 'one-off' for peer role
  NOW(),
  NOW() + INTERVAL ':ttl_days days',
  NOW(),
  false,
  0,
  :usage_limit,
  :ephemeral,
  :auto_groups
);
```

The SSM helper accepts `--ssm-instance` and `--ssm-region` to select the
management host. It defaults to the role's TTL and usage limit above. Both
overrides must be positive integers; a TTL above the role default also needs
an explanation in `--note`. `--groups` supplies comma-separated NetBird group
IDs; omitting it stores the JSON array `[]`.

The helper generates an uppercase UUID and stores its SHA256 hash and masked
display value, matching `mgmt-host-bootstrap`. It sends the SSM request as JSON
through standard input. The plaintext key appears only in the successful
one-time output, for the operator to put into the secret store. It is absent
from the SSM request, SQL, local temporary files, and both audit logs.

Each invocation creates a new key. A failed or unconfirmed SSM command can
have committed the key before the failure, for example if the file audit write
fails. Check the reported command ID or key ID in SSM history before retrying;
the helper does not resend a mint request automatically. The key row and its
database audit row are one transaction. The file audit line is written only
after that transaction succeeds.

Every creation emits a JSON line to `/var/log/setup-key-audit.log`:

```json
{"ts":"...","event":"create","key_id":"...","key_name":"vega-admin-000012","role":"admin","created_by":"<operator>","ttl_days":7,"usage_limit":3,"auto_groups":["mgmt-host-admins"]}
```

### Usage

When a peer enrolls successfully, Netbird management increments
`used_times`. An auxiliary Prometheus counter is incremented by the
`admit-audit` sidecar (see `ops/admit-audit/`); usage is visible without
leaking the key material itself.

The `AdmitSameSourceUserManyKeys` alert from `admit-ioc.yml` catches the
case where one operator (likely compromised) is burst-enrolling through
a single admin key.

### Expiration + auto-revocation

Two mechanisms, both safe:

1. **Netbird management refuses expired keys** natively — if `expires_at`
   is in the past, enrollment fails server-side. No operator action
   needed for the "expired key should stop working" invariant.
2. **Daily auto-revoke cron** sets `revoked = true` on every key with
   `expires_at < NOW() - INTERVAL '1 day'` so the row is
   tombstoned explicitly, not just implicitly-denied. Audit line emitted:

   ```json
   {"ts":"...","event":"auto_revoke","key_id":"...","reason":"expired"}
   ```

   Cron entry (installed by `ops/ansible/roles/setup-key-rotation/`):
   ```
   */15 * * * * /opt/mgmt-host/scripts/setup-key-auto-revoke.sh
   ```

### Manual rotation

Two patterns — use whichever matches the situation:

**Pre-expiry rotation** (normal cadence): create a new key of the same
role, distribute to users, revoke the old key once the new one is in use.
Grace window is typically 24h. Example:

```
ops/ansible/scripts/create-setup-key.sh --site vega --role admin \
    --ttl-days 7 --usage-limit 3 --note "weekly rotation" \
    --ssm-instance "$SSM_INSTANCE" --ssm-region "$SSM_REGION"
# returns token; operator distributes via secure channel
ops/ansible/scripts/revoke-setup-key.sh --key-name vega-admin-000011
```

**Emergency revocation** (suspected compromise): revoke the specific key
plus any key issued by the same operator in a suspicion window.

```
ops/ansible/scripts/revoke-setup-key.sh --key-name vega-admin-000012 \
    --cascade-from-operator <operator> --since "2026-04-20T12:00:00Z"
```

### Emergency kill switch

Full site lockdown — no new peer enrollments until an operator
re-authorizes. One SQL statement:

```sql
UPDATE setup_keys
SET revoked = true, updated_at = NOW()
WHERE account_id = :account_id AND revoked = false;
```

Emits a cascade of audit lines, one per key. Existing peers keep their
existing WG tunnels (setup keys gate enrollment, not ongoing operation).

## Audit trail

- **Operational log:** JSON lines at `/var/log/setup-key-audit.log` on
  each mgmt-host. Rotated daily via logrotate.
- **Authoritative record:** `setup_keys` table itself — columns
  `created_at`, `updated_at`, `expires_at`, `revoked`, `used_times`
  carry the full lifecycle. Spock replicates this table across sites.
- **Prometheus metrics** (via the admit-audit sidecar):
  - `setup_key_created_total{role="admin"}` counter
  - `setup_key_used_total{role="admin"}` counter  
  - `setup_key_revoked_total{reason="auto"|"manual"|"emergency"}` counter
  - `setup_key_expired_unused_total` counter (keys hit `expires_at`
    without any `used_times > 0` — waste signal)
- **Retention:** Postgres row retained 90 days post-revocation, then
  archived to pgBackRest off-site repo; Prometheus scrape retention 15
  days (per `vega_hosts/vars.yml`).

## Default rotation cadence

Adjustable via the operator runbook; defaults baked into the cron:

| Role | Cadence during shakedown | Steady-state cadence |
| --- | --- | --- |
| `bootstrap` | Rotated on every site-bring-up event | N/A (not used after first day) |
| `admin` | Weekly (Monday 0400 UTC) | Monthly (first Monday 0400 UTC) |
| `operator` | Every 2 weeks | Monthly |
| `peer` | No rotation — one-off by nature | No rotation |

## Integration with offline enrollment bundles (Block 2C)

When `docs/design/offline-enrollment.md` lands, the CA-signed bundle
carries its own expiry (`activation_deadline` 14 days,
`expires_at` 90 days). The bundle is functionally an offline-signed
one-shot setup key — rotation for bundles lives in the bundle
issuance registry (delivery-state tracking + daily auto-revoke cron
on no-show). Setup keys and bundles are **not interchangeable**: setup
keys are online-enrollment credentials; bundles are offline-CA
capabilities. Both follow the same lifecycle invariants (bounded TTL,
auto-revoke, audit trail).

## Implementation status

Configuration vars land in `ops/ansible/group_vars/vega_hosts/vars.yml`.
The helper scripts + cron job land in `ops/ansible/roles/setup-key-rotation/`. Auditable log integration
lands with the `admit-audit` sidecar.
