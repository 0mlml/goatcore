#!/usr/bin/env python3
"""check-offboardable-status-parity.py — the console's offboard-eligible set and
goat-cli's copy of it must be the same set.

WHY (#579): `onboardstore.OffboardableStatuses` (Go) decides which proposal rows
the console will offboard. `goat admin onboard --revoke` carries its own copy as
a Rust constant, because goat-cli ships to operators independently of the
per-goatnet console deploys (ADR 1083) and cannot import the Go set. Comments on
both sides name each other, which helps a reader and stops nothing: a state
added on one side only means the CLI silently skips rows the console considers
eligible, or posts offboards the console refuses.

One fact, two homes, and the duplication cannot be removed. So it is guarded.

HOW IT READS: entries, not tokens. Comments are stripped from both blocks
first, then each comma-separated entry must be something this script can
resolve: on the Go side a `Proposal\\w+` constant with a string value in the
file, on the Rust side a string literal. Anything else (a bare literal in the
Go slice, a name with no value, a non-literal Rust element) is Unreadable and
exits 2, never 0. A token scan let a constant with a digit or underscore in
its name slip past as parity, and a comment naming a constant raised a false
alarm; both are in the self-test now.

WHAT THIS DOES NOT COVER: version skew at RUNTIME, a current CLI pointed at a
goatnet whose console has not taken the newer set yet. That is the second half
of #579 and is handled in the CLI's 409 arm, not here.

Usage:
  check-offboardable-status-parity.py [<proposals.go> <admin.rs>]
  check-offboardable-status-parity.py --self-test

Exit: 0 the sets match, 1 they differ, 2 a side could not be read.
Rollback: rm this file and its step in .github/workflows/repo-guards.yml.
"""
from __future__ import annotations

import re
import sys
import tempfile
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
GO_FILE = REPO_ROOT / "ops/goat-id-console/internal/onboardstore/proposals.go"
RUST_FILE = REPO_ROOT / "crates/goat-cli/src/cmd/admin.rs"
RUST_CONST = "CONSOLE_OFFBOARDABLE_STATUSES"

COMMENT = re.compile(r"//[^\n]*|/\*.*?\*/", re.S)
GO_NAME = re.compile(r"Proposal\w+")
STRING_LITERAL = re.compile(r'"([^"]*)"')


class Unreadable(Exception):
    """A side of the pair could not be parsed. Never a pass."""


def strip_comments(text: str) -> str:
    return COMMENT.sub("", text)


def entries(block: str) -> list[str]:
    """Comma-separated entries of a Go slice or Rust array body, comments and
    the trailing comma's empty tail removed."""
    return [e.strip() for e in strip_comments(block).split(",") if e.strip()]


def go_statuses(text: str) -> set[str]:
    """The string values named by `var OffboardableStatuses = []string{...}`."""
    block = re.search(r"var\s+OffboardableStatuses\s*=\s*\[\]string\{(.*?)\}", text, re.S)
    if not block:
        raise Unreadable("no `var OffboardableStatuses = []string{...}` in the Go file")
    items = entries(block.group(1))
    if not items:
        raise Unreadable("OffboardableStatuses holds no entry")
    clean = strip_comments(text)
    values = set()
    for item in items:
        if not GO_NAME.fullmatch(item):
            raise Unreadable(
                f"OffboardableStatuses entry {item!r} is not a Proposal* constant; "
                "the set is named constants only, so a reader can find each state"
            )
        const = re.search(rf"\b{re.escape(item)}\s*=\s*\"([^\"]*)\"", clean)
        if not const:
            raise Unreadable(f"Go constant {item} has no string value in the file")
        values.add(const.group(1))
    return values


def rust_statuses(text: str) -> set[str]:
    """The string literals in `const CONSOLE_OFFBOARDABLE_STATUSES: [&str; N] = [...]`."""
    clean = strip_comments(text)
    block = re.search(rf"const\s+{RUST_CONST}\s*:[^=]*=\s*\[(.*?)\]\s*;", clean, re.S)
    if not block:
        raise Unreadable(f"no `const {RUST_CONST}` in the Rust file")
    items = entries(block.group(1))
    if not items:
        raise Unreadable(f"{RUST_CONST} holds no element")
    values = set()
    for item in items:
        lit = STRING_LITERAL.fullmatch(item)
        if not lit:
            raise Unreadable(f"{RUST_CONST} element {item!r} is not a string literal")
        values.add(lit.group(1))
    return values


def compare(go_text: str, rust_text: str) -> int:
    try:
        go, rust = go_statuses(go_text), rust_statuses(rust_text)
    except Unreadable as e:
        print(f"FAIL: could not read a side of the pair: {e}", file=sys.stderr)
        return 2
    if go == rust:
        print(f"ok: offboard-eligible statuses match on both sides: {sorted(go)}")
        return 0
    print("FAIL: the offboard-eligible status sets differ.", file=sys.stderr)
    if go - rust:
        print(f"  console only (the CLI will skip these rows): {sorted(go - rust)}", file=sys.stderr)
    if rust - go:
        print(f"  CLI only (the console will refuse these offboards): {sorted(rust - go)}", file=sys.stderr)
    print(f"  Edit {RUST_CONST} in crates/goat-cli/src/cmd/admin.rs and", file=sys.stderr)
    print("  OffboardableStatuses in onboardstore/proposals.go together.", file=sys.stderr)
    return 1


GO_FIXTURE = """
const (
	ProposalOnboarded = "onboarded"
	ProposalEnrolled = "enrolled"
	ProposalArchived = "archived"
)
var OffboardableStatuses = []string{
	ProposalOnboarded,
	ProposalEnrolled,
	ProposalArchived,
}
"""
# The two shapes the token scan missed: a digit and an underscore in the name.
GO_DIGIT = GO_FIXTURE.replace(
    '\tProposalArchived = "archived"\n', '\tProposalArchived = "archived"\n\tProposalHeld2 = "held2"\n'
).replace("\tProposalArchived,\n", "\tProposalArchived,\n\tProposalHeld2,\n")
GO_UNDERSCORE = GO_FIXTURE.replace(
    '\tProposalArchived = "archived"\n', '\tProposalArchived = "archived"\n\tProposal_Held = "held"\n'
).replace("\tProposalArchived,\n", "\tProposalArchived,\n\tProposal_Held,\n")
# A comment inside the slice that names a constant and quotes a word.
GO_COMMENTED = GO_FIXTURE.replace(
    "\tProposalArchived,\n",
    "\tProposalArchived,\n\t// ProposalRejected (\"rejected\") stays out on purpose.\n\t/* ProposalHeld too */\n",
)
GO_BARE_LITERAL = GO_FIXTURE.replace("\tProposalArchived,\n", '\tProposalArchived,\n\t"suspended",\n')
GO_UNRESOLVED_NAME = GO_FIXTURE.replace("\tProposalArchived,\n", "\tProposalArchived,\n\tProposalSuspended,\n")
GO_NO_VAR = GO_FIXTURE.replace("var OffboardableStatuses", "var Offboardable")

RUST_MATCHING = 'const CONSOLE_OFFBOARDABLE_STATUSES: [&str; 3] = ["onboarded", "enrolled", "archived"];'
RUST_WITH_HELD2 = 'const CONSOLE_OFFBOARDABLE_STATUSES: [&str; 4] = ["onboarded", "enrolled", "archived", "held2"];'
RUST_DRIFTED = 'const CONSOLE_OFFBOARDABLE_STATUSES: [&str; 2] = ["onboarded", "enrolled"];'
RUST_RENAMED = 'const SOMETHING_ELSE: [&str; 1] = ["onboarded"];'
RUST_COMMENTED = (
    "const CONSOLE_OFFBOARDABLE_STATUSES: [&str; 3] = [\n"
    '    "onboarded", // "rejected" is not here on purpose\n'
    '    "enrolled",\n'
    '    "archived",\n'
    "];"
)
RUST_NON_LITERAL = 'const CONSOLE_OFFBOARDABLE_STATUSES: [&str; 3] = ["onboarded", "enrolled", ARCHIVED];'


def self_test() -> int:
    cases = [
        ("matching sets pass", GO_FIXTURE, RUST_MATCHING, 0),
        ("a state missing on the CLI side is refused", GO_FIXTURE, RUST_DRIFTED, 1),
        ("a renamed Rust constant is unreadable, never a pass", GO_FIXTURE, RUST_RENAMED, 2),
        ("a Go name with a digit is seen: drift is refused", GO_DIGIT, RUST_MATCHING, 1),
        ("a Go name with a digit is seen: parity passes", GO_DIGIT, RUST_WITH_HELD2, 0),
        ("a Go name with an underscore is seen: drift is refused", GO_UNDERSCORE, RUST_MATCHING, 1),
        ("a comment inside the Go slice is not an entry", GO_COMMENTED, RUST_MATCHING, 0),
        ("a comment inside the Rust array is not an element", GO_FIXTURE, RUST_COMMENTED, 0),
        ("a bare literal in the Go slice is unreadable", GO_BARE_LITERAL, RUST_MATCHING, 2),
        ("a Go name with no value is unreadable", GO_UNRESOLVED_NAME, RUST_MATCHING, 2),
        ("a missing Go var is unreadable", GO_NO_VAR, RUST_MATCHING, 2),
        ("a non-literal Rust element is unreadable", GO_FIXTURE, RUST_NON_LITERAL, 2),
    ]
    ok = True
    for name, go_text, rust_text, want in cases:
        with tempfile.TemporaryFile("w+") as sink:
            stdout, stderr = sys.stdout, sys.stderr
            sys.stdout = sys.stderr = sink
            try:
                got = compare(go_text, rust_text)
            finally:
                sys.stdout, sys.stderr = stdout, stderr
        good = got == want
        ok = ok and good
        print(f"  {'ok  ' if good else 'FAIL'} {name}: exit {got}, want {want}")
    print(f"self-test: {'guard proven RED then GREEN' if ok else 'FAILURES ABOVE'}")
    return 0 if ok else 1


def main(argv: list[str]) -> int:
    if argv == ["--self-test"]:
        return self_test()
    if len(argv) not in (0, 2):
        print(__doc__, file=sys.stderr)
        return 2
    go_path, rust_path = (Path(argv[0]), Path(argv[1])) if argv else (GO_FILE, RUST_FILE)
    for p in (go_path, rust_path):
        if not p.is_file():
            print(f"FAIL: {p} does not exist", file=sys.stderr)
            return 2
    return compare(go_path.read_text(), rust_path.read_text())


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
