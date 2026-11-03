#!/usr/bin/env python3
"""Negative tests for check-gear-acting-group-scope.py.

A guard that cannot fail proves nothing. The load-bearing test here is
test_catches_the_real_regression: it replays the tree as it stood at
a09f17962^ and requires the guard to name NETWORK-JUMP. That defect was live
on zagros-prod for two days, so this is not a synthetic case.
"""

import copy
import subprocess
import sys
import tempfile
from pathlib import Path

import yaml

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent
GUARD = HERE / "check-gear-acting-group-scope.py"
TAXONOMY = "ops/ansible/netbird_acl_taxonomy.yml"
INTENTS = "ops/warrant/rule-intents/netbird-acl.yaml"

FAILURES = []


def check(name, cond, detail=""):
    if cond:
        print(f"ok    {name}")
    else:
        print(f"FAIL  {name}{(' — ' + detail) if detail else ''}")
        FAILURES.append(name)


def run(root):
    p = subprocess.run(
        [sys.executable, str(GUARD), str(root)],
        capture_output=True, text=True,
    )
    return p.returncode, p.stdout + p.stderr


def stage(tax_text, intents_text):
    """Write a tree the guard can read, reusing the real repo for everything else."""
    d = Path(tempfile.mkdtemp())
    for rel, text in ((TAXONOMY, tax_text), (INTENTS, intents_text)):
        dst = d / rel
        dst.parent.mkdir(parents=True, exist_ok=True)
        dst.write_text(text)
    return d


def git_show(rev, path):
    return subprocess.run(
        ["git", "-C", str(ROOT), "show", f"{rev}:{path}"],
        capture_output=True, text=True, check=True,
    ).stdout


# --- 1. the repo as it stands -------------------------------------------------
rc, out = run(ROOT)
check("the tree as committed passes", rc == 0, out.strip().splitlines()[-1] if out else "")

# --- 2. the real regression ---------------------------------------------------
# a09f17962 is the NETWORK-JUMP fix; its parent is the broken state that shipped.
try:
    pre = stage(git_show("a09f17962^", TAXONOMY), git_show("a09f17962^", INTENTS))
    rc, out = run(pre)
    check(
        "catches the real regression (NETWORK-JUMP at a09f17962^)",
        rc == 1 and "NETWORK-JUMP" in out,
        f"rc={rc}, NETWORK-JUMP in output: {'NETWORK-JUMP' in out}",
    )
    check(
        "names the gear class it cannot reach",
        "NETWORK" in out and "scope template" in out,
    )
except subprocess.CalledProcessError:
    check("catches the real regression (NETWORK-JUMP at a09f17962^)", False,
          "a09f17962 not in this clone — fetch full history")

# --- 3. synthetic: pull a gear-acting group out of the scope template ---------
tax = yaml.safe_load((ROOT / TAXONOMY).read_text())
intents_text = (ROOT / INTENTS).read_text()

mutated = copy.deepcopy(tax)
mutated["netbird_exercise_template"]["groups"] = [
    g for g in mutated["netbird_exercise_template"]["groups"]
    if g["suffix"] != "NETWORK-JUMP"
]
rc, out = run(stage(yaml.safe_dump(mutated), intents_text))
check(
    "removing NETWORK-JUMP from the scope template fails the guard",
    rc == 1 and "NETWORK-JUMP" in out,
    f"rc={rc}",
)

# --- 4. a qualified cross-scope source is not a violation ---------------------
mutated = copy.deepcopy(tax)
gear_suffixes = {g["suffix"] for g in mutated["netbird_exercise_gear_template"]["groups"]}
intents = yaml.safe_load(intents_text)
for goatnet in ("zagros", "zagros-prod"):
    sec = intents["netbird_acl_matrix"].get(goatnet)
    if not sec or "rules" not in sec:
        continue
    sec["rules"] = list(sec["rules"]) + [{
        "name": "synthetic qualified source",
        "sources": ["HOST:INFRA"],
        "destinations": [sorted(gear_suffixes)[0]],
        "protocol": "tcp",
        "action": "accept",
    }]
    break
rc, out = run(stage(yaml.safe_dump(tax), yaml.safe_dump(intents)))
check("an ADR 1124 qualified source (HOST:INFRA) is not flagged", rc == 0, out.strip())

# --- 5. malformed input is refused, not traced back ---------------------------
rc, out = run(stage("netbird_acl_taxonomy: [", intents_text))
check(
    "malformed taxonomy is refused with a message, not a traceback",
    rc == 2 and "REFUSING" in out and "Traceback" not in out,
    f"rc={rc}",
)

print()
if FAILURES:
    print(f"{len(FAILURES)} failure(s)")
    sys.exit(1)
print("gear-acting group scope guard: all negative tests pass")
