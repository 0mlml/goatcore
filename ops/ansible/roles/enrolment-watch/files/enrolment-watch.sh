#!/usr/bin/env bash
# Managed by ansible (roles/enrolment-watch). Do not edit on the host.
#
# Watches the netbird management server's own auth stream for a device that
# CANNOT ENROL and keeps trying. Emits Prometheus textfile metrics and a durable
# report.
#
# WHY THIS EXISTS. A WireGuard key can retry enrolment every few seconds, never
# succeed, and go on longer than the logs are kept, with nothing noticing.
#
# The failure shape is not a missing signal but an UNREAD one: a signal so
# constant it becomes the background, so a REAL enrolment failure during a cohort
# arrives in a log that already scrolls a stuck device past you many times a
# minute. The defect is nobody aggregating.
#
# WHAT IT IS NOT. It is not a firewall and must never become one. A failing
# client is usually internal — a misconfigured service, a stale setup key, a
# monitoring probe hitting an authenticated endpoint without credentials.
# Rate-limiting these would silence the symptom, leave the device permanently
# unenrolled, and destroy the evidence. The job is attribution, not suppression.
#
# EXIT CODES (docs/design/live-state-guard-scheduling.md §4). This runs
# unattended, so the code has to be readable without context:
#
#   0  the log was read, and no peer is stuck. Individual failures are normal —
#      a fat-fingered key, a client started before its key was minted — and are
#      counted, reported, and NOT failed on.
#   1  the condition this exists to catch: a peer key that has failed
#      repeatedly across the window and has never succeeded.
#   2  THE LOG COULD NOT BE READ. Says nothing about enrolment health. A 2 is
#      never clean — it is the state where this guard knows nothing, which is
#      indistinguishable from "all fine" if you only look at the exit code.
set -uo pipefail

CONTAINER="${ENROLWATCH_CONTAINER:-management-a}"
WINDOW="${ENROLWATCH_WINDOW:-60m}"
# A peer must fail at least this many times IN THE WINDOW before it is "stuck".
# Below it, a device is simply retrying while somebody mints its key.
THRESHOLD="${ENROLWATCH_THRESHOLD:-20}"
TEXTFILE_DIR="${ENROLWATCH_TEXTFILE_DIR:-}"
REPORT="${ENROLWATCH_REPORT:-/var/log/enrolment-watch/last-run.txt}"
BASELINE_FILE="${ENROLWATCH_BASELINE_FILE:-}"

mkdir -p "$(dirname "$REPORT")" 2>/dev/null || true
: > "$REPORT"

say() { echo "$*" | tee -a "$REPORT"; }

# --- read the source, or exit 2 ---------------------------------------------
if ! docker inspect "$CONTAINER" >/dev/null 2>&1; then
  echo "UNREADABLE: container $CONTAINER does not exist — not an enrolment verdict" >&2
  exit 2
fi
LOG="$(docker logs --since "$WINDOW" "$CONTAINER" 2>&1)" || {
  echo "UNREADABLE: could not read logs from $CONTAINER — not an enrolment verdict" >&2
  exit 2
}
if [ -z "$LOG" ]; then
  echo "UNREADABLE: $CONTAINER produced no log lines in $WINDOW — not an enrolment verdict" >&2
  exit 2
fi

say "enrolment-watch — container=$CONTAINER window=$WINDOW threshold=$THRESHOLD"
say "run: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
say ""

# --- failures, by peer key ---------------------------------------------------
# netbird logs `failed logging in peer <wg-pubkey>: <reason>`. The key is the
# only attribution the management server records for a peer that never enrols —
# there is no name, no IP and no row in the peers table until it succeeds, which
# is precisely why a stuck device is invisible from every other surface.
FAILS="$(printf '%s\n' "$LOG" | grep -oE 'failed logging in peer [A-Za-z0-9+/=]+' | awk '{print $NF}' | sort | uniq -c | sort -rn)"
SUCCESS_KEYS="$(printf '%s\n' "$LOG" | grep -oE 'peer [A-Za-z0-9+/=]{40,} (registered|logged in)' | awk '{print $2}' | sort -u)"

TOTAL_FAILS="$(printf '%s\n' "$LOG" | grep -c 'failed logging in peer' || true)"
say "total failed enrolment attempts in window: ${TOTAL_FAILS:-0}"

# Reasons, so the report says WHY rather than only how many.
say ""
say "by reason:"
printf '%s\n' "$LOG" | grep -oE 'failed logging in peer [A-Za-z0-9+/=]+: .*' \
  | sed -E 's/.*: //' | cut -c1-70 | sort | uniq -c | sort -rn | head -6 | tee -a "$REPORT"

# --- baseline: known-stuck peers do not fail the unit forever ----------------
is_baselined() {
  [ -n "$BASELINE_FILE" ] && [ -f "$BASELINE_FILE" ] || return 1
  local key="$1" line clear_by
  line="$(grep -F -- "$key" "$BASELINE_FILE" 2>/dev/null | head -1)" || return 1
  [ -n "$line" ] || return 1
  # An entry past its clear_by date STOPS suppressing — a baseline that never
  # expires is just a mute with extra steps.
  clear_by="$(printf '%s' "$line" | grep -oE 'clear_by=[0-9]{4}-[0-9]{2}-[0-9]{2}' | cut -d= -f2)"
  [ -n "$clear_by" ] || return 1
  [ "$(date -u +%Y-%m-%d)" \< "$clear_by" ] || return 1
  return 0
}

STUCK=0
STUCK_LINES=""
say ""
say "peers failing in this window:"
while read -r count key; do
  [ -z "${key:-}" ] && continue
  short="${key:0:12}"
  succeeded="no"
  printf '%s\n' "$SUCCESS_KEYS" | grep -qF -- "$key" && succeeded="yes"
  verdict="ok"
  if [ "$count" -ge "$THRESHOLD" ] && [ "$succeeded" = "no" ]; then
    if is_baselined "$key"; then
      verdict="STUCK (baselined)"
    else
      verdict="STUCK"
      STUCK=$((STUCK+1))
      STUCK_LINES="${STUCK_LINES}${short} ${count}\n"
    fi
  fi
  say "  $(printf '%-14s %6s attempts  succeeded=%-3s %s' "$short…" "$count" "$succeeded" "$verdict")"
done <<< "$FAILS"

say ""
if [ "$STUCK" -gt 0 ]; then
  say "VERDICT: $STUCK peer(s) stuck — repeatedly refused, never enrolled."
  say ""
  say "A stuck peer is a DEVICE SOMEBODY IS WAITING ON. It is not attack traffic"
  say "and must not be rate-limited: every instance found so far was our own"
  say "misconfiguration. To attribute it you need the client address, which the"
  say "management server does not record for a peer that never enrols — see the"
  say "role README for the Traefik access-log gap."
else
  say "VERDICT: no peer stuck."
fi

# --- metrics ----------------------------------------------------------------
if [ -n "$TEXTFILE_DIR" ] && [ -d "$TEXTFILE_DIR" ]; then
  tmp="$(mktemp)"
  {
    echo "# HELP goat_netbird_enrol_failures_total Failed peer enrolment attempts in the window."
    echo "# TYPE goat_netbird_enrol_failures_total gauge"
    echo "goat_netbird_enrol_failures_total ${TOTAL_FAILS:-0}"
    echo "# HELP goat_netbird_enrol_stuck_peers Peers repeatedly refused that have never enrolled."
    echo "# TYPE goat_netbird_enrol_stuck_peers gauge"
    echo "goat_netbird_enrol_stuck_peers ${STUCK}"
    echo "# HELP goat_netbird_enrol_watch_last_run_timestamp_seconds Unix time of the last completed run."
    echo "# TYPE goat_netbird_enrol_watch_last_run_timestamp_seconds gauge"
    echo "goat_netbird_enrol_watch_last_run_timestamp_seconds $(date -u +%s)"
    if [ -n "$STUCK_LINES" ]; then
      echo "# HELP goat_netbird_enrol_stuck_peer_attempts Attempts by a stuck peer, labelled by key prefix."
      echo "# TYPE goat_netbird_enrol_stuck_peer_attempts gauge"
      # here-string, not a pipe: the pipeline form silently emits nothing (header
      # written, no series), which is the worst kind of metric bug — the panel exists
      # and is always empty.
      while read -r k c; do
        [ -n "${k:-}" ] && echo "goat_netbird_enrol_stuck_peer_attempts{peer_key_prefix=\"${k}\"} ${c}"
      done <<< "$(printf '%b' "$STUCK_LINES")"
    fi
  } > "$tmp"
  # Atomic: node_exporter reads this directory continuously and a half-written
  # file is a parse error that looks like the guard broke.
  mv "$tmp" "${TEXTFILE_DIR%/}/enrolment_watch.prom"
  chmod 0644 "${TEXTFILE_DIR%/}/enrolment_watch.prom"
fi

[ "$STUCK" -gt 0 ] && exit 1
exit 0
