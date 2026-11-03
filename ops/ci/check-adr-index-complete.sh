#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# check-adr-index-complete.sh — every ADR on disk has its OWN row in
# docs/adr/INDEX.md.
#
# WHY THIS EXISTS: the index had a guard for DUPLICATE rows
# (check-adr-number-uniqueness.sh) and nothing whatsoever for a MISSING one. An
# ADR could exist on disk, be cited by other documents, and appear in no index
# — and no check anywhere would notice, because every existing guard asks a
# question about something that IS there.
#
# The trigger was concrete, : rebasing this branch onto main
# produced a conflict in INDEX.md between warrant's 1129 row and the surface
# canary's 1132 row. Git could apply EITHER side cleanly, and taking either one
# would have silently deleted the other ADR's row. Nothing downstream would
# have caught it — not the uniqueness guard (one row is not a duplicate), and
# not the doc-link ratchet (the ADR FILE still exists, so no link breaks). The
# only symptom is an ADR nobody can find from the index.
#
# Same fault class as the four guards found running on nothing that week:
# ABSENCE IS THE STATE NOBODY CHECKS. A row that was never added and a row that
# a merge deleted are indistinguishable, and both are invisible.
#
# RATCHET, not a gate: two ADRs are already unindexed and this does not force
# anyone to write their rows today. It fails on a NEW one, which is the case
# that is always a mistake — nobody deliberately adds an ADR and deliberately
# omits its row in the same change.
#
# Deliberately checks ONE direction. An INDEX row whose file does not exist is
# the opposite fault (ADRs cited-but-never-written, ~10 of them) and is already
# carried by check-doc-links.py's baseline; duplicating it here would mean two
# guards disagreeing about one number.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

INDEX="docs/adr/INDEX.md"
[ -r "$INDEX" ] || { echo "check-adr-index-complete: missing $INDEX" >&2; exit 1; }

# Known unindexed ADRs. SHRINK-ONLY: writing a row for one of these should be
# accompanied by removing it here, and the check tells you so. Never add to
# this list to make a failure go away — that is the whole point of the ratchet.
BASELINE="0865 1067"

# An ADR's OWN row starts the line: `| 0123 |`. A number appearing anywhere
# else on a line is a CROSS-REFERENCE from another ADR's row, which is exactly
# how 1067 hid — cited five times, indexed zero times. Matching loosely here
# would reproduce the bug this guard exists to catch.
indexed=$(grep -oE '^\| ?[0-9]{4} \|' "$INDEX" | grep -oE '[0-9]{4}' | sort -u)
ondisk=$(find docs/adr -maxdepth 1 -name '[0-9][0-9][0-9][0-9]-*.md' -exec basename {} \; \
         | grep -oE '^[0-9]{4}' | sort -u)

missing=$(comm -23 <(echo "$ondisk") <(echo "$indexed") || true)

new=""
for n in $missing; do
  case " $BASELINE " in
    *" $n "*) ;;                      # known debt, carried
    *) new="$new $n" ;;
  esac
done

# A baselined entry that is now indexed should leave the list, so the debt can
# only shrink. Reported, never fatal — a green build should not fail on someone
# else's good deed.
healed=""
for n in $BASELINE; do
  echo "$missing" | grep -qx "$n" || healed="$healed $n"
done

if [ -n "${new// /}" ]; then
  echo "check-adr-index-complete: FAIL — ADR(s) on disk with no row in $INDEX:" >&2
  for n in $new; do
    f=$(find docs/adr -maxdepth 1 -name "$n-*.md" -exec basename {} \; | head -1)
    echo "    $n  ($f)" >&2
  done
  echo "" >&2
  echo "  An ADR that exists, is cited, and appears in no index is findable by" >&2
  echo "  nobody. Add a row to $INDEX in ADR-number order." >&2
  echo "  If a MERGE dropped the row, re-read both sides: a conflict here" >&2
  echo "  applies cleanly either way and only one side keeps both rows." >&2
  exit 1
fi

if [ -n "${healed// /}" ]; then
  echo "check-adr-index-complete: OK — and$healed now indexed; drop from BASELINE in $0"
  exit 0
fi

echo "check-adr-index-complete: OK — every ADR on disk has a row (carrying $(echo $BASELINE | wc -w | tr -d ' ') known unindexed)"
