#!/usr/bin/env bash
# The F-270 conformance chain: a REAL portal mint must produce a usable
# grant-crypto recipient.
#
#   real ca-init CA -> certmint.Mint (production argv) -> real cert-sign
#     -> assert leaf is ECDSA-P256 (the Go test in internal/certmint)
#     -> gce seal wraps a DEK to the minted leaf
#     -> gce open with the minted key recovers the plaintext
#     -> an outsider key is DENIED (exit 3)
#
# Every portal-minted participant decrypts with exactly this leaf
# (docs/design/grant-keyed-encryption.md §3a), so this is the gate that keeps
# certmint's key type and the envelope's EC-only wrap rule from ever drifting
# apart again.
#
# Run from anywhere inside the repo. CI: the portal-mint-conformance job in
# .github/workflows/grant-crypto.yml.
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
BIN="$WORK/bin"
OUT="$WORK/out"
mkdir -p "$BIN" "$OUT"

echo "== build the real binaries"
(cd "$ROOT/ops/enrollment" && go build -o "$BIN" ./cmd/cert-sign ./cmd/ca-init && go build -o "$BIN" filippo.io/age/cmd/age-keygen)
(cd "$ROOT/ops/grant-crypto" && go build -o "$BIN" ./cmd/gce)

echo "== mint a real portal cert (certmint.Mint -> cert-sign) and assert P-256"
(cd "$ROOT/ops/goat-zenoh-portal" && \
  CERTMINT_CONFORMANCE_BIN_DIR="$BIN" CERTMINT_CONFORMANCE_OUT="$OUT" \
  go test ./internal/certmint -run TestMint_RealCertSign_LeafIsP256Recipient -count=1 -v)

CERT="$OUT/acme-cert.pem"
KEY="$OUT/acme-key.pem"
[ -s "$CERT" ] && [ -s "$KEY" ] || { echo "FAIL: mint test did not export the leaf"; exit 1; }

echo "== wrap a DEK to the minted leaf and open it with the minted key"
PAYLOAD="$WORK/payload"
echo "the well-oiled machine" > "$PAYLOAD"
"$BIN/gce" seal --rs conformance --gen 1 --keyexpr data/conformance \
  --in "$PAYLOAD" --member "acme=$CERT" --out "$WORK/sealed.json"
python3 - "$WORK/sealed.json" "$WORK/bundle.json" "$WORK/envelope.json" <<'PY'
import json, sys
sealed = json.load(open(sys.argv[1]))
json.dump(sealed["bundle"], open(sys.argv[2], "w"))
json.dump(sealed["envelope"], open(sys.argv[3], "w"))
PY
"$BIN/gce" open --bundle "$WORK/bundle.json" --envelope "$WORK/envelope.json" \
  --id acme --key "$KEY" --keyexpr data/conformance --out "$WORK/recovered"
cmp "$PAYLOAD" "$WORK/recovered" || { echo "FAIL: recovered plaintext differs"; exit 1; }
echo "member opens: OK"

echo "== an outsider key must be DENIED (exit 3)"
"$BIN/gce" gen-member outsider "$WORK/outsider"
set +e
"$BIN/gce" open --bundle "$WORK/bundle.json" --envelope "$WORK/envelope.json" \
  --id acme --key "$WORK/outsider/outsider.key.pem" --keyexpr data/conformance --out /dev/null
rc=$?
set -e
[ "$rc" -eq 3 ] || { echo "FAIL: outsider open exited $rc; want 3 (DENIED)"; exit 1; }
echo "outsider denied: OK"

echo "PASS: portal-minted leaf is a usable grant-crypto recipient"
