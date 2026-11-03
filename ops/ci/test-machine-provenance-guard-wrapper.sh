#!/usr/bin/env bash
# Pins the ONE thing the machine-provenance-guard wrapper exists to get right:
# translating the check's exit codes into the live-state-guard contract's, where
# 2 means A SOURCE COULD NOT BE READ and must never be mistaken for clean.
#
# This matters more than it looks. The check grades an unreachable portal as
# `unknown` and still exits 0 — correct for a hand-run tool whose membership
# stage is worth running anyway, wrong for an unattended one. If that 0 reached
# systemd, the guard would go green while seeing nothing, which is precisely the
# failure ADR 1130 exists to fix, reintroduced one layer down.
#
# The tests drive the wrapper against FAKE checks that reproduce the real output
# markers. Each marker is asserted separately, so rewording one in the check
# fails a test here instead of silently degrading a 2 into a 0.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WRAPPER="${ROOT}/ansible/roles/machine-provenance-guard/files/machine-provenance-guard.sh"

fails=0
tmproot="$(mktemp -d)"
trap 'rm -rf "$tmproot"' EXIT

# Build a sandbox whose CHECK is a stub emitting $1 and exiting $2. The stub sits
# at the same repo-shaped path the role installs to, because the wrapper resolves
# it from LIB_DIR — testing it anywhere else would test the sandbox, not the
# wrapper.
run_case() {
  local name="$1" output="$2" check_rc="$3" want_rc="$4" want_metric="$5"
  local box="${tmproot}/${name}"
  mkdir -p "${box}/lib/ops/ci" "${box}/report" "${box}/textfile"
  {
    echo '#!/usr/bin/env bash'
    printf 'cat <<%s\n%s\n%s\n' 'STUBEOF' "$output" 'STUBEOF'
    echo "exit ${check_rc}"
  } > "${box}/lib/ops/ci/check-node-class-membership.sh"
  chmod +x "${box}/lib/ops/ci/check-node-class-membership.sh"

  local got_rc=0
  REPORT_DIR="${box}/report" TEXTFILE_DIR="${box}/textfile" LIB_DIR="${box}/lib" \
    bash "$WRAPPER" >/dev/null 2>&1 || got_rc=$?

  if [ "$got_rc" != "$want_rc" ]; then
    echo "FAIL ${name}: wrapper exited ${got_rc}, want ${want_rc}"
    fails=$((fails + 1))
  fi

  local metric="${box}/textfile/machine_provenance_guard.prom"
  if [ "$want_metric" = "yes" ] && [ ! -f "$metric" ]; then
    echo "FAIL ${name}: expected a metric file and there is none"
    fails=$((fails + 1))
  fi
  if [ "$want_metric" = "no" ] && [ -f "$metric" ]; then
    echo "FAIL ${name}: metric was stamped over a run that could not read its sources"
    fails=$((fails + 1))
  fi
  if [ ! -f "${box}/report/last-run.txt" ]; then
    echo "FAIL ${name}: no report written — every run must leave evidence"
    fails=$((fails + 1))
  fi
}

CLEAN='mint provenance (4 gear-class peers):
  aligned           4/4
  pending-reconcile 0/4
  violation         0/4
  unknown           0/4

OK: 31 peers — no gear wearing a person role, no person wearing a
    gear class, nothing unclassified.'

VIOLATION='mint provenance (4 gear-class peers):
  aligned           3/4
  pending-reconcile 0/4
  violation         1/4
  unknown           0/4
  FAIL: violations present under PROVENANCE_ENFORCE=1 — a peer wearing a
  gear class with no registry custody edge did not come through the mint.'

# --- the contract ------------------------------------------------------------
run_case clean-read           "$CLEAN"                                                  0 0 yes
run_case violation-enforced   "$VIOLATION"                                              1 1 yes

# Every "could not read" marker maps to 2, whatever the check exited with.
run_case portal-unreachable \
  'mint provenance (4 gear-class peers):
  portal unreachable or /api/machines error — every gear peer grades UNKNOWN
  aligned           0/4
  pending-reconcile 0/4
  violation         0/4
  unknown           4/4'                                                                0 2 no
run_case portal-creds-missing \
  'mint provenance: SKIPPED — PORTAL_URL / PORTAL_OPERATOR_KEY not set'                 0 2 no
run_case mesh-unreadable \
  'FAIL: could not read https://netbird.example/api/peers — refusing to pass on an unread mesh' \
                                                                                        1 2 no
run_case taxonomy-missing \
  'FAIL: missing /usr/local/lib/goat/provenance-guard/ops/ansible/netbird_acl_taxonomy.yml — scope prefixes are sourced from the taxonomy' \
                                                                                        1 2 no
run_case pyyaml-missing \
  'FAIL: python3 yaml module not available (pip install pyyaml) — scope'                1 2 no
run_case no-site-prefix \
  'FAIL: no site_prefix values found in the taxonomy — cannot strip scopes'             1 2 no

# PARTIAL blindness is still blindness. The portal answered, most peers graded,
# and some did not — publishing that as a clean run would be a lie about
# coverage rather than about state.
run_case partial-unknown \
  'mint provenance (4 gear-class peers):
  aligned           3/4
  pending-reconcile 0/4
  violation         0/4
  unknown           1/4'                                                                0 2 no

# Report-only posture: violations exist, the check exits 0 because enforcement is
# off. That is standing debt, not a regression — the contract says never fail on
# a backlog, so the unit must stay green and the metric must still publish the
# count somebody works down.
run_case violations-report-only \
  'mint provenance (4 gear-class peers):
  aligned           3/4
  pending-reconcile 0/4
  violation         1/4
  unknown           0/4
  report-only: violations present; set PROVENANCE_ENFORCE=1 to fail on them'            0 0 yes

if [ "$fails" -eq 0 ]; then
  echo "OK: machine-provenance-guard wrapper — exit-code contract holds across 10 cases"
  echo "    (clean, enforced violation, 6 unreadable markers, partial unknown, report-only)"
  exit 0
fi
echo "${fails} failure(s)" >&2
exit 1
