#!/usr/bin/env bash
# ssh-key-backfill.sh — one-time helper for legacy host-shell keys.
#
# Purpose
# -------
# Before the console's ssh-key self-service flow existed, host shells were
# granted by hand: operators edited authorized_keys directly, and no record
# landed in the console's ssh_keys table. This script helps capture those
# legacy keys into a backfill plan so they can be recorded and attributed
# through the console.
#
# The script does **not** mutate the console or the hosts by itself. It
# reads an authorized_keys file, prompts for the handle/email mapping for
# each key (admin-confirmed), and writes a shell plan of `goat admin ssh-key`
# commands. Operators review the plan, adjust it, and run the commands
# manually — that ceremony is deliberate and part of the backfill decision.
#
# Data captured per key:
#   - full authorized_keys line (type, body, comment)
#   - chosen handle (kebab-case)
#   - login email for the admin
#   - whether the key is currently installed or belongs to an already
#     offboarded holder (recorded but grants nothing)
#
# Usage
# -----
#   ops/scripts/ssh-key-backfill.sh <goatnet> <authorized_keys_path>
#
# Example:
#   ops/scripts/ssh-key-backfill.sh site-a /var/lib/onboard-runner/legacy_authorized_keys
#
# The script writes `ssh-key-backfill.plan` in the current directory. Inspect
# it, edit as needed, then run the commands to actually record the keys via
# `goat admin ssh-key submit` + approve + install.
#
# Caveat: lines carrying authorized_keys options (`from="…"`, `command="…"`,
# `no-pty`, …) are recorded verbatim, but the console's key parser accepts
# plain `<type> <body> [comment]` lines only — the submit command in the plan
# will be refused. The script warns on such lines; strip the options by hand
# (and note them in `--note`) before running that block of the plan.

set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <goatnet> <authorized_keys_path>" >&2
  exit 64
fi

env="$1"
auth_file="$2"

if [[ ! -f "$auth_file" ]]; then
  echo "error: authorized_keys file '$auth_file' does not exist" >&2
  exit 65
fi

# Repo root, for the per-handle manifest probe below. The installer resolves
# scope from ops/operators/<env>/<handle>.yml; a handle without one is skipped
# `no_manifest` on every run and its row can NEVER reach `installed` — the
# plan must say so per key, not let the operator discover it run by run.
repo_root="$(cd "$(dirname "$0")/../.." && pwd)"

plan="ssh-key-backfill.plan"
echo "# ssh-key legacy backfill plan for goatnet $env" > "$plan"
echo "# Each block below came from one authorized_keys line." >> "$plan"
echo "# Review, then run commands manually with your operator tokens loaded." >> "$plan"

# Per-key counter: one person often holds several legacy keys under the same
# handle, and a bare "<handle>-legacy.pub" would silently overwrite the earlier
# key while the plan's submit lines all point at the survivor.
key_n=0

while IFS= read -r line; do
  # Skip blanks and comments.
  if [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]]; then
    continue
  fi

  echo
  echo "[backfill] authorized_keys line:"
  echo "  $line"

  # Option-prefixed lines (from="…", command="…", no-pty, …) won't pass the
  # console's plain "<type> <body> [comment]" parser — warn now so the operator
  # strips the options before running that block of the plan.
  # `sk-` covers the FIDO types the console supports
  # (sk-ssh-ed25519@openssh.com, sk-ecdsa-sha2-nistp256@openssh.com). oss-allow: key algorithm name
  if [[ ! "$line" =~ ^(ssh|ecdsa|sk)- ]]; then
    echo "  WARNING: line does not start with a key type — authorized_keys" >&2
    echo "  options detected? The plan's submit will be refused until the" >&2
    echo "  options are stripped from the .pub file (record them in --note)." >&2
  fi

  read -rp "Handle (kebab-case) for this key: " handle </dev/tty
  if [[ -z "$handle" ]]; then
    echo "  skipped (no handle entered)" >&2
    continue
  fi
  if [[ ! "$handle" =~ ^[a-z][a-z0-9-]*[a-z0-9]$ ]]; then
    echo "  skipped: handle must be kebab-case (^[a-z][a-z0-9-]*[a-z0-9]$)" >&2
    continue
  fi

  read -rp "Login email for this admin: " email </dev/tty
  if [[ -z "$email" ]]; then
    echo "  skipped (no email entered)" >&2
    continue
  fi

  # Closed prompt: anything that is not exactly installed/offboarded re-asks.
  # A typo ('Offboarded', 'offboard') silently falling through to the
  # active-admin branch would emit approve+install instructions for someone
  # who was supposed to be record-only.
  while :; do
    read -rp "State (installed/offboarded) [installed]: " state </dev/tty
    state=${state:-installed}
    state=$(printf '%s' "$state" | tr '[:upper:]' '[:lower:]')
    case "$state" in
      installed|offboarded) break ;;
      *) echo "  unrecognized state '$state' — answer 'installed' or 'offboarded'" >&2 ;;
    esac
  done

  note="legacy-backfilled"
  if [[ "$state" == "offboarded" ]]; then
    note="legacy-backfilled (already offboarded)"
  fi

  key_n=$((key_n + 1))
  keyfile="${handle}-legacy-${key_n}.pub"
  # Write the .pub file NOW so the plan never shells the authorized_keys
  # content. The plan only references the path. The counter keeps a
  # multi-key holder's files distinct.
  printf '%s\n' "$line" > "$keyfile"

  if [[ "$state" == "offboarded" ]]; then
    cat >> "$plan" <<EOF_PLAN

# handle=$handle email=$email state=$state
# original authorized_keys line:
# $line
goat admin ssh-key submit "$handle" --pubkey-file "$keyfile" --env "$env" --email "$email" --note "$note"
# OFFBOARDED: record only — do NOT approve or install this key.
# To make this terminal, look up its id by fingerprint and run:
#   goat admin ssh-key revoke <id>
EOF_PLAN
  else
    # The installer can only flip this row to `installed` if the handle has
    # an operator manifest — legacy admins often predate `goat admin onboard`
    # and have none, and without one the row is skipped `no_manifest` forever.
    manifest_rel="ops/operators/$env/$handle.yml"
    manifest_note=""
    if [[ ! -f "$repo_root/$manifest_rel" ]]; then
      manifest_note="# PREREQUISITE: no operator manifest found at $manifest_rel.
# The installer resolves scope from that manifest; without it this row is
# skipped (no_manifest) on EVERY run and can never be marked installed —
# finish this admin's onboarding (or author the manifest) BEFORE running
# the install command below."
      echo "  NOTE: no operator manifest at $manifest_rel — the plan records the prerequisite." >&2
    fi
    cat >> "$plan" <<EOF_PLAN

# handle=$handle email=$email state=$state
# original authorized_keys line:
# $line
${manifest_note:+$manifest_note
}goat admin ssh-key submit "$handle" --pubkey-file "$keyfile" --env "$env" --email "$email" --note "$note"
# After recording: approve via console or CLI, then run
#   goat admin ssh-key install --env "$env"
# There is no direct mark-installed verb: the installer play is what flips a
# row to installed, and it needs the handle's operator manifest
# (ops/operators/$env/$handle.yml) to resolve scope. Its authorized_keys
# write is an idempotent line re-place, so re-running it over a key that is
# already on the in-scope hosts changes nothing on disk and records the
# install.
EOF_PLAN
  fi

done < "$auth_file"

echo
echo "[backfill] plan written to $plan"
echo "Review it, edit the commands/notes, then run them manually to record the keys."
