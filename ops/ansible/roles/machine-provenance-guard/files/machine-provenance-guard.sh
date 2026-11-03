#!/usr/bin/env bash
# Managed by ansible (roles/machine-provenance-guard). Do not edit on the host.
#
# Wrapper around ops/ci/check-node-class-membership.sh. Two jobs: give the run a
# durable report, and make its exit code mean exactly one thing.
#
# EXIT CODES (docs/design/live-state-guard-scheduling.md §4). This runs
# unattended, so the code is a signal somebody has to interpret without context:
#
#   0  the mesh AND the portal registry were read, and no gear peer is
#      unaccounted for. Under report-only posture, violations may exist — that
#      is standing debt, it is in the report and the metrics, and it is NOT a
#      regression. The contract says never fail on a backlog.
#   1  the condition this guard exists to catch: a peer wearing a gear class
#      with no custody edge, under PROVENANCE_ENFORCE=1. Or a person/gear class
#      confusion from the membership stage. Read the report.
#   2  A SOURCE COULD NOT BE READ. Says nothing about provenance. Never treat a
#      2 as clean.
#
# Why 2 needs synthesising here rather than in the check: the check grades an
# unreachable portal as `unknown` and still exits 0, deliberately, because its
# membership stage is worth running on a mesh it CAN read. That is right for a
# hand-run tool and wrong for an unattended one — a run that saw nothing must
# not stamp a fresh timestamp and go quiet. So this wrapper classifies the
# output and overrides. The check is not forked.
set -uo pipefail

REPORT_DIR="${REPORT_DIR:-/var/lib/goat/machine-provenance-guard}"
TEXTFILE_DIR="${TEXTFILE_DIR:-}"
LIB_DIR="${LIB_DIR:-/usr/local/lib/goat/provenance-guard}"
CHECK="${LIB_DIR}/ops/ci/check-node-class-membership.sh"

mkdir -p "$REPORT_DIR"

if [ ! -x "$CHECK" ]; then
  echo "FAIL: ${CHECK} missing or not executable — nothing was read." >&2
  exit 2
fi

tmp="$(mktemp)"
rc=0
"$CHECK" > "$tmp" 2>&1 || rc=$?

# --- classify -----------------------------------------------------------------
# Markers the check prints when it could not read one of its two sources. Kept as
# literal strings from the check's own output; the test pins each one, so a
# reworded message fails a test rather than silently degrading a 2 into a 0.
unreadable=0
grep -qF "FAIL: could not read"                              "$tmp" && unreadable=1
grep -qF "mint provenance: SKIPPED"                          "$tmp" && unreadable=1
grep -qF "portal unreachable or /api/machines error"         "$tmp" && unreadable=1
grep -qF "FAIL: missing"                                     "$tmp" && unreadable=1
grep -qF "FAIL: python3 yaml module not available"           "$tmp" && unreadable=1
grep -qF "FAIL: no site_prefix values found in the taxonomy" "$tmp" && unreadable=1

# A non-empty `unknown` bucket means SOME gear peers could not be graded even
# though the portal answered. Partial blindness is still blindness: grading it 0
# would publish a clean run over an incomplete read.
if awk '/^  unknown  +[0-9]+\/[0-9]+$/ { split($2, a, "/"); if (a[1] + 0 > 0) found = 1 }
        END { exit !found }' "$tmp"; then
  unreadable=1
fi

if [ "$unreadable" -eq 1 ]; then
  final_rc=2
elif [ "$rc" -ne 0 ]; then
  final_rc=1
else
  final_rc=0
fi

# --- report -------------------------------------------------------------------
{
  echo "# machine-provenance-guard"
  echo "# finished:  $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "# exit:      ${final_rc}  (check rc ${rc})"
  case "$final_rc" in
    0) echo "# meaning:   read both sources; nothing unaccounted for" ;;
    1) echo "# meaning:   a gear peer has no custody edge — it did not come through the mint" ;;
    2) echo "# meaning:   A SOURCE COULD NOT BE READ. This says nothing. Not clean." ;;
  esac
  echo
  cat "$tmp"
} > "${tmp}.final"
mv -f "${tmp}.final" "${REPORT_DIR}/last-run.txt"

# --- metrics ------------------------------------------------------------------
# Stamped ONLY after both sources were read. On a 2 the previous timestamp is
# left to go stale on purpose: a metric that keeps advancing while the guard is
# blind is worse than one that visibly stops.
if [ -z "$TEXTFILE_DIR" ]; then
  echo "WARNING: TEXTFILE_DIR is unset — this run publishes NO metric and is visible" >&2
  echo "         only in this host's journal. Set machine_provenance_guard_textfile_dir" >&2
  echo "         to the directory this host's node_exporter actually reads, and confirm" >&2
  echo "         the series reaches Prometheus rather than just the disk." >&2
elif [ ! -d "$TEXTFILE_DIR" ]; then
  echo "WARNING: ${TEXTFILE_DIR} does not exist — running without metrics." >&2
elif [ "$final_rc" -eq 2 ]; then
  echo "NOTE: source unreadable — leaving the previous metric to go stale rather than" >&2
  echo "      stamping a fresh timestamp over a run that saw nothing." >&2
else
  prom="${TEXTFILE_DIR}/machine_provenance_guard.prom"
  ptmp="${prom}.$$"
  {
    echo "# HELP machine_provenance_guard_last_run_timestamp_seconds Unix time of the last run that read BOTH sources."
    echo "# TYPE machine_provenance_guard_last_run_timestamp_seconds gauge"
    echo "machine_provenance_guard_last_run_timestamp_seconds $(date +%s)"
    echo "# HELP machine_provenance_guard_peers Gear-class peers by provenance grade."
    echo "# TYPE machine_provenance_guard_peers gauge"
    awk '/^  (aligned|pending-reconcile|violation|unknown)  +[0-9]+\/[0-9]+$/ {
           split($2, a, "/");
           printf "machine_provenance_guard_peers{grade=\"%s\"} %d\n", $1, a[1]
         }' "$tmp"
    echo "# HELP machine_provenance_guard_enforcing 1 when a violation fails this unit."
    echo "# TYPE machine_provenance_guard_enforcing gauge"
    echo "machine_provenance_guard_enforcing ${PROVENANCE_ENFORCE:-0}"
  } > "$ptmp"
  mv -f "$ptmp" "$prom"   # atomic: node_exporter must never read a half-written file
fi

rm -f "$tmp"

# To the journal too, so `journalctl -u machine-provenance-guard` is
# self-sufficient and nobody needs to know the report path exists.
cat "${REPORT_DIR}/last-run.txt"

exit "$final_rc"
