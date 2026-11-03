# CGNAT-aware signal federation validation — operator runbook

End-to-end operator runbook for the ephemeral CGNAT validation env.
Design: [`../design/cgnat-validation-env.md`](../design/cgnat-validation-env.md).
Attempt-1 findings (why we pivoted to Path C):
`../reports/2026-04-24-cgnat-validation-attempt-1-findings.md`.

**Currently targeting:** attempt-2, Path C — 100 × t2.nano native-netbird
peers (50 per site). Attempt-1's nested-container swarm is retired.

**Estimated wall-clock:** ~30 h total (provision 15 min + enroll 15 min +
24 h soak + faults 1 h + evidence 30 min + teardown 5 min). Cost ~$16.

**Prerequisites:**

- `aws-vault` profile `legacy` active.
- `terraform` ≥ 1.6, `ansible` ≥ 2.16, `community.aws` collection.
- Two 32-byte pg passwords + one 32-byte netbird DataStoreEncryptionKey. **The netbird key MUST be generated with `openssl rand -base64 32`, not `-hex 32`** — netbird-mgmt's boot path base64-decodes the value and asserts decoded length == 32; hex-32 produces 64 chars which decode to 48 bytes and FATL the mgmt-plane (see F-024).

  ```bash
  CGNAT_PG_PASS_A="$(openssl rand -hex 32)"      # any 32+ bytes; pgx pgpass-safe (no /, +, =)
  CGNAT_PG_PASS_B="$(openssl rand -hex 32)"
  CGNAT_NB_ENCKEY="$(openssl rand -base64 32)"   # 44 chars including padding; decodes to 32 bytes
  ```

---

## 1. Provision

```bash
cd terraform
aws-vault exec legacy --no-session -- terraform apply \
  -target=aws_iam_role.cgnat_ssm \
  -target=aws_iam_role_policy_attachment.cgnat_ssm \
  -target=aws_iam_instance_profile.cgnat_ssm \
  -target=aws_subnet.cgnat_priv_a \
  -target=aws_subnet.cgnat_priv_b \
  -target=aws_eip.cgnat_natgw_a \
  -target=aws_eip.cgnat_natgw_b \
  -target=aws_nat_gateway.cgnat_a \
  -target=aws_nat_gateway.cgnat_b \
  -target=aws_route_table.cgnat_priv_a \
  -target=aws_route_table.cgnat_priv_b \
  -target=aws_route_table_association.cgnat_priv_a \
  -target=aws_route_table_association.cgnat_priv_b \
  -target=aws_security_group.cgnat_mgmt_a \
  -target=aws_security_group.cgnat_mgmt_b \
  -target=aws_security_group.cgnat_peer_a \
  -target=aws_security_group.cgnat_peer_b \
  -target=aws_instance.cgnat_mgmt_a \
  -target=aws_instance.cgnat_mgmt_b \
  -target=aws_eip.cgnat_mgmt_a \
  -target=aws_eip.cgnat_mgmt_b \
  -target=aws_instance.cgnat_peer_host_a \
  -target=aws_instance.cgnat_peer_host_b

# Capture outputs
terraform output cgnat_mgmt_a_instance_id
terraform output cgnat_mgmt_a_public_ip
terraform output cgnat_mgmt_b_instance_id
terraform output cgnat_mgmt_b_public_ip
terraform output cgnat_peer_host_a_instance_ids
terraform output cgnat_peer_host_b_instance_ids
```

Regenerate the inventory from terraform output (writes
`../../ops/ansible/inventory/cgnat-validation.ini`):

```bash
aws-vault exec legacy --no-session -- \
  ops/ansible/inventory/gen-cgnat-validation.sh
```

Wait ~90 s for cloud-init to complete on all 102 hosts (2 mgmt + 100
peer). SSM agent comes up as soon as the base Ubuntu boot finishes; the
peer-node cloud-init only installs curl/gnupg so it's fast.

## 2. Deploy mgmt-plane + enroll 100 native peers

```bash
cd ops/ansible
OBJC_DISABLE_INITIALIZE_FORK_SAFETY=YES \
  aws-vault exec legacy --no-session -- \
  ansible-playbook \
    -i inventory/cgnat-validation.ini \
    cgnat-validation.yml \
    -e postgres_superuser_password="$CGNAT_PG_PASS" \
    -e netbird_encryption_key="$CGNAT_NB_ENCKEY" \
    -e cgnat_site_a_setup_key="$(uuidgen)" \
    -e cgnat_site_b_setup_key="$(uuidgen)"
```

Phases:

1. **Phase 1** — mgmt-host.yml against both sites (postgres-spock +
   signal-server + netbird-management).
2. **Phase 2** — socat sidecar exposing mgmt API on `0.0.0.0:80 →
   127.0.0.1:8080` (the base compose binds loopback-only; this is the
   validated workaround from attempt-1 findings §2.4).
3. **Phase 3** — seed `accounts` + `setup_keys` rows in each site's
   mgmt DB via `docker exec postgres-a psql`.
4. **Phase 4** — `install-netbird.sh` via SSM on each peer in waves of
   10 (serial: 10). Each EC2 runs `curl … | sh` from `pkgs.netbird.io`
   then `netbird up --setup-key …`.
5. **Phase 5** — smoke: 3 min pause, query signal /metrics + peer
   counts.

Phase 5 should report site-α ≥ 45 peers and site-β ≥ 45 peers. If under
40, inspect SSM return codes from Phase 4 — typical failures are
transient pkgs.netbird.io fetch errors (re-run Phase 4 idempotently).

## 3. 24 h soak (hours 0–20)

Let it run. Periodically sample via SSM:

```bash
# Signal metrics — watch connected_peers + forward counters grow
aws-vault exec legacy --no-session -- aws ssm send-command \
  --region <cloud-region-1-notional> --instance-ids <mgmt-a-id> \
  --document-name AWS-RunShellScript \
  --parameters 'commands=["curl -sS http://127.0.0.1:9090/metrics | grep -E \"signal_server_(connected_peers|forward_sent_total|forward_received_total)\""]'
```

## 4. Fault injection (hours 20–21)

Run the §3.4 federation-load harness scenarios against this env:

```bash
# Scenario 1: kill signal-a, measure impact
bash ops/wave3/federation-load/scenarios/scenario-1-kill-server.sh

# Scenario 2: tc netem 2000ms on signal-a egress
bash ops/wave3/federation-load/scenarios/scenario-2-latency-inject.sh

# Scenario 3: recovery verification
bash ops/wave3/federation-load/scenarios/scenario-3-recovery.sh
```

## 5. Evidence capture (hour 21–22)

Capture per-site signal metrics + peer status + data-plane sampling:

```bash
for site in a b; do
  aws-vault exec legacy --no-session -- aws ssm send-command \
    --region $([ "$site" = "a" ] && echo <cloud-region-1-notional> || echo <cloud-region-alt-notional>) \
    --instance-ids <mgmt-$site-id> \
    --document-name AWS-RunShellScript \
    --parameters 'commands=["curl -sS http://127.0.0.1:9090/metrics > /tmp/signal-metrics-'$site'.txt; sudo docker exec -e PGPASSWORD=... postgres-a psql -U postgres netbird -c \"SELECT COUNT(*), connection_status FROM peers GROUP BY connection_status;\" > /tmp/peer-status-'$site'.txt"]'
done
```

File everything into `docs/reports/<date>-cgnat-validation-evidence.md`
(follow the pattern of
`../reports/2026-04-24-wave3-hadev-soak-evidence.md`).

## 6. Pass / fail verdict

Per design doc §3 pass criteria:

- `signal_server_connected_peers` ≥ 45 on each side, held continuously.
- `forward_sent_total{outcome="delivered"}` > 100/hr per site.
- `forward_sent_total{outcome="deadline_exceeded"}` < 0.1 % of delivered.
- Relay fallback rate ≥ 80 % of cross-site peer-pair establishments.
- No unbounded latency / error growth in scenarios 1–3.

## 7. Teardown (MANDATORY)

**Run immediately after pass/fail verdict is captured. Do NOT leave the
env running.**

Teardown command: see header of
`../../terraform/cgnat-validation.tf`.

Also:

- Reset `../../ops/ansible/inventory/cgnat-validation.ini` to stub (inventory is generated — don't leave stale instance IDs).
- Flip row in `cloud-env-lifecycle.md` to ⚫ RETIRED.
- Update `../project/roadmap.md` §7 status.
- Revoke the 1Password items for this run's secrets.
