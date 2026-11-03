#!/usr/bin/env bash
# Managed by ansible (roles/topic-decode-health-guard). Do not edit on the host.
#
# Wrapper around ops/ci/check-topic-decode-health.py. Three jobs: give the run a
# durable report, decide which grades count as a REGRESSION, and emit metrics.
#
# EXIT CODES (docs/design/live-state-guard-scheduling.md §4). This runs
# unattended, so the code is a signal somebody has to interpret without context:
#
#   0  the portal and the router were both read, and no topic is in a grade this
#      goatnet treats as a regression. Topics may still be NO-SAMPLE or STALE —
#      that is operational state, it is in the report and the metrics, and the
#      contract says never fail on a backlog.
#   1  the condition this guard exists to catch: a topic whose bytes do not
#      match its declared format. Read the report.
#   2  A SOURCE COULD NOT BE READ. Says nothing about decode health. Never
#      treat a 2 as clean.
#
# WHY THE CHECK'S OWN EXIT CODE IS NOT PASSED THROUGH. The check exits 1 on
# UNDECODABLE, DOUBLE-ENCODED, NO-SAMPLE **or** STALE. That is right for a
# hand-run tool — a human asking "is everything fine" wants to hear about a
# silent topic. It is wrong for an hourly unit: a sensor switched off makes its
# topics legitimately NO-SAMPLE and the unit would fail every hour until
# somebody muted it. A muted guard is the failure
# this role exists to prevent, arriving by a different road.
#
# So FAIL_ON names the grades that are a regression on THIS goatnet, and this
# wrapper classifies. The check is not forked, and its own exit 2 is honoured
# exactly as it comes.
#
# THE BASELINE, and why a guard needs one. FAIL_ON alone still fails forever on
# a topic that is already broken — which is the same muted-unit ending by a
# slower road. The contract says gate on the
# REGRESSION, not on standing debt.
#
# BASELINE_FILE names topics that are known-bad, each with a reason and a date
# it is expected to clear. Those are reported, counted and EXCLUDED from the
# exit code. Anything not in the file still fails, so a NEW bad topic is still
# caught the hour it appears — which is the whole point.
#
# Two things keep this from becoming a place bugs go to die:
#   - the file is rendered from group_vars, never edited on the host, so an
#     entry costs a reviewed commit that states why;
#   - an entry past its clear_by date STOPS suppressing and fails again. A
#     baseline without an expiry is just deleting the check.
set -uo pipefail

REPORT_DIR="${REPORT_DIR:-/var/lib/goat/topic-decode-health-guard}"
TEXTFILE_DIR="${TEXTFILE_DIR:-}"
LIB_DIR="${LIB_DIR:-/usr/local/lib/goat/decode-health}"
FAIL_ON="${FAIL_ON:-UNDECODABLE,DOUBLE-ENCODED}"
# Known-bad topics that must NOT fail this unit. See the header note below.
BASELINE_FILE="${BASELINE_FILE:-}"
STALE_AFTER="${STALE_AFTER:-3600}"
CHECK="${LIB_DIR}/check-topic-decode-health.py"

mkdir -p "$REPORT_DIR"

if [ ! -x "$CHECK" ]; then
  echo "FAIL: ${CHECK} missing or not executable — nothing was read." >&2
  exit 2
fi

# Two runs would be two reads of the fabric for one answer, so take JSON once
# and render the human table from it rather than calling the check twice.
json="$(mktemp)"
rc=0
"$CHECK" --json --stale-after "$STALE_AFTER" > "$json" 2>"${json}.err" || rc=$?

# The check exits 2 itself on an unreadable source or a missing credential, and
# that is authoritative — it knows which GET failed and this wrapper does not.
if [ "$rc" -eq 2 ]; then
  {
    echo "# topic-decode-health-guard"
    echo "# finished:  $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo "# exit:      2"
    echo "# meaning:   A SOURCE COULD NOT BE READ. This says nothing. Not clean."
    echo
    cat "${json}.err"
  } > "${REPORT_DIR}/last-run.txt"
  cat "${REPORT_DIR}/last-run.txt"
  rm -f "$json" "${json}.err"
  # No metric written. See the note further down: leaving the previous
  # timestamp to go stale is the point.
  exit 2
fi

# --- classify + render --------------------------------------------------------
# python3 rather than awk: the payload is JSON, and grep-ing a JSON document for
# grade names would count a grade appearing inside a `detail` string. The check
# already emitted structured output; parsing it as text would throw that away.
summary="$(python3 - "$json" "$FAIL_ON" "$BASELINE_FILE" <<'PY'
import json, sys, datetime

with open(sys.argv[1]) as fh:
    doc = json.load(fh)

fail_on = {g.strip() for g in sys.argv[2].split(",") if g.strip()}
rows = doc.get("topics", [])
# null means the axis was SKIPPED (no router). Distinct from [] = none found;
# reporting 0 for a check nobody ran is the same lie as a green guard that
# never read anything.
unregistered = doc.get("unregistered")
unreg_checked = unregistered is not None
unregistered = unregistered or []

# Baseline: "<key>\t<verdict>\t<clear_by>\t<why>" per line, rendered from
# group_vars. An entry whose clear_by has passed is treated as ABSENT, so it
# starts failing again rather than quietly suppressing forever.
today = datetime.date.today()
baseline, expired = {}, []
if len(sys.argv) > 3 and sys.argv[3]:
    try:
        with open(sys.argv[3]) as fh:
            for line in fh:
                line = line.rstrip("\n")
                if not line or line.startswith("#"):
                    continue
                parts = line.split("\t")
                if len(parts) < 3:
                    continue
                key, verdict, clear_by = parts[0], parts[1], parts[2]
                why = parts[3] if len(parts) > 3 else ""
                try:
                    if datetime.date.fromisoformat(clear_by) < today:
                        expired.append((key, verdict, clear_by, why))
                        continue
                except ValueError:
                    # An unparseable date must not silently suppress.
                    expired.append((key, verdict, clear_by, why))
                    continue
                baseline[(key, verdict)] = (clear_by, why)
    except FileNotFoundError:
        pass

counts = {}
for r in rows:
    counts[r["verdict"]] = counts.get(r["verdict"], 0) + 1

def baselined(r):
    return (r["key"], r["verdict"]) in baseline

regressions = [r for r in rows if r["verdict"] in fail_on and not baselined(r)]
suppressed = [r for r in rows if r["verdict"] in fail_on and baselined(r)]

# Line 1 is consumed by the shell; everything after is the human report.
print("RC " + ("1" if regressions else "0"))
print("COUNTS " + " ".join(f"{k}={v}" for k, v in sorted(counts.items())))
print("UNREGISTERED %s" % (len(unregistered) if unreg_checked else "skipped"))
print("BASELINED %d" % len(suppressed))
print("---")
print("failing grades on this goatnet: %s" % (", ".join(sorted(fail_on)) or "(none)"))
print("total registered topics: %d" % len(rows))
if suppressed:
    print()
    print("BASELINED (%d) — known-bad, declared, NOT failing this run:" % len(suppressed))
    for r in suppressed:
        clear_by, why = baseline[(r["key"], r["verdict"])]
        print(f"  {r['verdict']:<15} {r['key']}")
        print(f"                  clears by {clear_by} — {why}")
if expired:
    print()
    print("BASELINE EXPIRED (%d) — the date passed, so these fail again:" % len(expired))
    for key, verdict, clear_by, why in expired:
        print(f"  {verdict:<15} {key}  (was due {clear_by} — {why})")
print()

order = {"DOUBLE-ENCODED": 0, "UNDECODABLE": 1, "NO-SAMPLE": 2,
         "STALE": 3, "UNGRADED": 4, "OK": 5}
print(f"{'VERDICT':<15} {'FORMAT':<28} {'AGE':>8}  KEY")
print("-" * 110)
for r in sorted(rows, key=lambda r: (order.get(r["verdict"], 9), r["key"])):
    age = f"{int(r['age_s'])}s" if r.get("age_s") is not None else "-"
    if r["verdict"] in fail_on:
        flag = "  <-- baselined" if baselined(r) else "  <-- REGRESSION"
    else:
        flag = ""
    print(f"{r['verdict']:<15} {r['format']:<28} {age:>8}  {r['key']}{flag}")
    if r.get("detail"):
        print(f"{'':<15} {'':<28} {'':>8}  -> {r['detail']}")

if unregistered:
    # Advisory (ADR 1028): topic visibility is not access, so publishing
    # without a registry row is legal. Still worth printing — a stream nobody
    # declared is a stream nobody is grading.
    print(f"\nUNREGISTERED ({len(unregistered)}) — on the fabric with no registry row.")
    print("Advisory, does not fail this run:")
    for u in unregistered[:20]:
        print("  " + u["key"])
    if len(unregistered) > 20:
        print(f"  ... and {len(unregistered) - 20} more")
PY
)" || {
  echo "FAIL: could not parse the check's JSON output — treating as unreadable." >&2
  cat "${json}.err" >&2
  rm -f "$json" "${json}.err"
  exit 2
}

final_rc="$(printf '%s\n' "$summary" | sed -n '1s/^RC //p')"
counts_line="$(printf '%s\n' "$summary" | sed -n '2s/^COUNTS //p')"
unreg_count="$(printf '%s\n' "$summary" | sed -n '3s/^UNREGISTERED //p')"
baselined_count="$(printf '%s\n' "$summary" | sed -n '4s/^BASELINED //p')"
body="$(printf '%s\n' "$summary" | sed -n '6,$p')"

# --- report -------------------------------------------------------------------
{
  echo "# topic-decode-health-guard"
  echo "# finished:  $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "# exit:      ${final_rc}  (check rc ${rc})"
  case "$final_rc" in
    0) echo "# meaning:   read both sources; no NEW topic is in a failing grade" ;;
    1) echo "# meaning:   a topic's bytes do not match its declared format" ;;
  esac
  echo
  printf '%s\n' "$body"
} > "${REPORT_DIR}/last-run.txt"

# --- metrics ------------------------------------------------------------------
# Stamped ONLY after both sources were read. The exit-2 path above returns
# before reaching here on purpose: a metric that keeps advancing while the guard
# is blind is worse than one that visibly stops.
if [ -z "$TEXTFILE_DIR" ]; then
  echo "WARNING: TEXTFILE_DIR is unset — this run publishes NO metric and is visible" >&2
  echo "         only in this host's journal. Set topic_decode_health_guard_textfile_dir" >&2
  echo "         to the directory this host's node_exporter actually reads, and confirm" >&2
  echo "         the series reaches Prometheus rather than just the disk." >&2
elif [ ! -d "$TEXTFILE_DIR" ]; then
  echo "WARNING: ${TEXTFILE_DIR} does not exist — running without metrics." >&2
else
  prom="${TEXTFILE_DIR}/topic_decode_health_guard.prom"
  ptmp="${prom}.$$"
  {
    echo "# HELP topic_decode_health_guard_last_run_timestamp_seconds Unix time of the last run that read BOTH sources."
    echo "# TYPE topic_decode_health_guard_last_run_timestamp_seconds gauge"
    echo "topic_decode_health_guard_last_run_timestamp_seconds $(date +%s)"
    echo "# HELP topic_decode_health_guard_topics Registered topics by decode grade."
    echo "# TYPE topic_decode_health_guard_topics gauge"
    for kv in $counts_line; do
      grade="${kv%%=*}"
      n="${kv##*=}"
      echo "topic_decode_health_guard_topics{grade=\"${grade}\"} ${n}"
    done
    # Emitted ONLY when the axis actually ran. Publishing 0 for a skipped check
    # would make "nobody looked" graph identically to "nothing undeclared", which
    # is the same class of lie as a guard that reports clean without reading.
    # An absent series is honest and alertable with absent(); a fake zero is not.
    if [ -n "$unreg_count" ] && [ "$unreg_count" != "skipped" ]; then
      echo "# HELP topic_decode_health_guard_unregistered Keys on the fabric with no registry row (advisory, ADR 1028)."
      echo "# TYPE topic_decode_health_guard_unregistered gauge"
      echo "topic_decode_health_guard_unregistered ${unreg_count}"
    fi
    # Baselined topics are counted SEPARATELY and never folded into the grade
    # gauges. A baseline that made the debt invisible would be worse than the
    # red unit it replaced — this keeps it visible and graphable while it stops
    # being an alarm.
    echo "# HELP topic_decode_health_guard_baselined Known-bad topics declared in group_vars, excluded from the exit code."
    echo "# TYPE topic_decode_health_guard_baselined gauge"
    echo "topic_decode_health_guard_baselined ${baselined_count:-0}"
  } > "$ptmp"
  mv -f "$ptmp" "$prom"   # atomic: node_exporter must never read a half-written file
fi

rm -f "$json" "${json}.err"

# To the journal too, so `journalctl -u topic-decode-health-guard` is
# self-sufficient and nobody needs to know the report path exists.
cat "${REPORT_DIR}/last-run.txt"

exit "${final_rc:-2}"
