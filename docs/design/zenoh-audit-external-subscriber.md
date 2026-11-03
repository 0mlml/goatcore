# Zenoh fabric audit — external subscriber, Rust

| Field | Value |
|---|---|
| Status | Design (2026-05-17). Replaces the in-process `audit-logger` plugin pattern for both trial-fabric (nova-a) and production deployments. |
| Anchor ADRs | [ADR 0858](../adr/0858-zenoh-image-upstream-pivot.md) (image pivot enabled by this move), [ADR 0844](../adr/0844-zenoh-deployment-topology-and-acl-v1.md) (production posture), [ADR 0811](../adr/0811-audit-log-integrity.md) (Object-Lock storage), [ADR 0112](../adr/0112-tech-strategy-workload-shape-language-boundary.md) (Rust on the data path), [ADR 0822](../adr/0822-wide-event-observability-otlp-openobserve.md) (wide-event observability) |
| Findings driving this | F-092 + F-122 + F-136 (the ABI-coupling tax we'd stop paying) |

## What's wrong with the in-process audit-logger plugin

The current `zenoh-plugins/audit-logger/` plugin loads into `zenohd` as a `.so` via the zenoh plugin trait. Trial use was reasonable: it sees every sample in-process at zero packet copy, ships with the router image, and surfaces counters on `:9302/metrics` with no extra deployment.

Five problems:

1. **Plugin-ABI fragility.** The plugin trait embeds rustc-version + dep-graph hashes. Every zenoh release bump risks ABI breakage. F-092 (rustc channel) and F-136 (deps graph) are the same recurring tax. Two ABI-class findings in two weeks; the F-122 8-surface coupling is the underlying shape.
2. **Counts-only, not payloads.** Doesn't satisfy compliance "capture every publish" — for an audit trail you need the actual sample body, not just a counter.
3. **In-process = shared-fate with `zenohd`.** Router crash kills the audit stream. Compliance posture wants the audit log INDEPENDENT of the thing being audited.
4. **No tamper-evidence.** Prometheus counters are mutable; ADR 0811 commits to Object-Lock COMPLIANCE 1y, which counters don't produce.
5. **Single-router scope.** Doesn't aggregate across a federation; each router has its own counter.

## What replaces it

A standalone Rust service — `goat-audit-subscriber` — running as a normal zenoh client peer of every router we want audited.

### Shape

- **Repo:** sibling repo `goat-maintainer/goat-audit-subscriber` OR sibling Cargo crate in this monorepo at `ops/zenoh-trial-peers/audit-subscriber/`. Independent CI workflow + image build; zero coupling to the zenoh router image.
- **Language:** **Rust**, per ADR 0112 (per-byte data-path → Rust; consistent with synth-vendor + audit-logger choices). Uses `zenoh = "=1.9.0"` as a normal crate dependency — version coupling is at one surface (the Cargo.toml pin in our crate), not eight (the F-122 multi-axis coupling that hits in-process plugins).
- **Mode:** zenoh client, mTLS-authenticated leaf cert from the offline-CA (same plumbing synth-vendors use), declares a subscriber on `**` (or a per-vendor-prefix list per ACL allowlist).
- **Sink:** writes structured events to a MinIO bucket with Object-Lock retention per ADR 0811 + Block 95, **as the middle tier in a three-tier retention shape** ([`audit-cold-storage-offload.md`](audit-cold-storage-offload.md)). The three tiers are (1) **router rocksdb 7d** (per-vendor live cache at `/var/lib/goat-zenoh/rocksdb/<vendor>/`, `time_to_live: 604800000`, `garbage_collect_period: 86400` — already in place as the trial default and the role's `goat_zenoh_retention_days` knob), (2) **MinIO `audit-integrity` 30d GOVERNANCE** (this sink — GOVERNANCE-mode Object Lock so the substrate can FIFO-evict oldest under quota pressure instead of failing new writes + matching lifecycle Expiration + BOTH of F-171's clauses — `NoncurrentVersionExpiration: 1` to prune noncurrent versions AND a second rule carrying `ExpiredObjectDeleteMarker` to remove the delete markers those leave stranded; with only the first the bucket stops growing in bytes and keeps growing in keys, which costs CPU rather than disk and so never trips a quota alert; canonical for both trial and prod), (3) **cold-storage bucket COMPLIANCE** (operator-triggered offload at deployment milestones via `ops/scripts/offload-to-cold.sh`, 1y trial / 7y prod matching NIST 800-53 AU-11 — this is where evidence-grade preservation lives). The role parameterizes the MinIO-tier knobs (`minio_audit_retention_days: 30` + `minio_audit_object_lock_mode: GOVERNANCE` + `audit_subscriber_minio_expiration_days: 30` + `audit_subscriber_minio_noncurrent_days: 1`) — canonical defaults are the same fleet-wide; per-deployment overrides only change values. Event schema: `{anchor, vendor, identity_model, key_expr, ts, payload_hash, payload_len, payload_bytes?, src_zid, audit_subscriber_id}`. Payload bytes captured up to a size threshold (e.g. ≤16 KiB); larger payloads recorded by hash only with a pointer to MinIO if storage budget allows.

**Dual-shape identity (ADR 1025).** The subscriber is identity-model-aware so one binary/role serves both a per-vendor goatnet (nova-a) and a per-uuid slot-pool goatnet (ZAGROS). The enforced write-anchor is the *first key segment* in both shapes — a human vendor prefix under `per-vendor`, an opaque slot-anchor uuid under `per-uuid-slot-pool` — so capture keys on segment 0 with no per-model branching. Each event records `anchor` (the shape-neutral first segment), `identity_model` (`per-vendor` | `per-uuid-slot-pool`, from the `--identity-model` / `AUDIT_SUBSCRIBER_IDENTITY_MODEL` knob, default `per-vendor`), and a value-identical `vendor` alias kept so nova-a's in-portal audit tailer (which reads `vendor`) is unchanged. A downstream reader uses `identity_model` to decide whether `anchor` is a human name or a uuid to be joined against the portal slot registry's assigned handle. The `volatile/**` exclusion (ADR 1032) holds unchanged — `volatile/<uuid>/**` is dropped, the persisted `<uuid>/**` namespace is kept — so the per-uuid fabric needs no exclude-key change.
- **Metrics:** exposes Prometheus `/metrics` on its own port — `goat_audit_samples_total{vendor,key_pattern}`, `goat_audit_sample_bytes_total{vendor}`, `goat_audit_write_errors_total`, `goat_audit_drop_total`, `goat_audit_batches_uploaded_total`, `goat_audit_batch_size_events{outcome}`, `goat_audit_subscriber_session_state`. Scraped by site Prometheus.

### Why Rust (and not Go)

- **ADR 0112:** per-byte data-path workloads in Rust, per-request control in Go. Audit subscriber sees every sample on the router; that's a per-byte workload.
- **Crate-version coupling:** `zenoh = "=1.9.0"` in our crate's Cargo.toml is one surface to update on a zenoh bump. Identical pattern to `synth-vendor` and to the panoscope crate. Operator already knows this rhythm.
- **No CGO / runtime-link complications** at the audit level — the libpcsclite cross-compile dance from the portal cert-sign work was specific to cert-sign's PIV path (piv-go needs PCSC at link time), not zenoh client.
- **Consistency** — every Rust zenoh consumer in the repo (synth-vendor, panoscope, audit-subscriber) shares the same crate pin, same client-mode session pattern, same mTLS knobs. Adding a Go consumer would split that shape.

### Decoupling from zenoh image builds (✅ landed 2026-05-18)

This is the **structural win** beyond just "Rust vs Go." After the in-process plugin retired (Block 82S) + ADR 0858 step 5 landed:

- The in-tree `ops/zenoh/image/Dockerfile` + `ops/zenoh/base-image/Dockerfile` + both image-build workflows are GONE.
- Operator pulls upstream `eclipse/zenoh:1.9.0` directly (digest-pinned; cosign verification is the next-mile gate once Eclipse signs their images). The role downloads the matching upstream `libzenoh_backend_rocksdb.so` (SHA256-pinned per arch) and bind-mounts it from the host plugin dir — see F-139.
- Audit-subscriber ships on its OWN release cadence; an audit-subscriber bug never blocks a zenoh upgrade and vice versa.
- Two completely independent CI pipelines, both small + fast.

### Deployment topology

- **Trial fabric (nova-a):** single audit-subscriber container alongside the router, on the same docker network (`goat-zenoh_default`). One leaf cert. Subscribes to `**`. Writes to per-site MinIO on nova-a standup under [`ops/ansible/minio-bootstrap.yml`](../../ops/ansible/minio-bootstrap.yml) + `ops/ansible/group_vars/nova_hosts/minio.yml`.
- **Production (per-site):** one audit-subscriber per site, leaf cert minted by site offline-CA, subscribes to the site's full namespace. Writes to site-local MinIO with Object-Lock; the audit bucket is a separate one from the data buckets per ADR 0811.

## Decommissioning audit-logger plugin

1. Build audit-subscriber, deploy to nova-a alongside the existing audit-logger plugin (both running).
2. Verify subscriber-side metrics match (or exceed) the in-process plugin's count.
3. Drop `audit-logger` from the role's plugin list; re-deploy router.
4. Once stable on the trial fabric (~1-2 weeks soak), delete the `zenoh-plugins/audit-logger/` crate + its build steps from the zenoh image Dockerfile.
5. Trigger ADR 0858's image-pivot sequencing.

## What this design is NOT

- **Not a replacement for the storage_manager rocksdb backend.** Storage_manager is the router's "last-value cache" for REST GET queries; audit-subscriber is the durable event log for compliance + replay. Separate concerns, separate stores.
- **Not a Block 27 OTel pipeline merger.** Block 27 (wide-event OTLP → OpenObserve) is the operational observability path. Audit-subscriber is the **compliance** path — separate retention, separate access controls, separate storage tier per ADR 0811. They can coexist; an event might flow through both.
- **Not gated on production rollout.** Trial-fabric audit-subscriber lands first (Block 82-cluster); production absorption follows ADR 0844's deployment cadence.

## Open questions

- **Sample-payload capture threshold.** ≤16 KiB inline vs hash-only? Influences MinIO storage budget. Need to characterize trial fabric payload size distribution before committing a number.
- **High-availability subscriber.** Single subscriber per router = SPOF on the audit stream. Multi-subscriber with dedup-on-write? Or accept the SPOF in trial scope and revisit for production?
- **Replay support.** Should audit-subscriber double as the replay source for `goat-rollout-coordinator` (Block 63A) class operations? If yes, the bucket layout has to support time-range queries, not just append-only.

## Tracker

Block 82S — scaffold landed 2026-05-17 on track `audit-subscriber-pivot`. Crate at [`ops/zenoh-trial-peers/audit-subscriber/`](https://github.com/usareur-af/goatstuff/tree/main/ops/zenoh-trial-peers/audit-subscriber); CI at [`.github/workflows/audit-subscriber-image.yml`](https://github.com/usareur-af/goatstuff/blob/main/.github/workflows/audit-subscriber-image.yml); role at [`ops/ansible/roles/audit-subscriber/`](../../ops/ansible/roles/audit-subscriber/).
