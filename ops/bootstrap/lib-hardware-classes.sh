#!/usr/bin/env bash
# lib-hardware-classes.sh — the one reader of ops/bootstrap/hardware-classes.yaml.
#
# Sourced by publish-image.sh and pull-image.sh. It exists because the list was
# hardcoded in a `case` in each of them and written in prose in a third place,
# so a new model took three edits and got none.
#
# IMAGES ARE PER-PLATFORM, NOT PER-ROLE. build-image-orin.sh already says
# "single image; role selected at first boot" — role is a userdata.yml choice,
# not a build artifact. Encoding it in the class name meant six roles across
# five platforms would be thirty names for five binaries, and still could not
# express a site's backup, breakglass or jumphost machines.
#
# Legacy role-suffixed names (orin-mgmt, karbon-mgmt, …) resolve to their
# platform via the aliases block, so artifacts already published under them
# still pull.

_hc_file() { echo "${GOAT_HW_CLASSES:-$REPO_ROOT/ops/bootstrap/hardware-classes.yaml}"; }

_hc_section() {  # $1 = platforms|roles|aliases
  local f; f="$(_hc_file)"
  [[ -f "$f" ]] || { echo "missing hardware-classes.yaml: $f" >&2; return 1; }
  awk -v want="$1:" '
    $0 ~ "^"want"$" {inc=1; next}
    inc && /^[a-z]+:/ {inc=0}
    inc && /^  [a-z0-9-]+:/ {gsub(/[ :]/,""); print}
  ' "$f"
}

_valid_platforms() { _hc_section platforms; }
_valid_roles()     { _hc_section roles; }

# Resolve a legacy alias to its platform; echo the input if already a platform.
_resolve_platform() {
  local c="$1" f; f="$(_hc_file)"
  if _valid_platforms | grep -qx "$c"; then echo "$c"; return 0; fi
  local target
  # awk strips leading whitespace when splitting, so match the bare key.
  target="$(awk -v k="$c:" '/^aliases:/{inc=1} inc && $1==k{print $2; exit}' "$f")"
  [[ -n "$target" ]] && { echo "$target"; return 0; }
  return 1
}

# Fail with the known list rather than a bare rejection — the caller either
# mistyped or wants something that should exist, and seeing the list answers both.
_require_class() {
  local c="$1" resolved
  if resolved="$(_resolve_platform "$c")"; then
    [[ "$resolved" != "$c" ]] && echo "note: '$c' is a legacy name for platform '$resolved'" >&2
    # Sourced-library result available after _require_class returns success.
    # shellcheck disable=SC2034
    HW_PLATFORM="$resolved"
    return 0
  fi
  echo "unknown --hardware-class: $c" >&2
  echo "platforms (ops/bootstrap/hardware-classes.yaml):" >&2
  _valid_platforms | sed 's/^/  /' >&2
  echo "roles are chosen at first boot, not baked into an image:" >&2
  _valid_roles | sed 's/^/  /' >&2
  return 2
}
