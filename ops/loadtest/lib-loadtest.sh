#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Shared config + safety primitives for the Zenoh fabric load-testing apparatus.
# Sourced by run-loadtest.sh / probe-*.sh / wipe-loadtest.sh.
#
# Design: docs/design/zenoh-loadtest.md
#
# THE SAFETY CONTRACT LIVES HERE. The wipe can only ever address things this
# file declares load-test-owned; everything else is denied by construction.
# Read the assertions below before changing any path or bucket name.

set -euo pipefail

# ---- Load-test-owned namespace (the ONLY things the wipe may delete) --------

# Zenoh key prefix for all load traffic. The leaf cert ACL is scoped to this;
# a wrong prefix fails closed at the router.
: "${LOADTEST_KEY_PREFIX:=loadtest}"

# rocksdb: parent storage root (shared with real vendors) + the dedicated
# load-test subdir (the only rocksdb path the wipe may remove).
# nova-a reality (verified M-052): the live router bind-mounts rocksdb at
# /var/lib/goat-zenoh/rocksdb (host == container), and add-vendor.sh
# hardcodes the same — NOT /opt/docker-data/zenoh (empty on nova-a). Override
# per-host if a deployment's zenoh_storage_path differs.
: "${LOADTEST_ZENOH_STORAGE_ROOT:=/var/lib/goat-zenoh/rocksdb}"
: "${LOADTEST_ROCKSDB_DIR:=${LOADTEST_ZENOH_STORAGE_ROOT}/${LOADTEST_KEY_PREFIX}}"

# MinIO: the dedicated, quota-capped, ephemeral bucket (the only bucket the
# wipe may empty/remove). Must start with the loadtest- prefix.
: "${LOADTEST_BUCKET:=loadtest-ephemeral}"

# ---- Real data — the hard denylist (the wipe refuses to touch any of these) -

# Production buckets on nova-a per docs/design/audit-cold-storage-offload.md +
# group_vars/nova_hosts/minio.yml. The wipe aborts, loudly, on any match.
REAL_BUCKET_DENYLIST=(audit-integrity telemetry-otlp runtime-detection-archive)

# ---- Endpoints / tooling ----------------------------------------------------

# Router REST plugin (rocksdb GET-back probe). NOTE: nova-a publishes
# :8000 on the LAN IP (group_vars override for the portal), NOT loopback —
# data-GET from the host needs the router's container IP or LAN IP, e.g.
# http://172.21.0.2:8000 (container) or http://<lan-ip>:8000. The role default
# is loopback; override LOADTEST_REST_BASE to match the live publish address.
: "${LOADTEST_REST_BASE:=http://127.0.0.1:8000}"

# Loadgen container image + its scraped Prometheus endpoint.
: "${LOADTEST_LOADGEN_IMAGE:=ghcr.io/usareur-af/goat-loadgen:latest}"
: "${LOADTEST_LOADGEN_METRICS:=http://127.0.0.1:9304/metrics}"
: "${LOADTEST_ZENOH_ENDPOINT:=tls/goat-zenoh-router:7447}"
: "${LOADTEST_DOCKER_NETWORK:=goat-zenoh_default}"

# mTLS leaf material (mounted by the ansible deploy). Used by the loadgen
# container.
: "${LOADTEST_CERT:=/opt/loadtest/certs/loadtest-cert.pem}"
: "${LOADTEST_KEY:=/opt/loadtest/certs/loadtest-key.pem}"
: "${LOADTEST_ROOT_CA:=/opt/loadtest/certs/ca-root-cert.pem}"

# mc (MinIO client) alias configured against the ephemeral bucket's endpoint.
: "${LOADTEST_MC_ALIAS:=loadtest-minio}"

# ---- Disk safety (nova-a's single-rootfs Thor — see design doc) ---------------

# Filesystem to guard (the one holding /opt/docker-data). Free space below the
# floor refuses a run start AND aborts a run in progress.
: "${LOADTEST_DISK_GUARD_PATH:=${LOADTEST_ZENOH_STORAGE_ROOT}}"
: "${LOADTEST_DISK_FLOOR_GB:=20}"

# Output root for evidence + reports.
: "${LOADTEST_OUT_DIR:=/opt/loadtest/out}"

# ---- Helpers ----------------------------------------------------------------

loadtest::log()  { printf '[loadtest] %s\n' "$*" >&2; }
loadtest::die()  { printf '[loadtest][FATAL] %s\n' "$*" >&2; exit 1; }

# Free space, in whole GB, on the filesystem holding $1.
loadtest::free_gb() {
  local path="$1" avail_kb
  avail_kb=$(df -Pk "$path" | awk 'NR==2 {print $4}')
  echo $(( avail_kb / 1024 / 1024 ))
}

# Assert a bucket name is NOT a real/production bucket. Used everywhere a
# delete could be aimed at a bucket. Refuses on any denylist match.
loadtest::assert_not_real_bucket() {
  local bucket="$1" real
  for real in "${REAL_BUCKET_DENYLIST[@]}"; do
    if [[ "$bucket" == "$real" ]]; then
      loadtest::die "refusing: '$bucket' is a REAL production bucket (denylist). Never in the wipe path."
    fi
  done
}

# Assert a bucket is a legitimate load-test ephemeral bucket: matches the
# configured name AND the loadtest- prefix AND is not denylisted.
loadtest::assert_ephemeral_bucket() {
  local bucket="$1"
  [[ "$bucket" == "$LOADTEST_BUCKET" ]] \
    || loadtest::die "refusing: bucket '$bucket' != configured LOADTEST_BUCKET '$LOADTEST_BUCKET'"
  [[ "$bucket" == loadtest-* ]] \
    || loadtest::die "refusing: bucket '$bucket' does not match the required 'loadtest-*' prefix"
  loadtest::assert_not_real_bucket "$bucket"
}

# Assert a rocksdb dir is the dedicated load-test subdir: basename is exactly
# the load prefix AND its parent is exactly the configured storage root. This
# is what stops a wipe from ever removing a real vendor's storage dir.
loadtest::assert_loadtest_rocksdb_dir() {
  local dir="$1" base parent
  # Normalise without requiring existence (dir may already be gone).
  base=$(basename "$dir")
  parent=$(cd "$(dirname "$dir")" 2>/dev/null && pwd || dirname "$dir")
  [[ "$base" == "$LOADTEST_KEY_PREFIX" ]] \
    || loadtest::die "refusing: rocksdb dir basename '$base' != load prefix '$LOADTEST_KEY_PREFIX'"
  [[ "$parent" == "$LOADTEST_ZENOH_STORAGE_ROOT" ]] \
    || loadtest::die "refusing: rocksdb dir parent '$parent' != storage root '$LOADTEST_ZENOH_STORAGE_ROOT'"
  # Belt-and-suspenders: never the storage root itself, never '/'.
  [[ "$dir" != "$LOADTEST_ZENOH_STORAGE_ROOT" && "$dir" != "/" ]] \
    || loadtest::die "refusing: rocksdb dir '$dir' is the storage root or '/'"
}
