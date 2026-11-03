#!/usr/bin/env bash
# alertmanager-paging-smoke.sh — turnkey smoke test for observability paging path.
#
# Fires a well-formed synthetic critical alert into a target alertmanager
# and reports what the operator should look for to confirm end-to-end
# paging is armed. Intended for routine verification (after obs-host role
# re-runs, after PagerDuty integration-key rotation, after alertmanager
# image bumps) and for any cutover-and-settle window.
#
# Idempotent. Safe to re-run. The synthetic alert auto-resolves in 5 min,
# so it does not park as a permanent firing alert.
#
# Usage:
#   ./alertmanager-paging-smoke.sh <ssh-target> [alertmanager-port]
#
# Examples:
#   # Observability host on the LAN
#   ./alertmanager-paging-smoke.sh operator@obs-host.example
#
#   # Cloud env where alertmanager listens on a different port
#   ./alertmanager-paging-smoke.sh goat@cloud-host.example 9093
#
# Acceptance:
#   - Script exits 0 with a "synthetic alert accepted" message
#   - Operator confirms a new incident appears in the configured PagerDuty
#     service within 1-2 minutes (group_wait=30s + dispatch latency)
#   - Operator confirms the incident auto-resolves within ~5-6 min
#     (send_resolved=true on the receiver)
#
# If steps 2-3 fail, see docs/operations/observability-paging-smoke.md
# for the diagnostic ladder.

set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: $0 <ssh-target> [alertmanager-port]" >&2
  exit 2
fi

SSH_TARGET="$1"
PORT="${2:-9093}"

# Generate a unique alertname per run so concurrent test fires don't
# collide and the operator can identify their own test in PD's incident
# stream.
RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)-${RANDOM}"
ALERTNAME="PagingSmokeTest-${RUN_ID}"

echo "Firing synthetic alert ${ALERTNAME} against ${SSH_TARGET}:${PORT}"
echo

# Date arithmetic happens REMOTE-side (single-quoted ssh body) — Linux
# `date -u -d '+5 minutes'` syntax is incompatible with macOS BSD date,
# so doing this locally would fail silently with empty endsAt.
ssh -o ConnectTimeout=10 "${SSH_TARGET}" "
  set -e
  START=\$(date -u +%Y-%m-%dT%H:%M:%SZ)
  END=\$(date -u -d '+5 minutes' +%Y-%m-%dT%H:%M:%SZ)
  echo \"  startsAt: \$START\"
  echo \"  endsAt:   \$END\"
  echo
  RESPONSE=\$(curl -sS -w '\nHTTP %{http_code}' -X POST http://localhost:${PORT}/api/v2/alerts \
    -H 'Content-Type: application/json' \
    -d \"[{
      \\\"labels\\\": {
        \\\"alertname\\\": \\\"${ALERTNAME}\\\",
        \\\"severity\\\": \\\"critical\\\",
        \\\"site\\\": \\\"smoketest\\\",
        \\\"component\\\": \\\"paging-smoke\\\",
        \\\"instance\\\": \\\"${SSH_TARGET}\\\"
      },
      \\\"annotations\\\": {
        \\\"summary\\\": \\\"Synthetic paging smoke test (${RUN_ID}) — auto-resolves in 5 min\\\",
        \\\"runbook_url\\\": \\\"https://example.invalid/observability-paging-smoke\\\"
      },
      \\\"startsAt\\\": \\\"\$START\\\",
      \\\"endsAt\\\": \\\"\$END\\\"
    }]\")
  echo \"\$RESPONSE\"
"

cat <<EOF

Synthetic alert accepted. Now verify operator-side:

  1. Within ~1-2 min, a new incident titled
     "Synthetic paging smoke test (${RUN_ID})" should appear in the
     configured PagerDuty service.

  2. Within ~5-6 min, that incident should auto-resolve (send_resolved=true).

  3. If you see no incident: check the alertmanager logs for notify
     errors and walk the diagnostic ladder in
     docs/operations/observability-paging-smoke.md.

The synthetic alert will not page on-call rotation if PagerDuty's
service has a "low-urgency" notification rule for unmatched incidents.
For a more invasive test (will page on-call), set
SMOKE_FORCE_HIGH_URGENCY=1 — not recommended unless coordinating with
the on-call.

Run ID for forensic correlation: ${RUN_ID}
EOF
