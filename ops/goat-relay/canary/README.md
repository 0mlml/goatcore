# goat-relay prod-tier 24h canary helpers

Three operator-fired wrappers for the runbook at
`docs/operations/goat-relay-prod-canary.md`.

| Script | Runbook step | Where it runs |
|---|---|---|
| `iperf3-24h-driver.sh` | Step 3 | Client peer (the iperf3 client side of the canary pair) |
| `ssm-probe-loop.sh`    | Step 4 | Operator workstation (aws-vault usareur-af) |
| `aggregate.py`         | Step 5 | Operator workstation (post-run) |

## Posture

These run as **background observation, not a release gate**, producing
evidence for retroactive verification and retire-legacy decisions.

If the canary surfaces a regression after Phase 3 advertise has landed,
rollback is: revert the `relay_addresses` block in
`ops/ansible/group_vars/vega_hosts/vars.yml` + re-run
`site-bootstrap.yml -e site_id=vega`. Peers refresh management.json
on next heartbeat (~5 min); legacy tier resumes as the sole advertised
target. Evidence collected pre-rollback feeds an F-NNN finding.

See `docs/design/relay-blue-green-cutover.md`
§"Phase 3 — ADVERTISE" for the trade-off rationale.

## Run order

1. Operator stages canary peer pair per runbook Step 1 / 1.5 / 2 (sets
   per-peer `Relay.Addresses` override + iptables block-direct-P2P).
2. On the iperf3 server peer: `iperf3 -s -p 5201` (loop or daemonize).
3. On the iperf3 client peer:
   ```
   TARGET_HOST=<server-peer-mesh-ip> bash iperf3-24h-driver.sh
   ```
4. On the operator workstation (concurrently):
   ```
   bash ssm-probe-loop.sh
   ```
5. After ~24h, harvest `/tmp/canary/canary.jsonl` from the client peer:
   ```
   scp <client-peer>:/tmp/canary/canary.jsonl docs/reports/$(date -u +%Y-%m-%d)-goat-relay-prod-canary.jsonl
   python3 aggregate.py docs/reports/$(date -u +%Y-%m-%d)-goat-relay-prod-canary.jsonl
   ```
6. Author M-042 measurement entry per runbook Step 6.
