#!/usr/bin/env bash
# goat-obs-textfile.sh — host-side metric facts for node-exporter's
# textfile collector. Installed as a */5 root cron by ops/ansible/obs.yml;
# writes <outdir>/goat_obs_host.prom atomically (tmp + mv).
#
# Emits:
#   goat_container_info{id=..., cid=..., name=...} 1 — docker container
#       id→name map. cadvisor runs in raw-cgroup mode (docker factory
#       can't read the containerd-snapshotter driver), so its series have
#       no name label; join on(id) or on(cid) against this.
#   goat_container_health_status{cid=..., name=..., status=...} 1 — docker
#       HEALTHCHECK verdict per container. Only containers that define a
#       healthcheck appear; status is docker's own state machine
#       (starting|healthy|unhealthy). Raw-cgroup cadvisor cannot see
#       State.Health at all, so this is the only path to an alert on a
#       running-but-unhealthy container (the F-248 / F-253 router class:
#       docker ps says Up, the service is dead or seized).
#   goat_obs_textfile_last_run_timestamp_seconds — staleness canary.
#
# (Bucket quotas need no cron: current MinIO natively exports
# minio_bucket_quota_total_bytes on /minio/v2/metrics/bucket —
# job=minio-bucket in prometheus.yml.)
#
# Usage: goat-obs-textfile.sh <output-dir>

set -euo pipefail

OUTDIR="${1:?usage: goat-obs-textfile.sh <output-dir>}"
OUT="${OUTDIR}/goat_obs_host.prom"
TMP="$(mktemp "${OUTDIR}/.goat_obs_host.prom.XXXXXX")"
trap 'rm -f "${TMP}"' EXIT

{
  echo "# HELP goat_container_info Docker container id-to-name map for raw-cgroup cadvisor series."
  echo "# TYPE goat_container_info gauge"
  docker ps --no-trunc --format '{{.ID}} {{.Names}}' 2>/dev/null | while read -r cid64 cname; do
    printf 'goat_container_info{id="/system.slice/docker-%s.scope",cid="%s",name="%s"} 1\n' \
      "${cid64}" "${cid64:0:12}" "${cname}"
  done

  echo "# HELP goat_container_health_status Docker HEALTHCHECK verdict per container (only containers defining a healthcheck appear)."
  echo "# TYPE goat_container_health_status gauge"
  running_ids="$(docker ps -q 2>/dev/null || true)"
  if [ -n "${running_ids}" ]; then
    # shellcheck disable=SC2086  # id word-splitting is intended
    { docker inspect --format '{{.Id}} {{slice .Name 1}} {{with .State.Health}}{{.Status}}{{end}}' \
        ${running_ids} 2>/dev/null || true; } | while read -r cid64 cname hstatus; do
      [ -n "${hstatus:-}" ] || continue   # no HEALTHCHECK defined — skip
      printf 'goat_container_health_status{cid="%s",name="%s",status="%s"} 1\n' \
        "${cid64:0:12}" "${cname}" "${hstatus}"
    done
  fi

  echo "# HELP goat_obs_textfile_last_run_timestamp_seconds Unix time of the last successful goat-obs-textfile run."
  echo "# TYPE goat_obs_textfile_last_run_timestamp_seconds gauge"
  printf 'goat_obs_textfile_last_run_timestamp_seconds %s\n' "$(date +%s)"
} > "${TMP}"

chmod 0644 "${TMP}"
mv "${TMP}" "${OUT}"
trap - EXIT
