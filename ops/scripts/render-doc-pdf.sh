#!/usr/bin/env bash
# Render one of the team-facing HTML documents to PDF with headless Chrome.
#   ops/scripts/render-doc-pdf.sh docs/operations/example-guide.html [out.pdf]
# Default output is beside the source with a .pdf suffix. PDFs are gitignored;
# the HTML is the source of truth. Sibling of render-netbird-groups-reference.sh.
set -euo pipefail
SRC="${1:?usage: render-doc-pdf.sh <doc.html> [out.pdf]}"
[ -f "$SRC" ] || { echo "no such file: $SRC" >&2; exit 1; }
SRC="$(cd "$(dirname "$SRC")" && pwd)/$(basename "$SRC")"
PDF="${2:-${SRC%.html}.pdf}"
CHROME="${CHROME:-/Applications/Google Chrome.app/Contents/MacOS/Google Chrome}"
[ -x "$CHROME" ] || CHROME="$(command -v google-chrome || command -v chromium || true)"
[ -n "$CHROME" ] && [ -x "$CHROME" ] || { echo "no Chrome/Chromium found; set CHROME=/path/to/chrome" >&2; exit 1; }
"$CHROME" --headless --disable-gpu --no-pdf-header-footer --print-to-pdf="$PDF" "file://$SRC" >/dev/null 2>&1
echo "wrote $PDF"
