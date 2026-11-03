#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""check-cli-ui-parity.py — a new CLI verb cannot ship without a parity-matrix row.

THE FAILURE THIS EXISTS FOR. goat runs two operator surfaces — the `goat`
command line and the web UI (id-console + portal). They are meant to stay
complementary (ADR 1019): anything one surface can do, the other should, unless
we deliberately decide otherwise. In practice the CLI grows faster, and every
gap surfaced as its own bug report (goat#214, goat#229-231) until goat#239
audited the drift into docs/design/goat-cli-ui-parity.md. A matrix that is only
refreshed by hand rots the moment someone adds a verb and forgets the row — the
exact drift the audit closed reopens silently. This is the guard that notices.

WHAT IT CHECKS. First, read the matrix directly and require every ABSENT or
PARTIAL row to name an owning issue. This check is independent of the CLI
binary. Then build the CLI's own command inventory from the live clap tree
(`goat docs --markdown`, which already drops hidden commands — the easter eggs
and `_complete`) and confirm every operator-facing leaf command appears in the
matrix's verb column. A verb with no row at all fails the check.

WHAT IS LEFT OUT (matches the doc's own "What is left out" section):
  * hidden commands — already absent from `docs --markdown`;
  * `goat audit emit` — a host-side forwarding hook, not an operator action;
  * the whole `goat client` group — the goat-client desktop app is a different
    surface with its own contract, ruled out of scope by goat#239.

MATRIX NOTATION the parser understands (so it does not false-alarm on shorthand):
  * Markdown-table escaping — pipes are written `\\|`;
  * alternation — `admin queue ls|approve|reject` means three verbs;
  * glob suffix — `sponsor-*` covers `sponsor-approve` and `sponsor-reject`;
  * optional-arg brackets — `machine enroll[--bulk]` — the bracketed part is
    stripped before matching; optional subcommands need their own entries;
  * argument values — `admin pqc on|off` accounts for the leaf `admin pqc`.

Exit 0 when every gap has an owner and every operator-facing verb has a row.
Exit 1 on drift (the message names each unowned row or uncovered verb). Exit 75
when the inventory could not be produced (no goat binary, failed generator, or
empty/unrecognized output). Matrix ownership is checked before an unavailable
inventory returns 75.

Usage:
  ops/ci/check-cli-ui-parity.py                 # verdict, exit 1 on drift
  ops/ci/check-cli-ui-parity.py --report        # print every verb + coverage
  ops/ci/check-cli-ui-parity.py --goat-bin PATH # binary to enumerate (else $GOAT_BIN, else `goat`)
  ops/ci/check-cli-ui-parity.py --docs FILE     # read `docs --markdown` output from a file instead
"""
from __future__ import annotations

import argparse
import itertools
import os
import re
import subprocess
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
MATRIX = REPO_ROOT / "docs" / "design" / "goat-cli-ui-parity.md"

# Exit 75: "the question could not be asked" (EX_TEMPFAIL), same convention as
# the other parity checks — a warning, not a parity failure.
EX_CANT_ASK = 75

# Out of scope per goat#239 / the matrix "What is left out" section. A leaf whose
# path starts with one of these (space-delimited) is never expected to have a row.
SCOPE_SKIP = (
    "client",       # goat-client desktop app — different surface, different contract
    "audit emit",   # host-side forwarding hook, not an operator action
)

GAP_STATUS_RE = re.compile(r"\b(?:ABSENT|PARTIAL)\b")
ISSUE_REF_RE = re.compile(r"(?<![A-Za-z0-9_])(?:[Gg]oat)?#\d+\b")


def cli_leaf_commands(markdown: str) -> list[str]:
    """Leaf command paths from `goat docs --markdown` (## `goat ...` headings).

    A path is a leaf when it is not the prefix-group of another path: `goat admin`
    is a group, `goat admin members move` is a leaf.
    """
    paths = set()
    root_seen = False
    for line in markdown.splitlines():
        m = re.match(r"^## `goat(?: (.+))?`\s*$", line)
        if m:
            if m.group(1) is None:
                root_seen = True
            elif m.group(1).strip():
                paths.add(m.group(1).strip())
    if not root_seen or not paths:
        raise ValueError("expected a `goat` root heading and command headings from docs --markdown")
    return sorted(p for p in paths if not any(o.startswith(p + " ") for o in paths))


def matrix_rows(text: str):
    """Yield row dictionaries from Markdown tables with a header/separator.

    Escaped pipes belong to a cell. Prose and fenced examples are ignored.
    """
    header = None
    columns = None
    fence = None
    for line in text.splitlines():
        row = line.strip()
        if row.startswith(("```", "~~~")):
            marker = row[:3]
            if fence is None:
                fence = marker
            elif fence == marker:
                fence = None
            header = None
            columns = None
            continue
        if fence is not None or not row.startswith("|"):
            header = None
            columns = None
            continue
        cells = [cell.strip() for cell in re.split(r"(?<!\\)\|", row)[1:]]
        if cells and not cells[-1]:
            cells.pop()
        if cells and all(re.fullmatch(r":?-{3,}:?", cell) for cell in cells):
            columns = header if header and len(header) == len(cells) else None
        elif columns is not None:
            yield dict(zip(columns, cells))
        else:
            header = [cell.lower() for cell in cells]


def matrix_verb_cells(text: str):
    """Yield only Verb-column cells from parsed Markdown table rows."""
    for row in matrix_rows(text):
        if "verb" in row:
            yield row["verb"]


def unowned_gap_rows(text: str) -> list[tuple[str, str]]:
    """Return (verb, status) pairs for ABSENT/PARTIAL rows with no issue ref."""
    missing = []
    for row in matrix_rows(text):
        if "verb" not in row or "status" not in row:
            continue
        status = row["status"]
        if GAP_STATUS_RE.search(status) and not ISSUE_REF_RE.search(" ".join(row.values())):
            missing.append((row["verb"], status))
    return missing


def matrix_coverage(text: str) -> tuple[set[str], list[str]]:
    """Parse Verb-column entries into (exact command strings, glob patterns).

    Markdown pipe escaping is stripped, `[...]` optional-arg spans are dropped,
    and `a|b` alternation is expanded across every word position. A trailing `*`
    matches the rest of its command token, never descendant commands.
    """
    exact: set[str] = set()
    globs: list[str] = []
    for raw in itertools.chain.from_iterable(
        re.findall(r"`([^`]+)`", cell) for cell in matrix_verb_cells(text)
    ):
        token = raw.replace("\\|", "|")          # markdown-escaped pipes: \| -> |
        token = re.sub(r"\[[^\]]*\]", "", token)  # drop optional-arg brackets [..]
        token = re.sub(r"<[^>]*>", "", token)     # drop positional placeholders <..>
        token = re.sub(r"\s+", " ", token).strip()
        if not token:
            continue
        # Expand alternation in any word position: "a b|c" -> {"a b", "a c"}.
        opts = [w.split("|") for w in token.split(" ")]
        for combo in itertools.product(*opts):
            cmd = " ".join(w for w in combo if w)
            if not cmd:
                continue
            if cmd.endswith("*"):
                globs.append(cmd)
            else:
                exact.add(cmd)
    return exact, globs


def covered(leaf: str, exact: set[str], globs: list[str]) -> bool:
    """Match a complete leaf or an explicit wildcard in its final command token.

    A row for an ancestor never documents a new descendant. Conversely, a row
    may append argument values to a known leaf (`admin pqc on|off`): inventory
    headings contain command paths, not arguments, so that still covers `admin pqc`.
    """
    return (
        leaf in exact
        or any(re.fullmatch(re.escape(pattern[:-1]) + r"\S*", leaf) for pattern in globs)
        or any(cmd.startswith(leaf + " ") for cmd in exact)
    )


def in_scope(leaf: str) -> bool:
    return not any(leaf == s or leaf.startswith(s + " ") for s in SCOPE_SKIP)


def load_markdown(args) -> str | None:
    if args.docs:
        try:
            return Path(args.docs).read_text(encoding="utf-8")
        except (OSError, UnicodeError) as e:
            print(f"::warning::could not read command inventory: {e}", file=sys.stderr)
            return None
    goat = args.goat_bin or os.environ.get("GOAT_BIN") or "goat"
    try:
        out = subprocess.run(
            [goat, "docs", "--markdown"],
            capture_output=True, text=True, timeout=120,
        )
    except (OSError, UnicodeError, subprocess.TimeoutExpired) as e:
        print(f"::warning::could not run `{goat} docs --markdown`: {e}", file=sys.stderr)
        return None
    if out.returncode != 0:
        print(f"::warning::`{goat} docs --markdown` exited {out.returncode}: "
              f"{out.stderr.strip()[:200]}", file=sys.stderr)
        return None
    return out.stdout


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--goat-bin", help="goat binary to enumerate (else $GOAT_BIN, else `goat`)")
    ap.add_argument("--docs", help="read `docs --markdown` output from this file instead of running goat")
    ap.add_argument("--report", action="store_true", help="print every operator-facing verb and its coverage")
    args = ap.parse_args()

    try:
        matrix_text = MATRIX.read_text(encoding="utf-8")
    except FileNotFoundError:
        print(f"::error::parity matrix missing: {MATRIX.relative_to(REPO_ROOT)}", file=sys.stderr)
        return 1
    except (OSError, UnicodeError) as e:
        print(f"::error::could not read parity matrix: {e}", file=sys.stderr)
        return 1

    unowned = unowned_gap_rows(matrix_text)
    if unowned:
        print("::error::ABSENT or PARTIAL parity rows without an owning issue:", file=sys.stderr)
        for verb, status in unowned:
            print(f"  - {verb} ({status})", file=sys.stderr)
        print("\nName an owner in each row as goat#<number> or #<number>.", file=sys.stderr)
        return 1

    markdown = load_markdown(args)
    if markdown is None:
        print("cli-ui-parity: inventory unavailable — check did not run", file=sys.stderr)
        return EX_CANT_ASK

    try:
        leaves = [c for c in cli_leaf_commands(markdown) if in_scope(c)]
        if not leaves:
            raise ValueError("command inventory contains no operator-facing leaves")
    except ValueError as e:
        print(f"::warning::cli-ui-parity: inventory unavailable — {e}", file=sys.stderr)
        return EX_CANT_ASK

    exact, globs = matrix_coverage(matrix_text)
    missing = [c for c in leaves if not covered(c, exact, globs)]

    if args.report:
        for c in sorted(leaves):
            print(f"  {'ok ' if covered(c, exact, globs) else 'GAP'}  goat {c}")
        print(f"\n{len(leaves)} operator-facing verbs, {len(missing)} without a matrix row")

    if missing:
        print("::error::CLI verbs with no row in docs/design/goat-cli-ui-parity.md:", file=sys.stderr)
        for c in sorted(missing):
            print(f"  - goat {c}", file=sys.stderr)
        print("\nAdd a row for each (any verdict — UI/SIBLING/CLI-ONLY/ABSENT), "
              "per the matrix's maintenance rule. See goat#239.", file=sys.stderr)
        return 1

    print(f"cli-ui-parity: OK — all {len(leaves)} operator-facing verbs have a matrix row")
    return 0


if __name__ == "__main__":
    sys.exit(main())
