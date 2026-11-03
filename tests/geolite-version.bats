#!/usr/bin/env bats
# Behavior tests for ops/mgmt-host-bootstrap/testdata/geolite-version.sh.
#
# The mgmt-host-bootstrap workflow keys its geolocation cache on the version
# this script reads from the mirror's Content-Disposition, the filename
# netbird itself derives (mime.ParseMediaType). A parse that accepts one
# spelling of that header falls back to the sha256 pointer without a word,
# which is the state #1069 is about; PR #1070's review asked for the quoted
# form. These pin every spelling the Go parser accepts for this filename,
# the two sha256sum layouts of the pointer file, and that an unreachable
# mirror degrades to "unresolved" rather than failing the step.

REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
VERSION_SH="$REPO_ROOT/ops/mgmt-host-bootstrap/testdata/geolite-version.sh"

setup() {
  if [ ! -x "$VERSION_SH" ]; then
    skip "geolite-version.sh not present at $VERSION_SH"
  fi
}

@test "geolite-version: bare filename with CRLF line ends (the mirror's shape today)" {
  run bash "$VERSION_SH" served <<'EOF'
HTTP/2 200
Content-Length: 32429092
Content-Disposition: attachment; filename=GeoLite2-City_20260602.tar.gz
Last-Modified: Tue, 02 Jun 2026 17:32:55 GMT
EOF
  [ "$status" -eq 0 ]
  [ "$output" = "20260602" ]
}

@test "geolite-version: double-quoted filename" {
  run bash "$VERSION_SH" served <<'EOF'
Content-Disposition: attachment; filename="GeoLite2-City_20260602.tar.gz"
EOF
  [ "$status" -eq 0 ]
  [ "$output" = "20260602" ]
}

@test "geolite-version: lowercase header name and parameters after the filename" {
  run bash "$VERSION_SH" served <<'EOF'
content-disposition: attachment; filename="GeoLite2-City_20260602.tar.gz"; size=32429092
EOF
  [ "$status" -eq 0 ]
  [ "$output" = "20260602" ]
}

@test "geolite-version: RFC 5987 filename*= form with its charset prefix" {
  run bash "$VERSION_SH" served <<'EOF'
Content-Disposition: attachment; filename*=UTF-8''GeoLite2-City_20260602.tar.gz
EOF
  [ "$status" -eq 0 ]
  [ "$output" = "20260602" ]
}

@test "geolite-version: the first Content-Disposition wins when there are two" {
  run bash "$VERSION_SH" served <<'EOF'
Content-Disposition: attachment; filename=GeoLite2-City_20260602.tar.gz
Content-Disposition: attachment; filename=GeoLite2-City_20260908.tar.gz
EOF
  [ "$status" -eq 0 ]
  [ "$output" = "20260602" ]
}

@test "geolite-version: no Content-Disposition resolves to blank, not to a guess" {
  run bash "$VERSION_SH" served <<'EOF'
HTTP/2 200
Content-Length: 32429092
EOF
  [ "$status" -eq 0 ]
  [ -z "$output" ]
}

@test "geolite-version: a filename of another shape resolves to blank" {
  run bash "$VERSION_SH" served <<'EOF'
Content-Disposition: attachment; filename="GeoLite2-Country_20260602.tar.gz"
EOF
  [ "$status" -eq 0 ]
  [ -z "$output" ]
}

@test "geolite-version: sha256 pointer in sha256sum text layout" {
  run bash "$VERSION_SH" pointer <<'EOF'
a403000c5e5acb1eaa29485011e5a88c6352e7accd6e8bffb67665a1d4a14196  GeoLite2-City_20260908.tar.gz
EOF
  [ "$status" -eq 0 ]
  [ "$output" = "20260908" ]
}

@test "geolite-version: sha256 pointer in sha256sum binary layout" {
  run bash "$VERSION_SH" pointer <<'EOF'
a403000c5e5acb1eaa29485011e5a88c6352e7accd6e8bffb67665a1d4a14196 *GeoLite2-City_20260908.tar.gz
EOF
  [ "$status" -eq 0 ]
  [ "$output" = "20260908" ]
}

@test "geolite-version: a pointer that names no file resolves to blank" {
  run bash "$VERSION_SH" pointer <<'EOF'
<html><body>502 Bad Gateway</body></html>
EOF
  [ "$status" -eq 0 ]
  [ -z "$output" ]
}

@test "geolite-version: an unreachable mirror resolves to unresolved and exits 0" {
  # Port 9 refuses on the loopback, so the two curls fail at once rather
  # than waiting out their budget.
  GEOLITE_BASE_URL="http://127.0.0.1:9/geolocation-dbs/GeoLite2-City/download" \
    run bash "$VERSION_SH"
  [ "$status" -eq 0 ]
  [ "${lines[0]}" = "served=" ]
  [ "${lines[1]}" = "pointer=" ]
  [ "${lines[2]}" = "version=unresolved" ]
}

@test "geolite-version: an unknown subcommand exits 2" {
  run bash "$VERSION_SH" bogus
  [ "$status" -eq 2 ]
}
