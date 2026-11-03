#!/usr/bin/env bash
# Prove PATH #3: a CAC client cert -> Keycloak x509 -> OIDC token carrying the EDIPI.
set -euo pipefail
cd "$(dirname "$0")"
[ -f ../cac-test-pki/out/cac.crt ] || ../cac-test-pki/gen-test-cac.sh 1234567890

echo "==> up keycloak (import realm; ~40s first boot)"
docker compose up -d 2>&1 | tail -2
echo "==> waiting for keycloak realm"
for _i in $(seq 1 60); do
  curl -fsS http://localhost:8480/realms/goat >/dev/null 2>&1 && break || sleep 3
done
curl -fsS http://localhost:8480/realms/goat >/dev/null && echo "   realm 'goat' up"

NET=$(docker compose ps --format '{{.Name}}' keycloak | sed 's/-keycloak-1//')_cac
echo "==> CAC x509 direct-grant (curl --cert = presenting the CAC at the TLS layer)"
docker run --rm --network "$NET" \
  -v "$(cd ..; pwd)/cac-test-pki/out:/pki:ro" -v "$(pwd):/kc:ro" \
  badouralix/curl-jq sh -c '
    TOK=$(curl -sS --cacert /kc/kc-server.crt --cert /pki/cac.crt --key /pki/cac.key \
      https://keycloak:8443/realms/goat/protocol/openid-connect/token \
      -d grant_type=password -d client_id=proof -d scope=openid)
    ERR=$(echo "$TOK" | jq -r ".error // empty")
    if [ -n "$ERR" ]; then echo "  token error: $ERR - $(echo "$TOK"|jq -r ".error_description // empty")"; exit 1; fi
    echo "  --- access_token claims ---"
    echo "$TOK" | jq -r ".access_token" | cut -d. -f2 | sed "s/-/+/g;s/_/\//g" | (cat;echo "==") | base64 -d 2>/dev/null | jq "{sub,preferred_username,edipi,email}"
    EDIPI=$(echo "$TOK" | jq -r ".access_token" | cut -d. -f2 | sed "s/-/+/g;s/_/\//g" | (cat;echo "==") | base64 -d 2>/dev/null | jq -r ".edipi // empty")
    if [ "$EDIPI" = "1234567890" ]; then
      echo "  ==> PATH #3 PROVEN: CAC cert -> Keycloak x509 -> OIDC token carrying edipi=1234567890"
    else
      echo "  FAIL: edipi claim missing/mismatch (got: $EDIPI)"; exit 1
    fi
  '
echo "==> done. Kratos federation to this Keycloak reuses the path-#1-proven oidc broker"
echo "    + ops/identity/claim-maps/eams-a.jsonnet (edipi -> dod-piv binding). 'docker compose down -v' to clean up."
