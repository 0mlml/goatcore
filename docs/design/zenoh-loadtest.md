# Zenoh fabric load-testing apparatus

> **60-second summary:** A reusable apparatus for measuring the throughput ceiling and persistence behaviour of the goat Zenoh data fabric, run against the **real** nova-a router. Load traffic lives in a dedicated `loadtest/**` namespace whose persistence sinks — one rocksdb storage subdir and one ephemeral, quota-capped MinIO bucket served by a second instance of the real audit-subscriber binary — are dedicated and whole-unit wipeable. Real vendor data is never in the delete path. The apparatus measures the four ADR 1012 points (throughput ceiling, latency percentiles, density ceiling, behaviour past saturation) plus the two persistence questions the operator asked for: do rocksdb and MinIO get **complete copies** without blocking the publish path, and what is the **publish→storage delay** for each. Output is a self-contained HTML report with charts plus a committed `M-NNN` measurement, readable cold.

| Field | Value |
| --- | --- |
| Status | Design (2026-06-03). Apparatus build in flight on track `zenoh-loadtest-apparatus`. |
| Anchor ADRs | [ADR 1012](../adr/1012-load-testing-discipline.md) (load testing as a required test class — this apparatus is its first Zenoh-fabric instance), [ADR 1008](../adr/1008-soak-testing-discipline.md) (soak sibling), [ADR 0844](../adr/0844-zenoh-deployment-topology-and-acl-v1.md) (router topology + ACL), [ADR 0811](../adr/0811-audit-log-integrity.md) (Object-Lock storage tiers), [ADR 0858](../adr/0858-zenoh-image-upstream-pivot.md) (external audit-subscriber) |
| Reference designs | `zenoh-data-fabric.md`, [`zenoh-audit-external-subscriber.md`](zenoh-audit-external-subscriber.md), [`audit-cold-storage-offload.md`](audit-cold-storage-offload.md), [`zenoh-trial-fabric-synth-vendors.md`](https://github.com/usareur-af/goatstuff/blob/main/docs/design/zenoh-trial-fabric-synth-vendors.md) |
| First target host | nova-a (one host, trial fabric) |
| Apparatus location | `ops/loadtest/` (orchestrator, probes, wipe, summarizer) + `ops/zenoh-trial-peers/loadgen/` (Rust load generator) + `ops/ansible/zenoh-loadtest.yml` (deploy) |

---

## Why this exists

The fabric is on the [ADR 1012](../adr/1012-load-testing-discipline.md) load-bearing list (data path for mesh/fabric traffic) but has no throughput-ceiling measurement. The operator's ask: stand up a simple apparatus we can use to **extrapolate throughput capacity**, and while doing it, confirm that the two durable sinks behind the router — rocksdb and MinIO — keep up without blocking the publishers and capture complete copies, and measure the lag from a sample reaching the router to it landing in each store.

Two constraints shape every decision below:

1. **Do not fill the disk.** The host has the OS, Docker data, rocksdb, and MinIO all on the **same ext4 rootfs** — `/opt/docker-data` is a plain directory, not a separate mount (`require_docker_data_separate_mount: false` in `group_vars/nova_hosts/vars.yml`). A runaway load run fills the rootfs and wedges the host. rocksdb's 7-day TTL evicts nothing inside a short run, so it can grow unbounded for the duration. MinIO already enforces per-bucket hard quota caps; rocksdb does not.
2. **Never put real data in the delete path.** The wipe step must only ever address containers the load run created. It must be structurally unable to touch `audit-integrity`, `telemetry-otlp`, `runtime-detection-archive`, or any real vendor's rocksdb dir.

## What we measure

Per [ADR 1012](../adr/1012-load-testing-discipline.md)'s four points, instantiated for the fabric:

1. **Single-router throughput ceiling** — the sustained publish rate (msgs/s and MB/s) at which the router stops keeping up, with the binding resource named (CPU / mesh link / rocksdb write / audit-subscriber upload / channel backpressure).
2. **Latency under sustained load** — publisher-side put latency p50/p95/p99/p99.9 at expected operating rate, not just at saturation.
3. **State/density ceiling** — behaviour as concurrent publisher sessions scale (distinct ZIDs), to the point of router refusal.
4. **Behaviour past saturation** — graceful (flat latency, bounded queue, backpressure) vs catastrophic (latency blowup, drops, OOM, restart).

Plus the two persistence questions, measured for **each** sink (rocksdb and MinIO) independently:

- **Complete copies?** Distinct `seq` values stored ÷ distinct `seq` values published. `1.0` = no silent drops. The load generator stamps a monotonic `seq` in every payload so this is an exact set comparison, not a counter guess.
- **Publish→storage delay?** Distribution of `(storage_visible_ts − publish_ts)` per sink. rocksdb: time until a `seq` is GET-able back through the router. MinIO: time until the audit event for a `seq` appears as an object. The load generator stamps `publish_ns` in every payload so the delta is exact regardless of clock path.
- **Non-blocking / non-locking proof.** Zenoh storages and the audit-subscriber are asynchronous by construction (publish returns before any durable commit). The evidence that this holds under load: publisher put-latency percentiles stay **flat** while storage lag grows. A blocking or locking sink would back-pressure the publisher and show up as rising put latency correlated with storage lag. We plot both on one timeline so the decoupling is visible.

## Pre-registered predictions (committed before the first run)

The point of writing these down *before* measuring: a result that lands inside the predicted range confirms the causal model; a result outside it triggers RCA against a **named** assumption, instead of a number getting filed or reacted to blindly. Each prediction below names the resource it is bounded by and what to suspect if it is wrong.

These are order-of-magnitude engineering predictions with wide error bars, anchored where possible on real data — **not** measurements. The strongest anchor is M-043: a 3-hour soak of the real trial fabric on 2× t4g.medium (2 Graviton2 cores each) where the router ran at **< 1 % CPU and < 20 MiB RAM** under the natural ~2.8 msg/s synth-vendor load, with rocksdb growing **2.27 MB/h**. the host has substantially more CPU than a t4g.medium (more cores, faster cores), so routing headroom scales *up* from that anchor. The natural fabric load is tiny; the ceiling is far above it.

### Two regimes

- **Operating point** — the rate the real fabric actually runs at (single-digit msg/s aggregate). Everything is unloaded here; latency ≈ idle latency. This is the "expected middle of the distribution" for day-to-day behaviour.
- **Ceiling** — where we push rate up until a sink stops keeping up. This is the "throughput capacity" number we extrapolate from.

### Operating-point predictions (≈ 3 msg/s aggregate, 100-byte payloads)

| Metric | Predicted middle | Predicted range | Bound by | If observed outside range → suspect |
| --- | --- | --- | --- | --- |
| Publisher put latency p50 | ~100 µs | 30 µs – 500 µs | loopback mTLS put hand-off to transport | Higher → TLS renegotiation, container-bridge overhead, or put is awaiting more than transport hand-off. Lower → put isn't actually round-tripping. |
| Publisher put latency p99 | ~1 ms | 0.3 ms – 5 ms | scheduler / TLS jitter | Much higher → GC-like stalls, CPU contention from co-resident services on nova-a. |
| publish→rocksdb lag (median) | ~2 ms | 0.5 ms – 20 ms | in-process storage-manager async write to local NVMe | Higher → LSM compaction stall, NVMe contention with MinIO on the same rootfs. |
| **publish→MinIO lag (median)** | **~30 s** | **0 – 61 s** | **the 60 s batch timer** (`batch_interval_ms=60000`) — at low rate the batch flushes on the timer, not on fill, so lag is ~uniform 0–60 s plus upload | Median far from 30 s → batch interval misconfigured, or the writer is fill-driven not timer-driven (i.e. rate is already > ~167 msg/s). This is the most counter-intuitive prediction and the best model-check. |
| Completeness, both sinks | 1.000 | exactly 1.0 | nothing is saturated | < 1.0 here → a real defect (silent drop at idle); RCA immediately, do not extrapolate. |

The MinIO-lag prediction is exact arithmetic, not a guess: with `batch_events=10000` and `batch_interval_ms=60000`, the crossover rate is 10000 ÷ 60 s = **166.7 msg/s**. Below it, lag is timer-bound (mean ~30 s); above it, lag is fill-bound (~10000 ÷ rate + upload).

> **These were `256` / `5000` until 2026-08-20 (crossover 51.2 msg/s, median lag ~2.5 s).** They were widened because one flush is one S3 object and MinIO's scanner reads one `xl.meta` per object per pass, so the batch pair — not the byte volume — sets the store's scan cost: at 5 s, a live store held **~590k objects for ~78 GiB**. The batch interval is also the window in which audit lives only in the writer's memory, so it is a deliberate latency-and-loss trade, not a free win. See F-374.

### Ceiling predictions — three axes, not one number (push rate up, 100-byte payloads)

The durable path's behaviour decomposes into **three orthogonal quantities** that must be predicted and measured separately. The expectation for a correctly-built decoupled pipeline is **no locking, higher (but bounded) latency, and throughput that scales with parallelism** — *not* a single low fixed ceiling.

**Axis 1 — locking delay → expect ≈ 0.** No contention stalls, no head-of-line blocking from the sinks into the publish path. Measured directly by put-latency staying flat as storage lag grows (see "non-blocking proof" below). A nonzero locking signature (put-latency spikes correlated with storage activity) is a defect for a live plane.

**Axis 2 — latency → expect higher than zenoh, bounded.** Durability costs latency, by design, and that is healthy:

| Sink | Predicted median latency | Source of the latency | Not to be confused with |
| --- | --- | --- | --- |
| rocksdb | low single-digit ms | LSM write + fsync | locking — it's pipeline depth |
| MinIO | ~30 s at operating rate | the 60 s **batch window** (`batch_interval_ms`) | locking *or* slowness — batching is deliberate, for upload efficiency AND to bound object count |

Latency should rise gently with load and stay **bounded** until a lane saturates; **unbounded** latency growth is the early-warning that a lane is filling and drops are imminent. The knee where latency stops being bounded = that lane's saturation rate.

**Axis 3 — throughput → scalable per lane, not a fixed wall.** This is the axis I initially mis-framed. The current config runs **one lane** (a single audit-subscriber writer task doing serial S3 PUTs; one rocksdb storage on the prefix). The single-lane drain rate is therefore the **unit of scaling**, not a fundamental limit — durable throughput scales ~linearly as lanes are added until a *shared* resource caps it.

| Quantity | Predicted middle | Predicted range | Notes / scaling knob |
| --- | --- | --- | --- |
| Route-only throughput | ~1M msg/s | 200k – 4M | zenoh routing for tiny payloads, `--sessions > 1`. Not the constraint. |
| **rocksdb per-lane** rate | ~100k msg/s | 20k – 300k | one storage, one prefix. Scale: sharded storages / more compaction threads. |
| **MinIO per-lane** rate | ~30k msg/s | 5k – 150k | one writer, serial PUTs. Scale: pipelined in-flight PUTs / writer concurrency / N sharded audit-subscribers. |
| **Shared-resource wall (all lanes)** | bytes/s-bound, ~1M-class for 100 B msgs | wide | the single rootfs **NVMe write bandwidth** ÷ (bytes/msg × write-amplification), shared by rocksdb + MinIO. This is the eventual aggregate ceiling on one host. |

**Headline hypothesis:** there is **no low fixed durable ceiling**. Raw routing is ~1M-class; durable throughput is **scalable** — per-lane ~tens of thousands of msgs/s in the current single-lane config, growing ~linearly with added lanes until the **shared NVMe-bytes/s wall**, which on one host is itself ~1M-class for tiny payloads. So with enough lanes the durable path *can* track zenoh, exactly at the cost your model predicts: **no locking, higher latency, more parallelism.** Capacity planning is therefore "(per-lane rate) × (lanes provisioned), capped by NVMe bytes/s" — a slope, not a single number. The first run measures the per-lane unit and the latency-vs-load curve; a follow-on 1-lane-vs-2-lane scaling check confirms the slope is linear (scalable) or names the shared resource if it sub-linearizes early.

The earlier framing of these as a fixed "durable ceiling in the low thousands" was wrong twice over: it treated the burst buffer (`channel_depth=4096`) as the sustained rate, *and* treated one lane as the limit. Corrected to per-lane-times-lanes above. The MinIO latency prediction (~2.5 s, batch window) is independent of all this and stands.

### Why the sinks don't just throttle zenoh (backpressure vs drop)

A reasonable question: why is there a separate, lower "durable ceiling" at all — why don't rocksdb and MinIO simply *follow* zenoh, backpressuring the publisher down to their own rate so completeness is always 1.0? Two designs are possible, and they are mutually exclusive above the sink's rate:

- **Backpressure (lossless, throttled).** The sinks push back; zenoh slows the publisher to the slowest sink's rate (zenoh exposes this via reliable delivery + `CongestionControl::Block`). Completeness is always 1.0 and there is only one ceiling — the durable rate. The cost: this is **blocking/locking** behaviour, and it lets the durability tier stall the live data plane — one slow MinIO PUT head-of-line-blocks every publisher and every other subscriber on that path. A telemetry/video publisher freezes because an audit upload hiccupped.
- **Decouple-and-drop (non-blocking, lossy-under-overload).** The live plane routes at its own rate; each sink consumes asynchronously through a bounded queue and **drops with a counter** when it can't keep up (`goat_audit_drop_total`; rocksdb GC). The publisher is never slowed. The cost: under sustained overload the *durable copy* can be incomplete.

goat is built as **decouple-and-drop** for the live plane — storage must never be able to freeze operations. That is *why* the audit-subscriber sheds load through an internal `channel_depth=4096` + drop counter instead of applying flow control back to the router. The operating discipline that follows: size the durable tier (writer concurrency, sharded subscribers, faster storage) for the **real peak rate**, and keep live load below the measured drop knee — rather than letting storage throttle the mission.

The logical end-point of this discipline is the **non-persisted `volatile/` class** ([`zenoh-volatile-class.md`](zenoh-volatile-class.md), [ADR 1032](../adr/1032-zenoh-volatile-non-persisted-namespace-class.md)): for high-rate telemetry that doesn't need a durable copy at all, take the durability sinks *out of the path entirely* (no rocksdb storage entry, audit-subscriber `--exclude-key`). What remains is the route/wire path over WireGuard P2P — no central sink, so message-rate scales with node × independent-flow count. This load-test apparatus measures the persisted-path ceilings (where the sinks bind); `volatile/stream` is the path that is bound by the wire instead, and is the fabric's brokerless message-rate scaling story (`goat-net-scaling.md` §"Message-rate scale"). The ceilings here remain the source of truth for both.

> **Confirmed by M-053 + quantified by M-054:** under overload the publisher is **not** blocked (put-latency flat/sub-ms throughout) — the excess is **dropped**, so durability goes *silently incomplete* above the ceiling. The bind is the **in-process rocksdb write coupled to the routing thread** (durable plateau **~32k msg/s** per publisher; M-054), **not** the router itself (which routes ~1.2M/s) and **not** the storage tiers in isolation. (M-053 mis-attributed this to a "~7k/s router single core" — that was an un-pipelined-generator artifact; see M-054.) The two levers: (1) **buffer for spikes** — zenoh's `CongestionControl` + tx-queue/TCP-buffer config + the zenoh-ext `AdvancedPublisher` cache + sample-miss recovery (a 32 GB RAM buffer ≈ 30–130 M messages on the host); (2) **externalize + shard storage across machines** — the MinIO sink is already an external subscriber; moving the rocksdb subscriber off the routing thread and sharding storage subscribers by key-range scales durable capacity independently of routing throughput.

This is exactly what the **non-blocking proof** plot (put-latency overlaid on storage-lag) adjudicates, and it is not assumed — it is measured:

- put-latency **flat** while storage-lag grows ⟹ decoupling holds; the sinks are *not* following zenoh; throughput is the routing ceiling and durability degrades silently (drops) past the sink rate. **Expected.**
- put-latency **rising in lockstep** with storage-lag ⟹ a sink *is* leaking backpressure into the publish path (the "follow zenoh" behaviour) — a finding, and arguably a defect for a plane specified as non-blocking. RCA the path that correlates.

### Throughput RCA finding — pipeline the generator; per-stage non-blocking limits

M-054 ran the surgical decomposition and corrected M-053. The headline: **the fabric is not slow; the load generator was.** On nova-a:

| Configuration (100 B) | Throughput | What it is |
| --- | ---: | --- |
| zenoh P2P (`z_pub_thr`) | ~2.41M/s | raw hardware baseline, no router |
| zenoh routed (`z_pub_thr`) | ~1.23M/s | 1 pub → 1 router → 1 sub; router hop ~halves P2P |
| `goat-loadgen` route-only (fixed) | ~2.51M/s | pipelined generator now matches the reference |
| `goat-loadgen` + in-process rocksdb | ~32k/s | durable plateau — the real durable ceiling |
| OLD `goat-loadgen` (serial await) | ~11.5k/s | the M-053 artifact = `1/RTT`, one msg in flight |

**Each stage is an independent, non-blocking limit you scale separately.** The stages (route → persist → archive) do not compound into one fragile ceiling, and a slow stage never blocks the ones before it (sub-ms put-latency throughout the sweep — the live plane keeps flowing at routing speed while a saturated durable stage sheds its excess). Because every persistence stage is an independent **subscriber**, you scale each by running more subscribers **on separate machines**:

- **Router routing** (~1.2M/s per connection/core) — shard distinct peer connections across cores / add routers; P2P paths bypass the router entirely.
- **Durable persistence** (~32k/s per subscriber) — move the storage subscriber **off the routing thread onto its own machine**; shard storage subscribers by key-range across machines to grow durable capacity arbitrarily.
- **Audit → MinIO** (~85% idle here) — already an external subscriber on its own machine; add shards as object-store load grows.

The ~32k/s durable number is the *in-process, single-machine* floor, not a hard limit; distributing subscribers across machines raises durable capacity independently of routing throughput. Reproduce with `ops/loadtest/rca-throughput-decomposition.sh` (turnkey, no LLM, no prod impact).

### Sensitivity (what moves these)

- **Payload size:** predictions are for ~100-byte messages. At 4 KiB, route-only and rocksdb ceilings drop roughly with bytes/s, and the audit path carries 40× the upload bytes → MinIO ceiling drops further. Above the 16 KiB capture threshold the audit event stores hash-only (no `payload_bytes`), *cheapening* the audit path per message — a regime change to watch for.
- **`--pipeline-depth` (the one that bit us):** a publisher that awaits each `put()` serially keeps one message in flight and measures `1/RTT` (~11.5k/s), not throughput — this produced M-053's false ~7k ceiling. Keep a window of puts in flight (default 256); then a single session already approaches the route-only ceiling. Report depth + session count with every throughput number.
- **`--sessions`:** additional sessions add distinct connections for the multi-peer aggregate picture; with adequate pipeline depth they are not needed to reach the per-link route-only ceiling.
- **Co-resident contention:** nova-a runs the router, both audit-subscribers, MinIO, Postgres, Ory, panoscope, etc. on one box. Ceilings are *with* that contention present (intentionally — that is the real deployment), so they may read lower than an isolated-router benchmark would.

### RCA contract for the first measurement

[M-NNN](#) (the results doc) will carry a **predicted-vs-observed table** mirroring the rows above. Any metric landing outside its predicted range gets a one-paragraph RCA naming which assumption broke and the evidence checked — per the operator's standing bar that numbers are never filed or reacted to blindly. A confirmed prediction is logged as such (the model held); a refuted one is the valuable result.

## Isolation model — real router, dedicated namespace

There is no separate test stack. We load the **real** nova-a router so the numbers reflect the real storage-manager plugin, the real rocksdb backend, the real audit-subscriber binary, and real router contention. Isolation is by **namespace + dedicated sinks**, not by a parallel deployment:

```
goat-loadgen  ──put loadtest/<run-id>/<seq>──▶  nova-a zenoh router (REAL)
  (mTLS leaf, persistent session,                 │
   N sessions, controlled rate)                   │
                                                   ├─▶ storage-manager (REAL plugin)
                                                   │     └─ rocksdb storage entry "loadtest"
                                                   │        host:  /var/lib/goat-zenoh/rocksdb/loadtest/  ← dedicated subdir (nova-a real path, M-052)
                                                   │        (7d TTL, same as any vendor)
                                                   │
                                                   ├─▶ goat-audit-subscriber  [LOADTEST INSTANCE]
                                                   │     --subscribe-key 'loadtest/**'
                                                   │     └─ MinIO bucket  loadtest-ephemeral   ← dedicated, quota-capped, GOVERNANCE
                                                   │
                                                   └─▶ goat-audit-subscriber  [REAL INSTANCE]
                                                         --subscribe-key '<real-vendor-prefix-list>'   ← NARROWED from '**'
                                                         └─ MinIO bucket  audit-integrity   ← load bytes never arrive here
```

`loadtest` is **just another vendor** to the router. It is provisioned through the existing `add-vendor.sh` helper, which yields exactly two things in one shot: an `access_control` ingress write rule for `loadtest/**`, and a per-vendor rocksdb storage entry rooted at `/var/lib/goat-zenoh/rocksdb/loadtest/` (the live nova-a storage mount; M-052). Both are additive and removable; neither touches another vendor's config block or storage dir. (Per F-156, the ACL+storage take effect on the next router restart, not on SIGHUP.)

> **First live run — M-052 (2026-06-03):** the operating-range axes were measured on the real nova-a router and **no prediction was refuted** — dual-sink completeness 1.0 through 2000 msg/s, the publish→MinIO batch-timer crossover confirmed (2.49 s @ 10 msg/s), put-latency flat/falling under load (non-blocking). That run used the **shared production** audit-subscriber for the MinIO measurement (so it did not push the ceilings — saturating a shared subscriber would risk real-vendor drops). The isolated `loadtest-ephemeral` + 2nd-subscriber + narrowed-real-subscriber topology drawn above is what the **ceiling / drop-knee** follow-up needs; it remains the canonical apparatus shape.

The single non-trivial config touch is the **real audit-subscriber**: it subscribes to `**` by default, so it would otherwise sweep `loadtest/**` into the locked `audit-integrity` bucket. We narrow it to the explicit real-vendor prefix list — a one-line, reversible change the role's own defaults already flag as the intended hardening (`narrow per-prefix when the trial fabric ACL tightens`). With that in place, load bytes reach **only** the `loadtest-ephemeral` bucket, so the wipe never has to surgically delete from a shared, locked store.

### Why not a separate router or a separate host

- A parallel router would measure a router that isn't the one in production — defeating the point of a real-path ceiling number, and doubling the rocksdb/audit-subscriber footprint on an already-tight rootfs.
- A separate host isn't available; and router contention from concurrent real-vendor traffic is part of what we want to observe, not exclude.

## Safety: disk budget and the wipe contract

### Pre-flight and watchdog (in `run-loadtest.sh`)

- **Projected-size gate.** Before starting, compute worst-case bytes written `≈ rate × payload_bytes × duration × replication_factor` (rocksdb + MinIO copy ≈ 2×) and refuse to start if it would push `/opt/docker-data` past a configurable headroom floor (default: stop if projected free-space after run < 20 GB).
- **Free-space gate.** Refuse to start if current free space on the rootfs is already below the floor.
- **MinIO quota cap.** `loadtest-ephemeral` is created with a hard size quota (default 20 GiB) and GOVERNANCE Object Lock so it can FIFO-evict rather than fail. A breached quota surfaces as audit-subscriber `goat_audit_write_errors_total` — an explicit, alertable signal, not a silent disk fill.
- **Live watchdog.** A background loop samples rootfs free space every few seconds during the run and **aborts the loadgen** (SIGTERM the container) if free space crosses the floor. The abort is recorded in the run JSONL as the terminating condition; a run that hits the watchdog is still a valid (bounded-only) measurement.

### The wipe contract (`wipe-loadtest.sh`)

The wipe is **allowlist-only** and **dry-run by default**. It can delete exactly two things and nothing else:

1. the rocksdb subdir whose basename is the load namespace (`<zenoh_storage_path>/loadtest/`), and
2. the ephemeral MinIO bucket whose name matches the load bucket (`loadtest-ephemeral`).

Before deleting, it asserts:

- the rocksdb target basename is exactly `loadtest` and its parent is exactly the configured `zenoh_storage_path` — refuse otherwise;
- the bucket name is exactly the configured ephemeral name and matches `^loadtest-` — refuse otherwise;
- the target is **not** in the hard denylist `{audit-integrity, telemetry-otlp, runtime-detection-archive}` and the dir is **not** any other vendor subdir — refuse, loudly, on any match.

It requires `--apply` to mutate; with no flag it prints exactly what it would remove and the measured size, then exits. rocksdb dir removal is preceded by `docker compose stop` of the router only if a live-handle check requires it (normally a GET-drained subdir can be removed while the router runs, then the storage entry is dropped via `delete-vendor.sh`). The ephemeral bucket is emptied (`mc rm --recursive --force` under GOVERNANCE bypass scoped to that bucket alone) then removed.

## Components

| Component | Path | Role |
| --- | --- | --- |
| `goat-loadgen` | `ops/zenoh-trial-peers/loadgen/` | Rust load generator. Persistent mTLS client session(s); controlled `--rate`, `--payload-bytes`, `--duration`, `--sessions`, **`--pipeline-depth`**; stamps `{seq, publish_ns}` in each payload; Prometheus `loadgen_sent_total`, `loadgen_put_latency_seconds` histogram, `loadgen_send_errors_total`. Mirrors the synth-vendor session/mTLS pattern; `zenoh = "=1.9.0"`. **Must keep a window of puts in flight** (`--pipeline-depth`, default 256) — a serial `put().await` loop measures `1/RTT` (~11.5k/s), not throughput (see RCA finding below, M-054). |
| `rca-throughput-decomposition.sh` | `ops/loadtest/` | **Self-contained throughput RCA / decomposition harness** (one command, no LLM, no prod impact): stands up throwaway routers on a private docker network (rocksdb on a scratch dir wiped on exit), runs zenoh's own `z_pub_thr`/`z_sub_thr` reference (optional `--build-reference`) + the fixed pipelined `goat-loadgen` route-only + a durable rocksdb sweep, writes JSONL evidence, renders the HTML report. The reproducible apparatus behind M-054. |
| `render-rca.py` | `ops/loadtest/` | Deterministic stdlib-only renderer for the RCA evidence → self-contained HTML (inline SVG; verdict + decomposition + durable curve + non-blocking per-stage scaling model + hypotheses scored + reproduce). Same evidence → byte-identical report. |
| `run-loadtest.sh` | `ops/loadtest/` | Operating-range orchestrator: pre-flight gates, start watchdog, run loadgen, run both probes, collect JSONL evidence, call summarizer. |
| `benchmark-ceiling.sh` | `ops/loadtest/` | **Reusable ceiling benchmark** (one command): stands up the isolated sink (dedicated `loadtest/**` subscriber → throwaway bucket + header-safe writer), pauses the production subscriber for the window, sweeps a rate ladder measuring **rocksdb and MinIO separately** (rocksdb via GET-back — bounded by store-rate, not offered; MinIO via the dedicated subscriber's per-run metric) + router CPU, restores + tears down, renders the rich HTML report. First runs: M-052/M-053. |
| `probe-rocksdb.sh` | `ops/loadtest/` | After/within the run, GET back `loadtest/<run-id>/**` through the router; emit per-`seq` `{seq, publish_ns, rocksdb_visible_ns}` for completeness + lag. |
| `probe-minio.sh` | `ops/loadtest/` | List + parse `loadtest-ephemeral`; emit per-`seq` `{seq, publish_ns, minio_object_ns}` for completeness + lag. |
| `wipe-loadtest.sh` | `ops/loadtest/` | Allowlist-only, dry-run-default teardown (contract above). |
| `summarize.py` | `ops/loadtest/` | JSONL → self-contained HTML report + PNG charts + `M-NNN` measurement stub. Verdict line first. |
| Deploy | `ops/ansible/zenoh-loadtest.yml` | Provisions the `loadtest` vendor (cert + ACL + storage), the loadtest audit-subscriber instance + ephemeral bucket, and narrows the real subscriber. Bootstrap-vs-refresh split per house rule; operator runs it. |

### Evidence and report outputs

- Raw evidence: `docs/reports/<YYYY-MM-DD>-zenoh-fabric-load.jsonl` (per ADR 1012 evidence-path convention) — one record per sample tick plus the per-`seq` probe joins.
- Human report: self-contained HTML (charts inlined as base64 PNG) written next to the evidence, opened cold by anyone.
- Measurement of record: `docs/measurements/M-NNN-zenoh-fabric-load-<env>.md` + a row in `docs/measurements/INDEX.md`; verdict also lands in `docs/project/validation-state.md`.

### Charts (readable cold)

1. Throughput (msgs/s, MB/s) over the run, with the saturation knee marked.
2. Publisher put-latency percentiles (p50/p95/p99/p99.9) over the run.
3. Persistence lag over the run: publish→rocksdb and publish→MinIO, overlaid on put latency — the non-blocking decoupling, visible at a glance.
4. Completeness bars: published vs rocksdb-stored vs MinIO-stored distinct `seq`.
5. Rootfs free-space curve with the watchdog floor drawn in.

## Runbook (first simple run)

1. Operator runs `ops/ansible/zenoh-loadtest.yml` against nova-a (provisions the `loadtest` namespace + sinks at `--lanes 1`; narrows the real subscriber).
2. On nova-a: `ops/loadtest/run-loadtest.sh --rate-sweep <r0..rN> --payload-bytes <n> --duration <secs> --sessions <k>` — gated, watched, collects evidence. The default sweep brackets the 51 msg/s MinIO batch-timer crossover and climbs until per-sink latency goes unbounded / drops begin (the per-lane knee). Measures all three axes: locking (put-latency flatness), latency-vs-load curve per sink, per-lane throughput.
3. **Scaling check (the slope, not just the point):** re-provision with `--lanes 2` (two audit-subscriber shards + two rocksdb storages over key sub-ranges) and re-run the sweep. Aggregate per-lane-rate ≈ doubling confirms scalable throughput; sub-linear names the shared resource (NVMe bytes/s, MinIO node IOPS, or CPU).
4. `ops/loadtest/summarize.py <evidence.jsonl>` → HTML report + `M-NNN` stub (per-axis results + predicted-vs-observed + scaling slope).
5. `ops/loadtest/wipe-loadtest.sh` (review dry-run plan) then `--apply`.
6. Optionally revert the real-subscriber narrowing by re-running the play without the loadtest flag, or leave it (it is a hardening improvement).

## Extrapolation note

The first runs are single-router on the host. Throughput numbers extrapolate to other hosts by the binding resource named in the ceiling result (CPU-bound numbers scale with core count and per-core speed; mesh-link-bound numbers scale with the WireGuard path; rocksdb-write-bound numbers scale with the NVMe tier). The apparatus records which resource bound the ceiling so the extrapolation is grounded, not a flat multiply.

## Open questions (tracked, not blocking the first run)

- Per-`seq` rocksdb visibility timestamp is observed by GET-back polling, which bounds lag resolution to the poll interval; a router-side storage-commit hook would give exact commit times. Bounds-only is acceptable for the first ceiling runs.
- Whether to graduate to multi-router federation load (two routers over the mesh) once single-router numbers are in — deferred until a second host or a cloud validation env is available.
