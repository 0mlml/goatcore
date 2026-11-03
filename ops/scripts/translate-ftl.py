#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""translate-ftl.py — Claude-assisted Fluent (.ftl) catalog translator for goat-cli.

Closes *binary-string drift*. English is the arbiter catalog
(`crates/goat-cli/i18n/en/goat_cli.ftl`, ADR 1024). When a release adds new
message-ids to English but not to the other locale catalogs, the binary silently
falls back to English for those strings — and because the docs command reference
is generated from the binary (`goat --lang <X> docs --markdown`), the hosted docs
show English inside otherwise-translated pages. The currency gate over the prose
`.po` files can't see this (the drift is in `.ftl`, not the book).

This tool finds the message-ids that exist in English but are missing from each
locale and, with --apply, fills them via the Anthropic Messages API — preserving
every contract token verbatim.

  --check          Report missing ids per locale; exit non-zero if any drift. No API.
  --apply          Translate the missing ids via Claude and write them back.
                   Needs ANTHROPIC_API_KEY in the environment.
  --lang <code>    Restrict to one locale (repeatable). Default: every non-en,
                   non-easter-egg catalog under i18n/.
  --model <id>     Model override (default: claude-sonnet-5-5).

Machine-surface invariant (ADR 1024): backticked `tokens`, `{ $var }`
interpolations, message-ids, and Fluent select-expression syntax are reproduced
verbatim — they are contract, not prose. Every translated entry is validated to
carry the *same* contract tokens as its English source; a mismatch is refused and
never written, so a bad translation fails loud rather than corrupting the catalog
or leaking a localized command into the machine surface.

Runs locally (operator's ANTHROPIC_API_KEY) or in CI (repo secret). Never shipped
in the binary — the air-gap invariant (no runtime translation) is unchanged.
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
from datetime import date
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
# Every Fluent catalog set the CLIs ship (docs/design/goat-cli-i18n.md
# §"CLI catalog currency"): name -> (i18n dir, file name in each locale dir).
# One tool, one gate, every catalog — goatbuilder and the shared error catalog
# use goat's render boundary, so they use its translator and its gate too.
CATALOGS = {
    "goat-cli": (REPO / "crates" / "goat-cli" / "i18n", "goat_cli.ftl"),
    "goatbuilder": (REPO / "crates" / "goatbuilder" / "i18n", "goatbuilder.ftl"),
    "goat-errors": (REPO / "crates" / "goat-errors" / "i18n", "errors.ftl"),
}
# Catalogs whose drift --check REPORTS without failing: all of them. A change
# that adds or edits English does not owe its translations: every binary falls
# back to English for a message a locale lacks, and `--apply` fills the gap when
# someone chooses to (docs/design/goat-cli-i18n.md, "The currency gate").
REPORT_ONLY: set[str] = set(CATALOGS)
# Set by main() from --catalog; module-level so the helpers keep their shape.
I18N_DIR = CATALOGS["goat-cli"][0]
FTL_NAME = CATALOGS["goat-cli"][1]
# Ids per API request: a whole locale's drift in one reply truncates at the
# output limit and silently drops the tail (the tool then reports "model
# returned no entry"); 25 keeps every reply well inside it.
CHUNK = 25
ARBITER = "en"
# tlh (Klingon) is a deliberately-partial easter-egg catalog (goat-cli-i18n.md);
# it is NOT held to English completeness, so it is excluded from drift + fill.
SKIP_LOCALES = {ARBITER, "tlh"}

# Message-ids that intentionally stay English in every locale, so they are NOT
# treated as drift and never auto-filled. The machine-translation caveat is the
# trust anchor — "this output is machine-translated, the English is authoritative"
# is itself most trustworthy in the authoritative language (its golden test
# documents this: "the meta-caveat is the authoritative English by default").
ARBITER_ONLY = {"i18n-machine-translation-warning"}

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

MSG_START = re.compile(r"^(-?[A-Za-z][A-Za-z0-9_-]*) *=")
DEFAULT_MODEL = "claude-sonnet-5-5"
API_URL = "https://api.anthropic.com/v1/messages"


def parse_messages(path: Path) -> "tuple[dict[str, list[str]], list[str]]":
    """Return (id -> full source-line block, ordered id list).

    A message is its `id =` line plus the indented continuation lines that
    follow (multiline values, attributes, select expressions). A non-indented
    line or a blank line ends the message — standard Fluent block structure.
    """
    lines = path.read_text(encoding="utf-8").splitlines()
    msgs: dict[str, list[str]] = {}
    order: list[str] = []
    i, n = 0, len(lines)
    while i < n:
        m = MSG_START.match(lines[i])
        if not m:
            i += 1
            continue
        mid = m.group(1)
        block = [lines[i]]
        i += 1
        while i < n:
            if lines[i][:1] in (" ", "\t") and lines[i].strip() != "":
                block.append(lines[i])
                i += 1
                continue
            # A blank line ends the message only when no indented line follows
            # it. Fluent allows blank lines inside a multiline value, so a
            # multi-paragraph value continues past them (stopping here used to
            # cut every multi-paragraph tutor lesson to its first paragraph).
            j = i
            while j < n and lines[j].strip() == "":
                j += 1
            if j < n and j > i and lines[j][:1] in (" ", "\t"):
                block.extend(lines[i:j])
                i = j
                continue
            break
        msgs[mid] = block
        order.append(mid)
    return msgs, order


def contract_tokens(block: list[str]) -> "tuple[set[str], set[str]]":
    """The verbatim-reproduce tokens in a message block: backticked spans and
    Fluent placeables ({ ... }). These must survive translation unchanged."""
    text = "\n".join(block)
    backticked = set(re.findall(r"`[^`]*`", text))
    placeables = set(re.findall(r"\{[^{}]*\}", text))
    return backticked, placeables


def missing_ids(en_order: list[str], en_msgs, lang_msgs) -> list[str]:
    return [mid for mid in en_order if mid not in lang_msgs and mid not in ARBITER_ONLY]


def locale_dirs(only: "list[str] | None") -> list[str]:
    found = sorted(
        p.name for p in I18N_DIR.iterdir()
        if p.is_dir() and (p / FTL_NAME).exists() and p.name not in SKIP_LOCALES
    )
    if only:
        unknown = [c for c in only if c not in found]
        if unknown:
            sys.exit(f"FAIL: unknown/empty locale(s): {', '.join(unknown)}")
        return [c for c in found if c in only]
    return found


def anthropic_translate(lang_code: str, en_blocks: "list[tuple[str, list[str]]]", model: str) -> dict[str, str]:
    """Translate the given English message blocks into lang_code. Returns
    id -> translated single-or-multi-line value text (without the `id = ` prefix
    for single-line; full block text for multiline)."""
    api_key = os.environ.get("ANTHROPIC_API_KEY", "")
    lang_name = LANG_NAMES.get(lang_code, lang_code)

    source = "\n\n".join("\n".join(block) for _, block in en_blocks)
    system = (
        "You translate Fluent (.ftl) message catalogs for `goat` and `goatbuilder`, the "
        "operator command-line tools for a tactical data fabric. Translate ENGLISH into "
        f"{lang_name} ({lang_code}).\n\n"
        "ABSOLUTE RULES — these are contract, not prose; reproduce them byte-for-byte:\n"
        "1. Message ids (the text before ` = `) — keep identical, never translate.\n"
        "2. Anything inside backticks `like this` — commands, paths, flags, code — verbatim.\n"
        "3. Fluent placeables { $var } / { ... } — verbatim, same names, same order.\n"
        "4. Fluent select-expression syntax ({ $n -> [one] ... *[other] ... }) — keep the "
        "structure and variant keys; translate only the human prose inside each variant.\n\n"
        "Tone: terse, concrete, operator-facing — match the English register, not formal prose.\n"
        "Output ONLY the translated entries, one per source entry, in the SAME `id = value` "
        "form and SAME order. No commentary, no code fences, no blank-line padding changes."
    )
    body = json.dumps({
        "model": model,
        "max_tokens": 16000,
        "system": system,
        "messages": [{"role": "user", "content": source}],
    }).encode("utf-8")
    req = urllib.request.Request(API_URL, data=body, method="POST", headers={
        "x-api-key": api_key,
        "anthropic-version": "2023-06-01",
        "content-type": "application/json",
    })
    # Retry transient failures (read timeouts, network blips, 429/5xx) with
    # backoff so one slow locale doesn't abort a 29-locale batch. A 4xx other
    # than 429 is a real request error — raise immediately, no retry.
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
        except urllib.error.URLError as e:
            last_err = str(e.reason)
        except TimeoutError as e:
            last_err = f"read timed out ({e})"
        if attempt < 3:
            time.sleep(2 * attempt)
    if payload is None:
        raise RuntimeError(f"3 attempts failed: {last_err}")

    text = "".join(part.get("text", "") for part in payload.get("content", []))
    # Re-parse the returned id = value blocks the same way we parse a catalog.
    out: dict[str, list[str]] = {}
    lines = text.splitlines()
    i, n = 0, len(lines)
    while i < n:
        m = MSG_START.match(lines[i])
        if not m:
            i += 1
            continue
        mid = m.group(1)
        block = [lines[i]]
        i += 1
        while i < n and lines[i][:1] in (" ", "\t") and lines[i].strip() != "":
            block.append(lines[i])
            i += 1
        out[mid] = block
    return out


def main() -> int:
    ap = argparse.ArgumentParser(description="Claude-assisted .ftl catalog translator (goat-cli).")
    mode = ap.add_mutually_exclusive_group(required=True)
    mode.add_argument("--check", action="store_true", help="report drift, no API, exit non-zero on drift")
    mode.add_argument("--apply", action="store_true", help="translate missing ids via Claude and write back")
    mode.add_argument("--self-test", action="store_true", help="exercise the parser, the contract-token check and drift detection on inline fixtures; no API")
    ap.add_argument("--lang", action="append", help="restrict to locale code (repeatable)")
    ap.add_argument("--model", default=DEFAULT_MODEL, help=f"model id (default {DEFAULT_MODEL})")
    ap.add_argument("--catalog", action="append", choices=sorted(CATALOGS) + ["all"],
                    help="which catalog set (repeatable; default goat-cli, the historical behaviour; 'all' = every set)")
    args = ap.parse_args()
    if args.self_test:
        return self_test()
    names = args.catalog or ["goat-cli"]
    if "all" in names:
        names = sorted(CATALOGS)
    rc = 0
    for name in names:
        global I18N_DIR, FTL_NAME
        I18N_DIR, FTL_NAME = CATALOGS[name]
        if not (I18N_DIR / ARBITER / FTL_NAME).exists():
            print(f"{name}: no catalog at {I18N_DIR / ARBITER / FTL_NAME} — skipped")
            continue
        print(f"== {name}")
        r = run_catalog(args)
        if r and args.check and name in REPORT_ONLY:
            print(f"(reported, not failing: {name} drift is filled by "
                  f"ops/scripts/translate-ftl.py --apply --catalog {name}; English renders meanwhile)")
            r = 0
        rc |= r
    return rc


def run_catalog(args) -> int:

    en_path = I18N_DIR / ARBITER / FTL_NAME
    if not en_path.exists():
        sys.exit(f"FAIL: arbiter catalog missing at {en_path}")
    en_msgs, en_order = parse_messages(en_path)

    if args.apply and not os.environ.get("ANTHROPIC_API_KEY"):
        sys.exit("FAIL: --apply needs ANTHROPIC_API_KEY in the environment.")

    locales = locale_dirs(args.lang)
    total_missing = 0
    applied = 0
    rejected = 0
    failed: list[str] = []

    for code in locales:
        lang_path = I18N_DIR / code / FTL_NAME
        lang_msgs, _ = parse_messages(lang_path)
        miss = missing_ids(en_order, en_msgs, lang_msgs)
        if not miss:
            continue
        total_missing += len(miss)
        print(f"{code}: {len(miss)} missing — {', '.join(miss)}")

        if not args.apply:
            continue

        en_blocks = [(mid, en_msgs[mid]) for mid in miss]
        translated: dict[str, list[str]] = {}
        api_failed = False
        for k in range(0, len(en_blocks), CHUNK):
            try:
                translated.update(anthropic_translate(code, en_blocks[k:k + CHUNK], args.model))
            except RuntimeError as e:
                # One chunk's API failure must not abort the batch — record and
                # move on. --apply is idempotent (fills only missing ids), so a
                # re-run picks up the stragglers.
                print(f"  ! {code}: API error — {e}; chunk skipped (re-run --apply to retry)")
                api_failed = True
        if api_failed:
            failed.append(code)

        good: list[list[str]] = []
        for mid in miss:
            if mid not in translated:
                print(f"  ! {code}/{mid}: model returned no entry — skipped")
                rejected += 1
                continue
            en_bt, en_pl = contract_tokens(en_msgs[mid])
            tr_bt, tr_pl = contract_tokens(translated[mid])
            if en_bt != tr_bt or en_pl != tr_pl:
                print(f"  ! {code}/{mid}: contract-token mismatch — refused (not written)")
                rejected += 1
                continue
            good.append(translated[mid])
            applied += 1

        if good:
            with lang_path.open("a", encoding="utf-8") as fh:
                fh.write(f"\n# machine-translated additions ({date.today().isoformat()}) — "
                         f"review per i18n-provenance.toml\n")
                for block in good:
                    fh.write("\n".join(block) + "\n")
            print(f"  + {code}: wrote {len(good)} entr{'y' if len(good)==1 else 'ies'}")

    if args.check:
        if total_missing:
            print(f"\nDRIFT: {total_missing} missing translation(s) across {len(locales)} locale(s).")
            print("Fill them: ops/scripts/translate-ftl.py --apply  (needs ANTHROPIC_API_KEY)")
            return 1
        print(f"OK: all {len(locales)} locale catalogs cover the English arbiter.")
        return 0

    # --apply summary
    print(f"\napplied {applied}, refused {rejected}, locales-failed {len(failed)}.")
    if failed:
        print(f"API failures (re-run --apply to retry): {', '.join(failed)}")
    if rejected:
        print("Some entries were refused on contract-token mismatch — re-run or fix by hand.")
    return 1 if (rejected or failed) else 0


def self_test() -> int:
    """Inline fixtures for the parser, the contract-token check and drift."""
    import tempfile
    global I18N_DIR, FTL_NAME
    ok = True
    def check(cond, what):
        nonlocal ok
        print(("ok    " if cond else "FAIL  ") + what)
        ok = ok and cond
    with tempfile.TemporaryDirectory() as d:
        root = Path(d)
        (root / "en").mkdir(); (root / "fr").mkdir(); (root / "tlh").mkdir()
        (root / "en" / "t.ftl").write_text(
            "a-one = run `goat doctor` for { $ctx }\n"
            "a-multi =\n    first line\n    second `x`\n"
            "a-para =\n    first paragraph\n\n    second paragraph `y`\n"
            "a-plural = { $n ->\n    [one] one host\n   *[other] { $n } hosts\n}\n"
            "i18n-machine-translation-warning = english only\n", encoding="utf-8")
        (root / "fr" / "t.ftl").write_text("a-one = lancez `goat doctor` pour { $ctx }\n", encoding="utf-8")
        (root / "tlh" / "t.ftl").write_text("a-one = Qapla'\n", encoding="utf-8")
        I18N_DIR, FTL_NAME = root, "t.ftl"
        en, order = parse_messages(root / "en" / "t.ftl")
        fr, _ = parse_messages(root / "fr" / "t.ftl")
        check(order == ["a-one", "a-multi", "a-para", "a-plural", "i18n-machine-translation-warning"], "parser keeps order and multiline blocks")
        check(len(en["a-para"]) == 4 and "`y`" in contract_tokens(en["a-para"])[0], "a blank line inside a value does not end it (multi-paragraph)")
        check(len(en["a-multi"]) == 3, "a multiline value is one block")
        check(missing_ids(order, en, fr) == ["a-multi", "a-para", "a-plural"], "drift = English ids missing from the locale, arbiter-only ids excluded")
        check(locale_dirs(None) == ["fr"], "the easter-egg locale is never held to completeness")
        en_bt, en_pl = contract_tokens(en["a-one"])
        good_bt, good_pl = contract_tokens(fr["a-one"])
        bad_bt, bad_pl = contract_tokens(["a-one = lancez `goat docteur` pour { $ctx }"])
        check((en_bt, en_pl) == (good_bt, good_pl), "a translation that keeps every token passes")
        check(en_bt != bad_bt, "a translated command is caught")
    print("self-test: " + ("ok" if ok else "FAILED"))
    return 0 if ok else 1


if __name__ == "__main__":
    raise SystemExit(main())
