#!/usr/bin/env python3
"""warrant — derive the rule-intents source from today's hand-written matrix.

This ran once, at cutover, to produce
`ops/warrant/rule-intents/netbird-acl.yaml` from
`ops/ansible/netbird_acl_matrix.yml`. It is kept in the repo as provenance:
the cutover was a mechanical transformation anyone can re-run and check, not a
hand transcription of 1327 lines that a reviewer would have to trust.

The transformation is exactly one substitution, in the other direction from
generate.py: every literal `ports: [...]` line becomes `port_set: <name>`,
resolved by reverse lookup on (protocol, ports) against port-sets.yaml. A
combination the vocabulary does not name is a hard error — that is the check
that the seven sets actually cover the declaration.

Re-running it is safe and should be a no-op:

    ops/warrant/extract.py --check
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

try:
    import yaml
except ImportError:  # pragma: no cover
    sys.exit("warrant: PyYAML is required (pip install pyyaml)")

sys.path.insert(0, str(Path(__file__).resolve().parent))
from generate import (  # noqa: E402
    display,
    DEFAULT_POLICY_DIR,
    DEFAULT_TARGET,
    RE_META,
    RE_PORTS,
    RE_PROTOCOL,
    GenerateError,
)

import re  # noqa: E402

RE_PORTS_FULL = re.compile(r"^(?P<indent>\s*)ports:\s*(?P<list>\[.*\])\s*$")


def strip_metadata(text: str) -> str:
    """Drop registry-only lines, so the comparison is about the ACL content."""
    return "\n".join(
        line
        for line in text.split("\n")
        if not (not line.lstrip().startswith("#") and RE_META.match(line))
    )


def reverse_index(policy_dir: Path) -> dict[tuple[str, tuple[str, ...]], str]:
    """Map (protocol, ports) -> set name."""
    doc = yaml.safe_load((policy_dir / "port-sets.yaml").read_text()) or {}
    index: dict[tuple[str, tuple[str, ...]], str] = {}
    for entry in doc.get("port_sets") or []:
        for member in entry.get("members") or []:
            key = (member["protocol"], tuple(str(p) for p in member["ports"]))
            index[key] = entry["name"]
    return index


def extract(source: Path, policy_dir: Path) -> str:
    index = reverse_index(policy_dir)
    out: list[str] = []
    protocol: str | None = None
    unnamed: list[str] = []

    for lineno, line in enumerate(source.read_text().split("\n"), start=1):
        if not line.lstrip().startswith("#"):
            match = RE_PROTOCOL.match(line)
            if match:
                protocol = match.group("proto")
            match = RE_PORTS_FULL.match(line)
            if match and RE_PORTS.match(line):
                ports = tuple(str(p) for p in yaml.safe_load(match.group("list")))
                key = (protocol, ports)
                if key not in index:
                    unnamed.append(f"  {source.name}:{lineno}: {protocol} {list(ports)}")
                    out.append(line)
                    continue
                out.append(f"{match.group('indent')}port_set: {index[key]}")
                continue
        out.append(line)

    if unnamed:
        raise GenerateError(
            "these (protocol, ports) combinations are not named by any port-set, "
            "so the vocabulary does not yet cover the declaration:\n"
            + "\n".join(unnamed)
        )
    return "\n".join(out)


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--source", type=Path, default=DEFAULT_TARGET)
    ap.add_argument("--policy-dir", type=Path, default=DEFAULT_POLICY_DIR)
    ap.add_argument("--check", action="store_true", help="verify, write nothing")
    args = ap.parse_args()

    try:
        rendered = extract(args.source, args.policy_dir)
    except GenerateError as exc:
        print(f"warrant: {exc}", file=sys.stderr)
        return 2

    dest = args.policy_dir / "rule-intents" / "netbird-acl.yaml"
    if args.check:
        if not dest.exists():
            print(f"warrant: {dest} does not exist", file=sys.stderr)
            return 3
        # Registry metadata (`expires:` / `owner:`) exists only in the intents
        # and is dropped at generation, so a fresh extraction of the matrix can
        # never contain it. Comparing raw text would mean the first real
        # `expires:` anyone adds breaks this check forever — which would train
        # everyone to ignore it, or to delete the field rather than the rule.
        if strip_metadata(dest.read_text()) == strip_metadata(rendered):
            print("warrant: rule intents match a fresh extraction of the matrix")
            return 0
        print(f"warrant: {dest} differs from a fresh extraction", file=sys.stderr)
        return 3

    dest.parent.mkdir(parents=True, exist_ok=True)
    dest.write_text(rendered)
    print(f"warrant: wrote {display(dest)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
