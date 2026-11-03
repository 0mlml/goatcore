#!/usr/bin/env bash
# ops/ci/check-mermaid-renders.sh — every ```mermaid block must actually render.
#
# WHY: a mermaid block with a syntax error does not fail anything. It renders as
# an error box on GitHub and on any docs surface, and nothing else notices. One
# had been sitting in docs/design/failure-isolation.md — `A -.x| B[...]` opens an
# edge label with `|` and never closes it — long enough that nobody could say
# when it broke. Rendering all 184 blocks under docs/ found exactly that one, so
# the class is rare and cheap to hold at zero.
#
# DIFF-SCOPED by default: only markdown changed against the merge base is
# rendered, because mmdc drives a real headless browser and the full sweep takes
# minutes. Pass --all to render every block in docs/.
#
# Requires mmdc (@mermaid-js/mermaid-cli) and a Chrome/Chromium. Skips with a
# loud note when either is missing — a missing renderer must not read as a pass.
set -uo pipefail

BASE="${GITHUB_BASE_REF:-main}"
mode="${1:-diff}"

if ! command -v mmdc >/dev/null 2>&1; then
  echo "check-mermaid-renders: SKIP — mmdc not installed (npm i -g @mermaid-js/mermaid-cli)"
  exit 0
fi

# Point puppeteer at whatever Chrome exists; its bundled download is not assumed.
CHROME=""
for c in "$CHROME_PATH" "$(command -v google-chrome-stable)" "$(command -v google-chrome)" \
         "$(command -v chromium)" "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"; do
  [ -n "${c:-}" ] && [ -x "${c:-}" ] && { CHROME="$c"; break; }
done 2>/dev/null
PCFG=""
if [ -n "$CHROME" ]; then
  PCFG=$(mktemp)
  printf '{"executablePath":"%s","args":["--no-sandbox","--disable-gpu"]}\n' "$CHROME" > "$PCFG"
fi

# bash 3.2 compatible (no mapfile — ops/ci/check-bash-portability.sh forbids it).
files=()
if [ "$mode" = "--all" ]; then
  while IFS= read -r f; do
    [ -n "$f" ] && files+=("$f")
  done <<EOF
$(grep -rl '```mermaid' docs/ 2>/dev/null)
EOF
else
  while IFS= read -r f; do
    [ -n "$f" ] || continue
    [ -f "$f" ] || continue
    case "$f" in
      *.md) grep -q '```mermaid' "$f" && files+=("$f") ;;
    esac
  done <<EOF
$(git diff --name-only "origin/$BASE"...HEAD 2>/dev/null || git diff --name-only HEAD~1 2>/dev/null)
EOF
fi

if [ "${#files[@]}" -eq 0 ]; then
  echo "check-mermaid-renders: OK — no changed markdown carries a mermaid block"
  exit 0
fi

tmp=$(mktemp -d); n=0; fail=0
for f in "${files[@]}"; do
  python3 - "$f" "$tmp" <<'PY'
import sys, os, re
src, out = sys.argv[1], sys.argv[2]
safe = src.replace("/", "__")
for i, b in enumerate(re.findall(r"```mermaid\n(.*?)```", open(src).read(), re.S)):
    open(os.path.join(out, f"{safe}#{i}.mmd"), "w").write(b)
PY
done

for m in "$tmp"/*.mmd; do
  [ -e "$m" ] || continue
  n=$((n + 1))
  label=$(basename "$m" .mmd | sed 's|__|/|g')
  if [ -n "$PCFG" ]; then out=$(mmdc -p "$PCFG" -i "$m" -o "$m.svg" -q 2>&1); rc=$?
  else out=$(mmdc -i "$m" -o "$m.svg" -q 2>&1); rc=$?; fi
  if [ "$rc" -ne 0 ]; then
    if printf '%s' "$out" | grep -qiE "could not find (chrome|chromium)|ChromeLauncher"; then
      echo "check-mermaid-renders: SKIP — no Chrome available for mmdc"
      rm -rf "$tmp"; [ -n "$PCFG" ] && rm -f "$PCFG"
      exit 0
    fi
    echo "  FAIL  $label"
    printf '%s\n' "$out" | grep -iE "error|expecting|unexpected" | head -3 | sed 's/^/        /'
    fail=$((fail + 1))
  fi
done
rm -rf "$tmp"; [ -n "$PCFG" ] && rm -f "$PCFG"

if [ "$fail" -ne 0 ]; then
  echo
  echo "check-mermaid-renders: $fail of $n block(s) do not render — they show as an error box, not a diagram."
  exit 1
fi
echo "check-mermaid-renders: OK — $n mermaid block(s) render"
