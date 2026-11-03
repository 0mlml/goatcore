#!/usr/bin/env bash
# geolite-version.sh — the GeoLite2-City version netbird-mgmt will ask for.
#
# netbird names the file from a HEAD of the tarball URL and its
# Content-Disposition (getFilenameFromURL in management/server/geolocation/
# utils.go, through mime.ParseMediaType), then verifies the download against
# the sha256 pointer beside it. The two can disagree (#1069), so the workflow
# keys its cache on the former and reads the latter only to say so.
#
#   ./testdata/geolite-version.sh                    # served=… pointer=… version=…
#   ./testdata/geolite-version.sh served < headers   # the version a Content-Disposition names
#   ./testdata/geolite-version.sh pointer < sha256   # the version a sha256 pointer names
#
# `version` is the served one, else the pointer's, else "unresolved". The
# script exits 0 in every case: a stale cache entry is still a valid seed,
# and a blank field is the caller's cue to say that the fallback happened.
#
# The parse reads the filename the way mime.ParseMediaType does, as far as
# this filename shape needs: the header name in any case, the value bare or
# double-quoted, parameters after it ignored, the RFC 5987 filename*= form
# with its charset prefix, CRLF line ends, the first header when there are
# two. Before PR #1070's review the parse accepted the bare form only, so a
# mirror that started quoting the value would have keyed the cache on the
# pointer without a word. tests/geolite-version.bats pins each spelling.
set -euo pipefail

BASE_URL="${GEOLITE_BASE_URL:-https://pkgs.netbird.io/geolocation-dbs/GeoLite2-City/download}"

# Digits or nothing: a filename of any other shape resolves to blank rather
# than to a partial match.
version_of() {
  sed -E -n 's/^GeoLite2-City_([0-9]+)\.tar\.gz$/\1/p' | head -n 1
}

# Headers on stdin. match() rather than -F: a multi-character field separator
# is a regex whose backslash handling differs between gawk, mawk and BSD awk.
served_version() {
  tr -d '\r' \
    | awk 'tolower($0) ~ /^content-disposition:/ && match($0, /filename\*?=/) {
             print substr($0, RSTART + RLENGTH); exit
           }' \
    | sed -E 's/;.*//; s/^[[:space:]]*//; s/[[:space:]]*$//' \
    | sed -E "s/^[^']*'[^']*'//" \
    | sed -E 's/^"(.*)"$/\1/' \
    | version_of
}

# The sha256 file on stdin, in either sha256sum layout: "hash  name" (text)
# or "hash *name" (binary).
pointer_version() {
  tr -d '\r' | awk 'NF >= 2 { print $2; exit }' | sed -E 's/^\*//' | version_of
}

resolve() {
  local served pointer
  served="$(curl -sfI --max-time 30 "${BASE_URL}?suffix=tar.gz" | served_version)" || served=""
  pointer="$(curl -sfL --max-time 30 "${BASE_URL}?suffix=tar.gz.sha256" | pointer_version)" || pointer=""
  echo "served=$served"
  echo "pointer=$pointer"
  echo "version=${served:-${pointer:-unresolved}}"
}

case "${1:-resolve}" in
  resolve) resolve ;;
  served) served_version ;;
  pointer) pointer_version ;;
  *) echo "usage: $0 [resolve|served|pointer]" >&2; exit 2 ;;
esac
