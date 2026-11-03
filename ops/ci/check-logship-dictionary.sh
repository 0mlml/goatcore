#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# check-logship-dictionary.sh — goat-logship event-dictionary fence (ADR 1078 §7).
#
# The dictionary (ops/logging/goat-logship/dictionary.yaml) is the compliance
# artifact and the CONTRACT for the Vector transforms. Every OCSF event the
# collector emits carries a `metadata.dictionary_id`; this fence fails CLOSED
# if a transform emits an id that is not defined in the dictionary — so a
# producer can never ship an unmapped class (design property "no unmapped
# class ships").
#
# It extracts every dictionary_id literal from the Vector config template and
# asserts each is a `cef_id` row in the dictionary. It also warns (does not
# fail) on dictionary rows no transform emits yet — those are the sequenced
# Phase-2/3 sources, expected to be unused until their producer lands.

set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

DICT="ops/logging/goat-logship/dictionary.yaml"
VECTOR_TMPL="ops/ansible/roles/goat-logship/templates/vector.yaml.j2"

FAIL=0

[ -f "$DICT" ] || { echo "[logship-dict] FAIL: missing $DICT"; exit 1; }
[ -f "$VECTOR_TMPL" ] || { echo "[logship-dict] FAIL: missing $VECTOR_TMPL"; exit 1; }

# Defined ids: the `cef_id:` values in the dictionary.
DEFINED=()
while IFS= read -r id; do
  DEFINED+=("$id")
done < <(grep -oE 'cef_id:[[:space:]]*[^[:space:]]+' "$DICT" | sed -E 's/.*cef_id:[[:space:]]*//' | sort -u)

# Emitted ids: dictionary_id string literals in the VRL. Two conventions are
# matched so a transform cannot smuggle an id past the fence:
#   1. object-literal form  — `"dictionary_id": "fabric:write"`
#   2. the `dict` variable  — `dict = "auth:ssh"` (a transform that branches
#      the id + AU controls binds it to `dict`, then sets
#      `"dictionary_id": dict`). By convention the id-bearing variable is `dict`.
# The pass-through form (`"dictionary_id": dict`, no quoted literal) is skipped.
EMITTED=()
while IFS= read -r id; do
  EMITTED+=("$id")
done < <( {
    grep -oE 'dictionary_id"?[[:space:]]*[:=][[:space:]]*"[^"]+"' "$VECTOR_TMPL"
    grep -oE '\bdict[[:space:]]*=[[:space:]]*"[^"]+"' "$VECTOR_TMPL"
  } | sed -E 's/.*"([^"]+)"$/\1/' | sort -u)

if [ "${#EMITTED[@]}" -eq 0 ]; then
  echo "[logship-dict] FAIL: no dictionary_id literals found in $VECTOR_TMPL"
  exit 1
fi

# Fail-closed: every emitted id must be defined.
if (( ${#EMITTED[@]} > 0 )); then
  for id in "${EMITTED[@]}"; do
    found=0
    if (( ${#DEFINED[@]} > 0 )); then
      for d in "${DEFINED[@]}"; do [ "$id" = "$d" ] && found=1 && break; done
    fi
    if [ "$found" -eq 0 ]; then
      echo "[logship-dict] FAIL: transform emits '$id' with no dictionary row in $DICT"
      FAIL=1
    fi
  done
fi

# Warn-only: dictionary rows nothing emits yet (sequenced future producers).
if (( ${#DEFINED[@]} > 0 )); then
  for d in "${DEFINED[@]}"; do
    found=0
    if (( ${#EMITTED[@]} > 0 )); then
      for id in "${EMITTED[@]}"; do [ "$id" = "$d" ] && found=1 && break; done
    fi
    if [ "$found" -eq 0 ]; then
      echo "[logship-dict] note: dictionary defines '$d' with no live transform (future-phase source)"
    fi
  done
fi

if [ "$FAIL" -eq 0 ]; then
  echo "[logship-dict] PASS — all ${#EMITTED[@]} emitted dictionary_id(s) map to a dictionary row."
fi
exit "$FAIL"
