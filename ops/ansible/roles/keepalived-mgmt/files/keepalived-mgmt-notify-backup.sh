#!/bin/bash
# keepalived-mgmt-notify-backup.sh — fires when this node becomes
# BACKUP (yields the VIP). v1: log only. v2 (future) may demote this
# node's postgres if it was the recently-promoted primary, but that
# is a deliberate decision — a node that just promoted should not
# auto-demote without operator review.

set -uo pipefail
logger -t keepalived-mgmt-notify "BACKUP transition; node=$(hostname)"
exit 0
