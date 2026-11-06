#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# project-setting.sh — print one value this checkout declares about how it is
# deployed, from ops/goatbuilder/goatbuilder.yaml. The shell drivers read their
# project's names here (the AWS profile, the state directory, the source
# repository, the image namespace), in the order the builder reads them
# (crates/goatbuilder/src/settings.rs, docs/design/goatbuilder-portability.md):
# the environment variable a person sets for one command, then this
# declaration, then a value that names nobody's project.
#
# usage: project-setting.sh [--repo DIR] <section> <key>
#   project-setting.sh deploy aws_profile
#   project-setting.sh deploy state_dir
#   project-setting.sh repo source
#   project-setting.sh repo image_namespace
#   project-setting.sh --repo DIR trunk-remote     the git remote of DIR whose
#                                                  URL is repo.source
#
# Prints the value and exits 0. Prints nothing and exits 0 when the file, the
# section or the key is absent, so a caller writes its own fallback:
#   profile="${GOAT_AWS_PROFILE:-$(project-setting.sh deploy aws_profile)}"
#   profile="${profile:-default}"
#
# Reads the block form the generator writes (ops/goatbuilder/gen-config.py):
#   <section>:
#     <key>: value
# with awk alone, so it runs where python3 has no PyYAML.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
if [ "${1:-}" = "--repo" ]; then REPO="${2:?--repo needs a directory}"; shift 2; fi

declared() {  # declared <section> <key>
  local f="$REPO/ops/goatbuilder/goatbuilder.yaml"
  [ -f "$f" ] || return 0
  awk -v section="$1" -v key="$2" '
    /^[^ \t#]/ { inside = ($0 ~ ("^" section ":[ \t]*(#.*)?$")) ; next }
    inside && $0 ~ ("^  " key ":") {
      sub("^  " key ":[ \t]*", "")
      sub(/[ \t]+#.*$/, "")
      sub(/[ \t]+$/, "")
      if ($0 ~ /^".*"$/ || $0 ~ /^\047.*\047$/) $0 = substr($0, 2, length($0) - 2)
      print
      exit
    }
  ' "$f"
}

# The remote of this checkout that IS the source of record: the one whose URL
# ends in the declared repo.source (owner/name, with or without .git). A clone
# that keeps the trunk under a name other than `origin` is then judged against
# the trunk, not against a mirror or a fork.
trunk_remote() {
  local source name url
  source="$(declared repo source | tr '[:upper:]' '[:lower:]')"
  [ -n "$source" ] || return 0
  for name in $(git -C "$REPO" remote 2>/dev/null); do
    url="$(git -C "$REPO" remote get-url "$name" 2>/dev/null | tr '[:upper:]' '[:lower:]' || true)"
    url="${url%.git}"
    case "$url" in
      */"$source"|*:"$source") printf '%s\n' "$name"; return 0 ;;
    esac
  done
}

case "$#:${1:-}" in
  1:trunk-remote) trunk_remote ;;
  2:*) declared "$1" "$2" ;;
  *) echo "usage: project-setting.sh [--repo DIR] <section> <key> | trunk-remote" >&2; exit 2 ;;
esac
