#!/usr/bin/env bash
# publish-image.sh — sign a rapid-bringup image and put it on the mirror.
#
# THE GAP THIS CLOSES. pull-image.sh has existed since Block 68C and nothing
# ever fed it: the R2 bucket, the S3 bucket and the bootstrap-bundle bucket were
# all empty, so every goatnet we have was stood up off-mirror. This is the
# missing half — the producer.
#
# Object layout is pull-image.sh's, unchanged:
#     <hardware-class>-<tag>.img      the artifact
#     <hardware-class>-<tag>.img.sig  ssh-keygen -Y sign, namespace bootstrap-image
#
# Signing uses an OpenSSH key whose public half is listed in
# ops/bootstrap/allowed_signers. That is a DIFFERENT file from recipients.txt:
# allowed_signers says who may SIGN, recipients.txt says who may DECRYPT the
# bootstrap bundle. Pointing verification at the age roster is what left
# pull-image.sh's verify unexecutable.
#
# Spec: docs/design/rapid-site-bringup.md §5
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
DEFAULT_SIGNERS=""   # empty => generated from the operator roster at run time
DEFAULT_KEY="$HOME/.ssh/id_ed25519"
BUCKET="${IMAGE_MIRROR_BUCKET:-image-mirror}"
CF_API="https://api.cloudflare.com/client/v4/accounts"

usage() {
  cat <<EOF
usage: $(basename "$0") --hardware-class <class> \\
                        --tag <tag> --image <path> [--signing-key <path>]
                        [--signer-id <principal>] [--dry-run]

  Signs <path> and uploads <class>-<tag>.img{,.sig} to the R2 image mirror,
  then verifies the round trip by fetching both back and checking the
  signature exactly as pull-image.sh will.

  Needs CLOUDFLARE_API_TOKEN (or _PROD) and TF_VAR_cloudflare_account_id.
EOF
  exit 1
}

HW_CLASS=""; TAG=""; IMAGE=""; DRY=0
SIGNERS="$DEFAULT_SIGNERS"; KEY="$DEFAULT_KEY"; SIGNER_ID=""
while [ $# -gt 0 ]; do case "$1" in
  --hardware-class) HW_CLASS=$2; shift 2;;
  --tag) TAG=$2; shift 2;;
  --image) IMAGE=$2; shift 2;;
  --signing-key) KEY=$2; shift 2;;
  --signers) SIGNERS=$2; shift 2;;
  --signer-id) SIGNER_ID=$2; shift 2;;
  --dry-run) DRY=1; shift;;
  -h|--help) usage;;
  *) echo "unknown arg: $1" >&2; usage;;
esac; done

[ -n "$HW_CLASS" ] && [ -n "$TAG" ] && [ -n "$IMAGE" ] || usage
. "$REPO_ROOT/ops/bootstrap/lib-hardware-classes.sh"
_require_class "$HW_CLASS" || exit 2
[ -f "$IMAGE" ] || { echo "no image at $IMAGE" >&2; exit 2; }
[ -f "$KEY" ] || { echo "no signing key at $KEY" >&2; exit 2; }
[ -f "$SIGNERS" ] || { echo "no allowed_signers at $SIGNERS" >&2; exit 2; }

_ensure_signers() {
  [ -n "${SIGNERS:-}" ] && [ -f "$SIGNERS" ] && return 0
  SIGNERS="$(mktemp)"; trap 'rm -f "$SIGNERS"' EXIT
  "$REPO_ROOT/ops/bootstrap/gen-allowed-signers.sh" "$SIGNERS" >/dev/null || return 1
}

log() { printf '%s\n' "$*" >&2; }
OBJ="${HW_CLASS}-${TAG}.img"
SIG="${OBJ}.sig"
WORK="$(mktemp -d)"; trap 'rm -rf "$WORK"' EXIT

# The signer must be IN allowed_signers, or we would publish something the
# fleet cannot verify — a failure that only shows up at bringup, on a host with
# no operator sitting at it.
PUB="$(ssh-keygen -y -f "$KEY")"
grep -qF "$(echo "$PUB" | awk '{print $2}')" "$SIGNERS" || {
  log "refusing: the public half of $KEY is not listed in $SIGNERS."
  log "          publishing it would produce an artifact nothing can verify."
  exit 3; }
PRINCIPAL="${SIGNER_ID:-$(grep -F "$(echo "$PUB" | awk '{print $2}')" "$SIGNERS" | awk '{print $1}' | head -1)}"

cp "$IMAGE" "$WORK/$OBJ"
ssh-keygen -Y sign -f "$KEY" -n bootstrap-image "$WORK/$OBJ" >/dev/null 2>&1
log "signed  $OBJ  as $PRINCIPAL  ($(wc -c <"$WORK/$OBJ" | tr -d ' ') bytes)"

ssh-keygen -Y verify -f "$SIGNERS" -I "$PRINCIPAL" -n bootstrap-image \
  -s "$WORK/$SIG" < "$WORK/$OBJ" >/dev/null 2>&1 \
  || { log "refusing: local verify failed before upload"; exit 3; }
log "local verify: PASS"

[ "$DRY" = "1" ] && { log "dry-run: would upload $OBJ and $SIG to r2://$BUCKET"; exit 0; }

ACC="${TF_VAR_cloudflare_account_id:-${CLOUDFLARE_ACCOUNT_ID:-}}"
TOK="${CLOUDFLARE_API_TOKEN:-${CLOUDFLARE_API_TOKEN_PROD:-}}"
[ -n "$ACC" ] && [ -n "$TOK" ] || { log "need TF_VAR_cloudflare_account_id + CLOUDFLARE_API_TOKEN"; exit 2; }

put() {
  local f="$1" key="$2" code
  code=$(curl -s -o /dev/null -w '%{http_code}' -X PUT \
    -H "Authorization: Bearer $TOK" -H 'Content-Type: application/octet-stream' \
    --data-binary @"$f" "$CF_API/$ACC/r2/buckets/$BUCKET/objects/$key")
  [ "$code" = 200 ] || { log "upload $key failed: http $code"; return 1; }
  log "uploaded $key"
}
put "$WORK/$OBJ" "$OBJ"
put "$WORK/$SIG" "$SIG"

# Round-trip proof. Publishing without fetching back is how four empty buckets
# came to be described as "landed + actualized".
log "round trip: fetching both objects back"
RT="$WORK/rt"; mkdir -p "$RT"
for k in "$OBJ" "$SIG"; do
  curl -s --fail -H "Authorization: Bearer $TOK" \
    "$CF_API/$ACC/r2/buckets/$BUCKET/objects/$k" -o "$RT/$k" \
    || { log "round trip: could not fetch $k"; exit 4; }
done
cmp -s "$RT/$OBJ" "$WORK/$OBJ" || { log "round trip: artifact differs after upload"; exit 4; }
ssh-keygen -Y verify -f "$SIGNERS" -I "$PRINCIPAL" -n bootstrap-image \
  -s "$RT/$SIG" < "$RT/$OBJ" >/dev/null 2>&1 \
  || { log "round trip: signature failed to verify after upload"; exit 4; }
log "round trip: PASS — bytes identical, signature verifies"
log ""
log "published: $OBJ  (pull with: ops/bootstrap/pull-image.sh --hardware-class $HW_CLASS --tag $TAG --output <path>)"
