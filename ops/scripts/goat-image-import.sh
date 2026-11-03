#!/usr/bin/env bash
# goat-image-import — verify + push a cosign-signed OCI image bundle into
# the on-box registry on a closed-network mgmt-host.
#
# Run on nova-lan-A (or any closed-network mgmt-host with registry-mirror).
# Bundle is produced by goat-image-bundle-build.sh on a WAN-connected
# operator machine and carried in via USB.
#
# Inputs (positional):
#   $1   path to bundle tarball
#
# Environment overrides:
#   GOAT_REGISTRY        — registry endpoint (default: registry.nova.lan:5000)
#   GOAT_TRUST_ANCHOR    — cosign verify-blob trust anchor (default:
#                          /etc/nova-lan-ca/operator-pubkey.pem)
#
# Verifies cosign signatures on the manifest + each image tarball
# against the on-box trust anchor before pushing. Refuses on any
# signature mismatch.
#
# Requires: crane, cosign, yq, tar.

set -euo pipefail

BUNDLE="${1:-}"
REGISTRY="${GOAT_REGISTRY:-registry.nova.lan:5000}"
TRUST="${GOAT_TRUST_ANCHOR:-/etc/nova-lan-ca/operator-pubkey.pem}"

if [ -z "$BUNDLE" ]; then
  echo "usage: $0 <bundle.tar>" >&2
  exit 2
fi
if [ ! -f "$BUNDLE" ]; then
  echo "bundle not found: $BUNDLE" >&2
  exit 1
fi
if [ ! -f "$TRUST" ]; then
  echo "trust anchor not found: $TRUST" >&2
  echo "set GOAT_TRUST_ANCHOR or install the operator-pubkey via the closed-LAN bootstrap pack" >&2
  exit 1
fi

for tool in crane cosign yq tar; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "missing required tool: $tool" >&2
    exit 1
  fi
done

WORKDIR=$(mktemp -d)
trap 'rm -rf "$WORKDIR"' EXIT

tar -xf "$BUNDLE" -C "$WORKDIR"
cd "$WORKDIR/bundle"

# Verify the manifest's own signature first.
if ! cosign verify-blob --key "$TRUST" \
       --signature manifest.yaml.sig manifest.yaml 2>/tmp/goat-import-err; then
  echo "FAIL: manifest signature does not verify against $TRUST" >&2
  cat /tmp/goat-import-err >&2
  exit 1
fi
echo "ok: manifest signature verified"

count=$(yq '.images | length' manifest.yaml)
echo "importing $count images into $REGISTRY..."

for i in $(seq 0 $((count - 1))); do
  NAME=$(yq ".images[$i].name" manifest.yaml)
  TAG=$(yq ".images[$i].tag" manifest.yaml)
  SLUG=$(echo "$NAME" | tr '/' '_' | tr '@' '_' | tr ':' '_')
  TARBALL="images/${SLUG}__${TAG}.tar"

  if [ ! -f "$TARBALL" ]; then
    echo "FAIL: missing image tarball: $TARBALL" >&2
    exit 1
  fi
  if [ ! -f "${TARBALL}.sig" ]; then
    echo "FAIL: missing image signature: ${TARBALL}.sig" >&2
    exit 1
  fi

  if ! cosign verify-blob --key "$TRUST" \
         --signature "${TARBALL}.sig" "$TARBALL" 2>/tmp/goat-import-err; then
    echo "FAIL: $NAME:$TAG signature does not verify" >&2
    cat /tmp/goat-import-err >&2
    exit 1
  fi

  # Strip the upstream registry prefix so images are addressed via the
  # closed-LAN registry. ghcr.io/usareur-af/foo:bar -> registry.nova.lan/goat-maintainer/foo:bar
  TARGET_NAME=$(echo "$NAME" | sed -E 's|^[^/]+/||')
  TARGET="${REGISTRY}/${TARGET_NAME}:${TAG}"

  echo "  [$((i + 1))/$count] $NAME:$TAG -> $TARGET"
  crane push "$TARBALL" "$TARGET"
done

echo ""
echo "import complete: $count images pushed to $REGISTRY"
