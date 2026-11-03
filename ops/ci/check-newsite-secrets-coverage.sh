#!/usr/bin/env bash
# Entry point for the live YAML-to-generator coverage contract (#1522).
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec python3 "$SCRIPT_DIR/check-newsite-secrets-coverage.py" "$@"
