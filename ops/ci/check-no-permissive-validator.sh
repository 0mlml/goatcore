#!/usr/bin/env bash
# ops/ci/check-no-permissive-validator.sh — relay-auth fail-closed guard.
#
# The goat-relay data-plane listeners (WS + QUIC) MUST authenticate relay
# clients with the real timed-HMAC validator (goat-relay/src/auth.rs). A
# `PermissiveValidator` (`validate()` returns `Ok(())` for any input)
# previously shipped wired into both production listeners, turning the
# public :33073 relay into an open relay (anyone could register/evict
# peer-ID routing slots). See the relay-client-auth finding +
# docs/design/wg-cp0-prod.md "relay-client authentication".
#
# This guard refuses any reintroduction of a permissive / accept-all
# validator into the relay source tree. A legitimately accept-all
# validator for unit tests already exists in the protocol crate as the
# `#[cfg(test)]` `AcceptValidator` — production code has no business
# referencing a `PermissiveValidator`.
#
# Usage:
#   ops/ci/check-no-permissive-validator.sh
#
# Exit codes:
#   0  clean — no permissive validator present
#   1  a PermissiveValidator reference was found (prints file:line)
#   2  invalid usage / dependency missing

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

# Search the relay source trees only (not target/, not docs, not this
# script, not the CI workflow that names it for path-filtering).
search_dirs=()
[ -d goat-relay/src ] && search_dirs+=("goat-relay/src")
[ -d goat-relay-protocol/src ] && search_dirs+=("goat-relay-protocol/src")

if [ "${#search_dirs[@]}" -eq 0 ]; then
  echo "check-no-permissive-validator: no relay source dirs found; nothing to scan" >&2
  exit 0
fi

if ! command -v grep >/dev/null 2>&1; then
  echo "check-no-permissive-validator: grep not found" >&2
  exit 2
fi

# `PermissiveValidator` is the canonical name; also catch obvious aliases
# that re-create the same hole (an accept-all validator named "AllowAll"
# in the relay crate, mirroring upstream netbird's allow/allow_all). The
# protocol crate's test-only AcceptValidator/RejectValidator are NOT
# matched (different identifiers, and intentionally test-scoped).
pattern='PermissiveValidator|AllowAllValidator'

# Match the identifier in CODE only — drop comment lines so prose that
# documents the removed validator (e.g. auth.rs's module doc explaining
# the history) does not trip the guard. After grep's `path:lineno:` prefix
# the comment content starts with `//` (line/doc comment) or `*` (block
# continuation); filter those out.
hits="$(grep -RnE "$pattern" "${search_dirs[@]}" 2>/dev/null \
  | grep -vE ':[[:space:]]*//' \
  | grep -vE ':[[:space:]]*\*' || true)"

if [ -n "$hits" ]; then
  echo "ERROR: relay-auth fail-closed guard tripped — permissive/accept-all validator found in relay source:" >&2
  echo "$hits" >&2
  echo >&2
  echo "The relay must authenticate clients with goat-relay/src/auth.rs::TimedHmacValidator." >&2
  echo "If a test needs an accept-all validator, use the protocol crate's #[cfg(test)] AcceptValidator." >&2
  exit 1
fi

echo "check-no-permissive-validator: OK — no permissive validator in relay source"
exit 0
