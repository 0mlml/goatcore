#!/usr/bin/env bash
# zenoh-fabric-sampler — the LEADING-INDICATOR half of the wedge instrument
# (design: docs/design/zenoh-wedge-instrumentation.md). The symcap watcher catches
# the CRATER (stacks at onset); this samples the RUN-UP so a wedge can be correlated
# with the load that preceded it, and so a cardinality budget can alarm BEFORE the
# stall (not after, like the restart-rate alarm).
#
# WHAT IT MEASURES (and does NOT):
#   - per-namespace STORED-key cardinality (the `**` data walk, bucketed by owner):
#     a real, reproducible LOWER BOUND on routing edges — it caught the video tenant
#     (9,149 keys) — but it is NOT the full routing-edge count. The authoritative
#     edge count (e.g. a publisher with 64k single-publisher edges) is a DERIVED,
#     OBSERVED quantity from panoscope's topology / the router declare log (producer
#     3), not a single admin query. Labelled `stored_key_*` on the surface, never
#     called "edges".
#   - admin declaration count (`@/<zid>/router/**`, wildcard-collapsed) — the 847-class.
#   - scoped-GET vs full-walk latency: the read-hold cost trend (H1).
#   - declare-rate PROXY = delta of stored-key count / interval. Honest caveat: a
#     net-zero churn (equal declares+undeclares) is invisible to a count-diff; the
#     router declare log (producer 3) is the authoritative write-rate.
#
# SINGLE-FLIGHT + LOW FREQUENCY: the full `**` walk IS the expensive read this whole
# investigation is about. flock so only one runs; the timer fires at 60s, not 30s;
# and the walk records its OWN latency so the instrument's observer-cost is visible,
# not hidden.
set -u

CT="${ZENOH_CT:-zagros-sandbox-zenoh-router}"
REST_PORT="${ZENOH_REST_PORT:-8000}"
ROUTER="${ZENOH_ROUTER_NAME:-zagros}"
OUT="${FABRIC_JSON:-/opt/relay-health/www/ingest/zenoh-fabric.json}"
STATE="${FABRIC_STATE:-/var/lib/zenoh-fabric-sampler/state.json}"
TOPN="${FABRIC_TOPN:-8}"
LOCK="/run/zenoh-fabric-sampler.lock"
mkdir -p "$(dirname "$OUT")" "$(dirname "$STATE")" 2>/dev/null

# single-flight: bail (do not queue) if a prior sample is still walking
exec 9>"$LOCK"
flock -n 9 || { echo "zenoh-fabric-sampler: prior sample still running, skip" >&2; exit 0; }

pid=$(sudo docker inspect "$CT" --format '{{.State.Pid}}' 2>/dev/null)
if [ -z "${pid:-}" ]; then echo "zenoh-fabric-sampler: $CT not running" >&2; exit 0; fi

# timed REST GET inside the router netns (reaches the in-container bind, same as the
# health probe). echoes: "<ms> <bytes> <key-count>"
tget() {
  local ke="$1" t0 t1 body
  t0=$(date +%s%3N)
  body=$(sudo nsenter -t "$pid" -n curl -s -m "${2:-30}" "http://127.0.0.1:${REST_PORT}/${ke}" 2>/dev/null)
  t1=$(date +%s%3N)
  printf '%s %s %s' "$((t1-t0))" "$(printf '%s' "$body" | wc -c | tr -d ' ')" \
    "$(printf '%s' "$body" | grep -o '"key"' | wc -l | tr -d ' ')"
}

now=$(date +%s)

# scoped single-key GET (the cheap liveness read) vs the full admin declaration walk
read -r scoped_ms _ _         <<<"$(tget '@/local/router' 8)"
read -r admin_ms _ admin_n    <<<"$(tget '@/local/router/**' 25)"

# full data-plane walk -> total stored keys + per-owner top-N (cardinality attribution)
sudo nsenter -t "$pid" -n curl -s -m 45 "http://127.0.0.1:${REST_PORT}/**" -o /tmp/fabric-walk.json 2>/dev/null
# (walk already fetched; recompute counts + buckets in python, and read prev state for churn)
python3 - "$OUT" "$STATE" "$now" "$scoped_ms" "$admin_ms" "$admin_n" "$ROUTER" "$TOPN" <<'PY'
import json, sys, os
out, state_f, now, scoped_ms, admin_ms, admin_n, router, topn = sys.argv[1:9]
now=int(now); topn=int(topn)
from collections import Counter
try:
    d=json.load(open("/tmp/fabric-walk.json"))
except Exception:
    d=[]
keys=[e.get("key","") for e in d if isinstance(e,dict)]
total=len(keys)
c=Counter()
for k in keys:
    c[k.split("/",1)[0]]+=1
top=[{"owner":o,"stored_keys":n} for o,n in c.most_common(topn)]

# declare-rate proxy: |delta stored keys| / elapsed, from prev state
prev={}
try: prev=json.load(open(state_f))
except Exception: pass
churn=None; elapsed=None
if prev.get("ts") and prev.get("stored_keys") is not None:
    elapsed=now-int(prev["ts"])
    if elapsed>0:
        churn=round(abs(total-int(prev["stored_keys"]))/elapsed, 3)

rec={
  "router": router,
  "sampled_at": now,
  "stored_keys_total": total,          # LOWER BOUND on routing edges — not the edge count
  "top_owners_by_stored_keys": top,    # cardinality attribution (video tenant showed here)
  "admin_declarations": int(admin_n),  # wildcard-collapsed declaration count
  "admin_walk_ms": int(admin_ms),      # full admin `**` read-hold cost
  "scoped_get_ms": int(scoped_ms),     # cheap single-key read (contention baseline)
  "declare_rate_proxy_per_s": churn,   # |delta stored keys|/s — net-change proxy ONLY
  "proxy_interval_s": elapsed,
  "note": "stored-key counts are a LOWER BOUND on routing edges; authoritative edge/declare rate = panoscope topology + router declare log (producer 3)"
}
json.dump(rec, open(out,"w"))
json.dump({"ts":now,"stored_keys":total}, open(state_f,"w"))
print(f"zenoh-fabric-sampler: keys={total} admin_decl={admin_n} admin_walk={admin_ms}ms "
      f"scoped={scoped_ms}ms churn={churn}/s top={top[0] if top else None}", file=sys.stderr)
PY
