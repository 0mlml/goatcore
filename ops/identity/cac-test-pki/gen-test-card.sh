#!/usr/bin/env bash
# gen-test-card.sh — a TEST-ONLY PKI shaped like one sovereign PKI profile, for proving
# the card sign-in paths with no real card or CA. NOT a real PKI: a self-signed CA whose
# subject is the profile's example issuer DN, and a card certificate built from the
# profile's example in ops/identity/sovereign-pkis.yaml (subject DN, SAN UPN / e-mail /
# otherName), identifier substituted.
#
# Usage: ./gen-test-card.sh <pki id> [identifier] [email]
#   ./gen-test-card.sh us-dod 1234567890        CN MAINTAINER.TEST.MI.1234567890, UPN 1234567890121002@mil
#   ./gen-test-card.sh est-id 38001085718       CN "JÕEORG,JAAK-KRISTJAN,38001085718" (sketch profile)
# Output (gitignored): ./out/<pki id>/{ca.crt,ca.key,card.crt,card.key,card.p12,card.cnf,IDENTIFIER}
# p12 password: cardtest. Needs openssl and a python3 with PyYAML (PYTHON to override).
set -euo pipefail
cd "$(dirname "$0")"
PKI="${1:?usage: gen-test-card.sh <pki id> [identifier] [email]}"
IDENT="${2:-}"; EMAIL="${3:-}"
PY="${PYTHON:-python3}"
RENDER=../sovereign-pkis-render.py
OUT="out/$PKI"; mkdir -p "$OUT"

args=(--include-sketch openssl-cnf "$PKI")
[ -n "$IDENT" ] && args+=(--identifier "$IDENT")
[ -n "$EMAIL" ] && args+=(--email "$EMAIL")
CA_SUBJ="$("$PY" "$RENDER" --include-sketch openssl-cnf "$PKI" --ca-subj)"
"$PY" "$RENDER" "${args[@]}" > "$OUT/card.cnf"
"$PY" "$RENDER" "${args[@]}" --print-identifier > "$OUT/IDENTIFIER"

echo "==> test CA shaped like the $PKI issuer: $CA_SUBJ"
openssl req -x509 -newkey rsa:2048 -nodes -keyout "$OUT/ca.key" -out "$OUT/ca.crt" \
  -days 3650 -subj "$CA_SUBJ" -addext "basicConstraints=critical,CA:TRUE" \
  -addext "keyUsage=critical,keyCertSign,cRLSign" 2>/dev/null

echo "==> card certificate from $OUT/card.cnf"
openssl req -newkey rsa:2048 -nodes -keyout "$OUT/card.key" -out "$OUT/card.csr" \
  -config "$OUT/card.cnf" 2>/dev/null
openssl x509 -req -in "$OUT/card.csr" -CA "$OUT/ca.crt" -CAkey "$OUT/ca.key" -CAcreateserial \
  -out "$OUT/card.crt" -days 730 -extfile "$OUT/card.cnf" -extensions ext 2>/dev/null
openssl pkcs12 -export -out "$OUT/card.p12" -inkey "$OUT/card.key" -in "$OUT/card.crt" \
  -certfile "$OUT/ca.crt" -passout pass:cardtest 2>/dev/null

echo "==> what the proxy will see:"
echo -n "  subject (RFC 2253): "; openssl x509 -in "$OUT/card.crt" -noout -subject -nameopt RFC2253 | sed 's/^subject=//'
echo -n "  issuer  (RFC 2253): "; openssl x509 -in "$OUT/card.crt" -noout -issuer -nameopt RFC2253 | sed 's/^issuer=//'
openssl x509 -in "$OUT/card.crt" -noout -ext subjectAltName 2>/dev/null | sed 's/^/  /' || true
openssl verify -CAfile "$OUT/ca.crt" "$OUT/card.crt"
echo "==> done: $OUT/ (identifier $(cat "$OUT/IDENTIFIER"); p12 pass: cardtest)"
