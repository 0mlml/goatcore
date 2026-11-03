#!/bin/bash
# keepalived-mgmt-check.sh — health-check script for the keepalived-mgmt
# vrrp_script. Owns the failover-decision criteria.
#
# Returns 0 = healthy (this node should be MASTER/eligible), non-zero
# otherwise (this node demotes/yields).
#
# Checked components:
#   - postgres-a container is running (writable on A; read-only on B)
#   - postgres-a healthcheck reports healthy
#   - traefik container is running (mgmt-API frontend)
#
# Deliberately omits management-a — it crash-loops on the standby until
# B is promoted, so using it as a check would cause split-brain during
# A↔B transitions.

set -uo pipefail

fail() {
    logger -t keepalived-mgmt-check "FAIL: $1"
    exit 1
}

# Container running checks
docker inspect -f '{{.State.Running}}' postgres-a 2>/dev/null | grep -q '^true$' || fail "postgres-a not running"
docker inspect -f '{{.State.Running}}' traefik 2>/dev/null | grep -q '^true$' || fail "traefik not running"

# Postgres healthcheck
status=$(docker inspect -f '{{.State.Health.Status}}' postgres-a 2>/dev/null || echo "missing")
[ "$status" = "healthy" ] || fail "postgres-a health=$status"

exit 0
