#!/usr/bin/env bash
# Cut one person's access on every plane of one or all selected goatnets, and
# fail when any plane cannot run or be verified. Usage:
#   ops/scripts/offboard.sh <email-or-handle> --goatnet <goatnet> [--apply]
#   ops/scripts/offboard.sh <email-or-handle> --all-goatnets [--apply]
# See --help, ADR 1187, and docs/design/offboarding-one-driver.md for the full
# plane list, credentials, evidence, and deliberately excluded operations.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "$0")" && pwd)"
exec python3 "$SCRIPT_DIR/offboard_driver.py" "$@"
