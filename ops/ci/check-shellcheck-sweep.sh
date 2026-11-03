#!/usr/bin/env bash
# ops/ci/check-shellcheck-sweep.sh — shellcheck every tracked shell script under
# ops/, at warning severity.
#
# WHY: `make lint-shell` shellchecked six hand-picked files (Makefile SCRIPTS),
# so every other tracked script under ops/ merged with no shellcheck pass at all
# (#486). That is the claimed-scope-versus-actual-scope gap #227 closed for the
# bash 3.2 portability gate, in the sibling gate. The list here is DERIVED from
# git, so a new script is swept the day it is tracked.
#
# THE BOUNDARY, stated so a green run is not over-read:
#
#   SWEPT      every tracked `ops/**/*.sh`, plus the extensionless files under
#              ops/ whose first line is a bash or sh shebang, at `-S warning`:
#              parse errors, unsafe argument splitting, unchecked cd, masked
#              declaration failures, and unused variables.
#
#   NOT SWEPT  the info and style tiers. The six SCRIPTS in the Makefile stay
#              at full severity, as before. Runtime/generated sources and
#              public sourced-library variables carry narrow annotations at
#              the use or declaration, not directory-wide exclusions.
#
# Excluded codes match the Makefile pass: SC1091 (a sourced file that is not
# there at lint time) and SC2016 (single-quoted text that is meant literally).
#
# Usage:
#   ops/ci/check-shellcheck-sweep.sh            sweep the derived list
#   ops/ci/check-shellcheck-sweep.sh <file>...  check the named files only
#
# Exit: 0 clean, 1 findings, 2 could not run (no shellcheck, no files, a named
# file that does not exist). Rollback: rm this file and its Makefile line.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

command -v shellcheck >/dev/null 2>&1 || { echo "FAIL: shellcheck is not on PATH." >&2; exit 2; }

# has_shell_shebang <file>: true when the first line names bash or sh.
has_shell_shebang() {
  head -n 1 "$1" 2>/dev/null | grep -Eq '^#!.*(bash|/sh|[[:space:]]sh)([[:space:]]|$)'
}

files=()
if [ "$#" -gt 0 ]; then
  for f in "$@"; do
    [ -f "$f" ] || { echo "FAIL: explicitly named file '$f' does not exist." >&2; exit 2; }
    files+=("$f")
  done
else
  cd "$REPO_ROOT"
  while IFS= read -r f; do
    files+=("$f")
  done < <(git ls-files 'ops/*.sh' 'ops/**/*.sh' | sort -u)
  while IFS= read -r f; do
    case "$(basename "$f")" in *.*) continue ;; esac
    if has_shell_shebang "$f"; then
      files+=("$f")
    fi
  done < <(git ls-files ops)
fi

if [ "${#files[@]}" -eq 0 ]; then
  echo "FAIL: no files to sweep — are we in a git worktree?" >&2
  exit 2
fi

if ! shellcheck -x -e SC1091,SC2016 -S warning -f gcc "${files[@]}"; then
  echo >&2
  echo "FAIL: shellcheck reports error- or warning-level findings in the tracked ops shell tree." >&2
  echo "      Fix the script. A sourced library declares its shell with" >&2
  echo "      '# shellcheck shell=bash' on its first line." >&2
  exit 1
fi

echo "ok: ${#files[@]} tracked ops shell script(s) pass shellcheck at warning severity."
