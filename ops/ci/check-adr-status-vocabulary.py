#!/usr/bin/env python3
"""Every ADR's status must lead with an ADR-0002 token.

WHAT THIS GUARDS
----------------
ADR 0002 fixed the vocabulary on 2026-04-25 — Proposed, Accepted, Superseded,
Deprecated, transitions one-way. Nothing enforced it, and the corpus drifted in
a specific and consistent direction: ADRs began recording BUILD STATE in the
DECISION STATUS field. "Built, partially adopted", "Phase 1 built in code",
"Plans only — deliberately not built". Each of those is a true and useful
sentence about implementation, and none of them is a lifecycle state.

That conflation is not cosmetic. `check-adr-status-vs-code.sh` exists to catch
an ADR whose status UNDER-claims what is built; it can only do that if the
status field means "is this decision adopted". When the same field also carries
"is the code shipped", the two questions collide and neither is answerable.

THE RULE
--------
The status field must LEAD with one of the four tokens. Everything after it is
free prose and this guard does not read it. So this passes:

    Accepted — **built, partially adopted**; status corrected 2026-08-31...

and this fails:

    **Built, partially adopted** — status corrected 2026-08-31...

The information is identical. Only the first word changed, and it changed from
something no machine can rank into something ADR 0002 defines.

WHY IT DOES NOT INFER
---------------------
A status that does not lead with a token fails. It is never guessed at. An ADR
reading "Phase 1 deployed" is probably Accepted, and "probably" is how a
decision log ends up asserting a state nobody ratified.

BASELINE
--------
`ops/ci/adr-status-baseline-vocabulary.txt` holds the known non-conforming ADRs,
shrink-only — the same pattern as check-adr-status-vs-code.sh. A NEW offender
fails on its own PR. Fixing a baselined one makes its line stale and the guard
fails asking for the line to be removed, so the set only ever shrinks.

SELF-TEST (doctrine P6)
-----------------------
`--self-test` plants a violation and requires this guard to go red on it before
its verdict on the real tree is trusted. A guard that has never been seen to
fail is a guard nobody has tested.

Usage:
    python3 ops/ci/check-adr-status-vocabulary.py [docs/adr]
    python3 ops/ci/check-adr-status-vocabulary.py --self-test
"""

from __future__ import annotations

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from adr_status import CANON, adr_files, parse_file, parse_text  # noqa: E402

BASELINE = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                        "adr-status-baseline-vocabulary.txt")


def read_baseline() -> set[str]:
    if not os.path.exists(BASELINE):
        return set()
    out = set()
    with open(BASELINE, encoding="utf-8") as fh:
        for line in fh:
            line = line.split("#", 1)[0].strip()
            if line:
                out.add(line)
    return out


def self_test() -> int:
    """Plant a violation; require a red verdict on it."""
    planted = "\n".join([
        "# ADR 9999 — a planted violation",
        "",
        "| Field | Value |",
        "|---|---|",
        "| Status | **Built, shipped, and running** — not a lifecycle state |",
        "",
    ])
    st = parse_text(planted, "docs/adr/9999-planted.md")
    if st.token is not None:
        print("SELF-TEST FAILED: planted violation parsed as "
              f"token={st.token!r} — the guard would have passed it.")
        return 1

    # And the mirror case: a conforming status must NOT be flagged.
    ok = parse_text(planted.replace("**Built, shipped, and running**",
                                    "Accepted — **built, shipped, and running**"),
                    "docs/adr/9999-planted.md")
    if ok.token != "Accepted":
        print("SELF-TEST FAILED: a conforming status did not parse "
              f"(got token={ok.token!r}).")
        return 1

    print("self-test: guard goes red on a planted violation and green on its "
          "corrected twin.")
    return 0


def main(argv: list[str]) -> int:
    if "--self-test" in argv:
        return self_test()

    args = [a for a in argv[1:] if not a.startswith("--")]
    root = args[0] if args else "docs/adr"
    if not os.path.isdir(root):
        print(f"not a directory: {root}", file=sys.stderr)
        return 2

    baseline = read_baseline()
    offenders: list[tuple[str, str]] = []
    fixed: list[str] = []

    for path in adr_files(root):
        st = parse_file(path)
        name = os.path.basename(path)

        if st.form is None:
            offenders.append((name, "no Status field in any known header form"))
            continue

        if st.ok:
            if name in baseline:
                fixed.append(name)
            continue

        if name not in baseline:
            offenders.append((name, st.raw[:110]))

    rc = 0

    if offenders:
        rc = 1
        print("ADR status does not lead with an ADR-0002 token "
              f"({', '.join(CANON)}):\n")
        for name, detail in offenders:
            print(f"  {name}")
            print(f"      {detail}\n")
        print("Put the token first and keep the sentence after it:")
        print('  BAD   | Status | **Built, partially adopted** — corrected 2026-08-31 |')
        print('  GOOD  | Status | Accepted — **built, partially adopted**; corrected 2026-08-31 |')
        print("\nSee ADR 0002 for the four statuses and their transitions.")

    if fixed:
        rc = 1
        print("\nThese ADRs now conform but are still listed in the baseline.")
        print(f"The baseline is shrink-only — remove them from {BASELINE}:\n")
        for name in fixed:
            print(f"  {name}")

    if rc == 0:
        total = len(adr_files(root))
        print(f"ADR status vocabulary: {total - len(baseline)} of {total} "
              f"lead with an ADR-0002 token; {len(baseline)} baselined.")
    return rc


if __name__ == "__main__":
    sys.exit(main(sys.argv))
