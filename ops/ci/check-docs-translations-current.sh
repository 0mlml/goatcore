#!/usr/bin/env bash
# check-docs-translations-current.sh — lock the goat docs translations to
# the current English source (docs/design/goat-cli-hosted-docs.md).
#
# Instead of warning that translations *may* lag, we PREVENT lag: re-extract
# the POT from the current English chapters, msgmerge each po/<lang>.po
# against it, and FAIL if any language has untranslated or fuzzy entries —
# i.e. an English string changed but a translation did not keep up. English
# and all languages move together, or this gate blocks the merge. That is
# what "translations locked to the CLI version" means in practice: every
# goat-cli-v* release ships English + 29 languages that are provably in sync.
#
# When this fails, msgmerge has already told you exactly which entries drifted
# — refresh them (the translation fan-out, scoped to the fuzzy/untranslated
# entries) and re-run. See docs/goat-cli-book/po/README.md.
#
# Needs: mdbook, mdbook-i18n-helpers (mdbook-xgettext), gettext (msgmerge/msgfmt).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BOOK="$REPO_ROOT/docs/goat-cli-book"
cd "$BOOK"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# Fresh POT from the current English chapters (NOT the committed one — this
# catches "chapter edited but messages.pot/.po not refreshed" too).
MDBOOK_OUTPUT='{"xgettext": {"pot-file": "messages.pot"}}' \
  mdbook build -d "$TMP/potbuild" >/dev/null 2>&1 || {
    echo "FAIL: could not extract the POT (is mdbook-i18n-helpers installed?)"; exit 1; }
POT="$TMP/potbuild/messages.pot"
[ -f "$POT" ] || { echo "FAIL: POT not produced at $POT"; exit 1; }

# release-notes.md is kept English-only by design (docs/design/goat-cli-hosted-docs.md):
# every release appends an English entry there; the full guide is retranslated only
# on larger releases. Drop its strings from the POT so an English-only release-note
# edit never trips this gate. In a per-language build the untranslated strings fall
# back to English automatically. Portable awk filter (not msggrep, whose --location
# flag semantics vary across gettext builds): drop any PO entry whose `#:` location
# comment references release-notes.md.
#
# reference.md is likewise NOT gettext-translated (po/README.md): the command
# reference is regenerated per language from the binary itself at release
# (`goat --lang <X> docs --markdown`), so its localization lives in the Fluent
# catalogs, not the po files. The committed English snapshot exists for repo
# browsing and would otherwise demand 29 dead translations on every regen.
awk 'BEGIN{RS="";FS="\n"} {drop=0; for(i=1;i<=NF;i++) if($i ~ /^#:.*(release-notes|reference)\.md/) drop=1; if(!drop) printf "%s\n\n", $0}' \
  "$POT" > "$POT.filtered" && mv "$POT.filtered" "$POT"

shopt -s nullglob
pos=(po/*.po)
[ ${#pos[@]} -gt 0 ] || { echo "FAIL: no po/*.po files found"; exit 1; }

drift=0
for po in "${pos[@]}"; do
  lang="$(basename "$po" .po)"
  merged="$TMP/$lang.po"
  # Fuzzy-match against the fresh POT: changed English -> fuzzy, new -> untranslated.
  msgmerge --quiet "$po" "$POT" -o "$merged" 2>/dev/null
  stats="$(msgfmt --statistics -o /dev/null "$merged" 2>&1 || true)"
  if echo "$stats" | grep -qE "[1-9][0-9]* (untranslated|fuzzy)"; then
    echo "DRIFT: $lang — ${stats}"
    drift=1
  fi
done

if [ "$drift" -eq 0 ]; then
  echo "OK: all ${#pos[@]} docs languages are current with the English source."
else
  echo
  echo "Docs translations have drifted from English. Refresh the flagged"
  echo "languages (msgmerge has marked the exact entries) before merging —"
  echo "translations are locked to the CLI version. See docs/goat-cli-book/po/README.md."
fi
exit "$drift"
