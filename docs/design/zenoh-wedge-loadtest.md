# Zenoh wedge-loadtest harness

**What:** a disposable, reusable AWS harness that **reproduces a zenoh
router wedge on throwaway infra** and proves which fix breaks it — without
touching the live fabric. Distinct from the throughput apparatus in
[`zenoh-loadtest.md`](zenoh-loadtest.md) (ADR 1012): that measures the ceiling;
this deliberately deadlocks the router to validate a root cause and a fix.

| Field | Value |
|---|---|
| Status | Built 2026-07-29, track `zenoh-wedge-loadtest`. |
| Code | `ops/zenoh/wedge-loadtest/` |
| Root cause reproduced | storage-manager `adminspace_getter` deadlock |
| Investigation | `zenoh-wedge-investigation-playbook.md` |

## The defect

Confirmed by reproduction and a read of the zenoh 1.9.0
source: `adminspace_getter` in `plugins/zenoh-plugin-storage-manager/src/lib.rs`
takes the one global `std::sync::Mutex<StorageRuntimeInner>` (line 356) and,
**while holding it**, `tokio::task::block_in_place`s a synchronous `GetStatus`
round-trip to every one of ~1000 storage tasks (line 379). Each storage answers
only after taking its own `.lock().await` across a backend op. The mutex stays
pinned for the slowest storage; a second admin walk parks another tokio worker on
that same std Mutex (not `block_in_place`-wrapped, so no replacement worker
spawns); the routing `Tables` RwLock is meanwhile contended by transport threads
routing real peer traffic. Three pressures together → permanent deadlock.

Verified in-tree: `struct StorageRuntime(Arc<Mutex<StorageRuntimeInner>>)` (l.97),
guard taken in `adminspace_getter` (l.356), held across the `block_in_place`
`GetStatus` fan-out (l.379-382). Upstream [issue #1094](https://github.com/eclipse-zenoh/zenoh/issues/1094)
is the same class, **still open**; 1.9.0 is the latest release — no version to
bump to, so a fix is our own patch (snapshot the handles + drop the guard before
the fan-out, or bound/time-out the admin query).

## The three necessary conditions → harness components

The reproduction ladder (rev.3) proved each is necessary: admin-walk + worker
pressure alone pushed admin latency 500× but never froze; adding real mTLS peers
produced a transient pile-up; adding worker starvation produced the terminal
freeze whose stacks match a natural wedge exactly.

| Condition | Component |
|---|---|
| #1 concurrent admin walks | `lambda/admin_walk.py` — a fleet of ~100 pure-stdlib Lambdas GETting `storage_manager/storages/**` on `:8000` |
| #2 real routed peer traffic | `router/peergen/` — a zenoh-python sidecar: connection churn (Tables write-lock churn) + sustained publishers into stored slots |
| #3 worker-pool pressure | the router container runs `--cpus 1` (config), so worker starvation is reachable on an idle box |

## Architecture — disposable + isolated

```
scripts/run.sh (orchestrator)
  └─ terraform apply
       ├─ EC2 (AL2023, c7i.xlarge): docker → eclipse/zenoh:1.9.0, N-storage config
       │    (gen-config.py; memory backend by default), CPU-limited; + peer-gen sidecar
       ├─ Lambda (python3.12, in-VPC): admin_walk.py, invoked ×lambda_count async
       └─ SG (self-referencing): :7447 peers + :8000 admin — the :8000 rule is the FIX TOGGLE
  └─ wait storages open → fire fleet → SSM-probe for the freeze → capture → destroy
```

The target is a **fresh** router with synthetic `loadtest/**` slots; no real
fabric data is ever in scope. `memory` storages are the default (the deadlock
triggers on storage *count* / the fan-out, not the backend); `rocksdb` is opt-in
as a production-faithful amplifier (slower per-storage answers widen the hold window,
needs a rocksdb-capable image).

## Reproduced-freeze signature (what the probe asserts)

Per rev.3: `GET /@/local/router/config` **times out**, docker `health=unhealthy`,
**0 running** threads (all `S`), and **3+ waiters on one futex**. The orchestrator
SSM-captures the frozen kernel stacks to `.build/wedge-capture-*.txt` for a diff
against a natural wedge (identical blocked set = confirmed).

## Fix validation (built in)

- **P1 config fix — lock down `:8000`.** `run.sh --fix-lockdown` sets
  `expose_admin_8000=false`, which drops the SG ingress rule so the admin-walk
  Lambdas can't reach the storage adminspace — **removes condition #1**. Expected:
  no freeze.
- **Code fix — patched `adminspace_getter`.** `run.sh --router-image <patched>`
  points the rig at a zenoh image whose `adminspace_getter` drops the guard before
  the fan-out. Expected: no freeze even with `:8000` wide open — the durable fix.

## Safety + cost

- Isolated dev account, teardownable (`run.sh --destroy`, `force_destroy` bucket).
- One `c7i.xlarge` + ≤100 short-lived Lambdas for ~10 min = single-digit dollars.
- The unauthenticated `:8000` is only ever reachable **inside the rig's SG** (the
  self-referencing rule), never the public internet.

## Results

_(Filled after the first run — reproduced yes/no, the captured freeze signature,
and the fix-toggle deltas.)_
