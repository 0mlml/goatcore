#!/usr/bin/env bash
# Render docs/design/managing-netbird-groups.html to a PDF beside it.
# The HTML is the source; the PDF is gitignored and regenerated on demand,
# the same way the skills handbook is rendered. Needs Google Chrome.
set -euo pipefail
DIR="$(cd "$(dirname "$0")/../../docs/design" && pwd)"
SRC="$DIR/managing-netbird-groups.html"
PDF="$DIR/managing-netbird-groups.pdf"
CHROME="${CHROME:-/Applications/Google Chrome.app/Contents/MacOS/Google Chrome}"
[ -x "$CHROME" ] || CHROME="$(command -v google-chrome || command -v chromium || true)"
[ -n "$CHROME" ] && [ -x "$CHROME" ] || { echo "no Chrome/Chromium found; set CHROME=/path/to/chrome" >&2; exit 1; }
"$CHROME" --headless --disable-gpu --no-pdf-header-footer --print-to-pdf="$PDF" "file://$SRC" >/dev/null 2>&1
echo "wrote $PDF ($(wc -c <"$PDF" | tr -d ' ') bytes)"
