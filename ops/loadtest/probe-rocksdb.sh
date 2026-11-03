#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# probe-rocksdb.sh — measure the rocksdb sink (the live cache tier).
#
# Reads back the load-test keyspace through the router's REST plugin (the same
# GET path the inspector/portal use; mTLS-required tcp/7447 can't be probed
# without a client cert, so REST is the read surface — see the role's
# docker-compose healthcheck). Two modes:
#
#   --completeness <run-id> <sent>   GET loadtest/<run-id>/** ; emit a summary:
#                                    distinct stored seq vs published seq
#                                    (== "does rocksdb keep a complete copy?").
#
#   --lag <key>                      GET one key; emit publish->rocksdb-visible
#                                    lag for the freshest message (the
#                                    orchestrator calls this each tick; lag
#                                    resolution is bounded by the tick interval).
#
# Output: one JSON object per call, on stdout (the orchestrator appends to the
# run evidence JSONL).

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib-loadtest.sh
source "${HERE}/lib-loadtest.sh"

now_ns() { date +%s%N; }

mode="${1:-}"
case "$mode" in
  --completeness)
    run_id="${2:?run-id required}"; sent="${3:?sent count required}"
    keyexpr="${LOADTEST_KEY_PREFIX}/${run_id}/**"
    # REST GET returns a JSON array of {key, value, encoding, time}. Pull the
    # trailing seq out of each key; count distinct.
    body="$(curl -fsS "${LOADTEST_REST_BASE}/${keyexpr}" 2>/dev/null || echo '[]')"
    stats="$(printf '%s' "$body" | jq -c --argjson sent "$sent" '
      [ .[].key | capture("/(?<seq>[0-9]+)$") | .seq | tonumber ] as $seqs
      | ($seqs | unique) as $u
      | { stored_distinct: ($u | length),
          min_seq: ($u | min),
          max_seq: ($u | max),
          sent: $sent,
          completeness: (if $sent > 0 then (($u | length) / $sent) else null end) }
    ' 2>/dev/null || echo '{"stored_distinct":0,"sent":'"$sent"',"completeness":0}')"
    printf '{"probe":"rocksdb","kind":"completeness","ts_ns":%s,"run_id":"%s",%s}\n' \
      "$(now_ns)" "$run_id" "${stats#\{}"
    ;;
  --lag)
    key="${2:?key required}"
    obs="$(now_ns)"
    body="$(curl -fsS "${LOADTEST_REST_BASE}/${key}" 2>/dev/null || echo '[]')"
    # Present? Pull t_ns out of the stored value (our JSON payload).
    printf '%s' "$body" | jq -c --arg key "$key" --argjson obs "$obs" '
      if length > 0 then
        (.[0].value | (if type=="string" then fromjson else . end)) as $v
        | { probe:"rocksdb", kind:"lag", key:$key, present:true,
            t_ns: ($v.t_ns), observed_ns:$obs,
            lag_ns: ($obs - ($v.t_ns)) }
      else
        { probe:"rocksdb", kind:"lag", key:$key, present:false, observed_ns:$obs }
      end' 2>/dev/null \
      || printf '{"probe":"rocksdb","kind":"lag","key":"%s","present":false,"observed_ns":%s}\n' "$key" "$obs"
    ;;
  *)
    loadtest::die "usage: probe-rocksdb.sh --completeness <run-id> <sent> | --lag <key>"
    ;;
esac
