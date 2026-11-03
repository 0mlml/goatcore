# shellcheck shell=bash
# keep-awake.sh — hold a macOS idle-sleep assertion for the life of the
# calling script (#2122). Sourced by the long-running drivers; the goatbuilder
# binary does the same in crates/goatbuilder/src/awake.rs (ADR 1201).
#
# A deploy, release or standup drives hosts for minutes to an hour. A Mac that
# idles into sleep mid-play drops the mesh and stalls the run: a 145 s herd
# deploy took 1095 s for exactly that reason.
#
#   . "$(dirname "${BASH_SOURCE[0]}")/lib/keep-awake.sh"; goat_keep_awake
#
# `caffeinate -i -w <pid>` prevents idle sleep until <pid> exits, so the
# assertion can never outlive the run. GOAT_KEEP_AWAKE carries the holder's pid
# to child processes, so a script that calls goatbuilder (or the reverse)
# holds one assertion, not two. GOAT_NO_CAFFEINATE=1 opts out. Off macOS, or
# without caffeinate, it does nothing.

goat_keep_awake() {
  [ "${GOAT_NO_CAFFEINATE:-}" = "1" ] && return 0
  # A holder further up is alive: its assertion already covers this process.
  if [ -n "${GOAT_KEEP_AWAKE:-}" ] && kill -0 "$GOAT_KEEP_AWAKE" 2>/dev/null; then
    return 0
  fi
  # GOAT_KEEP_AWAKE_ANY_OS is for the helper's own test on a Linux runner.
  if [ "${GOAT_KEEP_AWAKE_ANY_OS:-}" != "1" ] && [ "$(uname -s 2>/dev/null)" != "Darwin" ]; then
    return 0
  fi
  command -v caffeinate >/dev/null 2>&1 || return 0
  caffeinate -i -w "$$" >/dev/null 2>&1 &
  disown 2>/dev/null || true
  export GOAT_KEEP_AWAKE="$$"
}
