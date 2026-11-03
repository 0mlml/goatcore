#!/usr/bin/env python3
"""check-deps-named.py — every tool a release run shells out to is named by its preflight.

"Had to install a couple random things" (2026-10-28, panoscope rc.1) is the
failure: a run that stops mid-way on a binary nobody said it needed. This test
lists the binaries the release path invokes — rc.sh and everything it calls —
and asserts each one is named in rc.sh's RC_DEPS preflight, and that
goatbuilder's doctor names the ones the binary itself shells out to.

usage: check-deps-named.py [--repo DIR]   exit 0 ok, 1 a tool is invoked but not named
"""
import argparse
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))


def read_text(path):
    with open(path, encoding="utf-8", errors="replace") as fh:
        return fh.read()


# The release path: rc.sh and the scripts it runs (transitively).
RC_SOURCES = [
    "ops/scripts/rc.sh",
    "ops/scripts/deploy-component.sh",
    "ops/scripts/pr-land.sh",
    "ops/scripts/ansible-preflight.sh",
    "ops/scripts/fresh-source.sh",
    "ops/scripts/door-open.sh",
    "ops/scripts/env-door.sh",
]
# Tools whose invocation we look for. `python3` scripts are tracked by their
# python deps check (PyYAML + jsonschema), not here.
TOOLS = ["gh", "git", "crane", "go", "cosign", "ansible-playbook", "ansible-inventory", "ansible", "aws-vault",
         "nc", "ssh", "jq", "yq", "op", "netbird", "node", "npm", "npx", "session-manager-plugin", "aws"]
# Named by rc.sh's RC_DEPS or by a specific preflight line elsewhere:
#   netbird — optional by design (a laptop without it deploys over the door); the
#             transport line prints "netbird profile reads ''" rather than refusing.
#   op      — not invoked by the scripts; the operator wraps the command in it.
#   session-manager-plugin, aws — checked by env-door.sh itself before it opens the door.
#   node/npm/npx — checked by rc.sh's browser_toolchain_check when a browser row is mapped.
#   yq      — rollback.sh's own preflight (`yq required`), not on the release path.
#   cosign  — doctor names it; the scripts do not invoke it.
OPTIONAL = {"netbird", "op", "session-manager-plugin", "aws", "node", "npm", "npx", "yq", "cosign"}

INVOKE = re.compile(r"(?:^|[\s;|&(`$]|\$\()(?:command -v )?(%s)(?=[\s;|&)]|$)" % "|".join(re.escape(t) for t in TOOLS), re.M)


def invoked(repo, path):
    found = set()
    for line in read_text(os.path.join(repo, path)).splitlines(True):
        s = line.split("#", 1)[0] if not line.lstrip().startswith("#") else ""
        for m in INVOKE.finditer(s):
            found.add(m.group(1))
    return found


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", default=os.path.abspath(os.path.join(HERE, "..", "..")))
    a = ap.parse_args()
    rc = read_text(os.path.join(a.repo, "ops/scripts/rc.sh"))
    m = re.search(r'^RC_DEPS="([^"]+)"', rc, re.M)
    if not m:
        print("FAIL: rc.sh has no RC_DEPS=\"…\" preflight line")
        return 1
    named = set(m.group(1).split())
    # An either-of requirement (the digest: any of crane, skopeo or docker, ADR 1204)
    # names every member: `RC_MISSING="$RC_MISSING crane|skopeo|docker"`.
    for alt in re.findall(r'RC_MISSING="\$RC_MISSING ([a-z0-9|-]+)"', rc):
        if "|" in alt:
            named |= set(alt.split("|"))
    used = set()
    per = {}
    for p in RC_SOURCES:
        u = invoked(a.repo, p)
        per[p] = u
        used |= u
    unnamed = sorted(t for t in used if t not in named and t not in OPTIONAL)
    fail = 0
    print(f"deps-named: rc.sh RC_DEPS names {sorted(named)}")
    for p, u in per.items():
        print(f"  {p}: invokes {sorted(u) or '-'}")
    if unnamed:
        fail = 1
        print(f"FAIL: invoked on the release path but not in rc.sh's RC_DEPS: {unnamed}")
    if "node_modules" not in rc or "npm ci" not in rc:
        fail = 1
        print("FAIL: rc.sh does not check the console's Playwright toolchain (node_modules / npm ci) before a browser row")
    # goatbuilder's doctor names what the binary shells out to.
    doc = read_text(os.path.join(a.repo, "crates/goatbuilder/src/doctor.rs"))
    doctor_named = set(re.findall(r'\(\s*\n?\s*"([a-z0-9_-]+)",\s*\n?\s*"', doc)) | set(re.findall(r'which\("([a-z0-9_-]+)"\)', doc))
    gb_src = ""
    for f in ("release.rs", "push.rs", "fastlane.rs", "rollback.rs"):
        gb_src += read_text(os.path.join(a.repo, "crates/goatbuilder/src", f))
    gb_used = set(re.findall(r'Command::new\("([a-z0-9_-]+)"\)', gb_src)) | set(re.findall(r'sh(?:_ok)?\("([a-z0-9_-]+)"', gb_src))
    gb_unnamed = sorted(t for t in gb_used if t in TOOLS and t not in doctor_named and t not in OPTIONAL)
    print(f"  goatbuilder: shells out to {sorted(t for t in gb_used if t in TOOLS)}; doctor names {sorted(doctor_named & set(TOOLS))}")
    if gb_unnamed:
        fail = 1
        print(f"FAIL: goatbuilder shells out to tools its doctor does not name: {gb_unnamed}")
    print("ok: every tool the release path invokes is named up front" if not fail else "deps-named: FAIL")
    return fail


if __name__ == "__main__":
    sys.exit(main())
