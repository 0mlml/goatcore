#!/usr/bin/env bash
# Managed by ansible (roles/mesh-membership-reconcile). Do not edit on the host.
#
# Thin wrapper around check-membership-reconcile.py. Its only jobs are to give
# the run a durable report file and to make its exit code mean one thing.
#
# EXIT CODES, because this runs unattended and a systemd failure is a signal
# somebody will have to interpret at 3am:
#   0  the mesh was read, and no declared-absent group has come back.
#      Drift may be non-zero — that is a backlog, not a regression, and it is
#      in the report and the metrics.
#   1  a group that was deliberately removed is present again. This is the
#      thing the unit exists to catch. Read the report.
#   2  the mesh could not be read at all (no token, API unreachable, bad URL).
#      Says nothing about membership. Do NOT read a 2 as "clean".
set -euo pipefail

REPORT_DIR="${REPORT_DIR:-/var/lib/goat/membership-reconcile}"
TEXTFILE_DIR="${TEXTFILE_DIR:-}"
CHECK=/usr/local/lib/goat/check-membership-reconcile.py

mkdir -p "$REPORT_DIR"

# No default path. A wrong-but-writable textfile dir is the F-335 trap: the file
# lands, nothing reads it, and every surface says the metric is being published.
# Unset means "this host publishes no metric", said out loud, every run.
metrics_arg=()
if [ -z "$TEXTFILE_DIR" ]; then
  echo "WARNING: TEXTFILE_DIR is unset — this run publishes NO metric and is" >&2
  echo "         visible only in this host's journal. Set" >&2
  echo "         mesh_membership_reconcile_textfile_dir to the directory this" >&2
  echo "         host's node_exporter actually reads." >&2
elif [ -d "$TEXTFILE_DIR" ]; then
  metrics_arg=(--metrics "$TEXTFILE_DIR/mesh_membership_reconcile.prom")
else
  echo "WARNING: $TEXTFILE_DIR does not exist — running without metrics." >&2
fi

tmp="$REPORT_DIR/.last-run.$$"
# Clean the scratch files on EVERY exit path, not just the happy one. Without
# this, any failure between writing $tmp and the mv leaves .last-run.<pid>
# behind — once a day, forever, in a directory nobody looks at until they are
# investigating something else. Unbounded growth from a job that is supposed to
# be invisible is the worst kind.
trap 'rm -f "$tmp" "$tmp.final"' EXIT

rc=0
python3 "$CHECK" --strict-reverted "${metrics_arg[@]}" > "$tmp" 2>&1 || rc=$?

{
  echo "# mesh-membership-reconcile"
  echo "# finished: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "# exit:     $rc"
  echo
  cat "$tmp"
} > "$tmp.final"
mv -f "$tmp.final" "$REPORT_DIR/last-run.txt"

# Bounded history. An audit whose only artifact is "the last run" cannot answer
# "when did this drift row appear", which is the first question anyone asks.
# Retention is enforced HERE rather than by a logrotate drop-in on purpose: an
# external rotation config that is missing fails silently and lets the directory
# grow forever, which is exactly the F-335 shape — a mechanism that looks
# present and is connected to nothing. Self-pruning cannot be forgotten.
HISTORY_DIR="$REPORT_DIR/history"
HISTORY_KEEP="${HISTORY_KEEP:-30}"
mkdir -p "$HISTORY_DIR"
cp -f "$REPORT_DIR/last-run.txt" "$HISTORY_DIR/$(date -u +%Y%m%dT%H%M%SZ).txt"
# Newest-first, drop everything past the keep count.
# shellcheck disable=SC2012  # ls is safe here: this directory holds only files
# this script creates, named `date -u +%Y%m%dT%H%M%SZ`.txt — fixed width, no
# spaces, no newlines. `find -printf` is not portable to the BSD userland this
# also has to run on.
ls -1t "$HISTORY_DIR" 2>/dev/null | tail -n "+$((HISTORY_KEEP + 1))" | while read -r old; do
  rm -f "$HISTORY_DIR/$old"
done

# To the journal as well, so `journalctl -u mesh-membership-reconcile` is
# self-sufficient and nobody has to know the report path exists. journald
# applies the host's own SystemMaxUse cap to this, so it is bounded by the same
# policy as everything else on the box rather than by anything this role sets.
cat "$REPORT_DIR/last-run.txt"

exit "$rc"
