#!/usr/bin/env bats
# Behavior tests for ops/mgmt-host-bootstrap/testdata/warm-geolite.sh.
#
# In CI the script either warms the fixture or says WHY it could not, and
# the workflow treats one reason differently from every other: the mirror
# refusing the download on its own checksum (#1069) runs the integration
# test without geolocation under a warning; anything else fails the run
# (PR #1070 review). These pin that classification with a fake `docker` on
# PATH, so they need no daemon and finish in a second: the loop's first poll
# sees the death before its first sleep.

REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
WARM_SH="$REPO_ROOT/ops/mgmt-host-bootstrap/testdata/warm-geolite.sh"

setup() {
  if [ ! -x "$WARM_SH" ]; then
    skip "warm-geolite.sh not present at $WARM_SH"
  fi
  # The script works relative to its own location, so it gets a tree of its
  # own with an empty fixture directory: a warmed local checkout must not
  # turn every test into "already warm".
  mkdir -p "$BATS_TEST_TMPDIR/tree/testdata/geolite" "$BATS_TEST_TMPDIR/bin"
  cp "$WARM_SH" "$BATS_TEST_TMPDIR/tree/testdata/"
  cat > "$BATS_TEST_TMPDIR/bin/docker" <<'EOF'
#!/usr/bin/env bash
# Stands in for `docker compose ...`: management's log is whatever the test
# put in FAKE_MGMT_LOG, postgres's is FAKE_PG_LOG, and management shows as
# exited when FAKE_MGMT_EXITED is set. With FAKE_UP_WRITES set, `up` writes
# the database pair, the way a warm that succeeds does.
case " $* " in
  *" logs "*" postgres "*) [ -z "${FAKE_PG_LOG:-}" ] || printf '%s\n' "$FAKE_PG_LOG" ;;
  *" logs "*) [ -z "${FAKE_MGMT_LOG:-}" ] || printf '%s\n' "$FAKE_MGMT_LOG" ;;
  *" ps "*) [ -z "${FAKE_MGMT_EXITED:-}" ] || echo management ;;
  *" up "*) [ -z "${FAKE_UP_WRITES:-}" ] ||
    touch testdata/geolite/GeoLite2-City_20260602.mmdb testdata/geolite/geonames_20260602.db ;;
esac
exit 0
EOF
  chmod +x "$BATS_TEST_TMPDIR/bin/docker"
  export PATH="$BATS_TEST_TMPDIR/bin:$PATH"
  export GITHUB_OUTPUT="$BATS_TEST_TMPDIR/github-output"
  : > "$GITHUB_OUTPUT"
  export WARM_TIMEOUT_S=0
}

warm() {
  run "$BATS_TEST_TMPDIR/tree/testdata/warm-geolite.sh"
}

reason() {
  sed -n 's/^reason=//p' "$GITHUB_OUTPUT"
}

@test "warm-geolite: the mirror's checksum refusal is reported as mirror-checksum" {
  export FAKE_MGMT_LOG='2026-09-11T13:59:58Z FATL management/internals/server/modules.go:48: failed to initialize geolocation: checksum mismatch: expected 9cd003dc, got a403000c'
  export FAKE_MGMT_EXITED=1
  warm
  [ "$status" -eq 1 ]
  [ "$(reason)" = "mirror-checksum" ]
  [[ "$output" == *"#1069"* ]]
}

@test "warm-geolite: management dying on anything else is management-died" {
  export FAKE_MGMT_LOG='2026-09-11T14:02:11Z FATL failed to connect to host=postgres user=netbird database=netbird: dial error (dial tcp 172.18.0.2:5432: connect: connection refused)'
  export FAKE_MGMT_EXITED=1
  warm
  [ "$status" -eq 1 ]
  [ "$(reason)" = "management-died" ]
  [[ "$output" != *"#1069"* ]]
}

@test "warm-geolite: a container gone without a FATL line is management-died" {
  export FAKE_MGMT_EXITED=1
  warm
  [ "$status" -eq 1 ]
  [ "$(reason)" = "management-died" ]
  [[ "$output" == *"(exited)"* ]]
}

@test "warm-geolite: the budget running out is timeout" {
  warm
  [ "$status" -eq 1 ]
  [ "$(reason)" = "timeout" ]
  [[ "$output" == *"TIMED OUT"* ]]
}

@test "warm-geolite: the requested version already present is warm, and reports nothing" {
  touch "$BATS_TEST_TMPDIR/tree/testdata/geolite/GeoLite2-City_20260602.mmdb" \
        "$BATS_TEST_TMPDIR/tree/testdata/geolite/geonames_20260602.db"
  GEOLITE_VERSION=20260602 warm
  [ "$status" -eq 0 ]
  [[ "$output" == *"already warm"* ]]
  [ -z "$(reason)" ]
}

@test "warm-geolite: an older pair restored from a stale cache is not warm" {
  touch "$BATS_TEST_TMPDIR/tree/testdata/geolite/GeoLite2-City_20260101.mmdb" \
        "$BATS_TEST_TMPDIR/tree/testdata/geolite/geonames_20260101.db"
  GEOLITE_VERSION=20260602 warm
  [ "$status" -eq 1 ]
  [[ "$output" == *"bringing the fixture stack up"* ]]
  [ "$(reason)" = "timeout" ]
}

# #1521: `down -v` in the EXIT trap removes the containers, and their logs
# with them, before the workflow's own dump step runs. A failed warm prints
# both services' logs first.
@test "warm-geolite: a failed warm prints the postgres and management logs before teardown" {
  export FAKE_MGMT_LOG='2026-09-25T03:17:09Z FATL management/internals/server/boot.go:65: failed to create store: dial error (dial tcp 172.18.0.3:5432: connect: connection refused)'
  export FAKE_PG_LOG='2026-09-25 03:17:08.912 UTC [48] LOG:  database system is ready to accept connections'
  export FAKE_MGMT_EXITED=1
  warm
  [ "$status" -eq 1 ]
  [ "$(reason)" = "management-died" ]
  [[ "$output" == *"last postgres logs before teardown"* ]]
  [[ "$output" == *"database system is ready to accept connections"* ]]
  [[ "$output" == *"last management logs before teardown"* ]]
}

@test "warm-geolite: a warm that succeeds prints no log dump" {
  export FAKE_UP_WRITES=1
  export FAKE_PG_LOG='postgres log line that must not appear'
  # The success path hands the files back with sudo chown; a stand-in keeps
  # the test off a real password prompt.
  printf '#!/usr/bin/env bash\nexit 0\n' > "$BATS_TEST_TMPDIR/bin/sudo"
  chmod +x "$BATS_TEST_TMPDIR/bin/sudo"
  GEOLITE_VERSION=20260602 warm
  [ "$status" -eq 0 ]
  [[ "$output" == *"both databases written"* ]]
  [[ "$output" != *"before teardown"* ]]
  [[ "$output" != *"must not appear"* ]]
  [ -z "$(reason)" ]
}
