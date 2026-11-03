#!/usr/bin/env bash
# Test the FULL CAC -> goat chain with a REAL CAC in a browser: CAC -> (mTLS proxy #2
# or Keycloak #3) -> Kratos federation -> a native goat identity carrying your EDIPI.
#
#   ./run.sh mtls     <dod-ca-bundle.pem>            # path #2 — works with ANY real CAC
#   ./run.sh keycloak <dod-ca-bundle.pem> <EDIPI>    # path #3 — needs your EDIPI (see README)
#   ./run.sh stop     [mtls|keycloak]                # tear the stack down
#
# The stack (incl. the landing page) is all containers via `up -d`, so it PERSISTS until
# you run `stop` — nothing dies when this script returns. Then open http://localhost:9090.
set -euo pipefail
cd "$(dirname "$0")"
MODE="${1:-}"; CA="${2:-}"; EDIPI="${3:-}"

if [ "$MODE" = stop ]; then
  if [ -n "${2:-}" ]; then set -- "$2"; else set -- mtls keycloak; fi
  for m in "$@"; do docker compose -f "docker-compose.$m-full.yml" down -v 2>/dev/null || true; done
  echo "stopped."; exit 0
fi
case "$MODE" in mtls|keycloak) ;; *) echo "usage: ./run.sh mtls|keycloak <dod-ca-bundle.pem> [EDIPI]   |   ./run.sh stop"; exit 1;; esac
[ -f "$CA" ] || { echo "Provide the DoD PKI CA bundle (root+intermediates, PEM). See README.md."; exit 1; }
grep -qE "^[^#]*\bcac-idp\b" /etc/hosts && grep -qE "^[^#]*\bkratos-idp\b" /etc/hosts || {
  echo "One-time host setup needed:"; echo "    echo '127.0.0.1 cac-idp kratos-idp' | sudo tee -a /etc/hosts"; exit 1; }

cp "$CA" dod-ca.pem
[ -f server.crt ] || openssl req -x509 -newkey rsa:2048 -nodes -keyout server.key -out server.crt \
  -days 365 -subj "/CN=cac-idp" -addext "subjectAltName=DNS:cac-idp" 2>/dev/null

COMPOSE="docker-compose.$MODE-full.yml"
if [ "$MODE" = keycloak ]; then
  [ -n "$EDIPI" ] || { echo "keycloak mode needs your EDIPI as arg 3 (Keycloak binds the token to a user). See README."; exit 1; }
  python3 - "$EDIPI" <<'PY'
import json,sys
r=json.load(open("realm-full.json")); e=sys.argv[1]
r["users"]=[{"username":e,"enabled":True,"email":f"{e}@mail.mil","firstName":"CAC","lastName":"User","emailVerified":True,"attributes":{"edipi":[e]}}]
json.dump(r,open("realm-full.rendered.json","w"),indent=2); print("provisioned Keycloak user for EDIPI",e)
PY
fi

# Clean start (fresh DB so /done shows only your login), then up -d (persists; no trap).
docker compose -f "$COMPOSE" down -v >/dev/null 2>&1 || true
docker compose -f "$COMPOSE" up -d 2>&1 | tail -1
echo "==> waiting for the browser-facing ports…"
# front-end port differs by mode: #2 nginx on 8543, #3 Keycloak on 8843
if [ "$MODE" = keycloak ]; then FE="https://cac-idp:8843/realms/goat/.well-known/openid-configuration"; else FE="https://cac-idp:8543/.well-known/openid-configuration"; fi
for _i in $(seq 1 60); do
  curl -fsS http://kratos-idp:4433/health/alive >/dev/null 2>&1 \
    && curl -fsS http://kratos-idp:4455/ >/dev/null 2>&1 \
    && curl -fsSk "$FE" >/dev/null 2>&1 \
    && curl -fsS http://localhost:9090/ >/dev/null 2>&1 && break || sleep 4
done
echo
echo "  Full chain up ($MODE) and it will STAY up. INSERT YOUR CAC, then open:"
echo "      http://localhost:9090"
echo "  Stop it with:  ./run.sh stop $MODE"
