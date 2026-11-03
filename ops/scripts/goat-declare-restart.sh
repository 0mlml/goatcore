#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# goat-declare-restart.sh — record that a restart was DELIBERATE, so the
# restart panels and the wedge alerts can tell intent from failure.
#
# THE DEFECT THIS EXISTS FOR. A container restart panel can show many restarts
# in 24h with the router among them. That is the signature of a fabric wedge, and
# operator-directed work such as a CA migration looks identical. A reader of the
# dashboard alone cannot tell them apart, because
# `changes(container_start_time_seconds[24h])` cannot see intent.
#
# Both halves of the problem are real and this closes the actor half:
#
#   ACTOR    — whatever causes the restart says so, here, before it acts.
#   RECEIVER — the panels subtract declared restarts from observed ones and
#              show "unplanned" separately; see the container-incident
#              dashboard and docs/design/deliberate-restart-tagging.md.
#
# A restart that is NOT declared is not assumed malicious — it is assumed
# unexplained, which is the honest default and the one worth alerting on.
#
# Usage:
#   goat-declare-restart.sh <textfile-dir> <container> <actor> <reason>
#
#   <textfile-dir>  node-exporter's --collector.textfile.directory. NOT a fixed
#                   path: goat-obs writes to its compose tree
#                   (<compose>/expected-vendors/textfile) while the node-exporter
#                   role uses /var/lib/node_exporter/textfile. Pass the one this
#                   host actually serves, the way goat-obs-textfile.sh does.
#   <container>     container name as `docker ps` shows it, matching the `name`
#                   label goat_container_info publishes.
#   <actor>         who is doing it — a track name, a playbook, an operator
#                   handle. Free text, no spaces.
#   <reason>        short slug: ca-rotation, image-bump, config-reload, converge.
#
# Example, from a role that is about to bounce the router:
#   goat-declare-restart.sh /opt/obs/expected-vendors/textfile \
#       zenoh-router own-ca ca-rotation
#
# Emits, in <textfile-dir>/goat_declared_restart.prom:
#   goat_declared_restart_total{container=,actor=,reason=}      counter
#   goat_declared_restart_last_timestamp_seconds{container=}    gauge
#
# The counter is monotonic per label set across invocations: this script reads
# the file it wrote last time and adds one, because a Prometheus counter that
# resets on every write is a counter that `increase()` reads as zero.

set -euo pipefail

DIR="${1:?usage: goat-declare-restart.sh <textfile-dir> <container> <actor> <reason>}"
CONTAINER="${2:?missing <container>}"
ACTOR="${3:?missing <actor>}"
REASON="${4:?missing <reason>}"

[ -d "$DIR" ] || { echo "goat-declare-restart: no such textfile dir: $DIR" >&2; exit 1; }

# Label values go into an exposition file; a quote or backslash in one produces a
# file node_exporter rejects WHOLE, taking every other metric in it down with the
# bad line. Refuse rather than emit that.
for v in "$CONTAINER" "$ACTOR" "$REASON"; do
  case "$v" in
    *[\"\\$'\n']*|"")
      echo "goat-declare-restart: label values may not be empty or contain quotes, backslashes or newlines: '$v'" >&2
      exit 1 ;;
  esac
done

OUT="${DIR}/goat_declared_restart.prom"
TMP="$(mktemp "${DIR}/.goat_declared_restart.prom.XXXXXX")"
trap 'rm -f "$TMP"' EXIT

NOW="$(date +%s)"
KEY="goat_declared_restart_total{container=\"${CONTAINER}\",actor=\"${ACTOR}\",reason=\"${REASON}\"}"

# Current value for THIS label set, 0 if the file or the series is new.
CUR=0
if [ -f "$OUT" ]; then
  CUR="$(awk -v k="$KEY" '$0 ~ "^" k " " {print $NF}' "$OUT" | tail -1)"
  [ -n "$CUR" ] || CUR=0
fi
NEXT=$(( CUR + 1 ))

{
  echo "# HELP goat_declared_restart_total Restarts an actor declared as deliberate before performing them."
  echo "# TYPE goat_declared_restart_total counter"
  # Carry every OTHER series forward unchanged — this file is the only record of
  # them, so rewriting it from scratch would reset counters the receiver reads
  # with increase().
  if [ -f "$OUT" ]; then
    grep '^goat_declared_restart_total{' "$OUT" 2>/dev/null | grep -vF "$KEY " || true
  fi
  echo "${KEY} ${NEXT}"
  echo "# HELP goat_declared_restart_last_timestamp_seconds Unix time of the last declared restart of this container."
  echo "# TYPE goat_declared_restart_last_timestamp_seconds gauge"
  if [ -f "$OUT" ]; then
    grep '^goat_declared_restart_last_timestamp_seconds{' "$OUT" 2>/dev/null \
      | grep -vF "goat_declared_restart_last_timestamp_seconds{container=\"${CONTAINER}\"} " || true
  fi
  echo "goat_declared_restart_last_timestamp_seconds{container=\"${CONTAINER}\"} ${NOW}"
} > "$TMP"

chmod 0644 "$TMP"
mv "$TMP" "$OUT"
trap - EXIT

echo "goat-declare-restart: ${CONTAINER} by ${ACTOR} (${REASON}) -> ${KEY} = ${NEXT}"
