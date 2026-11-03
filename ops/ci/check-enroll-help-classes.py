#!/usr/bin/env python3
"""
Guard: `goat machine enroll --help` tells a custodian the same thing the mint does.

  crates/goat-cli/src/cmd/machine.rs                   — the --node-class help
  ops/goat-zenoh-portal/internal/registry/machines.go  — the mint's class sets

Why this guard exists (#1400). The help is hand-written clap doc text and drifted
from the registry after ADR 1116: it offered SENSOR / EFFECTOR / PROCESSOR /
PLATFORM as self-service while the mint refused SENSOR and EFFECTOR as reserved
and PROCESSOR for want of a mesh group, and it never named the gear classes. A
tenant enrolling a vehicle followed the help and was refused.

Checks (exit 1 on any violation):

  1. Every class the help lists under "Custodian self-service" is enrollable
     (not reserved, not intake-only) and has a mesh-group derivation — or is
     PLATFORM, which derives from --workloads.
  2. Every enrollable, mapped gear class appears in that self-service list.
  3. Every infrastructure class appears somewhere in the help.
  4. Every reserved and intake-only class the help names sits in its
     "Refused at the mint" sentence, never in an enrollable one.

Usage:  python3 ops/ci/check-enroll-help-classes.py [repo-root]
"""

import re
import sys
from pathlib import Path

MACHINE_RS = "crates/goat-cli/src/cmd/machine.rs"
MACHINES_GO = "ops/goat-zenoh-portal/internal/registry/machines.go"

TOKEN = re.compile(r"\b[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)*\b")
QUOTED = re.compile(r'"([A-Z][A-Z0-9-]*)"')
MAP_KEY = re.compile(r'"([A-Z][A-Z0-9-]*)"\s*:\s*\{')
# The doc comment directly above the `node_class` field of `Enroll`.
HELP_BLOCK = re.compile(r"((?:[ \t]*///[^\n]*\n)+)(?:[ \t]*#\[arg[^\n]*\n)+[ \t]*node_class:")


def go_set(src: str, name: str) -> set[str]:
    m = re.search(rf"{name}\s*=\s*map\[string\]bool\{{([^}}]*)\}}", src)
    if not m:
        sys.exit(f"FAIL: could not find {name} in {MACHINES_GO}")
    return set(QUOTED.findall(m.group(1)))


def mesh_keys(src: str) -> set[str]:
    m = re.search(r"nodeClassMeshGroups\s*=\s*map\[string\]\[\]string\{(.*?)\n\t\}", src, re.DOTALL)
    if not m:
        sys.exit(f"FAIL: could not find nodeClassMeshGroups in {MACHINES_GO}")
    body = "\n".join(ln for ln in m.group(1).splitlines() if not ln.strip().startswith("//"))
    return set(MAP_KEY.findall(body))


def help_text(src: str) -> str:
    m = HELP_BLOCK.search(src)
    if not m:
        sys.exit(f"FAIL: could not find the --node-class doc comment in {MACHINE_RS}")
    return " ".join(ln.strip().removeprefix("///").strip() for ln in m.group(1).splitlines())


def segment(text: str, start: str, end: str) -> str:
    i = text.find(start)
    if i < 0:
        return ""
    j = text.find(end, i + len(start))
    return text[i + len(start): j if j >= 0 else len(text)]


def classes_in(text: str, known: set[str]) -> set[str]:
    return {t for t in TOKEN.findall(text) if t in known}


def check(rs_src: str, go_src: str) -> list[str]:
    gear = go_set(go_src, "gearNodeClasses")
    infra = go_set(go_src, "infraNodeClasses")
    function = go_set(go_src, "functionNodeClasses")
    reserved = go_set(go_src, "reservedNodeClasses")
    intake = go_set(go_src, "intakeOnlyNodeClasses")
    mapped = mesh_keys(go_src)
    known = gear | infra | function | reserved

    text = help_text(rs_src)
    self_service = classes_in(segment(text, "Custodian self-service:", "NETWORK ("), known)
    refused = classes_in(segment(text, "Refused at the mint:", "Case-insensitive"), known)
    named = classes_in(text, known)
    unmintable = reserved | intake

    errors = []
    for c in sorted(self_service):
        if c in unmintable or (c not in mapped and c != "PLATFORM"):
            errors.append(f"help offers {c} as self-service, but the mint refuses it")
    for c in sorted((gear & mapped) - unmintable - self_service):
        errors.append(f"enrollable gear class {c} is missing from the help's self-service list")
    for c in sorted(infra - named):
        errors.append(f"infrastructure class {c} is not named in the help")
    for c in sorted((named & unmintable) - refused):
        errors.append(f"{c} is named in the help but not in its 'Refused at the mint' sentence")
    if not self_service:
        errors.append("help has no 'Custodian self-service:' list")
    return errors


def main() -> int:
    root = Path(sys.argv[1] if len(sys.argv) > 1 else ".")
    errors = check((root / MACHINE_RS).read_text(), (root / MACHINES_GO).read_text())
    for e in errors:
        print(f"FAIL: {e}", file=sys.stderr)
    if errors:
        print(f"Fix the --node-class doc comment in {MACHINE_RS} to match {MACHINES_GO}.", file=sys.stderr)
        return 1
    print("OK: machine enroll --help matches the mint's node-class sets")
    return 0


if __name__ == "__main__":
    sys.exit(main())
