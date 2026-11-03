#!/usr/bin/env python3
"""Read an ADR's status as a token plus a note.

WHY THIS EXISTS
---------------
ADR 0002 fixed the vocabulary on 2026-04-25: an ADR is Proposed, Accepted,
Superseded or Deprecated, and transitions are one-way. The corpus drifted from
that in two directions at once and nothing was watching either:

  1. FOUR HEADER FORMATS. The field is never missing, but it is written as a
     markdown table row (the 0001 template), a `**Status:**` prose line, a
     `- **Status:**` bullet (the newest ADRs, 1123 onward), or YAML
     frontmatter. Any single-format reader silently sees a subset — which is
     how an earlier audit concluded the field was "missing" from 105 files.

  2. FREE-TEXT VALUES. Many statuses carry a genuinely useful sentence:
     "Accepted (target); see Implementation status below — Trivy/grype/osquery
     not yet in CI". That prose is not noise and must not be deleted. It is
     simply not queryable, because the token and the caveat share one field.

This module separates the two. The leading canon word is the TOKEN; everything
after it is the NOTE, preserved verbatim. Nothing is rewritten and no
information is lost — a status becomes machine-readable without becoming less
informative.

WHAT THIS DOES NOT DO
---------------------
It does not guess. A status whose leading word is not one of the four canon
values returns token=None, and `check-adr-status-vocabulary.py` fails on it.
Inferring "probably Accepted" from prose is exactly the move that lets a
decision log claim a state nobody ratified.

Usage as a library:
    from adr_status import parse_file, CANON
    st = parse_file("docs/adr/1141-four-node-host-site-baseline.md")
    st.token  # "Accepted"
    st.note   # "" or the caveat prose
    st.form   # "table" | "boldline" | "bullet" | "frontmatter"

Usage as a CLI:
    python3 ops/ci/adr_status.py docs/adr/            # TSV: number, token, form
    python3 ops/ci/adr_status.py docs/adr/ --json     # full records
    python3 ops/ci/adr_status.py docs/adr/ --counts   # token histogram

    python3 ops/ci/adr_status.py --token Superseded
        What has been replaced.

    python3 ops/ci/adr_status.py --token Accepted --qualified
        Accepted decisions whose status carries prose after the token. Read
        this as a shortlist to skim, NOT as "accepted but not built" — the
        note is often just a ratification date. Separating a date from a
        caveat needs a reader, and this tool deliberately does not guess.
"""

from __future__ import annotations

import json
import os
import re
import sys
from dataclasses import dataclass, asdict

# The four statuses of ADR 0002. This tuple is the vocabulary; adding a value
# here is an amendment to ADR 0002 and belongs in that ADR's PR, not this one.
CANON = ("Proposed", "Accepted", "Superseded", "Deprecated")

# Files under docs/adr/ that are not ADRs and carry no status.
NON_ADR = {"INDEX.md", "GLOSSARY.md", "README.md"}

# Header forms, in the order we try them. Each returns the raw status value.
#
# The bullet form must be tried BEFORE the bold-line form: `- **Status:** X`
# also matches a naive `**Status:**` search, and reporting the wrong form makes
# the migration diff misleading.
_TABLE = re.compile(r"^\|\s*Status\s*\|\s*(?P<v>.*?)\s*\|\s*$", re.M)
_BULLET = re.compile(r"^[-*]\s+\*\*Status:?\*\*:?\s*(?P<v>.*?)\s*$", re.M)
_BOLDLINE = re.compile(r"^\*\*Status:?\*\*:?\s*(?P<v>.*?)\s*$", re.M)
_FRONTMATTER = re.compile(r"\A---\s*$.*?^status:\s*(?P<v>.*?)\s*$", re.M | re.S)

_FORMS = (("table", _TABLE), ("bullet", _BULLET),
          ("boldline", _BOLDLINE), ("frontmatter", _FRONTMATTER))

# A canon word at the head of the value, optionally wrapped in markdown
# emphasis, followed by end-of-value or a separator.
#
# The emphasis markers are matched HERE rather than stripped from the ends of
# the whole value, because the common shape emphasises only the token:
# `**Accepted** (operator-ratified)`. Stripping outer markers
# leaves a stray `**` mid-string and the match fails — which is how four
# correctly-written ADRs first read as unparsed.
#
# The trailing group must allow a separator OR end-of-value. `\b` alone is
# wrong in the other direction: it would let "Proposedly" match "Proposed".
_LEAD = re.compile(
    r"^[*_`]*\s*(?P<tok>" + "|".join(CANON) + r")[*_`]*"
    r"(?P<rest>$|[\s,;:.()\[\]—–-].*)",
    re.I | re.S,
)


@dataclass
class Status:
    path: str
    number: str          # "1141", or "" for a non-ADR file
    form: str | None     # which header format carried it
    raw: str             # the field exactly as written
    token: str | None    # one of CANON, or None if it does not normalise
    note: str            # everything after the token, verbatim

    @property
    def ok(self) -> bool:
        return self.token is not None


def _number(path: str) -> str:
    m = re.match(r"(\d{4})-", os.path.basename(path))
    return m.group(1) if m else ""


def parse_text(text: str, path: str = "") -> Status:
    """Extract the status from an ADR's source. Never raises."""
    raw, form = "", None
    for name, pat in _FORMS:
        m = pat.search(text)
        if m:
            raw, form = m.group("v").strip(), name
            break

    token, note = None, ""
    if raw:
        m = _LEAD.match(raw)
        if m:
            # Title-case the canon spelling so `accepted.` and `Accepted`
            # collapse to one token.
            token = next(c for c in CANON if c.lower() == m.group("tok").lower())
            note = m.group("rest").strip().lstrip(",;:—–- ").strip()

    return Status(path=path, number=_number(path), form=form,
                  raw=raw, token=token, note=note)


def parse_file(path: str) -> Status:
    with open(path, encoding="utf-8") as fh:
        return parse_text(fh.read(), path)


def adr_files(root: str) -> list[str]:
    """Every numbered ADR under `root`, sorted. Index/glossary excluded."""
    out = []
    for name in sorted(os.listdir(root)):
        if not name.endswith(".md") or name in NON_ADR:
            continue
        if not re.match(r"\d{4}-", name):
            continue
        out.append(os.path.join(root, name))
    return out


def main(argv: list[str]) -> int:
    argv_rest = argv[1:]

    # --token <value> consumes the next argument.
    want_token = None
    if "--token" in argv_rest:
        i = argv_rest.index("--token")
        if i + 1 >= len(argv_rest):
            print("adr_status: --token needs a value", file=sys.stderr)
            return 2
        want_token = argv_rest[i + 1]
        del argv_rest[i:i + 2]

    args = [a for a in argv_rest if not a.startswith("--")]
    flags = {a for a in argv_rest if a.startswith("--")}
    root = args[0] if args else "docs/adr"

    if not os.path.isdir(root):
        print(f"adr_status: not a directory: {root}", file=sys.stderr)
        return 2

    records = [parse_file(p) for p in adr_files(root)]

    if want_token:
        match = next((c for c in CANON if c.lower() == want_token.lower()), None)
        if match is None:
            print(f"adr_status: --token must be one of {', '.join(CANON)} "
                  f"(got {want_token!r})", file=sys.stderr)
            return 2
        records = [r for r in records if r.token == match]

    # A "qualified" status is one carrying prose after the token — the decision
    # is in that state WITH a caveat. That is the set worth reading.
    if "--qualified" in flags:
        records = [r for r in records if r.note]

    if "--json" in flags:
        print(json.dumps([asdict(r) for r in records], indent=2))
        return 0

    if "--counts" in flags:
        counts: dict[str, int] = {}
        forms: dict[str, int] = {}
        for r in records:
            counts[r.token or "(unparsed)"] = counts.get(r.token or "(unparsed)", 0) + 1
            forms[r.form or "(none)"] = forms.get(r.form or "(none)", 0) + 1
        print(f"{len(records)} numbered ADRs\n")
        print("token:")
        for k, v in sorted(counts.items(), key=lambda kv: -kv[1]):
            print(f"  {v:>4}  {k}")
        print("\nheader form:")
        for k, v in sorted(forms.items(), key=lambda kv: -kv[1]):
            print(f"  {v:>4}  {k}")
        return 0

    for r in records:
        print(f"{r.number}\t{r.token or '-'}\t{r.form or '-'}\t{r.note[:60]}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
