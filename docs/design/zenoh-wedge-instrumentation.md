# Zenoh wedge instrumentation — make every hypothesis provable or falsifiable

**Status:** Proposed 2026-07-29. Companion to the wedge investigation
(zenoh-wedge-investigation-playbook.md),
findings F-268,
F-248 (router file-descriptor exhaustion),
F-253, and the
router-health band ([ADR 1097](../adr/1097-zenoh-router-service-health-band.md)).

## Why this exists

This doc instruments the router so that each wedge
hypothesis carries a signal that **forces a verdict**, and no cause can be claimed
without one.

**The load is legitimate and will grow** ([zenoh-brokerless-consequences.md](zenoh-brokerless-consequences.md),
`volatile/` class [ADR 1032](../adr/1032-zenoh-volatile-non-persisted-namespace-class.md)). The instrument
must therefore distinguish the two independent scaling axes:
- **message volume** — Zenoh's home turf; data on an established route never touches
  the routing write lock. Scales freely.
- **keyexpr cardinality** — the number of distinct routing edges; this is what grows
  the `RwLock`-protected routing table and drives the wedge.

A dashboard that watches message *rate* looks green right up to the stall. The
instrument must watch **cardinality**.

## The hypotheses and their kill/confirm signals

| # | Hypothesis | Confirms if | Falsifies if |
|---|---|---|---|
| H1 | Admin-space read-hold (`adminspace::resources_data`) holds the routing read lock across an O(N) fan-out | onset capture shows a thread **holding** (past the lock, not in `read_contended`) in `resources_data`; admin-walk latency rises before onset | no admin thread holds across N wedges |
| H2 | Data-query read-hold (`Session::query` / `route_send_response`) holds it | onset capture shows a thread **holding** in the query path; a broad `<tenant>/**` query logged just before | absent across N wedges |
| H3 | DECLARE write-storm saturates the write lock | declare-rate series spikes in the seconds before **every** wedge; writer queued with a declare backlog | declare rate flat pre-wedge |
| H4 | Keyexpr **cardinality** (routing-table size) is the amplifier | wedges occur only above an edge-count threshold; frequency tracks table size | wedges recur at low edge count |
| H5 | `adminspace_getter` storage-manager mutex (the shipped patch) | `StorageRuntimeInner` frames at onset | **KILLED** — absent in every 2026-07-29 capture; fix live, wedges continued |
| H6 | #2635 `undeclare_querier_inner` session-lock cycle | those frames at onset | **KILLED** — absent in every capture examined |
| H7 | Non-lock exhaustion (FD / admission — F-248/F-253) | `fd_used` near cap, or CLOSE-WAIT growth + `open_ms` climbing while REST is fast | fd/close-wait/open flat at onset |

**Verdict 2026-09-15 — H1 with H3, as a re-entrant read (F-524).** The wedge is not a long read-hold and not a declare storm on its own. `adminspace::resources_data` holds the `tables` read guard while it sends each reply, and the reply path (`route_send_response`) takes that read lock again; once a declaration's writer is queued, std's `RwLock` refuses the second read, so the admin thread is both holder and waiter. Reproduced on a bare router (admin walks + declaration churn wedge in 34 s; either alone does not) by `ops/zenoh/wedge-repro/ci-adminspace-deadlock.sh` (3/3 wedged unpatched, 3/3 answered patched). The fix is fabric-wedge patch #3 in `ops/zenoh/pqc-image/`. H4 (cardinality) is still open as an amplifier: more resources per walk means a longer window. The symcap signature for this class is `read_wait≈8 write_wait=1 admin=1` with the reader inside `resources_data`.

## The gap that blocks every verdict: **holder vs. waiter**

The 2026-07-29 captures showed *every* thread in `read_contended` / `write_contended`
— all **waiting**. That proves the lock was jammed but not **which thread held it long
enough to jam it**, so H1 vs H2 vs H3 stayed unsettled. A single snapshot at
"unhealthy" catches the pileup, not the culprit. The fix is **multiple rapid samples
at onset** (≈400 ms apart) to catch a thread mid-critical-section — the holder is the
thread *past* the lock acquire (in `resources_data` / `route_send_response` /
`send_declare`) and **not** in a `*_contended` frame.

## Instrumentation (three producers + one verdict)

1. **Onset capture++** (`zenoh-symcap.sh`) — on unhealthy, take **3 gdb samples
   ~400 ms apart** instead of one; each capture appends a **summary**: count of
   `read_contended` vs `write_contended` threads, the distinct futex uaddrs, and the
   candidate **holder** (a thread in a routing/query/admin frame *not* in `*_contended`).
   Directly discriminates H1/H2/H3/H5/H6/H7 by naming frames.
2. **Leading-indicator sampler** (`zenoh-fabric-sampler.sh`, ~60 s, single-flight) —
   emits `zenoh-fabric.json`: total routing-edge count, **top-N publishers by edge
   count** (cardinality attribution → H4), scoped-GET and full-walk latency (read-hold
   cost trend → H1), and a declare-rate **proxy** (edge-count delta / interval → H3,
   with the honest caveat that a net-zero churn is invisible to a count-diff).
   Single-flight + low frequency because the full walk is itself the expensive read;
   the instrument must not become a trigger.
3. **Declare/query logging** (router-side, staged, needs a restart) — scoped zenoh
   routing-module trace for **declares + query dispatch only** (never per-data-message),
   rate-limited, giving the write-rate and broad-query signal **with keyexpr + face
   attribution** — the direct H3 and "who queried `**`" evidence. Overhead measured
   before it stays on. *(Phase 2 — not in the first cut.)*
4. **Correlation verdict** (`zenoh-wedge-verdict.py`, offline) — joins each wedge's
   onset capture with the leading series into one row
   `{holder, read_waiters, write_waiters, declare_rate_pre, walk_ms_pre, edge_count,
   top_publisher}` and marks each hypothesis **supported / killed / inconclusive**.
   This is what makes "provable or falsifiable" operational rather than aspirational.

## Surfacing

`zenoh-fabric.json` folds into the router-health band (ADR 1097) and the Prometheus
bridge (`goat_zenoh_routing_edges`, `_top_publisher_edges`, `_admin_walk_ms`,
`_declare_rate`). Alerts: **cardinality budget** (`ZenohPublisherCardinality`: any
publisher edge count > budget) as a *wedge precursor*, and admin-walk-latency spike —
both fire **before** the restart-rate alarm, closing the "green until it stalls" gap.

Since the one-walker cutover the band's producer is the fabric crawler, not the shell
sampler, and the bridge carries one more gauge: `goat_zenoh_crawler_session_live`
(1 = the crawler holds a zenoh session; 0 = rocksdb-only degraded), with
`ZenohCrawlerSessionDegraded` at 15m/warning. It exists because the crawler no longer
exits when its session dies — census and budget classification are router-independent —
so process death stopped being the signal that the router was gone. In that state the
router-derived facets (`admin_declarations`, `admin_walk_ms`, `scoped_get_ms`) render
**null, not zero**: a panel reading them is looking at an absence and must not report it
as "no publishers". Detail in
`zenoh-cardinality-budget-and-lazy-crawler.md`
§"The crawler outlives the router it measures".

## Non-goals / honesty

- The count-diff declare proxy undercounts same-count churn; the router-log (producer 3)
  is the authoritative write-rate. The proxy is a stopgap, labelled as such on the surface.
- The full-walk sample has an observer cost (it *is* the expensive read); it runs at
  60 s single-flight and records its own latency so its contribution is visible, not hidden.
- None of this fixes the wedge. It tells us **which** fix (cardinality budget, firehose
  isolation, routing-domain sharding, or the core lock-hold patch) the evidence supports —
  so the next structural change is chosen, not guessed.
