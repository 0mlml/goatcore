#!/usr/bin/env python3
"""relay-status-record — persist the overall network status over time.

Run once per probe cycle (from serve.sh / the probe timer). Reads the prober's
relay-health.json (front-door reachability) + the canary quorum in peers.json,
collapses them to ONE five-level severity, and records the WORST value seen per
time bucket into a small SQLite store — day buckets (default view) + hour buckets
(the drill-down). Exports status-history.json for the dashboard's top timeline.

Five levels (0..4): up · slightly · moderate · severe · down. Worst-per-bucket
is the honest statuspage convention: a day that dipped to "severe" for an hour
reads severe for that day, and clicking in reveals the hour it happened.

Stdlib only (sqlite3, json) — runs under the same stock python3 as the ingest.

Usage:
  relay-status-record.py --relay-health <f> --peers <f> --db <f> --out <f>
"""
import argparse
import json
import os
import sqlite3
import sys
import time

LEVELS = ["up", "slightly", "moderate", "severe", "down"]  # index = severity code
# (step_seconds, window_seconds) per tier — hour for the drill, day for the bar.
TIERS = [("hour", 3600, 48 * 3600), ("day", 86400, 120 * 86400)]


def load(path):
    try:
        with open(path) as f:
            return json.load(f)
    except Exception:
        return None


def relay_up_ratio(rh):
    """Fraction of relays whose FRONT DOOR is up, by the non-flapping signals
    (:2333 rathole control + :33073 ws_tcp). Returns (up, total); total 0 = unknown."""
    relays = (rh or {}).get("relays") or []
    up = 0
    for r in relays:
        dp = r.get("dataplane") or {}
        if r.get("rathole_ctl") is True and dp.get("ws_tcp") is True:
            up += 1
    return up, len(relays)


def severity(rh, peers):
    """Collapse relay reachability + canary quorum into a 0..4 code, or None if
    we genuinely can't tell (no relays AND no fresh canaries — never a false down)."""
    up, total = relay_up_ratio(rh)
    q = ((peers or {}).get("quorum") or {})
    qv = q.get("verdict")  # up | degraded | down | unknown | None
    q_fresh = int(q.get("fresh") or 0)
    if total == 0 and (qv in (None, "unknown") or q_fresh == 0):
        return None  # nothing to go on
    # canary quorum is the external voice; a decisive "down" from fresh canaries
    # is authoritative even if the blackbox relay probe disagrees.
    if qv == "down" and q_fresh:
        return 4
    if total == 0:
        # only canaries to go on
        return {"up": 0, "degraded": 2, "down": 4}.get(qv, None)
    r = up / total
    if r == 0:
        return 4                       # down: no relay reachable
    if r < 0.5:
        return 3                       # severe
    if r < 0.8 or qv == "degraded":
        return 2                       # moderate
    if r < 1.0 or qv == "degraded":
        return 1                       # slightly degraded
    return 0                           # up


def record(db, sev, now):
    con = sqlite3.connect(db, timeout=10)
    con.execute("CREATE TABLE IF NOT EXISTS status_series("
                "tier TEXT, bucket INTEGER, sev INTEGER, PRIMARY KEY(tier, bucket))")
    for name, step, win in TIERS:
        bucket = (now // step) * step
        # worst-per-bucket: keep the MAX severity seen in the window
        con.execute("INSERT INTO status_series(tier,bucket,sev) VALUES(?,?,?) "
                    "ON CONFLICT(tier,bucket) DO UPDATE SET sev=MAX(sev, excluded.sev)",
                    (name, bucket, sev))
        con.execute("DELETE FROM status_series WHERE tier=? AND bucket < ?", (name, now - win))
    con.commit()
    con.close()


def export(db, out, now, current):
    con = sqlite3.connect(db, timeout=10)
    rows = con.execute("SELECT tier, bucket, sev FROM status_series ORDER BY bucket").fetchall()
    con.close()
    series = {"hour": [], "day": []}
    for tier, bucket, sev in rows:
        if tier in series:
            series[tier].append([bucket, sev])
    doc = {"day": series["day"], "hour": series["hour"],
           "current": current, "levels": LEVELS, "generated_at": now}
    tmp = out + ".tmp"
    with open(tmp, "w") as f:
        json.dump(doc, f)
    os.replace(tmp, out)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--relay-health", required=True)
    ap.add_argument("--peers", default="")
    ap.add_argument("--db", required=True)
    ap.add_argument("--out", required=True)
    a = ap.parse_args()
    now = int(time.time())
    rh = load(a.relay_health)
    peers = load(a.peers) if a.peers else None
    sev = severity(rh, peers)
    if sev is None:
        # nothing to record this cycle; still (re)export so the file exists
        export(a.db, a.out, now, None)
        sys.stderr.write("relay-status-record: severity unknown this cycle (no relays/canaries)\n")
        return
    record(a.db, sev, now)
    export(a.db, a.out, now, sev)
    sys.stderr.write(f"relay-status-record: {LEVELS[sev]} (sev={sev})\n")


if __name__ == "__main__":
    main()
