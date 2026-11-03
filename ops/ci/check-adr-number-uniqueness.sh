#!/usr/bin/env bash
# ops/ci/check-adr-number-uniqueness.sh — one number, one ADR.
#
# WHY: concurrent sessions allocate ADR numbers as "next free at authoring
# time", so two sessions racing can claim the same number with DIFFERENT
# filenames — which git then merges silently, leaving two ADRs with one
# number and every future citation ambiguous. This happened:
# open PR #2088 and merged PR #2090 both claimed 1111; caught only because a
# watching session diffed the tree, then hand-renumbered to 1112. Same class
# as the finding-ID races. This guard makes the collision fail loud at PR
# time on whichever branch merges second.
#
# GRANDFATHERED: 0823 carries two historical files (daily-state-reconciliation
# and mitigations-off-single-tenant-relay-risk-acceptance). Renumbering a
# merged ADR breaks external citations, so it stays. The baseline may only
# SHRINK — if 0823 is ever consolidated, remove it here rather than leaving a
# dead exemption.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

GRANDFATHERED=("0823")

dups=$(find docs/adr -maxdepth 1 -name '[0-9][0-9][0-9][0-9]-*.md' \
  | sed -E 's|.*/([0-9]{4})-.*|\1|' | sort | uniq -d)

fail=0
for n in $dups; do
  skip=0
  for g in "${GRANDFATHERED[@]}"; do
    [ "$n" = "$g" ] && skip=1
  done
  if [ "$skip" = 1 ]; then
    continue
  fi
  echo "FAIL: ADR number $n is claimed by more than one file:"
  find docs/adr -maxdepth 1 -name "${n}-*.md" | sort | sed 's/^/  /'
  fail=1
done

if [ "$fail" != 0 ]; then
  echo
  echo "Two ADRs racing for one number merge silently (different filenames)."
  echo "Renumber the LATER one to the next free number, update docs/adr/INDEX.md"
  echo "and every in-doc self-reference, and re-check numeric order in INDEX.md."
  exit 1
fi

# Ratchet hygiene: a grandfathered number that no longer duplicates should be
# removed from the baseline, not silently retained.
for g in "${GRANDFATHERED[@]}"; do
  count=$(find docs/adr -maxdepth 1 -name "${g}-*.md" | wc -l | tr -d ' ')
  if [ "$count" -lt 2 ]; then
    echo "NOTE: grandfathered ADR $g no longer duplicates (count=$count) — remove it from GRANDFATHERED."
  fi
done

echo "OK: ADR numbers unique (grandfathered: ${GRANDFATHERED[*]})"
