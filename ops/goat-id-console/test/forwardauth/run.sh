#!/usr/bin/env bash
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
traefik_image='traefik:v3.7.16@sha256:1336e85aaed293bd63d005086e0dd013b04078b26aaeb1c18b26a2f928af7e65'
container="goat-probation-forwardauth-${GITHUB_RUN_ID:-local}-$$"
fixture_pid=''
traefik_pid=''

cleanup() {
  docker rm -f "$container" >/dev/null 2>&1 || true
  if [ -n "$fixture_pid" ]; then
    kill "$fixture_pid" >/dev/null 2>&1 || true
  fi
  if [ -n "$traefik_pid" ]; then
    kill "$traefik_pid" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

python3 "$here/fixture.py" &
fixture_pid=$!

if [ -n "${TRAEFIK_BIN:-}" ]; then
  (cd "$here" && exec "$TRAEFIK_BIN" --configFile=traefik.yml) &
  traefik_pid=$!
else
  docker run --rm -d --name "$container" --network host -w /etc/traefik \
    -v "$here/traefik.yml:/etc/traefik/traefik.yml:ro" \
    -v "$here/dynamic.yml:/etc/traefik/dynamic.yml:ro" \
    "$traefik_image" --configFile=traefik.yml >/dev/null
fi

for _ in $(seq 1 30); do
  if curl -fsS -o /dev/null -X POST -H 'Content-Type: application/json' \
      --data '{}' http://127.0.0.1:18080/allowed; then
    break
  fi
  sleep 1
done

allowed=$(curl -fsS -X POST -H 'Content-Type: application/json' \
  --data '{}' http://127.0.0.1:18080/allowed)
test "$allowed" = 'content-type=application/json body={}'

headers=$(mktemp)
body=$(mktemp)
status=$(curl -sS -D "$headers" -o "$body" -w '%{http_code}' \
  -H 'Accept: text/html' http://127.0.0.1:18080/blocked)
test "$status" = 423
grep -Eiq '^Content-Type: text/html; charset=utf-8' "$headers"
grep -Eiq '^Cache-Control: no-store' "$headers"
grep -q 'action="/__goat/probation/request"' "$body"

petition=$(curl -fsS -X POST -H 'Content-Type: application/x-www-form-urlencoded' \
  --data 'token=hold-token' http://127.0.0.1:18080/__goat/probation/request)
test "$petition" = recorded

encoded_petition=$(curl --path-as-is -fsS -X POST \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data 'token=hold-token' http://127.0.0.1:18080/__goat/probation%2Frequest)
test "$encoded_petition" = recorded

trailing_status=$(curl --path-as-is -sS -o /dev/null -w '%{http_code}' -X POST \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data 'token=hold-token' http://127.0.0.1:18080/__goat/probation/request/)
test "$trailing_status" = 423

rm -f "$headers" "$body"
echo 'probation ForwardAuth runtime: PASS'
