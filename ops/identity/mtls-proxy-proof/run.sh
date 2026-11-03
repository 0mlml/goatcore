#!/usr/bin/env bash
# run.sh [<pki id>] — prove the mtls_proxy path for ONE sovereign PKI profile: a
# synthetic card from ../cac-test-pki (that profile's shape) -> nginx mTLS + the
# profile's identifier rule (the registry's maps + pki.js) -> Dex authproxy -> OIDC
# token whose preferred_username is "<pki>:<identifier>".
#
# Default profile us-dod. A sketch profile (est-id) is accepted: the proof is about
# the rendering, not the PKI's trust anchor. Needs docker, openssl, and a python3
# with PyYAML (PYTHON to override).
set -euo pipefail
cd "$(dirname "$0")"
PKI="${1:-us-dod}"
PY="${PYTHON:-python3}"
RENDER=../sovereign-pkis-render.py
ROLE=../../ansible/roles/icam-mtls-proxy
export PKI

[ -f "../cac-test-pki/out/$PKI/card.crt" ] || PYTHON="$PY" ../cac-test-pki/gen-test-card.sh "$PKI"
WANT="$PKI:$(cat "../cac-test-pki/out/$PKI/IDENTIFIER")"

echo "==> render the registry for $PKI (maps + njs wrappers) into gen/"
mkdir -p gen
"$PY" "$RENDER" --include-sketch nginx-maps --pki "$PKI" > gen/pki-maps.conf
{ echo "js_set \$pki_san_upn pki.sanUpn;"; echo "js_set \$pki_san_emails pki.sanEmails;"; } > gen/pki-js-set.conf
"$PY" "$RENDER" --include-sketch nginx-js --pki "$PKI" > gen/pki-profiles.js
if [ -s gen/pki-profiles.js ]; then
  echo "js_import profiles from /etc/nginx/pki-profiles.js;" > gen/pki-js-import.conf
  grep -o 'othername_[a-z0-9_]*' gen/pki-profiles.js | sort -u | while read -r fn; do
    echo "js_set \$pki_${fn} profiles.${fn};" >> gen/pki-js-set.conf
  done
else
  : > gen/pki-js-import.conf
  echo "export default {};" > gen/pki-profiles.js
fi
cp "$ROLE/files/pki.js" gen/pki.js
[ -f proxy.crt ] || openssl req -x509 -newkey rsa:2048 -nodes -keyout proxy.key -out proxy.crt -days 825 \
  -subj "/CN=card-proxy" -addext "subjectAltName=DNS:card-proxy" 2>/dev/null

echo "==> up nginx + dex"
docker compose up -d 2>&1 | tail -2
echo "==> waiting for dex discovery (through the proxy, no cert on the back-channel)"
NET=$(docker compose ps --format '{{.Name}}' nginx | sed 's/-nginx-1//')_mtls
for _i in $(seq 1 30); do
  docker run --rm --network "$NET" -v "$(pwd):/kc:ro" curlimages/curl:8.11.1 \
    -fsS --cacert /kc/proxy.crt https://card-proxy:8443/.well-known/openid-configuration >/dev/null 2>&1 && break || sleep 2
done

echo "==> card sign-in through the mTLS proxy (curl --cert), authorization-code flow"
docker run --rm --network "$NET" -e WANT="$WANT" -e PKI="$PKI" \
  -v "$(cd ..; pwd)/cac-test-pki/out/$PKI:/pki:ro" -v "$(pwd):/kc:ro" \
  badouralix/curl-jq sh -c '
    CA="--cacert /kc/proxy.crt"; CRT="--cert /pki/card.crt --key /pki/card.key"
    B="https://card-proxy:8443"
    curl -sS $CA $CRT -L --max-redirs 10 -D /tmp/hdrs -o /dev/null \
      "$B/auth?client_id=proof&response_type=code&scope=openid+email+profile&redirect_uri=http://localhost/cb&state=s" 2>/dev/null || true
    CODE=$(tr -d "\r" < /tmp/hdrs | grep -io "localhost/cb?[^ ]*code=[^& ]*" | sed -n "s/.*code=\([^& ]*\).*/\1/p" | tail -1)
    [ -n "$CODE" ] || { echo "  no code obtained"; cat /tmp/hdrs; exit 1; }
    echo "  got authorization code via card mTLS"
    TOK=$(curl -sS $CA "$B/token" -d grant_type=authorization_code -d code="$CODE" \
      -d client_id=proof -d scope="openid email profile" -d redirect_uri=http://localhost/cb)
    CLAIMS=$(echo "$TOK" | jq -r ".id_token" | cut -d. -f2 | sed "s/-/+/g;s/_/\//g" | (cat;echo "==") | base64 -d 2>/dev/null)
    echo "  --- id_token claims ---"; echo "$CLAIMS" | jq "{sub,preferred_username,name,email}"
    GOT=$(echo "$CLAIMS" | jq -r ".preferred_username // empty")
    if [ "$GOT" = "$WANT" ]; then
      echo "  ==> PROVEN ($PKI): card -> nginx mTLS + identifier -> Dex -> OIDC token carrying $WANT"
    else
      echo "  FAIL: expected preferred_username $WANT, got: $GOT"; exit 1
    fi
  '
echo "==> a card from an UNLISTED issuer is refused at the auth path"
docker run --rm --network "$NET" -v "$(pwd):/kc:ro" curlimages/curl:8.11.1 \
  -sS --cacert /kc/proxy.crt -o /dev/null -w "  no certificate -> HTTP %{http_code} (expect 403)\n" \
  "https://card-proxy:8443/auth?client_id=proof&response_type=code&scope=openid&redirect_uri=http://localhost/cb&state=s"
echo "==> done. Kratos federation to this Dex uses ops/identity/claim-maps/sovereign-pki.jsonnet."
echo "    'docker compose down -v' to clean up."
