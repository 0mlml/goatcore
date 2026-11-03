#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# probe-minio.sh — measure the MinIO sink (the durable tier).
#
# Walks the ephemeral bucket, parses the audit-subscriber's NDJSON batch
# objects, and answers the two persistence questions for the durable tier:
#
#   - complete copies?  distinct stored seq / published seq.
#   - publish->storage delay?  per event: (object-PUT time) - (publish t_ns),
#     where publish t_ns is decoded from the audit event's captured payload and
#     the object-PUT time is the object's LastModified. (The audit event's own
#     `ts` is when the subscriber OBSERVED the sample; LastModified is when the
#     batch actually landed in storage — that is the "in storage" moment.)
#
# Reads only — never mutates the bucket. Requires mc + jq + base64.
#
# Usage: probe-minio.sh <run-id> <sent>
# Output: per-event lag JSONL on stdout, then one summary object.

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib-loadtest.sh
source "${HERE}/lib-loadtest.sh"

run_id="${1:?run-id required}"
sent="${2:?sent count required}"
bucket_ref="${LOADTEST_MC_ALIAS}/${LOADTEST_BUCKET}"

command -v mc >/dev/null 2>&1 || loadtest::die "mc (MinIO client) not found"
command -v jq >/dev/null 2>&1 || loadtest::die "jq not found"

# ISO8601 -> epoch ns (best-effort; GNU date or BSD date).
iso_to_ns() {
  local iso="$1" s
  s=$(date -u -d "$iso" +%s 2>/dev/null) || s=$(date -u -j -f "%Y-%m-%dT%H:%M:%S" "${iso%%.*}" +%s 2>/dev/null) || s=0
  echo $(( s * 1000000000 ))
}

tmp_seqs="$(mktemp)"; trap 'rm -f "$tmp_seqs"' EXIT

# List objects (JSON) so we get each object's LastModified.
mc ls --recursive --json "$bucket_ref" 2>/dev/null | jq -rc 'select(.type=="file") | [.key, .lastModified] | @tsv' \
| while IFS=$'\t' read -r objkey lastmod; do
    obj_ns=$(iso_to_ns "$lastmod")
    # Each object is NDJSON: one audit event per line. Decode payload to get
    # t_ns; pull seq from the trailing path segment of key_expr; only ours.
    mc cat "${bucket_ref}/${objkey}" 2>/dev/null | jq -rc --arg run "$run_id" '
      select((.key_expr // "") | test("/" + $run + "/[0-9]+$"))
      | (.key_expr | capture("/(?<seq>[0-9]+)$") | .seq | tonumber) as $seq
      | (if .payload_bytes then (.payload_bytes | @base64d | fromjson | .t_ns) else null end) as $t
      | [$seq, $t] | @tsv
    ' 2>/dev/null | while IFS=$'\t' read -r seq t_ns; do
        echo "$seq" >> "$tmp_seqs"
        if [[ -n "$t_ns" && "$t_ns" != "null" ]]; then
          printf '{"probe":"minio","kind":"lag","seq":%s,"t_ns":%s,"obj_ns":%s,"lag_ns":%s}\n' \
            "$seq" "$t_ns" "$obj_ns" "$(( obj_ns - t_ns ))"
        fi
      done
  done

# Summary: distinct seq vs sent.
distinct=$(sort -un "$tmp_seqs" | wc -l | tr -d ' ')
min_seq=$(sort -un "$tmp_seqs" | head -1)
max_seq=$(sort -un "$tmp_seqs" | tail -1)
completeness=$(awk -v d="$distinct" -v s="$sent" 'BEGIN{ if (s>0) printf "%.6f", d/s; else print "null" }')
printf '{"probe":"minio","kind":"completeness","ts_ns":%s,"run_id":"%s","stored_distinct":%s,"sent":%s,"min_seq":%s,"max_seq":%s,"completeness":%s}\n' \
  "$(date +%s%N)" "$run_id" "${distinct:-0}" "$sent" "${min_seq:-0}" "${max_seq:-0}" "$completeness"
