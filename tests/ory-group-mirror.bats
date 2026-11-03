#!/usr/bin/env bats

setup() {
  mkdir -p "$BATS_TEST_TMPDIR/bin"
  export PATCH_CAPTURE="$BATS_TEST_TMPDIR/patch.json"
  export TMPDIR="$BATS_TEST_TMPDIR"
  export PATH="$BATS_TEST_TMPDIR/bin:$PATH"

  cat >"$BATS_TEST_TMPDIR/bin/curl" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
args=" $* "
emit_identity() {
  jq -nc --argjson groups "$1" '{id:"id-1",traits:{email:"person@example.test"},metadata_public:{groups:$groups,org:{short_tag:"acme"},unrelated:"keep-me"}}'
}
if [[ "$args" == *"/admin/identities?per_page=1000"* ]]; then
  emit_identity "$FIXTURE_GROUPS" | jq -s .
elif [[ "$args" == *" -X PATCH "* ]]; then
  output=
  data=
  while (($#)); do
    case "$1" in
      -o) output=$2; shift 2 ;;
      -d) data=$2; shift 2 ;;
      *) shift ;;
    esac
  done
  printf '%s' "$data" >"$PATCH_CAPTURE"
  status=${FAKE_PATCH_STATUS:-200}
  if [[ "$status" == 200 ]]; then
    printf '%s' '{}' >"$output"
  else
    printf '%s' '{"error":"unsupported patch operation"}' >"$output"
  fi
  printf '%s' "$status"
elif [[ -s "$PATCH_CAPTURE" ]]; then
  emit_identity "$(jq -c '.[0].value' "$PATCH_CAPTURE")"
else
  emit_identity "$FIXTURE_GROUPS"
fi
SH
  chmod +x "$BATS_TEST_TMPDIR/bin/curl"
}

@test "ory group mirror emits the Kratos-supported child-path add and verifies it" {
  run env APPLY=1 FIXTURE_GROUPS='["OLD"]' KRATOS_ADMIN=http://127.0.0.1:4434 \
    bash ops/scripts/ory-group-mirror.sh add OLD NEW

  [ "$status" -eq 0 ]
  [[ "$output" == *"groups now: OLD,NEW"* ]] || false
  run jq -ce 'length == 1 and .[0].op == "add" and .[0].path == "/metadata_public/groups" and .[0].value == ["OLD","NEW"]' "$PATCH_CAPTURE"
  [ "$status" -eq 0 ]

  rm "$PATCH_CAPTURE"
  run env APPLY=1 FIXTURE_GROUPS='["OLD","NEW"]' KRATOS_ADMIN=http://127.0.0.1:4434 \
    bash ops/scripts/ory-group-mirror.sh remove NEW OLD

  [ "$status" -eq 0 ]
  run jq -ce 'length == 1 and .[0].op == "add" and .[0].path == "/metadata_public/groups" and .[0].value == ["NEW"]' "$PATCH_CAPTURE"
  [ "$status" -eq 0 ]
}

@test "ory group mirror fails on a rejected patch" {
  run env APPLY=1 FIXTURE_GROUPS='["OLD"]' FAKE_PATCH_STATUS=400 KRATOS_ADMIN=http://127.0.0.1:4434 \
    bash ops/scripts/ory-group-mirror.sh add OLD NEW

  [ "$status" -ne 0 ]
  [[ "$output" == *"FAILED: {\"error\":\"unsupported patch operation\"}"* ]] || false
}
