#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""translate-po.py — Claude-assisted gettext (.po) translator for the goat docs book.

The prose sibling of `translate-ftl.py`. The goat docs site is localized with
mdbook-i18n-helpers (gettext): `docs/goat-cli-book/src/*.md` is the English
arbiter, `docs/goat-cli-book/po/<lang>.po` carries each language. The currency
gate (`ops/ci/check-docs-translations-current.sh`, workflow docs-i18n-currency)
hard-fails any PR where English moved but a translation did not — so a new or
edited chapter must land English + all 29 languages together.

The po/README.md calls that the "translation fan-out" but shipped no tool for it.
This is that tool: it fills the untranslated and fuzzy `msgstr` entries via the
Anthropic Messages API, preserving every markdown / contract token verbatim.

  --check          Report, per locale, the POT msgids the catalog is missing;
                   exit non-zero if any. No API. (A lighter mirror of the CI gate.)
  --apply          Translate the missing msgids via Claude and APPEND them to
                   each catalog. Needs ANTHROPIC_API_KEY in the environment.
  --lang <code>    Restrict to one locale (repeatable). Default: every po/*.po.
  --model <id>     Model override (default: claude-sonnet-4-6).

Append-only, on purpose: existing entries are left byte-for-byte, so the diff is
the new translation rather than a whole-file `msgmerge --update` reflow (the
committed catalogs keep their unwrapped one-line style). gettext accepts entries
in any order, and the release-time full msgmerge canonicalizes ordering /
locations and drops obsolete entries; idempotent (a re-run skips msgids already
present). Run `--apply` on a clean connection — it fans out one API call/locale.

Regenerate the POT before --apply so new chapter strings are present:
    cd docs/goat-cli-book
    MDBOOK_OUTPUT='{"xgettext": {"pot-file": "messages.pot"}}' mdbook build -d po

Contract-token invariant (mirrors translate-ftl.py / ADR 1024): backticked
`code` spans and markdown link targets `](target)` are reproduced verbatim —
they are contract, not prose. Every translated entry is validated to carry the
SAME backtick spans and the SAME link targets as its English source; a mismatch
is refused and never written, so a bad translation fails loud rather than
corrupting a command example or a cross-page link.

Needs: polib (`pip install polib`) and ANTHROPIC_API_KEY for --apply. Runs
locally or in CI; never shipped in the binary — the docs are a static build
artifact, no runtime translation.
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

try:
    import polib
except ImportError:
    sys.exit(
        "FAIL: this tool needs polib — `pip install polib` (a small pure-Python "
        "gettext library). It is an operator-side translation tool, not shipped "
        "in the binary."
    )

BOOK = Path(__file__).resolve().parents[2] / "docs" / "goat-cli-book"
PO_DIR = BOOK / "po"
POT = PO_DIR / "messages.pot"

LANG_NAMES = {
    "bg": "Bulgarian", "cnr": "Montenegrin", "cs": "Czech", "da": "Danish",
    "de": "German", "el": "Greek", "es": "Spanish", "et": "Estonian",
    "fi": "Finnish", "fr": "French", "hr": "Croatian", "hu": "Hungarian",
    "is": "Icelandic", "it": "Italian", "lb": "Luxembourgish", "lt": "Lithuanian",
    "lv": "Latvian", "mk": "Macedonian", "nb": "Norwegian Bokmål", "nl": "Dutch", # oss-allow: language name
    "pl": "Polish", "pt": "Portuguese", "ro": "Romanian", "sk": "Slovak", # oss-allow: language name
    "sl": "Slovenian", "sq": "Albanian", "sv": "Swedish", "tr": "Turkish",
    "uk": "Ukrainian",
}

DEFAULT_MODEL = "claude-sonnet-4-6"
API_URL = "https://api.anthropic.com/v1/messages"

# release-notes.md is English-only by design (docs/design/goat-cli-hosted-docs.md);
# the currency gate strips it from the POT, so its entries should never be
# translated here — leave them empty so a per-language build falls back to English.
ARBITER_ONLY_SRC = "release-notes.md"


def backticks(text: str) -> "set[str]":
    return set(re.findall(r"`[^`]*`", text))


def link_targets(text: str) -> "set[str]":
    # Markdown inline-link targets: the (target) in [text](target). mdbook-i18n
    # keeps the whole link in one unit; the target must survive translation.
    return set(re.findall(r"\]\(([^)]*)\)", text))


def release_notes_only(entry: "polib.POEntry") -> bool:
    """True when every source location is release-notes.md — kept English-only
    by design (the currency gate strips it), so never translated here."""
    return bool(entry.occurrences) and all(ARBITER_ONLY_SRC in occ[0] for occ in entry.occurrences)


def format_entry(pot_entry: "polib.POEntry", msgstr: str) -> str:
    """A gettext entry block to APPEND verbatim: the POT's source reference,
    then the msgid + the translated msgstr, each on one line (unwrapped — the
    committed catalogs' convention). Append-only keeps every existing line
    byte-for-byte, so the diff is the new translation, not a whole-file reflow."""
    refs = " ".join(f"{f}:{l}" if l else str(f) for f, l in pot_entry.occurrences)
    head = f"#: {refs}\n" if refs else ""
    return f'\n{head}msgid "{polib.escape(pot_entry.msgid)}"\nmsgstr "{polib.escape(msgstr)}"\n'


# A unique sentinel to delimit units in the request + response. Avoids the
# JSON-escaping fragility of multi-line Markdown with embedded quotes / code
# fences / backslashes (the same block-marker approach translate-ftl.py uses).
UNIT_RE = re.compile(r"^<<<GOAT-UNIT (\d+)>>>\s*$", re.MULTILINE)


def anthropic_translate(lang_code: str, sources: "list[str]", model: str) -> "list[str]":
    """Translate the given English strings into lang_code, 1:1 in order. Returns
    the translated strings. Uses sentinel-delimited blocks (not JSON) so
    multi-line Markdown survives without escaping ambiguity."""
    api_key = os.environ.get("ANTHROPIC_API_KEY", "")
    lang_name = LANG_NAMES.get(lang_code, lang_code)
    system = (
        "You translate the documentation strings of `goat`, an operator "
        "command-line tool for a tactical data fabric, from ENGLISH into "
        f"{lang_name} ({lang_code}). The input is a sequence of units, each "
        "introduced by a line `<<<GOAT-UNIT n>>>`. Output the translation of "
        "each unit, each introduced by the SAME `<<<GOAT-UNIT n>>>` marker line, "
        "in the same order, and NOTHING else — no commentary, no code fence "
        "around the whole reply.\n\n"
        "ABSOLUTE RULES — these are contract, reproduce them byte-for-byte:\n"
        "1. Anything inside backticks `like this` — commands, paths, flags, "
        "error codes (`E_NOT_ADMIN`), filenames — verbatim, untranslated.\n"
        "2. Markdown link targets: in [text](target) translate only the text, "
        "keep (target) identical (e.g. (everyday.md), (reference.md)).\n"
        "3. Keep Markdown emphasis markers (**bold**, _italic_) around the same "
        "spans, and keep fenced code blocks (```...```) and their command lines "
        "verbatim — you may translate a trailing `# comment` after a command, "
        "but never the command itself.\n"
        "4. Preserve the leading/trailing whitespace and newline structure of "
        "each unit.\n\n"
        "Tone: terse, concrete, operator-facing — match the English register."
    )
    user = "".join(f"<<<GOAT-UNIT {i}>>>\n{s}\n" for i, s in enumerate(sources))
    body = json.dumps({
        "model": model,
        "max_tokens": 16384,
        "system": system,
        "messages": [{"role": "user", "content": user}],
    }).encode("utf-8")
    req = urllib.request.Request(API_URL, data=body, method="POST", headers={
        "x-api-key": api_key,
        "anthropic-version": "2023-06-01",
        "content-type": "application/json",
    })
    payload = None
    last_err = ""
    for attempt in range(1, 4):
        try:
            with urllib.request.urlopen(req, timeout=180) as resp:
                payload = json.loads(resp.read().decode("utf-8"))
            break
        except urllib.error.HTTPError as e:
            detail = e.read().decode("utf-8", "replace")[:300]
            if e.code not in (429, 500, 502, 503, 529):
                raise RuntimeError(f"Anthropic API {e.code}: {detail}")
            last_err = f"{e.code}: {detail}"
        except (urllib.error.URLError, TimeoutError) as e:
            last_err = str(e)
        if attempt < 3:
            time.sleep(2 * attempt)
    if payload is None:
        raise RuntimeError(f"3 attempts failed: {last_err}")

    text = "".join(part.get("text", "") for part in payload.get("content", []))
    # Split on the unit markers; map index -> translated body (verbatim between
    # this marker and the next). rstrip the single trailing newline we added.
    parts = UNIT_RE.split(text)
    # re.split with one capture group yields: [pre, idx0, body0, idx1, body1, ...]
    out: dict[int, str] = {}
    for i in range(1, len(parts) - 1, 2):
        idx = int(parts[i])
        body = parts[i + 1]
        # The marker line is `<<<GOAT-UNIT n>>>\n`; re.split leaves the trailing
        # `\n` on the body. Strip exactly that separator newline (leading) and
        # the one we appended after each source (trailing) so the msgstr matches
        # the msgid's newline shape (msgfmt enforces both-or-neither leading \n).
        if body.startswith("\n"):
            body = body[1:]
        if body.endswith("\n"):
            body = body[:-1]
        out[idx] = body
    missing = [i for i in range(len(sources)) if i not in out]
    if missing:
        raise RuntimeError(f"model omitted unit(s) {missing} of {len(sources)}")
    return [out[i] for i in range(len(sources))]


def locale_files(only: "list[str] | None") -> "list[Path]":
    pos = sorted(PO_DIR.glob("*.po"))
    if only:
        names = {p.stem for p in pos}
        unknown = [c for c in only if c not in names]
        if unknown:
            sys.exit(f"FAIL: unknown locale(s): {', '.join(unknown)}")
        pos = [p for p in pos if p.stem in only]
    return pos


def main() -> int:
    ap = argparse.ArgumentParser(description="Claude-assisted .po book translator (goat docs).")
    mode = ap.add_mutually_exclusive_group(required=True)
    mode.add_argument("--check", action="store_true", help="report drift, no API, exit non-zero on drift")
    mode.add_argument("--apply", action="store_true", help="translate untranslated+fuzzy entries via Claude")
    ap.add_argument("--lang", action="append", help="restrict to locale code (repeatable)")
    ap.add_argument("--model", default=DEFAULT_MODEL, help=f"model id (default {DEFAULT_MODEL})")
    args = ap.parse_args()

    if args.apply and not os.environ.get("ANTHROPIC_API_KEY"):
        sys.exit("FAIL: --apply needs ANTHROPIC_API_KEY in the environment.")
    if not POT.exists():
        sys.exit(f"FAIL: {POT} missing — regenerate it (see this file's header).")

    pos = locale_files(args.lang)
    if not pos:
        sys.exit("FAIL: no po/*.po files found.")

    # The current source strings (the arbiter), POT order, minus release-notes.
    pot = polib.pofile(str(POT))
    pot_translatable = [e for e in pot if e.msgid and not release_notes_only(e)]

    total_drift = 0
    applied = 0
    rejected = 0
    failed: list[str] = []

    for po_path in pos:
        code = po_path.stem
        # Append-only: a locale is "missing" exactly the POT msgids it doesn't
        # already carry (new chapter strings). We translate and APPEND those,
        # leaving every existing line byte-for-byte — so the diff is the new
        # translation, not a whole-file `msgmerge` reflow. The release-time full
        # msgmerge canonicalizes ordering/locations + drops any obsolete entry;
        # gettext accepts entries in any order, so an appended block is valid and
        # the currency gate (which msgmerges into its own temp) stays green.
        po = polib.pofile(str(po_path))
        existing = {e.msgid for e in po}
        todo = [e for e in pot_translatable if e.msgid not in existing]
        if not todo:
            continue
        total_drift += len(todo)
        print(f"{code}: {len(todo)} to fill")

        if not args.apply:
            continue

        sources = [e.msgid for e in todo]
        try:
            translations = anthropic_translate(code, sources, args.model)
        except RuntimeError as e:
            print(f"  ! {code}: API error — {e}; skipping (re-run --apply to retry)")
            failed.append(code)
            continue

        blocks = []
        for entry, tr in zip(todo, translations):
            if backticks(entry.msgid) != backticks(tr) or link_targets(entry.msgid) != link_targets(tr):
                print(f"  ! {code}: contract-token mismatch on \"{entry.msgid[:50]}…\" — refused")
                rejected += 1
                continue
            blocks.append(format_entry(entry, tr))
            applied += 1
        if blocks:
            with po_path.open("a", encoding="utf-8") as fh:
                fh.write("".join(blocks))
            print(f"  + {code}: appended {len(blocks)} entr{'y' if len(blocks) == 1 else 'ies'}")

    if args.check:
        if total_drift:
            print(f"\nDRIFT: {total_drift} untranslated/fuzzy entr{'y' if total_drift == 1 else 'ies'} "
                  f"across {len(pos)} locale(s).")
            print("Fill them: ops/scripts/translate-po.py --apply  (needs ANTHROPIC_API_KEY)")
            return 1
        print(f"OK: all {len(pos)} locale po files cover the English source.")
        return 0

    print(f"\napplied {applied}, refused {rejected}, locales-failed {len(failed)}.")
    if failed:
        print(f"API failures (re-run --apply to retry): {', '.join(failed)}")
    if rejected:
        print("Some entries were refused on contract-token mismatch — re-run or fix by hand.")
    return 1 if (rejected or failed) else 0


if __name__ == "__main__":
    raise SystemExit(main())
