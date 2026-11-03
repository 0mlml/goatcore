#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# austerity.sh — contract a goatnet under resource pressure, and restore it.
#
# The shed order is the DEPLOY order reversed, because both are the dependency
# order: site-bootstrap builds a site bottom-up and contraction unwinds it from
# the top. Tier membership and the reasoning live in ops/austerity/tiers.yaml,
# not in this script — a judgement about blast radius belongs somewhere a
# reviewer can diff.
#
# WHY stop/start AND NOT down/up. `docker stop` leaves the container present
# with its config and volumes intact, so restore is `docker start` — seconds,
# no image pull, no re-render, and it works when the registry is unreachable.
# `compose down` would destroy that and make recovery depend on the network you
# are already short of. Under pressure, restore must not need anything.
#
# WHAT IT WILL NOT DO:
#   - touch tier 0 (the floor), ever — there is no flag for it
#   - touch a container matching NO tier: unclassified is reported, never
#     stopped. Silence is not consent, and a container nobody classified is
#     exactly the one whose loss you cannot predict.
#   - do anything at all without --apply. Dry-run is the default.
#
# USAGE
#   austerity.sh status                 what is running, by tier
#   austerity.sh contract 5             shed tier 5 (dry run)
#   austerity.sh contract 3 --apply     shed tiers 5,4,3 — cumulative
#   austerity.sh restore --apply        start everything stopped, floor-up
#   austerity.sh restore 4 --apply      restore down to tier 4 only
#
# Contraction is cumulative and ordered: `contract 3` sheds 5, then 4, then 3,
# in that order. Restore runs the reverse — lowest tier first — so anything
# depended upon is back before its dependents.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TIERS="${AUSTERITY_TIERS:-$HERE/../austerity/tiers.yaml}"
STATE="${AUSTERITY_STATE:-/var/lib/goat-austerity/shed.list}"
APPLY=0
DOCKER="${DOCKER:-docker}"

die(){ echo "austerity: $*" >&2; exit 1; }
[ -r "$TIERS" ] || die "tier file not readable: $TIERS"
command -v python3 >/dev/null || die "python3 required (parses tiers.yaml)"

# tier_of <container-name> -> tier number, or "-" when unclassified
tier_of(){
  python3 - "$TIERS" "$1" <<'PY'
import sys, yaml
tiers = yaml.safe_load(open(sys.argv[1]))["tiers"]
name = sys.argv[2]
# most specific wins: longest matching prefix across all tiers
best, blen = "-", -1
for t in tiers:
    for m in t["match"]:
        if name.startswith(m) and len(m) > blen:
            best, blen = str(t["tier"]), len(m)
print(best)
PY
}

running(){ $DOCKER ps    --format '{{.Names}}' 2>/dev/null | sort; }
stopped(){ $DOCKER ps -a --filter status=exited --format '{{.Names}}' 2>/dev/null | sort; }

classify(){                       # emits "<tier> <name>" for every running container
  while read -r n; do [ -n "$n" ] && echo "$(tier_of "$n") $n"; done < <(running)
}

cmd_status(){
  local map; map="$(classify)"
  echo "== running by tier (0 = floor, never shed) =="
  for t in 0 1 2 3 4 5; do
    local names count
    names="$(awk -v t="$t" '$1==t{printf "%s ",$2}' <<<"$map")"
    count="$(wc -w <<<"$names" | tr -d ' ')"
    printf "  tier %s  %-3s  %s\n" "$t" "$count" "${names:0:100}"
  done
  local un; un="$(awk '$1=="-"{printf "%s ",$2}' <<<"$map")"
  [ -n "$un" ] && { echo; echo "  UNCLASSIFIED (never touched — classify these in tiers.yaml):"; echo "    $un"; }
  [ -s "$STATE" ] && { echo; echo "== previously shed by this tool =="; sed 's/^/    /' "$STATE"; }
  return 0
}

cmd_contract(){
  local floor="$1"
  [[ "$floor" =~ ^[1-5]$ ]] || die "contract level must be 1-5 (0 is the floor and is never shed)"
  local map; map="$(classify)"
  mkdir -p "$(dirname "$STATE")" 2>/dev/null || true
  for t in 5 4 3 2 1; do
    [ "$t" -lt "$floor" ] && continue
    local names; names="$(awk -v t="$t" '$1==t{print $2}' <<<"$map")"
    [ -z "$names" ] && continue
    echo "-- tier $t --"
    while read -r n; do
      [ -z "$n" ] && continue
      if [ "$APPLY" = 1 ]; then
        $DOCKER stop "$n" >/dev/null 2>&1 && echo "   stopped  $n" || echo "   FAILED   $n"
        grep -qxF "$n" "$STATE" 2>/dev/null || echo "$n" >> "$STATE"
      else
        echo "   would stop  $n"
      fi
    done <<<"$names"
  done
  [ "$APPLY" = 1 ] || echo -e "\ndry run — nothing stopped. Re-run with --apply."
}

cmd_restore(){
  local down_to="${1:-1}"
  [ -s "$STATE" ] || { echo "austerity: nothing recorded as shed ($STATE)"; return 0; }
  # lowest tier first, so a dependency is back before its dependents
  local pairs=""
  while read -r n; do [ -n "$n" ] && pairs+="$(tier_of "$n") $n"$'\n'; done < "$STATE"
  for t in 1 2 3 4 5; do
    [ "$t" -lt "$down_to" ] && continue
    local names; names="$(awk -v t="$t" '$1==t{print $2}' <<<"$pairs")"
    [ -z "$names" ] && continue
    echo "-- tier $t --"
    while read -r n; do
      [ -z "$n" ] && continue
      if [ "$APPLY" = 1 ]; then
        $DOCKER start "$n" >/dev/null 2>&1 && { echo "   started  $n"; sed -i.bak "/^${n}$/d" "$STATE" 2>/dev/null || true; } \
          || echo "   FAILED   $n  (re-run its ansible role — the container may have been removed)"
      else
        echo "   would start  $n"
      fi
    done <<<"$names"
  done
  rm -f "${STATE}.bak" 2>/dev/null || true
  [ "$APPLY" = 1 ] || echo -e "\ndry run — nothing started. Re-run with --apply."
}

SUB="${1:-status}"; shift || true
ARG=""
for a in "$@"; do
  case "$a" in
    --apply) APPLY=1 ;;
    -h|--help) sed -n '3,40p' "$0"; exit 0 ;;
    *) ARG="$a" ;;
  esac
done

case "$SUB" in
  status)   cmd_status ;;
  contract) [ -n "$ARG" ] || die "contract needs a level, e.g. 'contract 5'"; cmd_contract "$ARG" ;;
  restore)  cmd_restore "${ARG:-1}" ;;
  -h|--help) sed -n '3,40p' "$0" ;;
  *) die "unknown subcommand '$SUB' (status | contract <1-5> | restore [level])" ;;
esac
