# wg-cp0 forwarder failover — operator runbook (Block 22P)

**Goal.** Shift the active wg-cp0 forwarder from one tier to another (or one host within a tier to another). End state: every peer's bundle is re-issued with the new ordering; the new active forwarder carries `198.18.0.0/24` in its `AllowedIPs`; the prior forwarder drops to warm-spare.

**When to fire.** Three named scenarios:

1. **Tier-impairment failover.** The current active tier (`relay_servers` legacy by default) is degraded — a relay's wg-cp0 listener crashed, the t3.micro is unreachable, AWS regional issue, etc. Flip to the new c7i tier (`goat_relay_hosts`) to restore Spock + mgmt-API + signal-fed flow without operator-fired re-issue per peer.
2. **Phase 4 retirement.** When the legacy NetBird Go relay's TCP 33073 is being retired (per `relay-blue-green-cutover.md` Phase 4) and we want the silent control plane on the c7i tier ahead of legacy box decommission.
3. **Host-level shift within a tier.** A specific relay box is being maintenance-cycled; flip the active forwarder host to a sibling within the same tier without changing tier identity.

**Risk envelope.** Bounded. Rotation is fully reversible — flip the var back, re-run the playbook, prior path restored. Bundles are idempotent on the device side (wg-cp0-bundle-apply is atomic conf+privkey write). Worst case: a peer fails to apply the new bundle, keeps running with the old one (still functional via legacy forwarder if the prior bundle is still valid).

## Pre-flight

1. The new active tier's wg-cp0 servers are up — verified via Pass 1A of a recent `wg-cp0-bootstrap.yml` run, OR by hand:
   ```bash
   ansible <new-tier-host> -i inventory/hosts-prod.ini \
     -m shell -a "sudo wg show wg-cp0 | head -5"
   # Expect: interface up, listening UDP 51821, recent handshakes from peers
   ```
2. Operator has aws-vault usareur-af access + dev-CA age identity (or YubiKey-PIV per ADR 0406).
3. `bundle-create` binary is built locally:
   ```bash
   cd ops/enrollment && go build -o cmd/bundle-create/bundle-create ./cmd/bundle-create
   ```
4. `inventory/wg-cp0-prod.ini` `[wg_cp0_relay_hosts:vars]` `wg_cp0_active_forwarder_tier` is the value you want to fail OVER FROM.

## Step 1 — flip the var

Two ways:

### Option A — durable inventory edit (preferred for non-emergency failover)

Edit `ops/ansible/inventory/wg-cp0-prod.ini`, in the `[wg_cp0_relay_hosts:vars]` block:

```diff
[wg_cp0_relay_hosts:vars]
-wg_cp0_active_forwarder_tier=relay_servers
+wg_cp0_active_forwarder_tier=goat_relay_hosts
```

Commit the change so the rotated state persists across operator-laptop swaps. Then run Step 2.

### Option B — emergency `-e` override (faster; doesn't persist)

Skip the inventory edit; pass the override on the playbook command line in Step 2:

```bash
... -e wg_cp0_active_forwarder_tier=goat_relay_hosts
```

Use this when you need failover NOW + will commit a follow-up inventory edit afterward. The bundle re-issue this triggers is durable on the peers (wg-cp0.conf is rewritten); the inventory revert is the only loose end.

## Step 2 — fire the rotation playbook

```bash
cd ops/ansible

OBJC_DISABLE_INITIALIZE_FORK_SAFETY=YES \
  direnv exec . aws-vault exec usareur-af --no-session -- \
  ansible-playbook \
    -i inventory/hosts-prod.ini \
    -i inventory/mgmt-hosts.ini \
    -i inventory/wg-cp0-prod.ini \
    wg-cp0-rotate-bundles.yml \
    -e ca_id=$DEV_CA_ID \
    -e ca_key_path=$(pwd)/../enrollment/ca/ca.key.age \
    -e age_identity_path=$HOME/.config/age/keys.txt \
    -e wg_cp0_allowlist_file=$(pwd)/../enrollment/wg-cp0-bundle-allowlist.json
```

(Add `-e wg_cp0_active_forwarder_tier=<new-tier>` if using Option B above.)

Expected outcome:

- Pass 1: per-host wg-cp0 pubkeys cached on the controller (slurped from each `wg_cp0_relay_hosts` member's `/etc/wireguard/wg-cp0-pub.key`).
- Pass 2: relays JSON assembled at `/tmp/wg-cp0-relays.json` on the controller, ordered with the new active tier first.
- Pass 3 per peer: bundle-create on controller → ship to peer → wg-cp0-bundle-apply on peer → cleanup. Verify reports per-peer wg-show peer count.

Total runtime: ~30–60s × N peers. Most of the time is per-peer SSM round-trip; the bundle-create itself is sub-second.

## Step 3 — verify cluster-wide

```bash
# Each mgmt host should now show the relays of both tiers, with the
# new active forwarder host carrying 198.18.0.0/24 in its AllowedIPs.
for orin in <each mgmt host>; do
  echo "=== $orin ==="
  ansible "$orin" -i inventory/mgmt-hosts.ini \
    -m shell -a "sudo wg show wg-cp0 allowed-ips | grep '198.18.0.0/24'"
done
# Expect: each Orin reports 198.18.0.0/24 against the new active
# forwarder host's pubkey (one line per Orin, all matching the new tier).
```

End-to-end Spock smoke (cross-site replication still healthy through the new forwarder):

```bash
ansible <a mgmt host> -i inventory/mgmt-hosts.ini \
  -m shell -a 'docker exec postgres-a psql -U postgres netbird -c \
    "SELECT subscription_name, status FROM spock.sub_show_status();"'
# Expect: every row reports `replicating`. If status is `caughtup` or
# `down` for any sub, the new forwarder is dropping packets — investigate
# before declaring failover successful.
```

## Step 4 — record the failover event

Append a row to `docs/project/active-work.yaml` `recently_completed:` with the date, the from-tier, the to-tier, the trigger reason, and any peer-count verification evidence. This is the audit trail for the failover.

If the failover was emergency-fired (Option B), commit the inventory edit now so the change is durable.

## Rollback

Same procedure, reversed:

1. Edit `wg_cp0_active_forwarder_tier` back to the prior value (or pass `-e` override to revert).
2. Re-run `wg-cp0-rotate-bundles.yml`.
3. Verify per Step 3.

## Failure modes + recovery

| Symptom | Likely cause | Recovery |
|---|---|---|
| Pass 1 "wg-cp0-pub.key: No such file" on a relay | wg-cp0-bootstrap.yml never ran on that tier | Run `wg-cp0-bootstrap.yml --limit <tier>_hosts` first, then retry rotate |
| Pass 3 "bundle-create: ... no such file or directory" | bundle-create binary not built locally | `cd ops/enrollment && go build ./cmd/bundle-create` |
| Pass 3 bundle-create returns exit 1 | age identity wrong / ca_key_path wrong | Verify `age --decrypt -i ~/.config/age/keys.txt $(pwd)/../enrollment/ca/ca.key.age` decrypts cleanly |
| Pass 3 wg-cp0-bundle-apply fails on a peer | binary missing on peer (Block 22K bundle-apply not deployed) OR ca.pub mismatch | Run `wg-cp0-bootstrap.yml --limit <peer>` to refresh the apply binary + ca.pub |
| Step 3 verify: peer reports old tier as forwarder | wg-cp0.conf not reloaded after apply | Check `/etc/wireguard/wg-cp0.conf` mtime; manual `sudo wg-quick down wg-cp0 && sudo wg-quick up wg-cp0` if needed |

## Cross-references

- `docs/operations/wg-cp0-mirror-to-new-tier-runbook.md` — Block 22O cutover (this runbook is the failover follow-on once both tiers exist).
- [`ops/ansible/wg-cp0-rotate-bundles.yml`](../../ops/ansible/wg-cp0-rotate-bundles.yml) — the playbook this runbook drives.
- `docs/operations/wg-cp0-bundle-ceremony.md` — first-time bundle issuance (operator-fired with offline CA).
- `docs/design/relay-paths.md` — canonical paths summary; §3a explains the active-forwarder-via-AllowedIPs mechanic.
- F-060 — original single-point-of-forwarding finding that motivated active-active.
- ADR 0316 §"Amendment 2026-05-07" — second cloud-tier deployment target, the wg_cp0-server-role decoupling that makes this failover possible.
