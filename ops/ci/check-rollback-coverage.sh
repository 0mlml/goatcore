#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Universal deploy-rollback coverage ratchet (ADR 1060), fail-closed.
#
# Every DEPLOYING ansible role (one whose tasks reference a container-deploy
# primitive) must EITHER declare a meta-dependency on `rollback-ledger` in
# its meta/main.yml OR appear in ops/ci/rollback-coverage-allowlist.txt (the
# shrinking Phase-C migration list). A new deploying role is on neither, so
# the build fails until it carries the ledger dependency — coverage can only
# grow.
#
# Three checks:
#   1. Every deploying role is covered OR allowlisted.
#   2. Every allowlist entry is still a deploying role (no stale rows).
#   3. The allowlist length is <= ROLLBACK_COVERAGE_BASELINE (only shrinks;
#      Phase C lowers the baseline as it migrates roles).
#
# Mirrors ops/ci/check-vendor-surface-ratchet.sh. Whole-tree (not diff-scoped)
# so it holds on every PR. References: docs/design/universal-deploy-rollback.md.
set -euo pipefail

ROOT="${1:-.}"
ROLES_DIR="$ROOT/ops/ansible/roles"
ALLOWLIST="$ROOT/ops/ci/rollback-coverage-allowlist.txt"

# Baseline = 0 after the Phase C fan-out (all 55 deploying roles covered;
# pilots goat-zenoh-portal + panoscope migrated). Lower this as Phase C
# migrates roles. The allowlist may never exceed it.
ROLLBACK_COVERAGE_BASELINE=0

note() { echo "$*"; }

[ -d "$ROLES_DIR" ] || { echo "FAIL: no roles dir at $ROLES_DIR"; exit 1; }
[ -f "$ALLOWLIST" ] || { echo "FAIL: no allowlist at $ALLOWLIST"; exit 1; }

# The deploying-role universe = (A) ∪ (B):
#   (A) roles whose tasks/ OR handlers/ reference a container-deploy primitive.
#       We scan tasks/+handlers/ (not README/files — mgmt-host-compose-watch
#       documents `docker compose up` in prose but only installs a systemd
#       watcher) and match both single-line `docker compose … up` and the
#       folded/handler forms where `up -d` / `up --…` sits on its own line, the
#       `--force-recreate` CLI flag / `force_recreate` module param, and the
#       docker_compose/docker_container modules. Bare `restart` is excluded.
#   (B) roles that already carry an in-repo deploy-history.yml — the strongest
#       human-curated "this role deploys" signal (a 0859-adopted deployer).
#       Unioning (B) guarantees the ratchet never regresses below existing
#       coverage even if a deploy primitive (e.g. systemd-run) escapes (A).
ROLLBACK_DEPLOY_PATTERN='docker compose.*[[:space:]]up([[:space:]]|$)|up[[:space:]]+-d|up[[:space:]]+--|--force-recreate|force_recreate|community\.docker\.docker_compose|docker_compose_v2|docker_container'
deploying_roles() {
  {
    command grep -rlnE "$ROLLBACK_DEPLOY_PATTERN" \
      "$ROLES_DIR"/*/tasks/ "$ROLES_DIR"/*/handlers/ 2>/dev/null \
      | awk -F/ '{for(i=1;i<=NF;i++) if($i=="roles"){print $(i+1); break}}'
    command find "$ROLES_DIR" -name deploy-history.yml \
      | awk -F/ '{for(i=1;i<=NF;i++) if($i=="roles"){print $(i+1); break}}'
  } | sort -u
}

# Read allowlist (strip comments/blanks).
ALLOW=()
while IFS= read -r a; do
  ALLOW+=("$a")
done < <(command grep -vE '^\s*(#|$)' "$ALLOWLIST" | sed 's/[[:space:]]//g' | sort -u)
is_allowlisted() { local r="$1"; (( ${#ALLOW[@]} > 0 )) || return 1; for a in "${ALLOW[@]}"; do [ "$a" = "$r" ] && return 0; done; return 1; }

# A role is covered if its meta/main.yml names the rollback-ledger dependency.
is_covered() {
  local meta="$ROLES_DIR/$1/meta/main.yml"
  [ -f "$meta" ] || return 1
  command grep -qE 'role:\s*rollback-ledger|^\s*-\s*rollback-ledger\s*$' "$meta"
}

fail=0
DEPLOYING=()
while IFS= read -r r; do
  DEPLOYING+=("$r")
done < <(deploying_roles)

# 1. Every deploying role covered or allowlisted.
uncovered=()
if (( ${#DEPLOYING[@]} > 0 )); then
  for r in "${DEPLOYING[@]}"; do
    if is_covered "$r"; then continue; fi
    if is_allowlisted "$r"; then continue; fi
    uncovered+=("$r")
  done
fi
if [ "${#uncovered[@]}" -gt 0 ]; then
  note "FAIL: deploying role(s) with neither a rollback-ledger meta-dependency nor an allowlist entry:"
  for r in "${uncovered[@]}"; do note "  - $r"; done
  note "  Add to roles/<role>/meta/main.yml:"
  note "    dependencies:"
  note "      - role: rollback-ledger"
  note "        vars: { rollback_component: <role>, rollback_artifact_type: image, rollback_ref: \"{{ <role>_image }}\" }"
  note "  (or, transitionally, add the role to $ALLOWLIST). See docs/design/universal-deploy-rollback.md."
  fail=1
fi

# 2. No stale allowlist rows (must still be deploying roles).
stale=()
if (( ${#ALLOW[@]} > 0 )); then
  for a in "${ALLOW[@]}"; do
    found=0
    if (( ${#DEPLOYING[@]} > 0 )); then
      for r in "${DEPLOYING[@]}"; do [ "$r" = "$a" ] && { found=1; break; }; done
    fi
    [ "$found" -eq 0 ] && stale+=("$a")
  done
fi
if [ "${#stale[@]}" -gt 0 ]; then
  note "FAIL: allowlist entries that are no longer deploying roles (remove them):"
  for a in "${stale[@]}"; do note "  - $a"; done
  fail=1
fi

# 3. Allowlist length only shrinks.
allow_len="${#ALLOW[@]}"
if [ "$allow_len" -gt "$ROLLBACK_COVERAGE_BASELINE" ]; then
  note "FAIL: allowlist has $allow_len rows; baseline is $ROLLBACK_COVERAGE_BASELINE (must only shrink)."
  note "  A deploying role was added to the allowlist instead of being covered. See ADR 1060."
  fail=1
fi

if [ "$fail" -ne 0 ]; then exit 1; fi

covered=$(( ${#DEPLOYING[@]} - allow_len ))
note "ok: rollback coverage — ${#DEPLOYING[@]} deploying roles, ${covered} covered, ${allow_len}/${ROLLBACK_COVERAGE_BASELINE} allowlisted (shrinking)."
