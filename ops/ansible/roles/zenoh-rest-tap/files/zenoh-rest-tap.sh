#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Managed by ansible (roles/zenoh-rest-tap). Do not edit on the host.
# Once a second, log the callers of the router's REST port from inside its
# network namespace. The PID is resolved on every loop so a router restart needs
# no restart here.
set -u
container="${1:?router container}"
port="${2:?REST port}"
log="${3:?log file}"
while true; do
  PID=$(docker inspect -f '{{.State.Pid}}' "$container" 2>/dev/null)
  if [ -n "$PID" ] && [ "$PID" != 0 ]; then
    nsenter -t "$PID" -n ss -Htan 2>/dev/null \
      | awk -v port=":${port}\$" '$1=="ESTAB" && ($4 ~ port){print $5}' \
      | sed 's/:[0-9]*$//' | sort | uniq -c \
      | awk -v ts="$(date -u +%FT%TZ)" 'NF{print ts" callers "$1"x "$2}' >> "$log"
  fi
  sleep 1
done
