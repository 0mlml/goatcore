#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# check-ansible-inventory-groups.sh — refuses split-brain between
# ops/ansible/inventory/*.ini and ops/ansible/group_vars/.
#
# Two inventory groups can resolve to the same host IP and still be
# *different host objects* in Ansible — group_vars don't cross-pollinate.
# When playbooks declare `hosts: a_hosts` while a service's overrides sit
# under `group_vars/b_hosts/vars.yml`, the overrides are stranded, role
# defaults win, and the service crash-loops behind a 502.
#
# This lint catches the structural shape of that bug. Specifically,
# it fails CI when a directory under `ops/ansible/group_vars/` does
# NOT correspond to any group declared in any `ops/ansible/inventory/
# *.ini` file. (The reverse direction — group with no group_vars dir
# — is benign and very common; not gated here.)
#
# Allowlist: a few group_vars dirs are not directly named inventory
# groups but are loaded by Ansible automatically via inventory plugins
# or play-level vars (`all`, `localhost`). Listed in ALLOWLIST below.
#
# Skip-marker: a `group_vars/<dir>/.lint-allow-orphan` file allows a
# dir to exist without an inventory group (e.g., a host_vars-shaped
# dir parked for a future inventory entry). Use sparingly; the whole
# point is to prevent the split-brain shape.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
INVENTORY_DIR="$REPO_ROOT/ops/ansible/inventory"
GROUP_VARS_DIR="$REPO_ROOT/ops/ansible/group_vars"

# group_vars dirs that intentionally exist without a direct inventory
# group declaration. Keep this list short and well-justified.
ALLOWLIST=(
  "all"          # Ansible loads group_vars/all/ for every host regardless
                 # of inventory; not a group, a special name.
  "localhost"    # implicit group for the local controller; not declared
                 # in *.ini but Ansible loads its group_vars when running
                 # against `localhost`.
)

FAIL=0

# -- Gather inventory group names ------------------------------------------
# Parse every *.ini under inventory/ (recursive — inventory/group_vars/
# subdirectories are NOT inventory files, so skip them; inventory plugins
# would be *.yml which we'd need to parse separately if they ever land).
# Strip `:vars`, `:children` modifiers — those are section qualifiers,
# not separate groups. Build a unique newline-delimited list of group names.
UNIQ=""
while IFS= read -r -d '' ini; do
  while IFS= read -r line; do
    # Match `[group]` or `[group:vars]` or `[group:children]` headers.
    # Capture just the bare group name.
    if [[ "$line" =~ ^\[([a-zA-Z0-9_-]+)(:(vars|children))?\]$ ]]; then
      UNIQ+="${BASH_REMATCH[1]}"$'\n'
    fi
  done < "$ini"
done < <(find "$INVENTORY_DIR" -maxdepth 1 -type f -name '*.ini' -print0)

# Deduplicate while preserving a simple newline list.
UNIQ="$(printf '%s\n' "$UNIQ" | sort -u)"

# -- Gather group_vars directory names -------------------------------------
declare -a GROUP_VARS_DIRS=()
while IFS= read -r -d '' d; do
  GROUP_VARS_DIRS+=("$(basename "$d")")
done < <(find "$GROUP_VARS_DIR" -mindepth 1 -maxdepth 1 -type d -print0)

# Fail-closed on empty discovery: zero dirs means the checkout is broken and
# this guard can no longer see the split-brain shape. (Also keeps the expansion
# below off the empty-"${arr[@]}"-under-set-u path that kills bash < 4.4.)
if [ "${#GROUP_VARS_DIRS[@]}" -eq 0 ]; then
  echo "FAIL: found zero group_vars dirs under $GROUP_VARS_DIR — discovery is broken." >&2
  exit 1
fi

# -- Cross-check: every group_vars dir must map to an inventory group ------
for dir in "${GROUP_VARS_DIRS[@]}"; do
  # Allowlist (special-name dirs).
  allowed=0
  for a in "${ALLOWLIST[@]}"; do
    if [[ "$dir" == "$a" ]]; then
      allowed=1
      break
    fi
  done
  if [[ "$allowed" -eq 1 ]]; then
    continue
  fi

  # Per-dir skip-marker.
  if [[ -f "$GROUP_VARS_DIR/$dir/.lint-allow-orphan" ]]; then
    continue
  fi

  if ! printf '%s\n' "$UNIQ" | grep -Fxq -- "$dir"; then
    echo "::error file=ops/ansible/group_vars/$dir/::group_vars/$dir/ has no matching inventory group in any ops/ansible/inventory/*.ini. Either:" >&2
    echo "  - add a [$dir] section to the appropriate inventory file, OR" >&2
    echo "  - delete the directory if its contents have moved elsewhere, OR" >&2
    echo "  - drop a 'group_vars/$dir/.lint-allow-orphan' marker if this is deliberate (use sparingly)." >&2
    FAIL=1
  fi
done

if [[ "$FAIL" -ne 0 ]]; then
  echo "" >&2
  echo "check-ansible-inventory-groups: FAILED (see lines above)" >&2
  exit 1
fi
inventory_count="$(printf '%s\n' "$UNIQ" | awk 'NF {c++} END{print c+0}')"
echo "check-ansible-inventory-groups: OK (${#GROUP_VARS_DIRS[@]} group_vars dirs, ${inventory_count} inventory groups)"
