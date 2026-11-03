#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Render every Prometheus-textfile template in EVERY branch it has, and lint
# the output as exposition format.
#
# WHY THIS EXISTS — a bug that reached a live goatnet . The ADR
# 1128 link-capacity template used `{%- endif %}` whitespace trimming. When
# BOTH sample branches were skipped — which is the `source: unknown` default,
# i.e. every host that has not been characterised — the trim ate the newline
# after `# TYPE`, producing:
#
#   # TYPE goat_link_capacity_bytes_per_second gauge# HELP goat_link_capacity_declared ...
#
# node_exporter's textfile collector refused the whole file and raised
# node_textfile_scrape_error 1. The metric was simply absent, so every
# percent-of-link panel stayed blank — which is ALSO what the honest
# undeclared-capacity state looks like. A rendering bug and the correct
# behaviour were indistinguishable on the dashboard.
#
# The template HAD been eyeballed after rendering. It was eyeballed in the
# `declared` branch, where the emitted sample supplies the missing newline.
# Rendering one branch and reading it is not a check; rendering EVERY branch
# and parsing it is.
#
# Run: ops/ci/check-textfile-exposition.sh

set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

command -v promtool >/dev/null 2>&1 || {
  echo "promtool not on PATH — skipping (CI installs it version-pinned)"
  exit 0
}

TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
fail=0

render_and_check() {
  local name="$1" tpl="$2" ctx="$3"
  python3 - "$tpl" "$ctx" "$TMP/$name.prom" <<'PY'
import datetime, json, sys
import jinja2
tpl, ctx, out = sys.argv[1], sys.argv[2], sys.argv[3]
env = jinja2.Environment(undefined=jinja2.ChainableUndefined)
# the ansible filters these templates use
env.filters['to_datetime'] = lambda s, f: datetime.datetime.strptime(s, f)
open(out, 'w').write(env.from_string(open(tpl).read()).render(**json.loads(ctx)))
PY
  if promtool check metrics < "$TMP/$name.prom" > "$TMP/$name.out" 2>&1; then
    echo "  ok    $name"
  else
    echo "  FAIL  $name"
    sed 's/^/          /' "$TMP/$name.out"
    echo "        --- rendered ---"
    sed 's/^/        | /' "$TMP/$name.prom"
    fail=1
  fi
}

TPL=ops/ansible/roles/node-exporter/templates/link-capacity.prom.j2
echo "link-capacity.prom.j2 — every branch:"
# The branch matrix is the point. `unknown` is the DEFAULT and the one that
# broke; down-only and up-only each exercise a single sample line.
render_and_check "cap-unknown"   "$TPL" \
  '{"node_exporter_link_capacity":{"down_mbps":0,"up_mbps":0,"source":"unknown","asserted_on":""}}'
render_and_check "cap-declared"  "$TPL" \
  '{"node_exporter_link_capacity":{"down_mbps":150,"up_mbps":20,"source":"declared","asserted_on":"2026-08-13"}}'
render_and_check "cap-measured"  "$TPL" \
  '{"node_exporter_link_capacity":{"down_mbps":940.5,"up_mbps":880,"source":"measured","asserted_on":"2026-08-13"}}'
render_and_check "cap-down-only" "$TPL" \
  '{"node_exporter_link_capacity":{"down_mbps":100,"up_mbps":0,"source":"declared","asserted_on":""}}'
render_and_check "cap-up-only"   "$TPL" \
  '{"node_exporter_link_capacity":{"down_mbps":0,"up_mbps":25,"source":"declared","asserted_on":"2026-08-13"}}'
render_and_check "cap-no-date"   "$TPL" \
  '{"node_exporter_link_capacity":{"down_mbps":50,"up_mbps":10,"source":"declared","asserted_on":""}}'

if [ "$fail" -ne 0 ]; then
  echo
  echo "textfile exposition check FAILED — node_exporter would set"
  echo "node_textfile_scrape_error 1 and serve NONE of the file's metrics."
  exit 1
fi
echo "textfile exposition check OK"
