#!/usr/bin/env bash
# fetch-served.sh <dir-url> <asset> <dest-dir> — download one served binary with the
# SHA256SUMS and version.json beside it, refuse it unless its checksum matches, and
# make it executable (macOS: re-sign ad hoc, F-208). Used by live.sh's T2.
set -euo pipefail
URL="$1"; ASSET="$2"; DEST="$3"
mkdir -p "$DEST"
curl -fsS --max-time 120 -o "$DEST/$ASSET" "$URL/$ASSET"
curl -fsS --max-time 30 -o "$DEST/SHA256SUMS" "$URL/SHA256SUMS"
curl -fsS --max-time 30 -o "$DEST/version.json" "$URL/version.json"
cd "$DEST"
line="$(grep " $ASSET\$" SHA256SUMS)" || { echo "fetch-served: $ASSET is not listed in $URL/SHA256SUMS" >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then echo "$line" | sha256sum -c -; else echo "$line" | shasum -a 256 -c -; fi
chmod +x "$ASSET"
if [ "$(uname -s)" = Darwin ] && file "$ASSET" | grep -q Mach-O; then codesign --force --sign - "$ASSET" >/dev/null 2>&1 || true; fi
echo "fetch-served: $ASSET ok, version $(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' version.json)"
