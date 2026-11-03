#!/usr/bin/env python3
"""fixture-expiry.py — per-fixture earliest `expires`, and the rule each pins.

Called by make-bundle.sh, and read by verify-bundle.sh out of the manifest. It
answers one question for a partner: *if I supply today's date as `now` instead
of the date in now.txt, which fixtures give me a different answer?*

The §6 step matters, and getting it wrong overstates the damage. Expiry is step
3, so a stale `now` pre-empts only the rules tested after it. A `refuse-*`
fixture for step 0, 1 or 2 still refuses for its own rule on any later date; the
step-4 fixtures are the ones silently taken over by expiry, and every composing
pair flips from agreement to refusal.

The step is read from each fixture's own REFUSAL.md rather than from a table
here, so a fixture added later cannot quietly get the wrong answer. If the step
cannot be read, this exits non-zero and the bundle does not build — a wrong
table would be worse than none, because it is the table telling a partner which
of their failures to take seriously.

Output, one line per fixture, as manifest comment lines:

    # fixture-expiry: <name> <earliest-expires> agree|refuse-step-<n>
"""

import json
import os
import re
import sys

# The structured row every REFUSAL.md carries, e.g. "| Spec | §6 step 4(b), ... |".
# Anchored on the row rather than on any "step N" in the prose, which names
# other steps while explaining why they are not the one that fires.
STEP_ROW = re.compile(r"^\|\s*Spec\s*\|.*?§6 step (\d)", re.M)

NOT_A_FIXTURE_DIR = ("testkeys", "malformed")


def earliest_expires(fixture_dir):
    """The earlier `expires` of the fixture's packages, or None if it has none."""
    dates = []
    for name in sorted(os.listdir(fixture_dir)):
        if not (name.startswith("package-") and name.endswith(".canonical.json")):
            continue
        if ".signing-input." in name:
            continue
        with open(os.path.join(fixture_dir, name), "rb") as fh:
            value = json.loads(fh.read().decode("utf-8")).get("expires")
        if value:
            dates.append(value)
    return min(dates) if dates else None


def kind_of(name, fixture_dir):
    if name.startswith("compose-"):
        return "agree"
    refusal = os.path.join(fixture_dir, "REFUSAL.md")
    if not os.path.exists(refusal):
        sys.exit("fixture-expiry: %s has no REFUSAL.md; cannot tell which rule it pins" % name)
    with open(refusal, "rb") as fh:
        match = STEP_ROW.search(fh.read().decode("utf-8"))
    if not match:
        sys.exit(
            "fixture-expiry: %s/REFUSAL.md has no '| Spec | ... step N |' row; cannot tell "
            "which rule it pins, and guessing would mistell a partner which of their failures "
            "to take seriously" % name
        )
    return "refuse-step-" + match.group(1)


def main():
    if len(sys.argv) != 2:
        sys.exit("usage: fixture-expiry.py <vectors-dir>")
    root = sys.argv[1]

    for name in sorted(os.listdir(root)):
        fixture_dir = os.path.join(root, name)
        if not os.path.isdir(fixture_dir) or name in NOT_A_FIXTURE_DIR:
            continue
        expires = earliest_expires(fixture_dir)
        if expires is None:
            continue
        print("# fixture-expiry: %s %s %s" % (name, expires, kind_of(name, fixture_dir)))


if __name__ == "__main__":
    main()
