#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

# The role's CI entry point. .github/workflows/ansible-syntax-check.yml
# discovers roles/*/tests/syntax-check.sh by that exact name, so anything this
# role wants gated has to be reachable from here. The glob is deliberately not
# broadened to *.sh: other roles keep 2-hour soak runs and docker-dependent
# fixtures under tests/ that must never fire on a PR.

fail=0

if command -v ansible-playbook >/dev/null 2>&1; then
  echo "[isitup/syntax-check] ansible-playbook --syntax-check"
  ansible-playbook -i 'localhost,' -c local playbook.yml --syntax-check
  echo "[isitup/syntax-check] PASS"
else
  echo "[isitup/syntax-check] SKIP (ansible-playbook not on PATH)"
fi

# check.sh grading behaviour. Not a syntax check, but this is the only hook CI
# looks at, and a grading bug is invisible to any syntax check: a probe that dials
# an FQDN that 404s by design grades a healthy Zenoh router degraded.
# Needs only bash + python3.
if command -v python3 >/dev/null 2>&1; then
  ./front-door-probe.sh || fail=1
  # The rendered services.env itself: a `"` in a row's desc must not split one
  # array entry into two and put a nameless row on the board. Skips itself where
  # jinja2 is absent.
  ./free-text-columns.sh || fail=1
  # Which rows reach the page at all (#1518): one per id, unlisted and
  # past-retire_after rows dropped, lifecycle/owner/source carried to
  # meta.json. Skips itself where jinja2 is absent.
  python3 ./row-list.py || fail=1
else
  echo "[isitup/front-door-probe] SKIP (python3 not on PATH)"
  echo "[isitup/free-text-columns] SKIP (python3 not on PATH)"
  echo "[isitup/row-list] SKIP (python3 not on PATH)"
fi

exit "$fail"
