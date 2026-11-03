#!/usr/bin/env bash
# render-skills-handbook.sh — regenerate the HTML and PDF exports of the
# goat Skills Handbook from its Markdown source. The Markdown is canonical;
# the HTML and PDF are outputs. Re-run after editing the .md.
#
#   Requires: pandoc, and Google Chrome (for HTML -> PDF).
#   Usage:    ops/scripts/render-skills-handbook.sh
set -euo pipefail

DIR="$(cd "$(dirname "$0")/../../docs/design" && pwd)"
SRC="$DIR/goat-skills-handbook.md"
HTML="$DIR/goat-skills-handbook.html"
PDF="$DIR/goat-skills-handbook.pdf"

command -v pandoc >/dev/null || { echo "pandoc not installed (brew install pandoc)"; exit 1; }

HEADER="$(mktemp -t skills-hb-header).html"
cat > "$HEADER" <<'STYLEEOF'
<style>
  :root{ --ink:#15211f; --muted:#546460; --line:#dbe2e5; --accent:#0a5a52;
         --soft:#eef4f2; --code:#0a5a52; }
  html{ -webkit-print-color-adjust:exact; print-color-adjust:exact; }
  body{ font-family:"IBM Plex Sans",-apple-system,Segoe UI,Helvetica,Arial,sans-serif;
        color:var(--ink); line-height:1.6; max-width:44rem; margin:2.2rem auto;
        padding:0 1.4rem; font-size:16px; }
  h1{ font-family:Georgia,"Times New Roman",serif; font-size:2.3rem; line-height:1.1;
      letter-spacing:-.01em; margin:0 0 .3rem; }
  h1 + p em{ color:var(--muted); font-size:1.05rem; }
  h2{ font-family:Georgia,serif; font-size:1.45rem; margin:2.4rem 0 .5rem;
      padding-top:1.1rem; border-top:1px solid var(--line); }
  h3{ font-size:.86rem; text-transform:uppercase; letter-spacing:.08em;
      color:var(--muted); margin:1.8rem 0 .6rem; }
  a{ color:var(--accent); }
  code{ font-family:"IBM Plex Mono",ui-monospace,Menlo,monospace; font-size:.86em;
        background:var(--soft); border:1px solid var(--line); border-radius:4px;
        padding:.03em .34em; color:var(--code); }
  pre{ background:var(--soft); border:1px solid var(--line); border-left:3px solid var(--accent);
       border-radius:8px; padding:.8rem 1rem; overflow-x:auto; }
  pre code{ background:none; border:none; padding:0; }
  blockquote{ border-left:3px solid var(--line); margin:1.1rem 0; padding:.2rem 0 .2rem 1rem;
              color:var(--muted); font-size:.95rem; }
  table{ border-collapse:collapse; width:100%; font-size:.92rem; margin:1rem 0; }
  th,td{ border:1px solid var(--line); padding:.45rem .7rem; text-align:left; vertical-align:top; }
  th{ background:var(--soft); font-size:.78rem; text-transform:uppercase; letter-spacing:.04em;
      color:var(--muted); }
  hr{ border:none; border-top:1px solid var(--line); margin:2.2rem 0; }
  strong{ font-weight:600; }
  li{ margin:.3rem 0; }
  @media print{ body{ margin:0; max-width:none; font-size:11pt; } h2{ break-before:auto; }
                h2,h3{ break-after:avoid; } pre,table,blockquote{ break-inside:avoid; } }
</style>
STYLEEOF

echo "rendering HTML -> $HTML"
pandoc "$SRC" \
  --standalone \
  --metadata title="goat Skills Handbook" \
  --include-in-header "$HEADER" \
  -o "$HTML"
rm -f "$HEADER"

CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
if [ -x "$CHROME" ]; then
  echo "rendering PDF  -> $PDF"
  "$CHROME" --headless --disable-gpu --no-pdf-header-footer \
    --print-to-pdf="$PDF" "file://$HTML" >/dev/null 2>&1
else
  echo "Google Chrome not found — skipped PDF. Install Chrome or print $HTML by hand."
fi

echo "done."
