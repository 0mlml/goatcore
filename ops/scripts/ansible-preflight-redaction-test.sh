#!/usr/bin/env bash
# Pins the redaction in ansible-preflight.sh's bypass hints.
#
# Every refusal in that script prints a re-run line. Those lines used to
# interpolate `$*` — the full argv — and an ansible-playbook argv routinely
# carries a live secret, because `-e key=value` is how a password reaches a
# play. the stale-tree refusal printed a Postgres superuser
# password to a terminal and into a session transcript, from the script whose
# job is to stop unsafe runs.
#
# A case flipping from redacted to leaked is a SECURITY REGRESSION, not a test
# to update.
set -euo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/ansible-preflight.sh"
[ -f "$SCRIPT" ] || { echo "FAIL: $SCRIPT not found"; exit 1; }

# Load the helper out of the real script so the test cannot drift from it.
HELPER="$(mktemp)"; trap 'rm -f "$HELPER"' EXIT
sed -n '/^__safe_argv() {/,/^}/p' "$SCRIPT" > "$HELPER"
[ -s "$HELPER" ] || { echo "FAIL: __safe_argv not found in $SCRIPT — did the redaction get removed?"; exit 1; }
# shellcheck disable=SC1090
. "$HELPER"

fails=0
check() { # check <description> <expected> -- <argv...>
  local desc="$1" want="$2"; shift 2; [ "$1" = "--" ] && shift
  local got; got="$(__safe_argv "$@")"
  if [ "$got" = "$want" ]; then
    printf '  ok   %s\n' "$desc"
  else
    printf '  FAIL %s\n       want: %s\n       got:  %s\n' "$desc" "$want" "$got"
    fails=$((fails + 1))
  fi
}

echo "ansible-preflight bypass-hint redaction:"

check "inline -e value is redacted, key kept" \
  "-i inv.ini play.yml -e postgres_superuser_password=<redacted>" \
  -- -i inv.ini play.yml -e postgres_superuser_password=hunter2

check "--extra-vars long form is redacted too" \
  "--extra-vars cloudflare_api_token=<redacted>" \
  -- --extra-vars cloudflare_api_token=LIVE_TOKEN_VALUE

check "two -e pairs both redacted" \
  "-e a=<redacted> -e b=<redacted>" \
  -- -e a=one -e b=two

check "-e @file keeps the path (not a secret, and the safe form)" \
  "-i inv.ini play.yml -e @/tmp/vars.yml" \
  -- -i inv.ini play.yml -e @/tmp/vars.yml

check "a bare -e value with no '=' is fully redacted" \
  "-e <redacted>" \
  -- -e opaque

check "non -e arguments are untouched" \
  "-i inv.ini play.yml --limit thor -vv" \
  -- -i inv.ini play.yml --limit thor -vv

check "a value that merely contains -e is not treated as a flag" \
  "-i inv.ini --limit host-e1" \
  -- -i inv.ini --limit host-e1

# The whole point: no refusal path may interpolate raw argv again.
echo "no refusal path interpolates raw argv:"
if grep -nE '\$0 \$\*|\$0 "\$\@"' "$SCRIPT" >/dev/null 2>&1; then
  echo "  FAIL a bypass hint interpolates raw argv:"
  grep -nE '\$0 \$\*|\$0 "\$\@"' "$SCRIPT" | sed 's/^/       /'
  fails=$((fails + 1))
else
  echo "  ok   every bypass hint uses \$SAFE_ARGV"
fi

if [ "$fails" -gt 0 ]; then
  echo "FAILED: $fails case(s). A leaked value here is a security regression."
  exit 1
fi
echo "PASS"
