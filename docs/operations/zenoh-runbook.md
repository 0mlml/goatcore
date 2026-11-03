# Zenoh Runbook

Operational procedures for the Phase 2 Zenoh pilot. Design doc: `docs/design/zenoh-data-fabric.md`. Implementation tracker: `docs/project/zenoh-implementation.md`.

> **Scope:** two-router federated pair on integdev (EC2 <cloud-region-1-notional>), running upstream `eclipse/zenoh:1.9.0` pinned by content digest per [ADR 0858](../adr/0858-zenoh-image-upstream-pivot.md) (supersedes ADR 0708). The rocksdb storage backend `.so` is downloaded from the matching upstream `eclipse-zenoh/zenoh-backend-rocksdb` 1.9.0 release (SHA256-pinned per arch) and bind-mounted into the container via the host plugin dir — see F-139. Data flows: gatewatch status events + synthetic load from ops/zenoh/loadgen/. Additive to the existing HTTP gatewatch API — never replaces it in Phase 2. Not for production tactical data until Phase 3 gate passes.

---

## Quick reference

| Item | Value |
|---|---|
| Image | `eclipse/zenoh:1.9.0@sha256:...` — upstream, digest-pinned ([ADR 0858](../adr/0858-zenoh-image-upstream-pivot.md)) |
| RocksDB backend | `libzenoh_backend_rocksdb.so` from upstream `eclipse-zenoh/zenoh-backend-rocksdb` 1.9.0 release; SHA256-pinned per arch in role defaults |
| Container | `zenoh-router` (docker compose, `restart: unless-stopped`) |
| Listen endpoint | `tcp/0.0.0.0:7447` |
| Metrics | `:8088/metrics` (REST plugin) |
| Config template | `ops/ansible/roles/zenoh/templates/config.json5.j2` |
| Config on host | `/etc/zenoh/config.json5` |
| Storage | `/var/lib/zenoh/rocksdb/` (7-day TTL, `ops/**`) |
| Plugin dir | `/opt/zenoh/plugins/` (host bind-mount; carries the rocksdb backend `.so`) |
| Ansible role | `ops/ansible/roles/zenoh/` |
| Deploy via | `ops/ansible/integdev-bootstrap.yml` Phase 7 |
| Logs | `docker logs zenoh-router -f` |

---

## Deploy

```bash
cd ops/ansible
OBJC_DISABLE_INITIALIZE_FORK_SAFETY=YES aws-vault exec legacy --no-session -- \
  ansible-playbook -i inventory/hosts.ini -i inventory/mgmt-hosts.ini \
  --limit integdev_hosts \
  -e "postgres_superuser_password=... netbird_encryption_key=... pgbackrest_cipher_pass=..." \
  integdev-bootstrap.yml
```

Phase 7 of the bootstrap:
1. Creates `/etc/zenoh/`, `/opt/zenoh/plugins/`, `/var/lib/zenoh/rocksdb/` on each host
2. Renders `/etc/zenoh/config.json5` from `ops/ansible/roles/zenoh/templates/config.json5.j2`
3. Renders `/opt/zenoh/docker-compose.yml` from `ops/ansible/roles/zenoh/templates/docker-compose.yml.j2`
4. Runs `docker compose up -d`, waits for health endpoint

**Verify after deploy:**

```bash
curl http://<host-ip>:8088/metrics | head
docker logs zenoh-router | tail -20
```

### Routine refresh (after first bootstrap)

Once both integdev hosts are Netbird-enrolled and their mesh IPs are filled into `ops/ansible/inventory/mgmt-hosts-mesh.ini`, use the refresh playbook — SSH-over-mesh, no SSM, no aws-vault, ~3-5 min:

```bash
cd ops/ansible
ansible-playbook \
  -i inventory/hosts.ini \
  -i inventory/mgmt-hosts.ini \
  -i inventory/mgmt-hosts-mesh.ini \
  integdev-refresh.yml --limit integdev_hosts \
  -e postgres_superuser_password="$INTEGDEV_PG_PASS"
```

Refresh runs only drift-prone roles (observability, admit-audit, Zenoh) plus the smoke gate. It skips one-time installs (NTP daemons, mgmt-host compose, account seed, pgBackRest stanza) — those need a full bootstrap re-run.

---

## Upgrade

1. Bump `zenoh_image` (image digest pin) AND `zenoh_rocksdb_backend_*` (version + per-arch SHA256s) in `ops/ansible/roles/zenoh/defaults/main.yml` in lockstep — see [`docs/design/zenoh-version-pinning.md`](../design/zenoh-version-pinning.md) §"Bumping zenoh" for the resolver commands.
2. Re-run `integdev-bootstrap.yml` (Phase 7 is idempotent — pulls new image, downloads + verifies new backend `.so`, recreates container).
3. Verify: `docker logs zenoh-router | grep version`, metrics endpoint responds, `storage_manager: Spawning volume 'rocksdb' with backend 'rocksdb'` log line present.

**Plugin ABI compatibility:** Upstream image and the upstream rocksdb backend `.so` are both products of the same upstream Eclipse Zenoh release tag at the same version, so the plugin-trait ABI is aligned by construction (the F-122 plugin-ABI tax that the retired in-tree build was managing is gone). `bash ops/ci/check-zenoh-pinning.sh` enforces the version-of-record at CI time.

---

## Debug

### Is a publisher reaching the router?

```
zenoh-debug-client subscribe 'ops/gatewatch/status/**'
# Then trigger a gatewatch event and confirm it appears
```

### Is storage retaining data?

```
zenoh-debug-client get 'ops/gatewatch/status/**' --duration 1
# Should return matching samples from RocksDB
```

### Is an interceptor loading?

```bash
docker logs zenoh-router | grep -i 'plugin\|interceptor'
# Look for "audit-logger starting" on startup
```

### Are metrics being emitted?

```bash
curl -s http://<host-ip>:8088/metrics | grep zenoh_
# Should show zenoh_audit_samples_total, zenoh_audit_sample_bytes,
# zenoh_audit_processing_duration_seconds
```

---

## Rollback

Zenoh is additive in Phase 2 — the HTTP gatewatch API is always the canonical path. To disable Zenoh without losing data:

```bash
docker stop zenoh-router
```

Status page and consumers continue to use the HTTP endpoint. Re-enable with `docker start zenoh-router` or re-run the bootstrap.

---

## Load test (Week 6 soak)

Gatewatch natural traffic is too quiet for meaningful soak (~5-10 events/hour). The Zenoh pilot ships with a synthetic load generator at `ops/zenoh/loadgen/` that runs three scenarios in sequence during Week 6.

### Scenarios

| Scenario | Duration | Rate | Pattern | Purpose |
|---|---|---|---|---|
| Baseline | 4h | 10 events/hour | Exponential inter-event gaps (mean 6 min) | Match natural gatewatch rate; sanity check |
| Persistent 10x | 24h | 100 events/hour sustained | Same exponential distribution | Hold 10x over multi-day steady-state |
| Surge 2x on 10x | 24h | 100/hour baseline + 20-event bursts every 15 min | Bursty overlay | Peaks ~200/hour during surge windows; tests buffering + interceptor tail latency |

Total soak is ~72 hours; includes settle time between scenarios.

### Running the load generator

```bash
# From the integdev host or local machine with network access to the router:
cd ops/zenoh
ZENOH_ENDPOINT=http://<host-ip>:8088 SCENARIO=baseline docker compose up -d loadgen

# Or directly:
cd ops/zenoh/loadgen
ZENOH_ENDPOINT=http://<host-ip>:8088 SCENARIO=persistent10x go run .
```

Scenarios: `baseline` (10/hr), `persistent10x` (100/hr), `burst` (1000/hr × 10min, set by `BURST_DURATION`).

Events carry `synthetic: true` equivalent via key expression — `ops/loadgen/**` paths are labelled `synthetic=true` by the audit-logger interceptor (`zenoh_audit_samples_total{synthetic="true|false"}`).

### Load generator metrics

Load generator exposes its own Prometheus endpoint on `:9301/metrics`:

- `loadgen_sent_total{scenario}` — events successfully handed to Zenoh
- `loadgen_send_errors_total{scenario}` — Zenoh publish failures (network, backpressure)
- `loadgen_scenario_active{scenario}` — gauge; 1 when active

Compare against:

- `zenoh_messages_received_total` on the router
- Storage `zenoh_storage_insert_total`
- `enrollment_*`-style counters for any consumers

**Key equation at steady state:** `loadgen_sent_total == zenoh_messages_received_total == zenoh_storage_insert_total` (within small delta for in-flight). Significant divergence means drops.

### Gate criteria (Phase 2 gate review consumes these)

| Criterion | Measurement | Pass threshold |
|---|---|---|
| No dropped messages at steady-state | `loadgen_sent - zenoh_storage_insert` over 10x scenario | < 0.01% |
| Interceptor p99 latency under surge | Histogram over surge windows | Document baseline, confirm no degradation trend |
| Storage disk growth matches retention | `du /var/lib/zenoh/rocksdb` hourly snapshots | Within 10% of expected (retention * rate) |
| No process restarts | `systemctl status zenoh-router` + journalctl | Zero unexpected restarts across 72h |
| No memory growth trend | Process RSS over 72h | Stable within ±10% after warmup |

**If any criterion fails:** document the failure mode, decide whether it's a tuning issue (raise resource limit, adjust RocksDB params) or a gate failure (invoke pre-decided NATS JetStream + Go fall-back).

### Clean-up after soak

```bash
docker stop zenoh-loadgen
# Storage: let the 7-day TTL age out synthetic samples, or wipe and restart:
docker exec zenoh-router sh -c 'rm -rf /var/lib/zenoh/rocksdb/*'
docker restart zenoh-router
```

Load generator itself has no persistent state beyond metrics scrape history.

---

## Phase 2 gate review

At the end of Phase 2, answer these three questions with pilot data before Phase 3 commits:

1. **Interceptor model:** did the Rust `.so` pattern produce testable, reviewed code, or did it degrade into config-like shims?
2. **Operational burden:** did the "one system, one config" promise hold, or did Zenoh sneak in new failure modes?
3. **Team velocity:** did Rust interceptor development move at a pace that lets us ship features on this stack?

**If gate fails:** fall back to NATS JetStream + Go consumers (pre-decided — see `docs/project/implementation-plan.md` Decision Points). Disable Zenoh as above, file follow-up work in a new block, do not re-litigate the decision.

---

## References

- Design: `docs/design/zenoh-data-fabric.md`
- Work tracking: `docs/project/implementation-plan.md` Block 11
- Ansible role: `ops/ansible/roles/zenoh/`
- Plugin source: `zenoh-plugins/` (Cargo workspace)
- Upstream: https://zenoh.io/docs
