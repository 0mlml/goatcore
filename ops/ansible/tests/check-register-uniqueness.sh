#!/usr/bin/env bash
# check-register-uniqueness.sh — F-075-class static lint.
#
# F-075 (luks-unlock): two different tasks reused the same
# `register: <name>`. `register:` fires even on `when:`-skipped tasks, so
# the second task's "skipped" results overwrote the first task's real
# probe results, and a downstream `loop: <name>.results` then saw every
# entry as `item is not skipped: false` → silently false-skipped the
# entire enrollment chain. The bug shipped + sat unnoticed because
# ansible-playbook --syntax-check doesn't catch it.
#
# This lint walks every role's tasks/*.yml and any orchestrator playbook,
# tracks per-play scope (each top-level `- hosts:` / `- name:` block is a
# new scope), and warns when the same `register:` name appears twice in
# the same scope. Per-play scoping is correct because Ansible variable
# scope is per-play — reusing `register:` across plays that target
# disjoint host groups is harmless. The legitimate "re-probe after wipe"
# pattern uses a distinct suffix (e.g. `fido2_slot_check_after_wipe`)
# per the F-075 fix.
#
# Exit codes:
#   0  no within-scope duplicates found
#   1  duplicates found; details on stdout

set -euo pipefail

cd "$(dirname "$0")/../../.."

EXIT=0
SCAN_ROOTS=(
  "ops/ansible/roles"
  "ops/ansible"
)

# Per-file override for cases where re-using a `register:` name is
# intentional and reviewed (currently empty; add with PR justification).
ALLOWLIST_FILE="ops/ansible/tests/register-uniqueness-allowlist.txt"

is_allowlisted() {
  local file="$1"
  [[ -f "$ALLOWLIST_FILE" ]] || return 1
  grep -Fxq "$file" "$ALLOWLIST_FILE"
}

scan_file() {
  local f="$1"
  # Per-play scope tracking: bump play_idx on every top-level `- hosts:`
  # or `- name:` (column-0, list-marker) line. Within a tasks/*.yml file
  # that is included by `roles:`, there is no top-level `- hosts:` — the
  # whole file is one scope (play_idx stays at 0). Within an orchestrator
  # playbook, every play boundary increments play_idx so reuse across
  # plays does not register as a collision.
  awk '
    BEGIN { play_idx = 0 }
    # New play boundary at column 0: `- hosts:` or `- name:` (mutually
    # consistent with how site-bootstrap / *-bootstrap playbooks open a
    # play). `- name:` inside a task list is indented, so the column-0
    # constraint distinguishes play-level from task-level.
    /^- (hosts|name):/ { play_idx++ }
    /^[[:space:]]*register:[[:space:]]/ {
      name = $0
      sub(/^[[:space:]]*register:[[:space:]]*/, "", name)
      sub(/[[:space:]]*#.*$/, "", name)
      sub(/[[:space:]]+$/, "", name)
      if (name != "") printf "%d\t%s\n", play_idx, name
    }
  ' "$f" | sort | uniq -d
}

for root in "${SCAN_ROOTS[@]}"; do
  while IFS= read -r -d '' f; do
    # Skip files under tests/ — fixtures legitimately re-stage the same
    # scenario and may reuse names within a single block of mock probes.
    [[ "$f" == *"/tests/"* ]] && continue

    dups=$(scan_file "$f" || true)

    if [[ -n "$dups" ]]; then
      if is_allowlisted "$f"; then
        echo "[register-uniqueness] ALLOWLISTED: $f"
        echo "$dups" | sed 's/^/  ALLOWLISTED: /'
        continue
      fi
      echo "[register-uniqueness] FAIL: $f re-uses register name(s) within a single play scope:"
      echo "$dups" | awk -F'\t' '{ printf "  - play_idx=%s  register=%s\n", $1, $2 }'
      EXIT=1
    fi
  done < <(find "$root" -type f \( -name '*.yml' -o -name '*.yaml' \) -print0)
done

if [[ "$EXIT" -eq 0 ]]; then
  echo "[register-uniqueness] PASS — no within-play register-name collisions."
fi

exit "$EXIT"
