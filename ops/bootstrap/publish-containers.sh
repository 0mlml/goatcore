#!/usr/bin/env bash
# publish-containers.sh — stage a goatnet's whole container set on the mirror.
#
# THE PROBLEM THIS SOLVES. A cold site is six blank machines (ADR 1141: a core
# pair plus an obs pair, twice over for a full host site). Each one currently
# apt-installs an engine over the internet and then pulls 37 container images
# over whatever link the site has, before ansible does anything. Six machines
# times 37 images is the afternoon that rapid-site-bringup.md's one-hour claim
# is measured against — and the OS image mirror alone does not touch it, because
# the OS is the small half.
#
# So: resolve the goatnet's pinned images ONCE, `docker save` them into a single
# archive, sign it, and put it on the mirror. At the site, one download and one
# `docker load` per machine, off a local copy if the link is bad.
#
# Pins come from the goatnet's own group_vars, so the archive matches what that
# goatnet actually deploys rather than whatever :latest resolved to on the day
# it was baked.
#
# Object layout mirrors publish-image.sh:
#     containers-<goatnet>-<tag>.tar      the archive
#     containers-<goatnet>-<tag>.tar.sig  ssh-keygen -Y sign, ns bootstrap-image
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
DEFAULT_SIGNERS=""   # empty => generated from the operator roster at run time
DEFAULT_KEY="$HOME/.ssh/id_ed25519"
BUCKET="${IMAGE_MIRROR_BUCKET:-image-mirror}"
CF_API="https://api.cloudflare.com/client/v4/accounts"

usage() {
  cat <<EOF
usage: $(basename "$0") --goatnet <group> --tag <tag> [--signing-key <path>]
                        [--platform <os/arch>] [--list-only] [--dry-run]

  --goatnet     ansible group, e.g. zagros_dev_hosts (pins read from its group_vars)
  --platform    default linux/amd64 — Karbon 801/802 are x86; use linux/arm64 for Orin
  --list-only   resolve and print the image set, pull nothing

  Needs CLOUDFLARE_API_TOKEN (or _PROD) + TF_VAR_cloudflare_account_id to upload.
EOF
  exit 1
}

GROUP=""; TAG=""; PLATFORM="linux/amd64"; LIST=0; DRY=0
SIGNERS="$DEFAULT_SIGNERS"; KEY="$DEFAULT_KEY"
while [ $# -gt 0 ]; do case "$1" in
  --goatnet) GROUP=$2; shift 2;;
  --tag) TAG=$2; shift 2;;
  --platform) PLATFORM=$2; shift 2;;
  --signing-key) KEY=$2; shift 2;;
  --signers) SIGNERS=$2; shift 2;;
  --list-only) LIST=1; shift;;
  --dry-run) DRY=1; shift;;
  -h|--help) usage;;
  *) echo "unknown arg: $1" >&2; usage;;
esac; done
[ -n "$GROUP" ] || usage
[ "$LIST" = 1 ] || [ -n "$TAG" ] || usage

_ensure_signers() {
  [ -n "${SIGNERS:-}" ] && [ -f "$SIGNERS" ] && return 0
  SIGNERS="$(mktemp)"; trap 'rm -f "$SIGNERS"' EXIT
  "$REPO_ROOT/ops/bootstrap/gen-allowed-signers.sh" "$SIGNERS" >/dev/null || return 1
}

log() { printf '%s\n' "$*" >&2; }
GV="$REPO_ROOT/ops/ansible/group_vars/$GROUP"
[ -d "$GV" ] || { log "no group_vars for $GROUP"; exit 2; }

# Resolve pins from the goatnet's own vars. Digest-pinned entries are taken
# verbatim: a bundle built from tags would drift from what the site deploys,
# which is the whole failure mode digests exist to prevent.
IMAGES="$(grep -rhoE 'ghcr\.io/\{\{ ghcr_org \}\}/[a-z0-9._-]+(:[A-Za-z0-9._-]+)?(@sha256:[0-9a-f]{64})?' "$GV" 2>/dev/null \
          | sed "s|{{ ghcr_org }}|${GHCR_ORG:-usareur-af}|" | sort -u)"
COUNT="$(printf '%s\n' "$IMAGES" | grep -c . || true)"
[ "$COUNT" -gt 0 ] || { log "no ghcr images found in $GV"; exit 2; }

log "goatnet $GROUP: $COUNT distinct images (platform $PLATFORM)"

# SUPPLY CHAIN, FIRST GATE: refuse anything not pinned by digest.
# A bundle assembled from a mutable tag cannot be reproduced and cannot be
# audited — what was staged is not provably what the site deploys. ghcr.io
# resolves a bare name to :latest, which anyone with push rights can move.
UNPINNED="$(printf '%s\n' "$IMAGES" | grep -v '@sha256:' || true)"
if [ -n "$UNPINNED" ]; then
  log "REFUSING: these references are not digest-pinned, so the bundle would not be reproducible:"
  printf '%s\n' "$UNPINNED" | sed 's/^/    /' >&2
  log "  Pin them in group_vars (ops/pins/generate-pins.py renders the digest) and re-run."
  log "  Override for a deliberate exception only: ALLOW_UNPINNED=1"
  [ "${ALLOW_UNPINNED:-0}" = "1" ] || exit 3
  log "  ALLOW_UNPINNED=1 set — continuing, and recording them as unpinned in the manifest."
fi

if [ "$LIST" = 1 ]; then printf '%s\n' "$IMAGES"; exit 0; fi

command -v docker >/dev/null || { log "docker required to save images"; exit 2; }
WORK="$(mktemp -d)"; trap 'rm -rf "$WORK"' EXIT
OBJ="containers-${GROUP}-${TAG}.tar"
SIG="${OBJ}.sig"

log "pulling $COUNT images (this is the slow part, done ONCE here instead of six times at the site)"
FAILED=0
while read -r img; do
  [ -z "$img" ] && continue
  docker pull --platform "$PLATFORM" "$img" >/dev/null 2>&1 || { log "  pull FAILED: $img"; FAILED=$((FAILED+1)); }
done <<< "$IMAGES"
[ "$FAILED" -eq 0 ] || { log "$FAILED image(s) failed to pull — refusing to publish a partial set"; exit 3; }

# SECOND GATE: what docker actually resolved must match what we asked for.
# A registry that served a different digest than the reference names is the
# attack this whole exercise is about, and it is silent unless checked.
MANIFEST="$WORK/MANIFEST.txt"
{
  echo "container bundle manifest"
  echo "goatnet:    $GROUP"
  echo "tag:        $TAG"
  echo "platform:   $PLATFORM"
  echo "built_at:   $(date -u +%FT%TZ)"
  echo "built_by:   $(git -C "$REPO_ROOT" config user.email 2>/dev/null || echo unknown)"
  echo "repo_commit: $(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
  echo ""
  printf "%-72s %s\n" "IMAGE" "RESOLVED DIGEST"
} > "$MANIFEST"
MISMATCH=0
while read -r img; do
  [ -z "$img" ] && continue
  got="$(docker image inspect "$img" --format '{{index .RepoDigests 0}}' 2>/dev/null | sed 's/.*@//')"
  [ -n "$got" ] || got="(no repo digest — locally built or untagged)"
  want="$(printf '%s' "$img" | sed -n 's/.*@\(sha256:[0-9a-f]*\)/\1/p')"
  if [ -n "$want" ] && [ "$want" != "$got" ]; then
    log "  DIGEST MISMATCH $img"
    log "    asked for: $want"
    log "    got:       $got"
    MISMATCH=$((MISMATCH+1))
  fi
  printf "%-72s %s\n" "$img" "$got" >> "$MANIFEST"
done <<< "$IMAGES"
[ "$MISMATCH" -eq 0 ] || { log "$MISMATCH digest mismatch(es) — refusing to publish"; exit 3; }
log "digest check: all $COUNT images resolved to the digest their reference names"

log "saving to $OBJ"
image_refs=()
while IFS= read -r img; do
  [ -z "$img" ] || image_refs+=("$img")
done <<< "$IMAGES"
docker save "${image_refs[@]}" -o "$WORK/$OBJ"
ARCHIVE_SHA="$(shasum -a 256 "$WORK/$OBJ" | cut -d' ' -f1)"
{
  echo ""
  echo "archive:    $OBJ"
  echo "bytes:      $(wc -c <"$WORK/$OBJ" | tr -d ' ')"
  echo "sha256:     $ARCHIVE_SHA"
} >> "$MANIFEST"
log "archive: $(wc -c <"$WORK/$OBJ" | tr -d ' ') bytes  sha256 $ARCHIVE_SHA"

PUB="$(ssh-keygen -y -f "$KEY")"
grep -qF "$(echo "$PUB" | awk '{print $2}')" "$SIGNERS" || {
  log "refusing: $KEY is not in $SIGNERS — the archive would be unverifiable"; exit 3; }
PRINCIPAL="$(grep -F "$(echo "$PUB" | awk '{print $2}')" "$SIGNERS" | awk '{print $1}' | head -1)"
ssh-keygen -Y sign -f "$KEY" -n bootstrap-image "$WORK/$OBJ" >/dev/null 2>&1
ssh-keygen -Y verify -f "$SIGNERS" -I "$PRINCIPAL" -n bootstrap-image \
  -s "$WORK/$SIG" < "$WORK/$OBJ" >/dev/null 2>&1 || { log "local verify failed"; exit 3; }
log "signed as $PRINCIPAL, local verify PASS"

[ "$DRY" = 1 ] && { log "dry-run: would upload $OBJ"; exit 0; }

ACC="${TF_VAR_cloudflare_account_id:-${CLOUDFLARE_ACCOUNT_ID:-}}"
TOK="${CLOUDFLARE_API_TOKEN:-${CLOUDFLARE_API_TOKEN_PROD:-}}"
[ -n "$ACC" ] && [ -n "$TOK" ] || { log "need CLOUDFLARE_API_TOKEN + TF_VAR_cloudflare_account_id"; exit 2; }

# R2's REST API refuses a body over ~300 MB with 413, and a 37-image archive is
# well past that, so parts are the normal case here rather than an edge case.
PART_MB="${PART_MB:-200}"
split -b "${PART_MB}m" "$WORK/$OBJ" "$WORK/part."
N=0
for f in "$WORK"/part.*; do
  sfx="${f##*part.}"; N=$((N+1))
  code=$(curl -s -o /dev/null -w '%{http_code}' -X PUT -H "Authorization: Bearer $TOK" \
    -H 'Content-Type: application/octet-stream' --data-binary @"$f" \
    "$CF_API/$ACC/r2/buckets/$BUCKET/objects/$OBJ.$sfx")
  [ "$code" = 200 ] || { log "upload part $sfx failed: http $code"; exit 4; }
  psha="$(shasum -a 256 "$f" | cut -d' ' -f1)"
  printf "part %-6s %-14s %s\n" "$sfx" "$(wc -c <"$f" | tr -d ' ')" "$psha" >> "$MANIFEST"
  log "  uploaded $OBJ.$sfx  sha256 ${psha:0:16}…"
done
code=$(curl -s -o /dev/null -w '%{http_code}' -X PUT -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/octet-stream' --data-binary @"$WORK/$SIG" \
  "$CF_API/$ACC/r2/buckets/$BUCKET/objects/$SIG")
[ "$code" = 200 ] || { log "signature upload failed: http $code"; exit 4; }
log "  uploaded $SIG"

# The manifest is signed too. An unsigned manifest is a list of hashes anyone
# can rewrite, which verifies nothing.
ssh-keygen -Y sign -f "$KEY" -n bootstrap-image "$MANIFEST" >/dev/null 2>&1
for x in "MANIFEST.txt" "MANIFEST.txt.sig"; do
  code=$(curl -s -o /dev/null -w '%{http_code}' -X PUT -H "Authorization: Bearer $TOK" \
    -H 'Content-Type: application/octet-stream' --data-binary @"$WORK/$x" \
    "$CF_API/$ACC/r2/buckets/$BUCKET/objects/containers-${GROUP}-${TAG}.$x")
  [ "$code" = 200 ] || { log "manifest upload failed ($x): http $code"; exit 4; }
done
log "  uploaded manifest + signature"

log ""
cat "$MANIFEST" >&2
log ""
log "published $OBJ in $N part(s) + signature + signed manifest."
log "At the site:  cat ${OBJ}.* > $OBJ && ssh-keygen -Y verify ... && docker load -i $OBJ"
