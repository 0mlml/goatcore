#!/usr/bin/env python3
"""zenoh-health-to-prom — bridge the zenoh-router band into Prometheus for alerting.

The relay-health ingest stores the router's service health (zenoh-health.json) and
a wedge-event timeline (zenoh-wedges.json). Grafana/alerting need those as
Prometheus metrics; this reads the two JSON files and writes `goat_zenoh_*`
node-exporter textfile metrics. Drop the .prom in a node-exporter
`--collector.textfile.directory` (the obs stack already runs that collector).

WHY THIS EXISTS (2026-07-28 incident): the container-health `FabricRouterUnhealthy`
alert has `for: 10m`, but the wedge-watch watchdog auto-restarts a wedged router in
~1-2 min, so `unhealthy` never persists 10m and the alert never fires — the router
wedges hourly with PagerDuty silent. The load-bearing signal to alert on is the
RESTART RATE (goat_zenoh_restarts_24h), which this exposes.

Usage:
  zenoh-health-to-prom.py --health <ingest>/zenoh-health.json \
      --wedges <ingest>/zenoh-wedges.json --out <textfile_dir>/zenoh-health.prom
Atomic write (tmp + rename) so node-exporter never scrapes a half-written file.
"""
import argparse
import json
import os
import sys
import time


def load(path):
    try:
        with open(path) as f:
            return json.load(f)
    except Exception:
        return {}


def num(v):
    try:
        return float(v)
    except (TypeError, ValueError):
        return None


def metric(lines, name, value, labels, mtype="gauge", help_text=None):
    if value is None:
        return
    if help_text and name not in metric._seen:
        lines.append(f"# HELP {name} {help_text}")
        lines.append(f"# TYPE {name} {mtype}")
        metric._seen.add(name)
    lab = ",".join(f'{k}="{v}"' for k, v in labels.items())
    lines.append(f"{name}{{{lab}}} {value}")


metric._seen = set()


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--health", required=True)
    ap.add_argument("--wedges", default="")
    ap.add_argument("--fabric", default="")
    ap.add_argument("--out", default="")
    a = ap.parse_args()

    health = load(a.health).get("routers", {})
    wedges = load(a.wedges).get("routers", {}) if a.wedges else {}
    # zenoh-fabric.json is a single record (one router per sampler); tolerate either
    # a bare record or a {routers:{...}} wrapper.
    fabric_raw = load(a.fabric) if a.fabric else {}
    fabric = fabric_raw.get("routers", {}) if "routers" in fabric_raw else (
        {fabric_raw.get("router", "zagros"): fabric_raw} if fabric_raw else {})
    now = int(time.time())
    L = []
    for name, doc in sorted(health.items()):
        z = (doc or {}).get("zenoh", {})
        c, adm, w, cap = (z.get("container") or {}, z.get("admission") or {},
                          z.get("wedge") or {}, z.get("capacity") or {})
        lab = {"router": name}
        metric(L, "goat_zenoh_healthy", 1 if c.get("health") == "healthy" else 0, lab,
               help_text="1 if the router container HEALTHCHECK is healthy")
        metric(L, "goat_zenoh_open_ms", num(adm.get("open_ms")), lab,
               help_text="TCP/TLS admission-proxy attempt latency in ms")
        open_ok = adm.get("open_ok")
        metric(L, "goat_zenoh_open_ok", 1 if open_ok is True else 0 if open_ok is False else None, lab,
               help_text="1 if the TCP/TLS admission proxy returned successfully; does not confirm application admission")
        reason = adm.get("open_reason")
        if reason in ("ok", "timeout", "connection_refused", "connection_error", "tls_error",
                      "client_config_error", "probe_error", "not_configured"):
            metric(L, "goat_zenoh_open_probe_status", 1, {**lab, "reason": reason},
                   help_text="bounded result of the TCP/TLS admission proxy")
        metric(L, "goat_zenoh_rest_ok", 1 if adm.get("rest_ok") is True else 0, lab,
               help_text="1 if the REST adminspace answered")
        metric(L, "goat_zenoh_close_wait", num(w.get("close_wait")), lab,
               help_text="CLOSE-WAIT sockets in the router netns")
        metric(L, "goat_zenoh_fd_used", num(cap.get("fd_used")), lab)
        metric(L, "goat_zenoh_fd_cap", num(cap.get("fd_cap")), lab)
        metric(L, "goat_zenoh_uptime_seconds", num(c.get("uptime_s")), lab,
               help_text="router container uptime (resets on a watchdog wedge-restart)")
        metric(L, "goat_zenoh_report_timestamp_seconds", num(doc.get("received_at")), lab,
               help_text="unix time of the last stored report (staleness canary)")
        wd = wedges.get(name, {})
        metric(L, "goat_zenoh_restarts_24h", num(wd.get("restarts_24h")) or 0, lab,
               help_text="watchdog wedge-recoveries (router restarts) in 24h — the wedge-frequency signal")
        metric(L, "goat_zenoh_restarts_7d", num(wd.get("restarts_7d")) or 0, lab)
        metric(L, "goat_zenoh_currently_wedged", 1 if wd.get("currently_wedged") else 0, lab,
               help_text="1 if the router is deadlocked right now (not yet auto-restarted)")
    # Fabric cardinality/load leading indicators (zenoh-fabric-sampler.sh, wedge
    # instrumentation). Cardinality — NOT message rate — is the axis that wedges the
    # router; these are the precursor signals so an alarm can fire before the stall.
    for name, f in sorted(fabric.items()):
        lab = {"router": name}
        metric(L, "goat_zenoh_stored_keys_total", num(f.get("stored_keys_total")), lab,
               help_text="total stored keys (LOWER BOUND on routing edges — not the edge count)")
        metric(L, "goat_zenoh_admin_declarations", num(f.get("admin_declarations")), lab,
               help_text="admin-space declaration count (wildcard-collapsed)")
        metric(L, "goat_zenoh_admin_walk_ms", num(f.get("admin_walk_ms")), lab,
               help_text="full admin `**` read-hold cost ms (H1 read-hold precursor)")
        metric(L, "goat_zenoh_scoped_get_ms", num(f.get("scoped_get_ms")), lab,
               help_text="single-key admin GET ms (contention baseline; varies when the routing lock is hot)")
        metric(L, "goat_zenoh_declare_rate_proxy", num(f.get("declare_rate_proxy_per_s")), lab,
               help_text="|delta stored keys|/s — a NET-CHANGE proxy for declare churn (router log is authoritative)")
        # per-owner cardinality attribution — the abuser detector (labelled series).
        # The crawler (zenoh-cardinality-budget-and-lazy-crawler.md §4) also
        # classifies each owner against the topic-budget tiers, so the budget
        # is ALERTABLE on the tier the owner actually occupies rather than on a
        # single hardcoded threshold. `budget_state` is absent on rows written
        # by the retired shell sampler — those keep the count-only series.
        #
        # Layer 2b (allocation): when the crawler runs with a budget source it
        # emits `owners_budget` — EVERY non-skipped owner with its granted
        # allowance + grant-relative state (tier list gains "granted" = within
        # a platform-admin allowance above the request tier). That surface is
        # then authoritative for budget_state; the top-N loop keeps only the
        # count attribution, so no owner gets two conflicting state series.
        tiers = ("self", "request", "granted", "exception", "unmeasured")
        owners_budget = f.get("owners_budget") or []
        for row in (f.get("top_owners_by_stored_keys") or [])[:8]:
            owner = str(row.get("owner", "?"))
            metric(L, "goat_zenoh_owner_stored_keys", num(row.get("stored_keys")),
                   {**lab, "owner": owner},
                   help_text="stored keys per owner namespace — cardinality attribution (the user93x/video detector)")
            state = row.get("budget_state")
            if state and not owners_budget:
                # One series per tier, 1/0 — a range query can answer "who is in
                # exception tier" without re-deriving the thresholds in PromQL.
                for tier in tiers:
                    metric(L, "goat_zenoh_owner_budget_state",
                           1 if state == tier else 0,
                           {**lab, "owner": owner, "tier": tier},
                           help_text="topic-budget tier the owner's observed cardinality lands in (1=current tier); reconciliation, never interception")
            if row.get("truncated"):
                metric(L, "goat_zenoh_owner_count_truncated", 1, {**lab, "owner": owner},
                       help_text="1 = the owner's count is a LOWER BOUND (enumeration hit the lazy threshold); never treat as a total")
        for row in owners_budget:
            owner = str(row.get("owner", "?"))
            state = row.get("budget_state")
            if state:
                for tier in tiers:
                    metric(L, "goat_zenoh_owner_budget_state",
                           1 if state == tier else 0,
                           {**lab, "owner": owner, "tier": tier},
                           help_text="topic-budget tier the owner's observed cardinality lands in (1=current tier); reconciliation, never interception")
            granted = num(row.get("granted"))
            if granted:
                # Observed-vs-granted for the dashboard: pair with
                # goat_zenoh_owner_stored_keys (top-N) or owners_budget's own
                # stored_keys via goat_zenoh_owner_budget_observed below.
                metric(L, "goat_zenoh_owner_budget_granted", granted,
                       {**lab, "owner": owner},
                       help_text="granted topic-budget allowance (distinct keys) for this owner; absent = no grant on record")
                metric(L, "goat_zenoh_owner_budget_observed", num(row.get("stored_keys")),
                       {**lab, "owner": owner},
                       help_text="observed distinct keys for a granted owner (lower bound when truncated) — the observed half of observed-vs-granted")
        # Budget-source health: 1 = allowances fetched live this cycle. A dead
        # source silently blanks the exception tier (owners read unmeasured),
        # so this is the alertable guard. Absent entirely in legacy mode.
        bss = f.get("budget_source_state")
        if bss and bss != "none":
            metric(L, "goat_zenoh_budget_source_live", 1 if bss == "live" else 0, lab,
                   help_text="1 = topic-budget allowance source fetched live this cycle (0 = stale/unavailable — grant changes are NOT landing)")
        # Crawler zenoh-session health. The crawler now stays UP without a
        # zenoh session when it has a usable rocksdb source (census + budget
        # classification are router-independent), which is the right behaviour
        # — but it removed the only signal that used to say the router was
        # gone: the process no longer exits, so container health stays green.
        # Without this gauge the degraded state is visible only as the
        # publisher/liveliness facets going null, i.e. as silence.
        #
        # Mirrors the daemon's own predicate exactly (`!= "unavailable"`)
        # rather than testing `== "live"`, so a future intermediate state
        # ("degraded", "reconnecting", …) reads as live here instead of
        # silently paging. Emitted only when the field is present, so an older
        # crawler's band JSON produces no series at all — and the alert riding
        # on it carries the usual 6h presence guard.
        zss = f.get("zenoh_session_state")
        if zss:
            metric(L, "goat_zenoh_crawler_session_live", 0 if zss == "unavailable" else 1, lab,
                   help_text="1 = the fabric crawler holds a zenoh session (0 = rocksdb-only degraded: census + budgets still measured, publishers/liveliness are NOT)")
        metric(L, "goat_zenoh_fabric_sampled_at_seconds", num(f.get("sampled_at")), lab,
               help_text="unix time of the last fabric sample (staleness canary)")

    metric(L, "goat_zenoh_prom_last_run_timestamp_seconds", now, {},
           help_text="unix time this exporter last ran (staleness guard)")

    out = "\n".join(L) + "\n"
    if a.out:
        tmp = a.out + ".tmp"
        with open(tmp, "w") as f:
            f.write(out)
        os.replace(tmp, a.out)
    else:
        sys.stdout.write(out)
    return 0


if __name__ == "__main__":
    sys.exit(main())
