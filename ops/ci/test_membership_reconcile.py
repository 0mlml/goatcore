#!/usr/bin/env python3
"""Offline tests for check-membership-reconcile.py's declaration reader.

The live half of that check needs mesh credentials and runs on a schedule. This
half needs nothing and runs on every PR, because the part most likely to break is
not the API call — it is the representation of a whole class of change (a
deliberate REMOVAL of a key-granted group). A parser regression would
silently make the check blind to it, and the live job would still print
a confident OK.

  python3 ops/ci/test_membership_reconcile.py
"""

import os
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
CHECK = os.path.join(HERE, "check-membership-reconcile.py")

failures = []


def check(label, cond, detail=""):
    print(f"  {'ok  ' if cond else 'FAIL'}  {label}{'  — ' + detail if detail and not cond else ''}")
    if not cond:
        failures.append(label)


def load(path):
    """Import load_deviations from the check without executing its main body."""
    with open(CHECK) as source_file:
        src = source_file.read()
    start = src.index("def load_deviations")
    end = src.index("\npeers = get(")
    ns = {"os": os}
    exec(src[start:end], ns)
    return ns["load_deviations"](path)


print("parser — repeated keys survive")
with tempfile.NamedTemporaryFile("w", suffix=".yaml", delete=False) as f:
    f.write(
        "deviations:\n"
        "  - peer: alpha\n"
        "    group: G-ONE\n"
        "    group: G-TWO\n"
        "    absent: A-ONE\n"
        "    absent: A-TWO\n"
        "    decided_by: operator\n"
        "    decided_on: 2026-08-14\n"
        "  - peer: beta\n"
        "    absent: B-ONE\n"
        "    decided_by: operator\n"
        "    decided_on: 2026-08-01\n"
    )
    tmp = f.name

d = load(tmp)
row = d["alpha"][0]
# A real YAML parser collapses duplicate keys to the last one. That would leave
# one group and one absent here, silently discarding half of every declaration.
check("both `group:` values kept", row["groups"] == ["G-ONE", "G-TWO"], repr(row["groups"]))
check("both `absent:` values kept", row["absent"] == ["A-ONE", "A-TWO"], repr(row["absent"]))
check("decided_on captured", row.get("on") == "2026-08-14", repr(row.get("on")))
check("a row may be absent-only", d["beta"][0]["groups"] == [] and d["beta"][0]["absent"] == ["B-ONE"])
os.unlink(tmp)

print("script is syntactically valid and refuses to run unauthenticated")
r = subprocess.run([sys.executable, "-m", "py_compile", CHECK], capture_output=True)
check("py_compile clean", r.returncode == 0, r.stderr.decode()[:200])
env = dict(os.environ)
env.pop("NB_API_URL", None)
env.pop("NB_API_TOKEN", None)
r = subprocess.run([sys.executable, CHECK], capture_output=True, env=env)
check("exits 2 with no credentials", r.returncode == 2, f"exit={r.returncode}")
check("says why", b"NB_API_URL" in r.stderr, r.stderr.decode()[:200])

# An UNREADABLE mesh must exit 2, never 1. 1 means "a declared removal came
# back", and confusing the two turns an outage into a false membership finding —
# and hides a real one behind noise. A traefik serving a self-signed cert is
# the usual cause of an unreadable mesh.
env["NB_API_URL"] = "https://127.0.0.1:1"   # refused, fast, no network needed
env["NB_API_TOKEN"] = "irrelevant"
r = subprocess.run([sys.executable, CHECK], capture_output=True, env=env, timeout=120)
check("unreachable mesh exits 2, not 1", r.returncode == 2, f"exit={r.returncode}")
check("does not claim a clean mesh when it read nothing",
      b"OK: no undeclared additions" not in r.stdout)

# THE UNUSED-KEY AXIS. Same reasoning as the `absent:` axis above: the
# failure it covers is an ABSENCE — a key that minted no peer — so there is no
# peer row to notice going missing if this regresses. Two properties are
# load-bearing and both are cheap to hold offline.
print()
print("unused-key axis")

# 1. A bad threshold must degrade, not detonate. This value is rendered from a
#    group_var; a typo must not take the whole audit down with a ValueError before
#    it reads the mesh, or a threshold mistake becomes an outage of the two OTHER
#    axes as well.
env = dict(os.environ)
env.pop("NB_API_URL", None)
env.pop("NB_API_TOKEN", None)
env["MEMBERSHIP_UNUSED_KEY_DAYS"] = "banana"
r = subprocess.run([sys.executable, CHECK], capture_output=True, env=env)
check("a non-numeric threshold does not crash the run", r.returncode == 2,
      f"exit={r.returncode} (2 = the credential gate, i.e. it got that far)")
check("and says it fell back", b"not a whole number of days" in r.stderr,
      r.stderr.decode()[:200])
env["MEMBERSHIP_UNUSED_KEY_DAYS"] = "-1"
r = subprocess.run([sys.executable, CHECK], capture_output=True, env=env)
check("a negative threshold falls back too", b"not a whole number of days" in r.stderr,
      r.stderr.decode()[:200])

# 2. An unused key must never become a strict failure. Exit 1 is reserved for a
#    reverted removal — a regression somebody has to undo. An unanswered
#    invitation is a backlog row that is NORMAL on a goatnet mid-onboarding, and
#    a guard that goes red on the normal state is one people stop reading.
with open(CHECK) as source_file:
    src = source_file.read()
tail = src[src.index("if reverted and (STRICT"):]
check("no strict exit is gated on `unused`", "unused" not in tail,
      "an unanswered invitation must not fail the run; exit 1 means a reverted removal")

print()
if failures:
    print(f"FAIL: {len(failures)} check(s) failed")
    sys.exit(1)
print("OK: declaration reader behaves")
