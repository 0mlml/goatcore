#!/usr/bin/env bash
# goat-image-bundle-build — assemble a cosign-signed OCI image bundle for
# closed-LAN distribution.
#
# Run on a WAN-connected operator machine (bootstrap-pack assembly box).
# Produces a single tarball that's carried onto the closed LAN via USB
# and unpacked into nova-lan-A's on-box registry by goat-image-import.sh.
#
# Inputs:
#   --manifest <path>      YAML listing images to bundle (schema below)
#   --signing-key <path>   cosign private key (operator-held)
#   --output <path>        output tarball path
#
# Manifest schema (YAML):
#   images:
#     - name: ghcr.io/usareur-af/goat-zenoh-portal
#       tag: sha-de6157cc
#     - name: traefik
#       tag: v3.2
#     - name: postgres
#       tag: 16-alpine
#     ...
#
# Produces a bundle with layout:
#   bundle/
#     manifest.yaml         (copy of input manifest)
#     manifest.yaml.sig     (cosign signature of manifest.yaml)
#     images/
#       <name-slashes-flattened>__<tag>.tar      (crane-format tarball)
#       <name-slashes-flattened>__<tag>.tar.sig  (cosign signature)
#
# Requires: crane, cosign, yq, tar.

set -euo pipefail

MANIFEST=""
SIGNING_KEY=""
OUTPUT=""

while [ $# -gt 0 ]; do
  case "$1" in
    --manifest) MANIFEST="$2"; shift 2;;
    --signing-key) SIGNING_KEY="$2"; shift 2;;
    --output) OUTPUT="$2"; shift 2;;
    *) echo "unknown arg: $1" >&2; exit 2;;
  esac
done

if [ -z "$MANIFEST" ] || [ -z "$SIGNING_KEY" ] || [ -z "$OUTPUT" ]; then
  echo "usage: $0 --manifest <path> --signing-key <path> --output <path>" >&2
  exit 2
fi

for tool in crane cosign yq tar; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "missing required tool: $tool" >&2
    exit 1
  fi
done

WORKDIR=$(mktemp -d)
trap 'rm -rf "$WORKDIR"' EXIT

BUNDLE="$WORKDIR/bundle"
mkdir -p "$BUNDLE/images"

cp "$MANIFEST" "$BUNDLE/manifest.yaml"
cosign sign-blob --key "$SIGNING_KEY" --yes \
  --output-signature "$BUNDLE/manifest.yaml.sig" \
  "$BUNDLE/manifest.yaml"

count=$(yq '.images | length' "$BUNDLE/manifest.yaml")
echo "bundling $count images..." >&2

for i in $(seq 0 $((count - 1))); do
  NAME=$(yq ".images[$i].name" "$BUNDLE/manifest.yaml")
  TAG=$(yq ".images[$i].tag" "$BUNDLE/manifest.yaml")
  SLUG=$(echo "$NAME" | tr '/' '_' | tr '@' '_' | tr ':' '_')
  TARBALL="$BUNDLE/images/${SLUG}__${TAG}.tar"
  echo "  [$((i + 1))/$count] $NAME:$TAG -> $(basename "$TARBALL")" >&2
  crane pull "$NAME:$TAG" "$TARBALL"
  cosign sign-blob --key "$SIGNING_KEY" --yes \
    --output-signature "$TARBALL.sig" \
    "$TARBALL"
done

tar -C "$WORKDIR" -cf "$OUTPUT" bundle/
BUNDLE_SIZE=$(du -h "$OUTPUT" | cut -f1)
SHA256=$(sha256sum "$OUTPUT" | cut -d' ' -f1)
echo "" >&2
echo "bundle: $OUTPUT" >&2
echo "  size:   $BUNDLE_SIZE" >&2
echo "  sha256: $SHA256" >&2
