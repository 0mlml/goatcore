#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# A deploy reads each goatnet's portal operator key from <SITE>_PORTAL_OPERATOR_API_KEY.
# The bare PORTAL_OPERATOR_API_KEY is whichever goatnet's key the shell exported;
# reading it deploys one goatnet's root operator key to another's portal.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
hits=$(git grep -nE "lookup\(['\"]env['\"], *['\"]PORTAL_OPERATOR_API_KEYS?['\"]\)" -- ops/ansible || true)
if [ -n "$hits" ]; then
  echo "FAIL: bare portal operator key lookup; read <SITE>_PORTAL_OPERATOR_API_KEY instead:" >&2
  echo "$hits" >&2
  exit 1
fi
echo "ok: no bare PORTAL_OPERATOR_API_KEY lookup in ops/ansible"
