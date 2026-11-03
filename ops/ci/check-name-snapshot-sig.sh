#!/usr/bin/env bash
# check-name-snapshot-sig.sh — every committed name-fallback snapshot must
# carry a valid detached signature from its declared CA
# (docs/design/name-fallback-resolver.md, ADR 1082).
#
# The signature is base64(ASN.1 DER ECDSA-Sig-Value, P-256) over SHA-256 of
# the exact name-snapshot.json bytes. The verification key is NEVER taken on
# the artifact's word alone: ca_id must resolve to a committed SPKI at
# ops/enrollment/public-keys/<ca_id>.pem — an artifact naming an unknown CA
# fails. An unsigned or stale-signed snapshot cannot merge (the F-197 lesson
# applied to the fallback layer itself: silent drift deploys dark).
set -euo pipefail

cd "$(dirname "$0")/../.."

fail=0
found=0
for snap in ops/dns/*/name-snapshot.json; do
  [ -e "$snap" ] || continue
  found=1
  sig="${snap}.sig"
  if [ ! -f "$sig" ]; then
    echo "FAIL: $snap has no ${sig##*/} — sign it with ops/enrollment/cmd/name-snapshot-sign" >&2
    fail=1
    continue
  fi
  ca_id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["ca_id"])' "$snap")
  pubkey="ops/enrollment/public-keys/${ca_id}.pem"
  if [ ! -f "$pubkey" ]; then
    echo "FAIL: $snap declares ca_id=$ca_id but $pubkey is not committed" >&2
    fail=1
    continue
  fi
  tmp_sig=$(mktemp)
  tmp_pub=$(mktemp)
  trap 'rm -f "$tmp_sig" "$tmp_pub"' EXIT
  openssl base64 -d -A -in "$sig" -out "$tmp_sig"
  # public-keys PEMs carry RFC-1421 headers (CA-ID:, Created-At:, Note:)
  # inside the block; openssl refuses headers on PUBLIC KEY blocks, so
  # rebuild a clean PEM from the base64 body before verifying.
  python3 - "$pubkey" >"$tmp_pub" <<'PYEOF'
import sys
body, inside = [], False
for line in open(sys.argv[1]):
    line = line.strip()
    if line == "-----BEGIN PUBLIC KEY-----":
        inside = True
    elif line == "-----END PUBLIC KEY-----":
        break
    elif inside and line and ":" not in line:
        body.append(line)
print("-----BEGIN PUBLIC KEY-----")
print("\n".join(body))
print("-----END PUBLIC KEY-----")
PYEOF
  if openssl dgst -sha256 -verify "$tmp_pub" -signature "$tmp_sig" "$snap" >/dev/null 2>&1; then
    echo "OK: $snap signature valid (ca $ca_id)"
  else
    echo "FAIL: $snap signature does not verify against $pubkey —" \
         "the snapshot changed after signing; re-run name-snapshot-sign and commit both" >&2
    fail=1
  fi
  rm -f "$tmp_sig"
done

if [ "$found" -eq 0 ]; then
  echo "OK: no name snapshots committed (no site has name_snapshot_enabled yet)"
fi
exit "$fail"
