#!/usr/bin/env bash
# check-console-door-declared.sh — every goatnet that runs goat-id-console must
# DECLARE who its oauth2-proxy admits, rather than inheriting the role default.
#
# WHY THIS EXISTS
#   `id_admin_console_oauth2_proxy_allowed_groups` decides whether the identity
#   console's proxy admits any authenticated member or only named Ory groups.
#   A goatnet that does not declare it inherits the role default
#   "PLATFORM-ADMIN,IAM-ADMIN", which makes /ssh-key and /operator-key admin-only
#   — the opposite of what those pages are for
#   (internal/handlers/handlers.go: a gate ordinary members cannot pass "would
#   send them back to mailing a key around out-of-band — the exact path the
#   rails exist to close").
#
#   That is what an undeclared value looks like when the role default is
#   RESTRICTIVE: inheriting it fails closed, so it looks safe, and it announces
#   itself not as a problem but as a feature that quietly does not work for the
#   people it was built for.
#
# WHY A GUARD AND NOT THE PARITY REPORT
#   The consistency intent grades members WITHIN a goatnet against each other.
#   A goatnet with a single member group cannot skew against itself, so an
#   absent declaration grades green there. This
#   guard asks a different question the report cannot: "is there an opinion
#   recorded at all?"
#
# COVERAGE IS DERIVED, NEVER ENUMERATED (environment-parity.md §3)
#   The goatnet list comes from the tree — any group_vars dir that turns the
#   console on. A fourth environment is covered by existing, not by somebody
#   remembering to add it here. A guard with a hand-maintained env list is a
#   guard the next environment silently escapes.
#
# It does NOT check the VALUE. "" and "PLATFORM-ADMIN,IAM-ADMIN" are both
# legitimate; which one a goatnet wants is a real decision. The defect this
# catches is the absence of a decision.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
GV="$ROOT/ops/ansible/group_vars"
KEY="id_admin_console_oauth2_proxy_allowed_groups"
ENABLE="id_admin_console_enabled"

missing=0
checked=0

# Derive: every group_vars dir whose vars.yml turns the console on. grep for the
# literal true so a commented-out or false line does not pull an env in.
while IFS= read -r vars_file; do
  [ -f "$vars_file" ] || continue
  grep -qE "^[[:space:]]*${ENABLE}:[[:space:]]*true[[:space:]]*$" "$vars_file" || continue
  member="$(basename "$(dirname "$vars_file")")"
  checked=$((checked + 1))
  if grep -qE "^[[:space:]]*${KEY}:" "$vars_file"; then
    value="$(grep -E "^[[:space:]]*${KEY}:" "$vars_file" | head -1 | sed 's/^[^:]*:[[:space:]]*//')"
    echo "  ok   $member — declares ${KEY}: ${value}"
  else
    echo "  FAIL $member — runs the console and declares no door" >&2
    missing=$((missing + 1))
  fi
done < <(find "$GV" -mindepth 2 -maxdepth 2 -name vars.yml | sort)

if [ "$checked" -eq 0 ]; then
  # Not "everything passed" — the discovery found nothing, which means the
  # shape of the tree changed and this guard is now inert. Fail loudly rather
  # than report a green that means nothing.
  echo "FAIL: found no group_vars member with ${ENABLE}: true — discovery is broken, not clean." >&2
  exit 1
fi

if [ "$missing" -gt 0 ]; then
  cat >&2 <<EOF

FAIL: $missing goatnet member(s) run goat-id-console without declaring ${KEY}.

Do not fix this by removing the console from that member. Add the key to its
group_vars/<member>/vars.yml with a comment saying WHY, e.g.

  # Console front door. EMPTY = any authenticated Ory member reaches the
  # console; roles are enforced in-app by adminGate, not here.
  ${KEY}: ""

or, to keep the restrictive door as a recorded choice:

  ${KEY}: "PLATFORM-ADMIN,IAM-ADMIN"

Either value is fine. The undeclared state is the defect: the role default is
restrictive, so inheriting it silently makes the self-service pages
(/ssh-key, /operator-key) admin-only without anybody deciding that.

Background: docs/design/environment-parity.md §"Worked example — the console
front door".
EOF
  exit 1
fi

echo "OK: all $checked console-running member(s) declare ${KEY}."
