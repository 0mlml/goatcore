#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# ADR 1128 — keep every bandwidth-attribution producer in step with the CLOSED
# category grammar in ops/observability/bandwidth-categories.yaml.
#
# The grammar is closed so that a typo shows up as a visible odd row rather
# than a vanished one. That property only holds if nothing invents a
# key the grammar has not heard of, which is what this guard enforces:
#
#   1. every `category:` in a logship route rule is a known workload key
#   2. every `category:` in a node-net-health baseline rule is a known
#      workload key
#   3. every category the Grafana dashboard hard-codes is a known key
#   4. the two planes do not share keys — a key that means one thing in the
#      transport plane and another in the workload plane is exactly the
#      confusion the two-plane split exists to prevent
#
# Run: ops/ci/check-bandwidth-categories.sh

set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

GRAMMAR=ops/observability/bandwidth-categories.yaml
DASHBOARD=ops/compose/goat-obs/grafana/dashboards/mesh-bandwidth-attribution.json

[ -f "$GRAMMAR" ] || { echo "FAIL: $GRAMMAR missing"; exit 1; }

fail=0
note() { echo "  $*"; }

# --- read the grammar -------------------------------------------------------
WORKLOAD=()
while IFS= read -r k; do
  [ -n "$k" ] && WORKLOAD+=("$k")
done < <(python3 -c "
import yaml
g = yaml.safe_load(open('$GRAMMAR'))
print('\n'.join(c['key'] for c in g['workload']))
")
TRANSPORT=()
while IFS= read -r k; do
  [ -n "$k" ] && TRANSPORT+=("$k")
done < <(python3 -c "
import yaml
g = yaml.safe_load(open('$GRAMMAR'))
print('\n'.join(c['key'] for c in g['transport']))
")

# ${WORKLOAD+...}: bash 3.2 under set -u dies expanding an empty array.
known_workload() { printf '%s\n' ${WORKLOAD+"${WORKLOAD[@]}"} | grep -qxF "$1"; }

echo "grammar: ${#WORKLOAD[@]} workload keys, ${#TRANSPORT[@]} transport keys"

# --- 4. planes must not share keys ------------------------------------------
overlap=$(comm -12 <(printf '%s\n' ${WORKLOAD+"${WORKLOAD[@]}"} | sort) \
                   <(printf '%s\n' ${TRANSPORT+"${TRANSPORT[@]}"} | sort))
if [ -n "$overlap" ]; then
  echo "FAIL: keys used in BOTH planes — the two-plane split stops meaning anything:"
  note "$overlap"
  fail=1
fi

# --- 1 + 2. every producer's category: is a known workload key --------------
# Scoped to the TWO variables that carry bandwidth categories. A bare grep for
# `category:` is wrong here and was caught being wrong: `isitup_services` has
# carried its own unrelated category vocabulary (browse/platform/admin) since
# long before this ADR, and a whole-tree grep condemns all of it.
# mktemp, not `done < <(python3 <<PY …)`: bash 3.2 scans process substitution
# lexically for the closing `)`, and an apostrophe inside the heredoc body
# (e.g. a Python comment) unbalances that scan — "no closing `)'" at runtime.
# A pipe is no good either: the while body sets fail=1, which a piped subshell
# would discard.
hits_tmp=$(mktemp)
trap 'rm -f "$hits_tmp"' EXIT
python3 - <<'PY' > "$hits_tmp"
import glob, yaml
# The only two variables whose `category:` is a bandwidth category.
KEYS = ("logship_http_category_rules", "net_baseline_rules")
files = ["ops/ansible/roles/goat-logship/defaults/main.yml",
         "ops/ansible/roles/node-net-health/defaults/main.yml"]
files += glob.glob("ops/ansible/group_vars/**/*.yml", recursive=True)
for f in files:
    try:
        doc = yaml.safe_load(open(f)) or {}
    except Exception:
        continue                      # not our file to police if it won't parse
    if not isinstance(doc, dict):
        continue
    for k in KEYS:
        for rule in doc.get(k) or []:
            if not isinstance(rule, dict):
                continue
            c = rule.get("category")
            if isinstance(c, str) and c and "{{" not in c:
                print(f"{f}|{c}")
    # the fallback is a category too
    fb = doc.get("logship_http_category_fallback")
    if isinstance(fb, str) and fb and "{{" not in fb:
        print(f"{f}|{fb}")
PY
while IFS= read -r hit; do
  file=${hit%%|*}; val=${hit#*|}
  if ! known_workload "$val"; then
    echo "FAIL: unknown bandwidth category '$val'"
    note "$file"
    note "add it to $GRAMMAR or fix the typo"
    fail=1
  fi
done < "$hits_tmp"

# --- 3. dashboard keys ------------------------------------------------------
# The dashboard is allowed to NAME categories (ordering, colour assignment). It
# is not allowed to name one that does not exist.
if [ -f "$DASHBOARD" ]; then
  vals_tmp=$(mktemp)
  trap 'rm -f "$hits_tmp" "$vals_tmp"' EXIT
  python3 - <<PY > "$vals_tmp"
import json, re
d = json.load(open("$DASHBOARD"))
# Category names appear in fixed-colour overrides and in legend ordering.
found = set()
def walk(o):
    if isinstance(o, dict):
        for k, v in o.items():
            if k in ("matcher", "options") and isinstance(v, dict):
                n = v.get("options") if k == "matcher" else None
                if isinstance(n, str):
                    found.add(n)
            walk(v)
    elif isinstance(o, list):
        for i in o:
            walk(i)
walk(d)
# only consider things that look like grammar keys (kebab, no spaces)
for f in sorted(found):
    if re.fullmatch(r"[a-z][a-z0-9-]+", f or ""):
        print(f)
PY
  while IFS= read -r val; do
    known_workload "$val" || printf '%s\n' ${TRANSPORT+"${TRANSPORT[@]}"} | grep -qxF "$val" || {
      echo "FAIL: dashboard references unknown category '$val'"
      note "$DASHBOARD"
      fail=1
    }
  done < "$vals_tmp"
else
  echo "note: $DASHBOARD not present yet — skipping dashboard check"
fi

# --- every grammar entry must carry its why ---------------------------------
missing_why=$(python3 -c "
import yaml
g = yaml.safe_load(open('$GRAMMAR'))
for plane in ('transport','workload'):
    for c in g[plane]:
        if not str(c.get('why','')).strip():
            print(f\"{plane}/{c['key']}\")
")
if [ -n "$missing_why" ]; then
  echo "FAIL: grammar entries with no 'why' — an unexplained category is"
  echo "      indistinguishable from a mistake:"
  note "$missing_why"
  fail=1
fi

if [ "$fail" -ne 0 ]; then
  echo
  echo "bandwidth category check FAILED"
  exit 1
fi
echo "bandwidth category check OK"
