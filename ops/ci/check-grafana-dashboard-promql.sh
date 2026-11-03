#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Parse-check every PromQL expression in every committed Grafana dashboard.
#
# WHY: the suite has 19 dashboards and CI validated the PROMETHEUS RULES in
# them and nothing else — a dashboard panel with a syntax error ships happily,
# renders an empty graph, and reads exactly like "no data", which on a capacity
# dashboard is the most dangerous possible failure: an operator concludes the
# link is idle. goat-obs-checks.yml runs promtool over rules/ and tests/; this
# closes the dashboards/ half.
#
# HOW: Grafana template variables are not PromQL, so they are substituted with
# representative literals before parsing, then the expressions are wrapped in a
# synthetic recording-rule file and handed to `promtool check rules`. That
# reuses the same parser the server will use rather than approximating it.
#
# PARSING IS NOT ENOUGH — a wrong metric name parses perfectly:
# minio_bucket_quota_bytes for MinIO's minio_bucket_quota_total_bytes,
# traefik_entrypoint_open_connections for Traefik v3's traefik_open_connections,
# minio_bucket_oldest_object_seconds for a metric with no emitter at all. Each
# renders an empty graph, so the panel an operator opens to investigate a firing
# alert reads "No data".
#
# So the second half joins every metric name in every panel against
# ops/pins/obs-metric-sources.yaml — the same inventory
# check-obs-rules-have-scrapers.sh uses for ALERT RULES — plus the
# recording rules this repo's own rule files define. A name matching neither
# fails the guard.
#
# WHAT THAT HALF DOES NOT CATCH, stated plainly so nobody reads more into a
# green run than it earns. The inventory is keyed on metric-family PREFIXES,
# so it catches a name from a family nothing produces and NOT a misspelled or
# renamed name inside a family that IS produced, such as the wrong names above:
# they sit inside `minio_bucket_` and `traefik_`, families the fleet produces.
# What it does catch is a whole family with no producer, and — via pass 3 — a
# dashboard the walker cannot read at all.
#
# Catching those needs EXACT metric names, i.e. a committed snapshot of the
# names a live Prometheus actually holds. That snapshot has to be the UNION
# across goatnets before it can gate anything: relay-fleet-health's goat_relay_*
# and mesh-bandwidth-attribution's goat_http_* are correct references to
# producers that are switched off on some goatnets, so a snapshot from one goatnet would
# fail correct dashboards. Not built — it needs a reading from more than one
# goatnet, which is the whole cost of it.
#
# THIRD: a dashboard that yields ZERO expressions passes silently. named-processes.json
# stores panels under the pre-Grafana-5 `rows[]` key, so the old walker saw
# nothing and promtool was handed an empty rule file. Both are closed below:
# rows[] is walked, and zero expressions is a failure unless the dashboard has
# no queries at all by design.
#
# Run: ops/ci/check-grafana-dashboard-promql.sh [dashboard.json ...]

set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

# The parse half needs promtool; the metric-name join (pass 2) does not, so a
# missing promtool narrows this guard rather than disabling it.
HAVE_PROMTOOL=1
command -v promtool >/dev/null 2>&1 || {
  echo "promtool not on PATH — parse check skipped (CI installs it "
  echo "version-pinned); the metric-name join below still runs."
  HAVE_PROMTOOL=0
}

DASHBOARD_DIR=ops/compose/goat-obs/grafana/dashboards

DASHBOARDS=("$@")
if [ ${#DASHBOARDS[@]} -eq 0 ]; then
  DASHBOARDS=()
  while IFS= read -r d; do
    DASHBOARDS+=("$d")
  done < <(find "$DASHBOARD_DIR" -name '*.json' | sort)

  # Fail-closed on empty discovery. find writes its complaint to stderr and
  # a process substitution's exit status is discarded, so a renamed or moved
  # dashboard tree would otherwise leave DASHBOARDS empty and this guard
  # would print "OK" having parsed nothing -- exactly the silent pass it
  # exists to prevent. Same treatment as the zero-group_vars-dirs branch in
  # check-ansible-inventory-groups.sh.
  if [ "${#DASHBOARDS[@]}" -eq 0 ]; then
    echo "FAIL: found zero dashboards under $DASHBOARD_DIR -- discovery is broken." >&2
    echo "      Either the tree moved (update DASHBOARD_DIR) or the checkout is incomplete." >&2
    exit 1
  fi
fi

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

fail=0
for d in "${DASHBOARDS[@]}"; do
  python3 - "$d" "$TMP" <<'PY'
import json, re, sys, os
path, tmp = sys.argv[1], sys.argv[2]
d = json.load(open(path))

# Grafana-isms -> representative PromQL literals. Substitution is textual and
# deliberately dumb: the goal is to prove the EXPRESSION SHAPE parses, not to
# reproduce what Grafana will interpolate at render time.
SUBS = [
    (r'\$__rate_interval', '5m'),
    (r'\$__interval', '1m'),
    (r'\$__range', '6h'),
    (r'\[\$?\{?__auto\}?\]', '[5m]'),
    # A variable used as a RANGE must become a duration, not a label literal:
    # named-processes.json ranges over [$interval], and substituting the
    # generic 'x' below yields [x], which is not a duration. This rule has to
    # run before the two generic ones.
    (r'\[\s*\$\{?\w+(?::[a-z]+)?\}?\s*\]', '[5m]'),
    (r'\$\{(\w+)(:[a-z]+)?\}', 'x'),   # ${var} / ${var:regex}
    (r'\$[a-zA-Z_]\w*', 'x'),          # $host, $category, ...
]

def panels(o):
    # panels[] is the modern schema; rows[] is pre-Grafana-5 and still renders
    # (Grafana migrates it at load), so a dashboard on the old schema has real
    # queries the old walker never saw.
    for p in o.get('panels', []) or []:
        yield p
        for q in p.get('panels', []) or []:
            yield q
    for r in o.get('rows', []) or []:
        for p in r.get('panels', []) or []:
            yield p
            for q in p.get('panels', []) or []:
                yield q

rows, raw_rows = [], []
for p in panels(d):
    for t in p.get('targets', []) or []:
        e = t.get('expr')
        if not e or not e.strip():
            continue
        raw_rows.append((p.get('title', '?'), t.get('refId', '?'), e))
        for pat, rep in SUBS:
            e = re.sub(pat, rep, e)
        rows.append((p.get('title', '?'), t.get('refId', '?'), e))

name = os.path.basename(path).replace('.json', '')
out = [f"# synthetic: {path}", "groups:", f"  - name: {name}", "    rules:"]
for i, (title, ref, e) in enumerate(rows):
    safe = json.dumps(e)
    out.append(f"      # panel: {title} [{ref}]")
    out.append(f"      - record: check:panel{i}")
    out.append(f"        expr: {safe}")
open(os.path.join(tmp, name + '.rules.yml'), 'w').write("\n".join(out) + "\n")

# Raw (un-substituted) expressions for the metric-name join in the second pass.
raw = [{"panel": t, "ref": r, "expr": e} for t, r, e in raw_rows]
json.dump(raw, open(os.path.join(tmp, name + '.exprs.json'), 'w'))
print(f"{path}: {len(rows)} expressions")
PY

  name=$(basename "$d" .json)
  [ "$HAVE_PROMTOOL" -eq 1 ] || continue
  if ! promtool check rules "$TMP/${name}.rules.yml" >"$TMP/${name}.out" 2>&1; then
    echo "FAIL: PromQL parse error in $d"
    # Echo the panel comment nearest each reported line so the failure names a
    # PANEL, not a line number in a file that does not exist in the repo.
    sed 's/^/    /' "$TMP/${name}.out"
    fail=1
  fi
done

# ---------------------------------------------------------------------------
# PASS 2 — every metric name a panel queries has a producer, and every
#          dashboard contributed at least one expression.
# ---------------------------------------------------------------------------
if ! python3 - "$TMP" <<'PY'
import glob, json, os, re, sys

tmp = sys.argv[1]
try:
    import yaml
except ImportError:
    print("PyYAML not installed — skipping the metric-name join "
          "(CI installs it; a local run without it checks parsing only)")
    raise SystemExit(0)

INV = "ops/pins/obs-metric-sources.yaml"
RULES_GLOB = "ops/compose/goat-obs/prometheus/rules/*.yml"

# The join needs the source inventory. A tree without it — a test sandbox, or
# this script copied somewhere on its own — gets the parse check and says so,
# rather than failing on a file it was never given. Same contract as the
# missing-PyYAML branch above. Note this is deliberately NOT the fail-closed
# treatment that zero-dashboard discovery gets: discovering no dashboards means
# the guard silently checked nothing it was pointed at, whereas a missing
# inventory means one of two checks cannot run and the other still did.
if not os.path.exists(INV):
    print(f"{INV} not present — metric-name join skipped (parse check still ran)")
    raise SystemExit(0)

inv = yaml.safe_load(open(INV)) or {}
prefixes = list(inv.get("always_available") or [])
prefixes += [s["prefix"] for s in (inv.get("sources") or []) if s.get("prefix")]

# Recording rules are produced by this repo's own rule files, not scraped, so
# they will never appear in the source inventory. Read them from the rule files
# that ship beside the dashboards.
recorded = set()
for f in glob.glob(RULES_GLOB):
    for m in re.finditer(r"^\s*-\s*record:\s*(\S+)\s*$", open(f).read(), re.M):
        recorded.add(m.group(1).strip("\"'"))

prefixes = sorted(set(prefixes), key=len, reverse=True)

def covered(n):
    if n in recorded:
        return True
    for p in prefixes:
        if p.endswith("*") and n.startswith(p[:-1]):
            return True
        if p.endswith("_") and n.startswith(p):
            return True
        if n == p:
            return True
    return False

# PromQL functions and keywords are not metric names.
FUNCS = {
    "sum", "rate", "irate", "avg", "max", "min", "count", "without", "increase",
    "topk", "bottomk", "histogram_quantile", "histogram_count", "label_replace",
    "label_join", "clamp", "clamp_max", "clamp_min", "delta", "idelta", "deriv",
    "predict_linear", "time", "vector", "scalar", "absent", "absent_over_time",
    "count_values", "quantile", "stddev", "stdvar", "last_over_time",
    "avg_over_time", "max_over_time", "min_over_time", "sum_over_time",
    "count_over_time", "stddev_over_time", "quantile_over_time",
    "present_over_time", "changes", "resets", "round", "abs", "ceil", "floor",
    "and", "or", "unless", "bool", "sort", "sort_desc", "timestamp",
    "holt_winters", "sgn", "exp", "ln", "log2", "log10", "sqrt", "by", "on",
    "ignoring", "group_left", "group_right", "offset", "hour", "day_of_week",
}
QUOTED = re.compile(r'"[^"]*"|\'[^\']*\'')
GVAR = re.compile(r"\$\{[^}]*\}|\$[A-Za-z_][A-Za-z0-9_]*")
CLAUSE = re.compile(r"\b(?:by|without|on|ignoring|group_left|group_right)\s*\([^()]*\)")
SELECTOR = re.compile(r"\{[^{}]*\}")
IDENT = re.compile(r"(?<![\w:.])([a-zA-Z_][a-zA-Z0-9_]*(?::[a-zA-Z0-9_:]+)+"
                   r"|[a-zA-Z_][a-zA-Z0-9_]*)")

def metric_names(expr):
    # Quoted literals carry label VALUES and the label-name arguments to
    # label_replace/label_join; Grafana variables are not PromQL at all;
    # by()/on()/group_left() clauses and {...} selectors carry label NAMES.
    e = GVAR.sub(" ", QUOTED.sub(" ", expr))
    for _ in range(6):
        e2 = SELECTOR.sub(" ", CLAUSE.sub(" ", e))
        if e2 == e:
            break
        e = e2
    out = set()
    for m in IDENT.finditer(e):
        n = m.group(1)
        if n in FUNCS or n.isdigit() or n.startswith("__"):
            continue
        if re.fullmatch(r"\d+[smhdwy]", n):
            continue
        if e[m.end():m.end() + 1] == "(":      # a function call, not a metric
            continue
        out.add(n)
    return out

bad = 0
for f in sorted(glob.glob(os.path.join(tmp, "*.exprs.json"))):
    name = os.path.basename(f)[:-len(".exprs.json")]
    rows = json.load(open(f))
    if not rows:
        print(f"FAIL: {name}.json contributed ZERO expressions.")
        print("      Either the dashboard has no queries, or its panels sit "
              "under a key this walker does not read.")
        bad += 1
        continue
    unknown = {}
    for r in rows:
        for n in metric_names(r["expr"]):
            if not covered(n):
                unknown.setdefault(n, (r["panel"], r["ref"]))
    for n, (panel, ref) in sorted(unknown.items()):
        print(f"FAIL: {name}.json panel {panel!r} [{ref}] queries {n!r}, "
              f"which no source in {INV} produces and no rule file records.")
        bad += 1

if bad:
    print()
    print(f"{bad} metric-name / coverage failure(s).")
    print(f"A name that nothing produces renders an empty panel, which reads "
          f"exactly like a quiet system.")
    print(f"Fix the name, or add its producer to {INV}.")
    raise SystemExit(1)
print("dashboard metric-name join OK")
PY
then
  fail=1
fi

if [ "$fail" -ne 0 ]; then
  echo
  echo "dashboard PromQL check FAILED"
  exit 1
fi
echo "dashboard PromQL check OK"
