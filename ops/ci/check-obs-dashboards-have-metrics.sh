#!/usr/bin/env bash
# check-obs-dashboards-have-metrics.sh — a goatnet does not install a dashboard
# whose panels it cannot draw.
#
# THE DEFECT THIS PREVENTS. A Grafana can serve ZERO dashboards when its role
# provisions a datasource and nothing to render with it. Shipping every shared
# dashboard instead is worse — a container dashboard with no cadvisor behind it
# answers "how are the containers" with blank graphs, and a blank graph reads as
# idle, not as "nothing is collecting this".
#
# Same family as check-obs-rules-have-scrapers.sh, one altitude up. That guard
# asks whether a loaded ALERT can fire. This asks whether an installed PANEL can
# draw. Both exist because a consumer of data is declared separately from the
# thing that collects it, and nothing else compares them.
#
# WHY IT SCORES AGAINST DECLARED CONFIG, NOT A LIVE HOST. It has to work for a
# goatnet that does not exist yet — the whole point is that standing up a new
# env cannot repeat this. So it resolves each dashboard's metrics through
# ops/pins/obs-metric-sources.yaml to the JOB that produces them, and asks
# whether the site's obs_scrape_targets names that job.
#
# FAILS ONLY AT ZERO, deliberately. "This dashboard can draw nothing at all" is
# a fact. "This dashboard is only 40% populated" is an editorial call about
# whether a partial view helps or misleads, and a guard that makes editorial
# calls gets argued with and then disabled. Partial coverage is reported as a
# note so the judgement is visible and someone can exclude it on purpose.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

python3 - <<'PY'
import glob, json, os, re, sys

try:
    import yaml
except ImportError:
    print("check-obs-dashboards-have-metrics: SKIP — pyyaml not available")
    sys.exit(0)

ROLE_DEFAULTS = "ops/ansible/roles/obs-host/defaults/main.yml"
DASH_DIR      = "ops/compose/goat-obs/grafana/dashboards"
SOURCES       = "ops/pins/obs-metric-sources.yaml"

PROMQL = {
    "abs","absent","absent_over_time","and","atan2","avg","avg_over_time","bool",
    "bottomk","by","ceil","changes","clamp","clamp_max","clamp_min","count",
    "count_over_time","count_values","day_of_month","day_of_week","day_of_year",
    "days_in_month","delta","deriv","exp","floor","group","group_left",
    "group_right","histogram_quantile","holt_winters","hour","idelta","ignoring",
    "increase","irate","label_join","label_replace","last_over_time","ln","log10",
    "log2","max","max_over_time","min","min_over_time","minute","month","offset",
    "on","or","predict_linear","present_over_time","quantile","quantile_over_time",
    "rate","resets","round","scalar","sgn","sort","sort_desc","sqrt","stddev",
    "stddev_over_time","stdvar","sum","sum_over_time","time","timestamp","topk",
    "unless","vector","without","year","inf","nan",
}


def norm(job):
    return re.sub(r"[-_]", "", str(job).strip().lower())


def load(path):
    with open(path, encoding="utf-8") as fh:
        return yaml.safe_load(fh) or {}


def load_group(group):
    merged = {}
    for path in sorted(glob.glob("ops/ansible/group_vars/%s/*.yml" % group)):
        merged.update(load(path))
    return merged


def metrics_in_dashboard(path):
    """Every metric family the dashboard's panel targets reference."""
    try:
        doc = json.load(open(path, encoding="utf-8"))
    except Exception:
        return set()
    exprs = []

    def walk(o):
        if isinstance(o, dict):
            if isinstance(o.get("expr"), str):
                exprs.append(o["expr"])
            for v in o.values():
                walk(v)
        elif isinstance(o, list):
            for v in o:
                walk(v)

    walk(doc)
    found = set()
    for e in exprs:
        e = re.sub(r'"[^"]*"', " ", e)
        e = re.sub(r"\{[^{}]*\}", " ", e)
        e = re.sub(r"\b(by|without|on|ignoring|group_left|group_right)\s*\([^()]*\)", " ", e)
        for m in re.finditer(r"[a-zA-Z_][a-zA-Z0-9_:]*", e):
            w = m.group(0)
            if w in PROMQL or "_" not in w:
                continue
            if e[m.end():].lstrip().startswith("("):
                continue
            found.add(w)
    return found


sources_doc = load(SOURCES)
always = sources_doc.get("always_available") or []
sources = sorted(sources_doc.get("sources") or [],
                 key=lambda s: len(str(s.get("prefix", ""))), reverse=True)


def servable(metric, jobs, gates):
    # A recording rule's output carries a colon and is computed by Prometheus
    # itself from series it already has — not a scrape target.
    if ":" in metric:
        return True
    if any(metric == a or (a.endswith("_") and metric.startswith(a)) for a in always):
        return True
    for s in sources:
        p = str(s.get("prefix", ""))
        if metric == p or (p.endswith("_") and metric.startswith(p)):
            if norm(s["job"]) not in jobs:
                return False
            gate = s.get("requires_var")
            if gate and not gates.get(gate):
                return False
            return True
    # No declared source. Unknown, not absent — do not count it either way.
    return None


role = load(ROLE_DEFAULTS)
all_dash = sorted(os.path.basename(p) for p in glob.glob(os.path.join(DASH_DIR, "*.json")))

failures, notes, checked = [], [], 0

for obs_dir in sorted(glob.glob("ops/ansible/group_vars/*_obs_hosts")):
    group = os.path.basename(obs_dir)
    obs = load_group(group)

    enabled = obs.get("obs_grafana_dashboards_enabled",
                      role.get("obs_grafana_dashboards_enabled", True))
    if not enabled:
        continue

    site_group = group[: -len("_obs_hosts")] + "_hosts"
    gates = dict(load_group(site_group))
    gates.update(obs)

    jobs = {norm(t.get("job")) for t in (obs.get("obs_scrape_targets") or []) if t.get("job")}
    jobs |= {norm("prometheus"), norm("node-exporter")}
    if obs.get("obs_blackbox_enabled"):
        jobs.add(norm("blackbox-https"))

    excluded = set(obs.get("obs_grafana_dashboards_exclude") or [])
    installed = [d for d in all_dash if d not in excluded]
    if not installed:
        continue
    checked += 1

    for dash in installed:
        mets = metrics_in_dashboard(os.path.join(DASH_DIR, dash))
        known = [m for m in mets if servable(m, jobs, gates) is not None]
        if not known:
            continue
        served = [m for m in known if servable(m, jobs, gates)]
        pct = 100 * len(served) // len(known)
        if not served:
            failures.append(
                "%s installs %s and can draw NONE of it — 0 of %d metric families "
                "have a scraped producer.\n"
                "      Either scrape what feeds it, or add it to "
                "obs_grafana_dashboards_exclude with the reason."
                % (group, dash, len(known)))
        elif pct < 50:
            notes.append("%s: %s only %d%% populated (%d/%d families) — consider "
                         "excluding it, a mostly-blank dashboard reads as idle"
                         % (group, dash, pct, len(served), len(known)))

for n in notes:
    print("  note: %s" % n)

if failures:
    print("check-obs-dashboards-have-metrics: FAIL")
    for f in failures:
        print("  - %s" % f)
    print()
    print("  A panel with no series renders empty, and empty reads as idle rather")
    print("  than as 'nothing is collecting this'. That is the same failure as an")
    print("  alert rule with no metric behind it, one altitude up.")
    sys.exit(1)

if checked == 0:
    print("check-obs-dashboards-have-metrics: OK — no obs group installs dashboards")
else:
    print("check-obs-dashboards-have-metrics: PASS — %d obs group(s), every installed "
          "dashboard can draw something" % checked)
PY
