# Relay throughput ceiling — operational guidance

**Status:** Living document. Filed 2026-04-29 from M-005 RCA evidence.

This is the **operator-facing summary** of what we measured for the netbird relay's per-VPS / per-peer-pair throughput ceiling, and what it means for day-to-day operations.

For the engineering background see F-014, F-015, F-016, F-017, M-001, M-002, M-005.

---

## TL;DR

| If you're asking | The answer |
|---|---|
| What's the per-peer-pair throughput when traffic routes through a relay? | **~25-30 Mbps single-flow**, **~25 Mbps total at any concurrency** (multi-flow degrades or stays flat) |
| What's the per-relay aggregate throughput? | Higher — relay handles many independent peer-pairs in parallel, each at low rate. Per-pair ceiling above doesn't aggregate (each pair is independent). Untested at fleet scale; M-002 (multi-pair) is the test that would close this. |
| When does this bite? | When **a single peer-pair** sustains traffic above ~25 Mbps. Use cases that trigger it: peer-to-peer video over relay, large file transfers, multi-stream backups, video-conferencing-over-relay. |
| Can the operator change this with a sysctl or config? | **No.** The ceiling is structural to the relay binary's Go runtime (76% of time in futex + nanosleep / scheduler), not in any tunable. |
| What's the fix path? | Block 33 — relay rewrite in Rust + kTLS + io_uring + shard-per-core. ~4-7 weeks vendor-handoff work. Triggered when production crosses the 25 Mbps per-peer-pair threshold. |

---

## When to worry

The ceiling matters when **a single peer-pair** is pushing sustained traffic.

**Workloads that DO hit the ceiling:**
- Peer-to-peer video calls (typical bitrates 2-8 Mbps per camera per direction → at HD or 4K can saturate)
- File transfers over relay (`scp`, `rsync`, `rclone` with default settings = bursty + long-running)
- Continuous sensor streams (radar, LIDAR, video feeds at >5 Mbps)
- Multi-stream backups (rsync `--parallel`, restic with concurrent uploads)

**Workloads that DON'T hit the ceiling:**
- Mesh control-plane chatter (heartbeats, key exchange, ACL updates)
- Web traffic between peers (browsing, REST APIs at typical rates)
- Voice-only calls (typically <100 Kbps per direction)
- Periodic config / state sync (small messages, infrequent)

**Pairings that route through relay** (ceiling applies to these):
- Both peers behind CGNAT (symmetric NAT) → P2P direct usually fails → relay
- One peer behind firewall blocking inbound UDP → relay
- Peers in geographies where direct paths are blocked

**Pairings that bypass relay** (P2P direct, ceiling does NOT apply):
- Both peers with public IPs → STUN-discovered direct path
- Peers on the same LAN
- Peers in geographies where direct paths work

---

## Symptoms

If a workload is hitting the ceiling, you'll see:

1. **Throughput plateau around 25-30 Mbps** regardless of how the application's bandwidth is configured.
2. **Multi-flow not improving things** — adding more parallel streams doesn't increase total throughput; usually decreases it.
3. **Relay container memory growing** — the relay's process accumulates buffer when downstream send rate < inbound rate. The leading-edge indicator is memory growth from baseline ~12 MB toward 500+ MB. (See Prometheus alert at the bottom of this doc.)
4. **TCP retransmits visible in `iperf3` or `ss -tin`** on the peer side.
5. **`netbird status -d` showing "Connection type: Relayed"** for the affected peer — confirms it's on the relay path.

---

## What to do if you hit it

**Short-term workarounds (operator-side):**

1. **Verify it's actually the ceiling and not something else.** Use `iperf3` between the two affected peer mesh IPs. If you see 25 Mbps stable with multi-flow degradation, it's F-015/F-017. If you see something else (different bottleneck, packet loss, etc.), file a finding.

2. **Encourage P2P direct path** if both peers can reach each other directly. Check `netbird status -d` — `Connection type: Direct` means no relay involvement; `Relayed` means it's hitting the ceiling. Direct connectivity can sometimes be coaxed with:
   - Adjusting firewall rules to allow inbound UDP from the peer's public IP
   - Verifying STUN servers are reachable from both peers
   - Checking for symmetric NAT — if both peers are behind it, P2P direct is unlikely without TURN

3. **Application-level chunking** if the workload allows: split a large transfer into multiple smaller files / sessions distributed over time, rather than one big concurrent transfer.

4. **Use dedicated bridge VPS** for partner / external traffic — bridges are NOT subject to the relay ceiling because they speak partner protocols directly (no rels:// wrap). See `bridge-peering.md`.

**Long-term fix:** Block 33 ships when production crosses the threshold for sustained traffic. Until then, the ceiling is an operational constraint to plan around, not a problem to solve in production.

### Free ~25% throughput improvement — set GOMAXPROCS=1 on relay container

**This is actionable today, no engineering work required.** Per M-006, setting `GOMAXPROCS=1` as a docker env var on the netbird-relay container yields ~25% single-flow throughput improvement (36.6 Mbps vs 27-30 Mbps default) on a 2-vCPU instance.

**Mechanism:** Go's default GOMAXPROCS=NumCPU spawns one OS thread per core; on a 2-vCPU instance that produces measurable inter-thread futex contention. GOMAXPROCS=1 collapses to a single OS thread → eliminates inter-thread contention.

**Status: in the role.** `-e GOMAXPROCS=1` was added to the relay docker-run in [`ops/ansible/relay-server.yml`](../../ops/ansible/relay-server.yml) (commit `cc5019f`, 2026-04-29). Applying to the live relay fleet is a routine `ansible-playbook -i inventory/hosts.ini relay-server.yml` run, gated on operator change-window choice. Multi-flow performance unchanged; if a relay regularly handles many concurrent peer-pairs with bulk traffic, the trade-off may shift — re-test on bigger relay instances (4+ vCPU).

---

## Why we can't fix it with a sysctl or config

Standard suspects we tested and ruled out:

| Hypothesis | Test | Verdict |
|---|---|---|
| Peer NIC is the cap (t3.nano AWS baseline matches 32 Mbps coincidentally) | iperf3 direct TCP from peer to relay's public IP — got 330 Mbps single, 1 Gbps 4-parallel | ❌ RULED OUT |
| Linux TCP buffer at 50ms BDP caps single flow | Tuned `tcp_rmem`/`wmem` to 16 MB on peer; re-ran | ❌ RULED OUT (~10% improvement only) |
| Docker / docker-proxy serializes packets | Extracted relay binary, ran natively on host (no Docker) | ❌ RULED OUT at single-flow (binary same speed as container) |
| Relay CPU is bound | Profiled — 33% peak CPU on 2 vCPU, no thread saturated | ❌ RULED OUT |
| Relay caps its own socket buffers via setsockopt | strace showed ZERO setsockopt calls | ❌ RULED OUT — Linux TCP autotuning is in effect |
| **Go runtime futex + scheduler overhead dominates** | strace -c during sustained load: futex 51.8%, nanosleep 24.3% | ✅ **CONFIRMED — 76% of time in Go runtime, not I/O** |

The structural cause is the relay being a Go program with goroutine scheduler + mutex contention dominating its measured CPU time. **Forcing QUIC instead of WebSocket doesn't help meaningfully** — quic-go runs in the same Go runtime with the same overhead. **Increasing socket buffers doesn't help** — the relay isn't capping itself, the runtime is. **Bigger relay instance helps aggregate** (more pairs in parallel) but not per-pair (each pair lands on one relay's runtime).

---

## Prometheus alert spec — early warning signal

The leading indicator of a peer-pair hitting the ceiling is **netbird-relay container memory growth**. Baseline is ~12 MB; we observed growth to 500+ MB in 2 minutes during M-002 multi-flow stress. Add this alert to the relay's Prometheus rules:

```yaml
# ops/monitoring/relay-alerts.yml additions
groups:
  - name: relay-throughput-ceiling
    rules:
      - alert: NetbirdRelayMemoryGrowth
        expr: |
          (
            container_memory_usage_bytes{name="netbird-relay"} -
            container_memory_usage_bytes{name="netbird-relay"} offset 5m
          ) / 1024 / 1024 > 100
        for: 5m
        labels:
          severity: warning
          component: netbird-relay
          finding: F-015
        annotations:
          summary: "netbird-relay memory grew {{ $value | printf \"%.0f\" }} MB in 5 min on {{ $labels.instance }}"
          description: |
            Container memory growth >100 MB in 5 min suggests a peer-pair is hitting
            the F-015 / F-017 throughput ceiling and the relay is buffering inbound
            data faster than it can forward. Investigate which peer-pair is sending
            sustained high-bandwidth traffic via this relay.

            Per docs/operations/relay-throughput-ceiling.md: per-peer-pair ceiling
            is ~25-30 Mbps. Workloads that exceed this need either P2P-direct, or
            chunking, or to wait for Block 33 (relay rewrite).
          runbook: https://github.com/usareur-af/goatcore/blob/main/docs/operations/relay-throughput-ceiling.md

      - alert: NetbirdRelayMemoryHigh
        expr: |
          container_memory_usage_bytes{name="netbird-relay"} / 1024 / 1024 > 400
        for: 10m
        labels:
          severity: critical
          component: netbird-relay
        annotations:
          summary: "netbird-relay memory at {{ $value | printf \"%.0f\" }} MB on {{ $labels.instance }} (sustained >400 MB)"
          description: |
            Container is at ~half of t3.micro's 911 MB total memory. If this grows
            to ~700 MB+ the OOM killer will reap the relay process. This is the
            F-017 backpressure pattern — buffer accumulation under sustained
            multi-flow load.
          runbook: https://github.com/usareur-af/goatcore/blob/main/docs/operations/relay-throughput-ceiling.md
```

Wire into the existing Prometheus + Alertmanager skeleton at `ops/monitoring/`.

---

## Related artifacts

- Findings: F-014, F-015, F-016, F-017
- Measurements: M-001, M-002, M-005
- Implementation: Block 33 — relay rewrite (held; vendor-handoff candidate)
- Vendor handoff plan: `../project/vendor-handoff-pickup-plan.md`
- ADR: [ADR 1012 — load-testing discipline](../adr/1012-load-testing-discipline.md)
