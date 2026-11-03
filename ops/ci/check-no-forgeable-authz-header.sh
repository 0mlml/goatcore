#!/usr/bin/env bash
# ops/ci/check-no-forgeable-authz-header.sh — the F-244 anti-regression guard.
#
# THE HOLE: the goat-built
# surfaces sit behind an oauth2-proxy sidecar in REVERSE-PROXY mode. In that
# mode oauth2-proxy strips a client-supplied `X-Forwarded-Groups` from the
# upstream request and sets its own from the validated session — so that
# header is trustworthy. It does NOT strip `X-Auth-Request-*`: those are set
# only on the /oauth2/auth SUBREQUEST RESPONSE (the nginx auth_request
# pattern), never on the upstream request here. So any authz path that reads
# `X-Auth-Request-Groups` is bypassable: a logged-in FLOOR viewer forges
# `X-Auth-Request-Groups: ADMINS` and escalates to full view.
#
# THE RULE: goat-built surface authorization reads the groups claim from
# `X-Forwarded-Groups` ONLY. This guard fails the build if any surface source
# reads an `X-Auth-Request-Groups` header in a way that could feed authz.
# Comments, tests that assert it is IGNORED, and doc strings are allowed; a
# real `.get(...)`/`Header.Get(...)`/header-read of that name is not.
#
# Scope: the goat-built surfaces that gate on the groups claim.
#   - ops/panoscope/src   (Rust; reads GROUPS_HEADER)
#   - ops/goat-id-console/internal  (Go; actorFromHeaders)
# Add new groups-gating surfaces here as they land (flashlight/stethoscope
# when they adopt viewer scoping).
#
# Usage: ops/ci/check-no-forgeable-authz-header.sh   (exit 1 on violation)

set -euo pipefail
cd "$(dirname "$0")/../.."

SCAN_DIRS=(
  "ops/panoscope/src"
  "ops/goat-id-console/internal"
)

# Match a real header READ of x-auth-request-groups: a GETTER call on the
# header name, case-insensitive. Deliberately does NOT match:
#   - bare mentions in comments/docstrings explaining why we don't read it,
#   - test/builder SETTERS (`.header("X-Auth-Request-Groups", ...)`,
#     `.insert("x-auth-request-groups", ...)`) — a regression test that FORGES
#     the header to prove it is ignored must be allowed to exist.
# Getter forms covered: Rust `.get(...)` / `.get_all(...)`, Go `.Get(...)`.
PATTERN='\.[gG]et(_all)?\([[:space:]]*["'"'"']x-auth-request-groups["'"'"']'

violations=0
for d in "${SCAN_DIRS[@]}"; do
  [ -d "$d" ] || continue
  # grep -iE, exclude nothing — the pattern itself requires a getter, so a
  # comment like `// we deliberately do NOT read X-Auth-Request-Groups` (no
  # getter call) does not match.
  if hits=$(grep -rniE "$PATTERN" "$d" 2>/dev/null); then
    echo "FAIL ($d): a surface reads the forgeable X-Auth-Request-Groups header for authorization."
    echo "$hits"
    echo "  Fix: read the groups claim from X-Forwarded-Groups ONLY (the header oauth2-proxy strips-and-sets)."
    violations=1
  fi
done

if [ "$violations" -ne 0 ]; then
  echo
  echo "See docs/findings/F-244-* and docs/measurements/M-079-zagros-rbac-live-fire.md."
  exit 1
fi

echo "ok: no goat-built surface reads the forgeable X-Auth-Request-Groups header for authz."
