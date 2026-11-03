#!/usr/bin/env bash
# gen-test-cac.sh — the us-dod profile of gen-test-card.sh, kept for the callers that
# expect the old file names (ops/identity/cac-live-test, keycloak-cac-proof):
# out/{ca.crt,ca.key,cac.crt,cac.key,cac.p12,EDIPI} point at out/us-dod/.
# Usage: ./gen-test-cac.sh [EDIPI]   (default 1234567890)
set -euo pipefail
cd "$(dirname "$0")"
./gen-test-card.sh us-dod "${1:-1234567890}"
mkdir -p out
for pair in ca.crt:ca.crt ca.key:ca.key card.crt:cac.crt card.key:cac.key card.p12:cac.p12; do
  ln -sf "us-dod/${pair%%:*}" "out/${pair##*:}"
done
echo "EDIPI=$(cat out/us-dod/IDENTIFIER)" > out/EDIPI
echo "==> out/{ca.crt,cac.crt,cac.key,cac.p12} -> out/us-dod/ (p12 pass: cardtest)"
