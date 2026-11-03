#!/usr/bin/env python3
"""warrant — remove past-due rule intents and regenerate. ADR 1129 C3.

`expires:` on a rule intent is the entire expiry mechanism. No store, no state
machine, no lifecycle service: CI goes red when a grant is past due, and this
script — run on a schedule — opens the PR that removes it.

WHY REMOVAL IS SAFE TO AUTOMATE AND GRANTING NEVER IS. Deleting a rule can only
take access away, and `git revert` puts it back. That asymmetry is the same one
the narrowing prover rests on, and it is why the expiring half of this design
costs a field and a check rather than an approval product.

IT STILL OPENS A PR. The removal is reviewed like any other change — the point
is that the work of noticing is automated, not the decision. A scheduled job
that pushed to main would be a robot with more authority than the humans it
works for.

MEMBERSHIP ENTRIES TOO (docs/design/mesh-change-control.md §4.3). An entry in
ops/warrant/membership/<scope>/ (any fragment) whose `expires` is past is
listed and, with --apply, deleted from the fragment that holds it. Deleting a
grant-only entry takes access away; deleting a hold or an `exact` entry
RETURNS the peer to its baseline (declared, else the minting key's), which
is a widening — the PR this opens then waits for its owner like any other, and
the listing says which kind each one is.

Usage:
    expire.py --list                 # what is past due, exit 0 always
    expire.py --apply                # strip past-due intents + entries, regenerate
    expire.py --apply --today DATE   # pretend it is DATE (tests)
"""

from __future__ import annotations

import argparse
import datetime as dt
import re
import subprocess
import sys
from pathlib import Path
import importlib

try:
    importlib.import_module("yaml")  # Keep the dependency availability check.
except ImportError:  # pragma: no cover
    sys.exit("warrant: PyYAML is required (pip install pyyaml)")

HERE = Path(__file__).resolve().parent
REPO_ROOT = HERE.parents[1]

RE_RULE_START = re.compile(r"^(?P<indent>\s*)-\s+name:\s*(?P<name>.+?)\s*$")
RE_EXPIRES = re.compile(r"^\s*expires:\s*(?P<date>\S+)\s*$")
RE_OWNER = re.compile(r"^\s*owner:\s*(?P<owner>.+?)\s*$")


class Rule:
    def __init__(self, name: str, start: int, end: int, expires, owner):
        self.name = name
        self.start = start  # index of the `- name:` line
        self.end = end  # exclusive
        self.expires = expires
        self.owner = owner


def parse_date(raw: str):
    try:
        return dt.date.fromisoformat(str(raw).strip().strip('"').strip("'"))
    except ValueError:
        return None


def scan(lines: list[str]) -> list[Rule]:
    """Find every rule block and any expiry on it.

    Line-oriented on purpose: the intents file carries hand-written comment
    banners that a YAML round-trip would destroy, and preserving them is what
    keeps the generated matrix byte-identical.
    """
    starts: list[tuple[int, str, str]] = []
    for i, line in enumerate(lines):
        if line.lstrip().startswith("#"):
            continue
        m = RE_RULE_START.match(line)
        if m:
            starts.append((i, m.group("indent"), m.group("name")))

    rules: list[Rule] = []
    for idx, (start, indent, name) in enumerate(starts):
        end = starts[idx + 1][0] if idx + 1 < len(starts) else len(lines)
        # Trailing comment lines belong to the NEXT rule, not this one — they
        # are its banner. Give them back, or removing a rule silently deletes
        # the following rule's explanation.
        while end - 1 > start and (
            lines[end - 1].strip() == "" or lines[end - 1].lstrip().startswith("#")
        ):
            end -= 1
        expires = owner = None
        for line in lines[start:end]:
            m = RE_EXPIRES.match(line)
            if m:
                expires = m.group("date")
            m = RE_OWNER.match(line)
            if m:
                owner = m.group("owner")
        rules.append(Rule(name, start, end, expires, owner))
    return rules


def expire_membership(membership_dir: Path, today: dt.date, apply: bool) -> tuple[int, int]:
    """List (and with *apply*, delete) past-due membership entries.

    Walks every scope directory (ops/warrant/membership/<scope>/) and deletes
    each due entry from whichever fragment holds it. Returns (count due, exit
    code). Exit 2 when a file cannot be read."""
    sys.path.insert(0, str(HERE))
    import membership as mem

    due_total = 0
    for d in mem.scope_dirs(membership_dir):
        try:
            view = mem.load_scope(d)
        except mem.SourceError as exc:
            print(f"warrant: {exc}", file=sys.stderr)
            return due_total, 2
        due = mem.expired_entries(view, today)
        if not due:
            continue
        due_total += len(due)
        print(f"warrant: {len(due)} membership entr{'y' if len(due) == 1 else 'ies'} "
              f"past due in {d.name}/ as of {today.isoformat()}:")
        for e, when in due:
            kind = ("grant only — removal narrows" if mem.entry_mode(e) == "delta" and not e.get("absent")
                    else "RETURNS the peer to its baseline — removal widens, waits for its owner")
            print(f"  - {e.get('peer')}  expired {when.isoformat()}  owner: {e.get('owner') or '<none>'}  ({kind})")
        if apply:
            removed = mem.remove_scope_entries(d, {str(e.get("peer")) for e, _ in due})
            for f, peer in removed:
                print(f"  removed {peer} from {d.name}/{f.relative_to(d).as_posix()}")
    return due_total, 0


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--policy-dir", type=Path, default=HERE)
    # Membership declarations live beside the rule intents by default; a
    # relocated --policy-dir carries its own.
    ap.add_argument("--membership-dir", type=Path, default=None)
    # --policy-dir relocates the POLICY; the generated matrix lives where it
    # lives. Passing it through explicitly rather than letting generate.py
    # resolve a default from its own path — a copy of that script run from
    # elsewhere resolves the matrix beside the copy and writes into nowhere.
    ap.add_argument(
        "--target",
        type=Path,
        default=REPO_ROOT / "ops" / "ansible" / "netbird_acl_matrix.yml",
    )
    ap.add_argument("--list", action="store_true")
    ap.add_argument("--apply", action="store_true")
    ap.add_argument("--today", default=None)
    args = ap.parse_args()

    today = dt.date.fromisoformat(args.today) if args.today else dt.date.today()
    membership_dir = args.membership_dir or (args.policy_dir / "membership")
    m_due, mrc = expire_membership(membership_dir, today, args.apply)
    if mrc:
        return mrc
    intents = args.policy_dir / "rule-intents" / "netbird-acl.yaml"
    lines = intents.read_text().split("\n")

    due = []
    for rule in scan(lines):
        if not rule.expires:
            continue
        when = parse_date(rule.expires)
        if when is None:
            print(
                f"warrant: {rule.name} has expires: {rule.expires}, which is not "
                "a YYYY-MM-DD date",
                file=sys.stderr,
            )
            return 2
        if when < today:
            due.append((rule, when))

    if not due:
        # "nothing past due" is the phrase warrant-expiry.yml keys on, so it is
        # printed only when NEITHER rule intents nor membership entries are due.
        if m_due:
            print(f"warrant: no rule intents past due as of {today.isoformat()}")
        else:
            print(f"warrant: nothing past due as of {today.isoformat()}")
        return 0

    print(f"warrant: {len(due)} rule intent(s) past due as of {today.isoformat()}:")
    for rule, when in due:
        print(f"  - {rule.name}  expired {when.isoformat()}  owner: {rule.owner or '<none>'}")

    if not args.apply:
        return 0

    drop: set[int] = set()
    for rule, _ in due:
        drop.update(range(rule.start, rule.end))
    remaining = [line for i, line in enumerate(lines) if i not in drop]
    intents.write_text("\n".join(remaining))

    # Regenerate, so the removal reaches the applied ACL in the same commit.
    # An expiry that edits the registry and leaves the matrix stale is F-324
    # with a timer on it.
    proc = subprocess.run(
        [
            sys.executable,
            str(args.policy_dir / "generate.py"),
            "--policy-dir", str(args.policy_dir),
            "--target", str(args.target),
        ],
        cwd=str(REPO_ROOT),
        capture_output=True,
        text=True,
    )
    print(proc.stdout.strip() or proc.stderr.strip())
    return proc.returncode


if __name__ == "__main__":
    raise SystemExit(main())
