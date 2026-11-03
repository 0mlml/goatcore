#!/usr/bin/env bash
# Public TCP :33073 probe of the current fleet inventories and their DNS pools.
# Usage: relay-fleet-healthcheck.sh [--environment <goatnet>|all]
#        [--regions rla,rlb] [--json]
# Exit 0: healthy; 1: unavailable listener/DNS; 2: invalid input/environment.
set -euo pipefail
command -v python3 >/dev/null 2>&1 || { echo "python3 required" >&2; exit 2; }
exec python3 "$(cd "$(dirname "$0")" && pwd)/relay-fleet-healthcheck.py" "$@"
