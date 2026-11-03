#!/usr/bin/env bash
# Create a NetBird setup key through SSM; print the plaintext once on success.
# See docs/operations/setup-key-rotation.md. Python handles JSON, SQL literals,
# and key hashing so shell expansion cannot change the request.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec python3 "$SCRIPT_DIR/create-setup-key.py" "$@"
