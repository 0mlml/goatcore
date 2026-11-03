#!/usr/bin/env python3
"""warrant — generate ops/ansible/netbird_acl_matrix.yml from the registry.

    port-sets.yaml ──┐
                     ├─→ generate.py ─→ netbird_acl_matrix.yml
    rule-intents/ ───┘

WHAT THIS SYNTHESIZES IS EXACTLY ONE LINE PER PORT-CARRYING RULE: the
`ports:` line, resolved from the named set. Everything else in the intents
file — comments, ordering, blank lines, YAML anchors, the assertions section —
is copied through verbatim.

That is deliberate, and it is what makes the byte-identical gate mean
something. A generator that stored the rendered ports and echoed them back
would pass the gate while proving nothing. Here the ports exist in exactly one
place (port-sets.yaml), so a byte-identical result is positive evidence that
set resolution reproduces the hand-written declaration.

THE GENERATED FILE STAYS FULLY EXPANDED. An operator reading the applied ACL
sees literal ports, as today — the indirection lives in the source, not in
what is reviewed or applied (docs/design/warrant.md §2.3).

Usage:
    generate.py                      # write the matrix in place
    generate.py --check              # exit 3 if the file is not byte-identical
    generate.py --stdout             # print, write nothing
    generate.py --policy-dir DIR     # read the registry from elsewhere

`--policy-dir` is the open-source seam (docs/project/warrant-plan.md §3A):
the mechanism ships public, and it runs against any operator's policy without
that policy — or its decision history — travelling with it.
"""

from __future__ import annotations

import argparse
import difflib
import re
import sys
from pathlib import Path

try:
    import yaml
except ImportError:  # pragma: no cover - environment guard
    sys.exit("warrant: PyYAML is required (pip install pyyaml)")

REPO_ROOT = Path(__file__).resolve().parents[2]
DEFAULT_POLICY_DIR = REPO_ROOT / "ops" / "warrant"
DEFAULT_TARGET = REPO_ROOT / "ops" / "ansible" / "netbird_acl_matrix.yml"

# A rule's protocol line, e.g. "        protocol: tcp"
RE_PROTOCOL = re.compile(r"^(?P<indent>\s*)protocol:\s*(?P<proto>\S+)\s*$")
# The registry's stand-in for a rendered ports line, e.g. "        port_set: substrate"
RE_PORT_SET = re.compile(r"^(?P<indent>\s*)port_set:\s*(?P<name>\S+)\s*$")
# A rendered ports line in the generated file — rejected in the intents source.
RE_PORTS = re.compile(r"^\s*ports:\s*\[")
# Registry-only metadata (ADR 1129 C3). These annotate the DECISION, not the
# permit: they are dropped at generation so nothing downstream — the cutover
# play, the NetBird API, an operator reading the applied ACL — ever sees them.
# That is what makes expiry cost one field and one check rather than a service.
RE_META = re.compile(r"^\s*(?:expires|owner):\s*\S")


class GenerateError(RuntimeError):
    """A registry error worth naming precisely, not a traceback."""


def load_port_sets(policy_dir: Path) -> dict[tuple[str, str], list[str]]:
    """Map (set-name, protocol) -> ports, and refuse an ambiguous vocabulary."""
    path = policy_dir / "port-sets.yaml"
    if not path.exists():
        raise GenerateError(f"no port-sets.yaml at {path}")
    doc = yaml.safe_load(path.read_text()) or {}
    sets = doc.get("port_sets")
    if not sets:
        raise GenerateError(f"{path} declares no port_sets")

    arms: dict[tuple[str, str], list[str]] = {}
    # (protocol, ports) must name exactly one set, or `why-blocked` cannot
    # propose "the" minimal set and the extractor cannot be deterministic.
    seen_shapes: dict[tuple[str, tuple[str, ...]], str] = {}
    for entry in sets:
        name = entry.get("name")
        if not name:
            raise GenerateError(f"{path}: a port_set entry has no name")
        if not entry.get("purpose", "").strip():
            raise GenerateError(f"{path}: port_set '{name}' has no purpose")
        if not entry.get("owner"):
            raise GenerateError(f"{path}: port_set '{name}' has no owner")
        for member in entry.get("members") or []:
            proto = member.get("protocol")
            ports = [str(p) for p in (member.get("ports") or [])]
            if not proto or not ports:
                raise GenerateError(
                    f"{path}: port_set '{name}' has a member with no protocol or no ports"
                )
            if (name, proto) in arms:
                raise GenerateError(
                    f"{path}: port_set '{name}' declares two '{proto}' arms; "
                    "a rule names one arm, so the second could never be reached"
                )
            arms[(name, proto)] = ports
            shape = (proto, tuple(ports))
            if shape in seen_shapes and seen_shapes[shape] != name:
                raise GenerateError(
                    f"{path}: {proto} {ports} is declared by both "
                    f"'{seen_shapes[shape]}' and '{name}'. Two names for one shape "
                    "means the vocabulary has re-fragmented, which is the thing "
                    "this registry exists to prevent."
                )
            seen_shapes[shape] = name
    return arms


def display(path: Path) -> str:
    """Repo-relative when it can be, absolute otherwise.

    `--policy-dir` exists so this generator can run against a policy tree that
    is not in this repo (the open-source seam). A bare relative_to() throws in
    exactly that case, turning a successful run into a traceback.
    """
    try:
        return str(path.relative_to(REPO_ROOT))
    except ValueError:
        return str(path)


def render_ports(ports: list[str]) -> str:
    """Render a ports list in the declaration's own style: ["443", "53"]."""
    return "[" + ", ".join(f'"{p}"' for p in ports) + "]"


def generate(policy_dir: Path) -> str:
    """Expand the intents file into the full matrix text."""
    arms = load_port_sets(policy_dir)
    intents_path = policy_dir / "rule-intents" / "netbird-acl.yaml"
    if not intents_path.exists():
        raise GenerateError(f"no rule intents at {intents_path}")

    out: list[str] = []
    protocol: str | None = None
    for lineno, line in enumerate(intents_path.read_text().split("\n"), start=1):
        stripped = line.lstrip()
        if not stripped.startswith("#"):
            match = RE_PROTOCOL.match(line)
            if match:
                protocol = match.group("proto")
            if RE_PORTS.match(line):
                raise GenerateError(
                    f"{intents_path}:{lineno}: a literal `ports:` line in the intents "
                    "source. Ports live in port-sets.yaml — name the set with "
                    "`port_set:` instead. (This is the round-trip CI rejects: "
                    "editing the generated file and pasting it back.)"
                )
            if RE_META.match(line):
                # Registry metadata: reviewed in the intents diff, never applied.
                continue
            match = RE_PORT_SET.match(line)
            if match:
                name = match.group("name")
                if protocol is None:
                    raise GenerateError(
                        f"{intents_path}:{lineno}: `port_set: {name}` with no "
                        "`protocol:` above it in the rule"
                    )
                if (name, protocol) not in arms:
                    known = sorted({n for (n, _p) in arms})
                    raise GenerateError(
                        f"{intents_path}:{lineno}: port_set '{name}' has no "
                        f"'{protocol}' arm. Known sets: {', '.join(known)}"
                    )
                ports = render_ports(arms[(name, protocol)])
                out.append(f"{match.group('indent')}ports: {ports}")
                continue
        out.append(line)
    return "\n".join(out)


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument(
        "--policy-dir",
        type=Path,
        default=DEFAULT_POLICY_DIR,
        help="registry directory (port-sets.yaml + rule-intents/); the OSS seam",
    )
    ap.add_argument("--target", type=Path, default=DEFAULT_TARGET)
    ap.add_argument("--check", action="store_true", help="verify, write nothing")
    ap.add_argument("--stdout", action="store_true", help="print, write nothing")
    args = ap.parse_args()

    try:
        rendered = generate(args.policy_dir)
    except GenerateError as exc:
        print(f"warrant: {exc}", file=sys.stderr)
        return 2

    if args.stdout:
        sys.stdout.write(rendered)
        return 0

    if args.check:
        if not args.target.exists():
            print(f"warrant: {args.target} does not exist", file=sys.stderr)
            return 3
        current = args.target.read_text()
        if current == rendered:
            print(f"warrant: {display(args.target)} is byte-identical "
                  "to the registry's expansion")
            return 0
        diff = difflib.unified_diff(
            current.split("\n"),
            rendered.split("\n"),
            fromfile=f"{args.target.name} (on disk)",
            tofile="generated from the registry",
            lineterm="",
            n=2,
        )
        print("warrant: the generated matrix is NOT byte-identical.", file=sys.stderr)
        print("  on disk = what is deployed; generated = what the registry says.",
              file=sys.stderr)
        for row in diff:
            print(f"  {row}", file=sys.stderr)
        return 3

    if not args.target.parent.is_dir():
        # Almost always means --target was left to default while this script
        # ran from outside the repo. Say that, rather than raising FileNotFound
        # from three frames down.
        print(
            f"warrant: cannot write {args.target} — {args.target.parent} does not "
            "exist.\n  Pass --target explicitly when running from outside the repo.",
            file=sys.stderr,
        )
        return 2
    args.target.write_text(rendered)
    print(f"warrant: wrote {display(args.target)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
