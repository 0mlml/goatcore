#!/bin/bash
# keepalived-mgmt-notify-fault.sh — fires when this node enters FAULT
# state (health check failed). The VIP is yielded automatically; this
# script's job is to surface the failure.

set -uo pipefail
logger -t keepalived-mgmt-notify "FAULT transition; node=$(hostname); investigate postgres-a + traefik state"
exit 0
