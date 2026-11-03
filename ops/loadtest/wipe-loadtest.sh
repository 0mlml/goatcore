#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# wipe-loadtest.sh — allowlist-only, dry-run-default teardown of load-test data.
#
# Deletes EXACTLY two things and nothing else:
#   1. the dedicated load-test rocksdb subdir   ($LOADTEST_ROCKSDB_DIR)
#   2. the ephemeral MinIO bucket               ($LOADTEST_BUCKET)
#
# Every deletion is gated by the assertions in lib-loadtest.sh: the bucket must
# match the configured ephemeral name + 'loadtest-*' prefix and not be on the
# real-bucket denylist; the rocksdb dir basename must be the load prefix and its
# parent the configured storage root. Anything else aborts, loudly, before any
# mutation. Real audit/telemetry/vendor data is unreachable from this script by
# construction — see docs/design/zenoh-loadtest.md "The wipe contract".
#
# Usage:
#   wipe-loadtest.sh                 # DRY RUN: print exactly what would go, with sizes
#   wipe-loadtest.sh --apply         # actually delete (after you've read the dry-run)
#
# Env overrides (see lib-loadtest.sh): LOADTEST_BUCKET, LOADTEST_ROCKSDB_DIR,
# LOADTEST_MC_ALIAS, LOADTEST_ZENOH_STORAGE_ROOT, ...

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib-loadtest.sh
source "${HERE}/lib-loadtest.sh"

APPLY=0
for arg in "$@"; do
  case "$arg" in
    --apply) APPLY=1 ;;
    -h|--help) grep '^#' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) loadtest::die "unknown arg: $arg (use --apply or nothing for dry-run)" ;;
  esac
done

# Re-run the safety assertions up front: if either target is mis-aimed we abort
# before touching anything at all.
loadtest::assert_ephemeral_bucket "$LOADTEST_BUCKET"
loadtest::assert_loadtest_rocksdb_dir "$LOADTEST_ROCKSDB_DIR"

# ---- Measure what's there ---------------------------------------------------

rocksdb_size="(absent)"
if [[ -d "$LOADTEST_ROCKSDB_DIR" ]]; then
  rocksdb_size=$(du -sh "$LOADTEST_ROCKSDB_DIR" 2>/dev/null | awk '{print $1}')
fi

bucket_ref="${LOADTEST_MC_ALIAS}/${LOADTEST_BUCKET}"
bucket_summary="(mc unavailable)"
if command -v mc >/dev/null 2>&1; then
  if mc ls "$bucket_ref" >/dev/null 2>&1; then
    bucket_summary=$(mc du "$bucket_ref" 2>/dev/null | awk '{print $1" "$2}' || echo "(empty)")
  else
    bucket_summary="(bucket absent or alias not configured)"
  fi
fi

cat >&2 <<EOF
[loadtest] wipe plan ($([ "$APPLY" = 1 ] && echo APPLY || echo DRY-RUN))
  rocksdb subdir : $LOADTEST_ROCKSDB_DIR    size=${rocksdb_size}
  minio bucket   : $bucket_ref               usage=${bucket_summary}
  denylist (never touched): ${REAL_BUCKET_DENYLIST[*]}
EOF

if [[ "$APPLY" != 1 ]]; then
  loadtest::log "dry-run only. Re-run with --apply to delete the two targets above."
  exit 0
fi

# ---- Apply ------------------------------------------------------------------

# 1. rocksdb subdir. Optionally drop the storage entry first via the role's
#    delete-vendor helper if present (keeps the router config clean); the data
#    dir removal is the load-bearing step.
if [[ -d "$LOADTEST_ROCKSDB_DIR" ]]; then
  loadtest::assert_loadtest_rocksdb_dir "$LOADTEST_ROCKSDB_DIR"   # assert again, immediately before rm
  if command -v goat-zenoh-delete-vendor >/dev/null 2>&1; then
    loadtest::log "dropping zenoh storage entry for '${LOADTEST_KEY_PREFIX}' via delete-vendor helper"
    goat-zenoh-delete-vendor "${LOADTEST_KEY_PREFIX}" || loadtest::log "delete-vendor returned nonzero (continuing to data removal)"
  fi
  loadtest::log "removing rocksdb subdir: $LOADTEST_ROCKSDB_DIR"
  rm -rf -- "$LOADTEST_ROCKSDB_DIR"
  loadtest::log "rocksdb subdir removed"
else
  loadtest::log "rocksdb subdir already absent: $LOADTEST_ROCKSDB_DIR"
fi

# 2. ephemeral bucket: empty (GOVERNANCE bypass scoped to THIS bucket only),
#    then remove the bucket.
if command -v mc >/dev/null 2>&1 && mc ls "$bucket_ref" >/dev/null 2>&1; then
  loadtest::assert_ephemeral_bucket "$LOADTEST_BUCKET"            # assert again, immediately before rm
  loadtest::log "emptying ephemeral bucket: $bucket_ref"
  # --force removes objects; --bypass clears GOVERNANCE-mode locked objects.
  # Both are scoped to $bucket_ref — a single, asserted, loadtest-* bucket.
  mc rm --recursive --force --bypass "$bucket_ref" || loadtest::log "object removal returned nonzero (continuing)"
  loadtest::log "removing bucket: $bucket_ref"
  mc rb --force "$bucket_ref" || loadtest::log "bucket removal returned nonzero"
  loadtest::log "ephemeral bucket removed"
else
  loadtest::log "ephemeral bucket already absent or mc unavailable: $bucket_ref"
fi

loadtest::log "wipe complete. Real buckets (${REAL_BUCKET_DENYLIST[*]}) and all other vendor storages untouched."
