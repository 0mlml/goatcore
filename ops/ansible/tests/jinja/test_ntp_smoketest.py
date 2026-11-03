#!/usr/bin/env python3
"""ops/ansible/tests/jinja/test_ntp_smoketest.py — test the NTP smoketest's
Jinja2 parsing logic against a real chrony_exporter /metrics fixture.

Would have caught the regex-vs-Jinja-split-vs-match confusion that cost
runs 5 + 6 + 7 of vega-bootstrap.yml on 2026-04-20.

Approach:
    1. Load ops/ansible/tests/fixtures/chrony_exporter_healthy.txt —
       a snapshot of a REAL exporter output from vega-a.
    2. Load ops/ansible/roles/ntp-hygiene/tasks/smoketest.yml.
    3. Extract the two set_fact Jinja expressions (offset + source count).
    4. Render them with a `jinja2.Environment` configured the same way
       Ansible does — filters, multiline-regex flags, etc.
    5. Assert: offset parses to a float in a sane range; source count > 0.

Run: python3 ops/ansible/tests/jinja/test_ntp_smoketest.py
Exit 0 = pass, 1 = fail.
"""
from __future__ import annotations

import pathlib
import re
import sys

try:
    import yaml
except ImportError:
    sys.stderr.write("pip install pyyaml jinja2\n")
    sys.exit(2)

try:
    from jinja2 import Environment
except ImportError:
    sys.stderr.write("pip install jinja2\n")
    sys.exit(2)


REPO_ROOT = pathlib.Path(__file__).resolve().parents[3].parent
FIXTURES = REPO_ROOT / "ops/ansible/tests/fixtures"
SMOKETEST_YML = REPO_ROOT / "ops/ansible/roles/ntp-hygiene/tasks/smoketest.yml"


def ansible_env() -> Environment:
    """Jinja2 environment with the Ansible-style filters we use.
    Not a faithful reimplementation of Ansible's full filter set — just
    the bits the smoketest uses."""
    env = Environment()
    # regex_findall — Ansible's version
    def regex_findall(value, pattern, multiline=False, ignorecase=False):
        flags = 0
        if multiline:
            flags |= re.MULTILINE
        if ignorecase:
            flags |= re.IGNORECASE
        return re.findall(pattern, value, flags=flags)
    env.filters["regex_findall"] = regex_findall

    def regex_search(value, pattern, *captures, multiline=False, ignorecase=False):
        flags = 0
        if multiline:
            flags |= re.MULTILINE
        if ignorecase:
            flags |= re.IGNORECASE
        m = re.search(pattern, value, flags=flags)
        if not m:
            return None
        if captures:
            # Ansible's convention: back-references can be passed as bare
            # digits, '\1'-style strings, or as ASCII control chars (the
            # unescaped-backslash quirk from YAML). Normalize.
            out = []
            for c in captures:
                if isinstance(c, str):
                    stripped = c.lstrip("\\").lstrip("\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09")
                    # If the result is digits, treat as a group number.
                    if stripped.isdigit():
                        out.append(m.group(int(stripped)))
                        continue
                    # The char \x01 itself means group 1 (YAML double-quote
                    # interpretation of '\1' gave us 0x01).
                    if len(c) == 1 and ord(c) <= 9:
                        out.append(m.group(ord(c)))
                        continue
                try:
                    out.append(m.group(c))
                except IndexError:
                    out.append(None)
            return out
        return m.group(0)
    env.filters["regex_search"] = regex_search

    env.filters["first"] = lambda x: x[0] if x else None
    env.filters["length"] = len
    env.filters["float"] = float
    env.filters["int"] = int
    env.filters["abs"] = abs
    env.filters["default"] = lambda x, d: x if x is not None else d

    return env


def extract_jinja(smoketest_yml: pathlib.Path, task_name_substr: str) -> str:
    """Find the set_fact task whose name contains task_name_substr and
    return the first value in its set_fact dict as a Jinja string."""
    with smoketest_yml.open() as f:
        tasks = yaml.safe_load(f) or []
    for t in tasks:
        if not isinstance(t, dict):
            continue
        if task_name_substr not in (t.get("name") or ""):
            continue
        sf = t.get("ansible.builtin.set_fact") or t.get("set_fact") or {}
        if not sf:
            continue
        (_, expr), *_ = sf.items()
        return str(expr)
    raise KeyError(f"no set_fact task matching {task_name_substr!r}")


def main() -> int:
    fixture = FIXTURES / "chrony_exporter_healthy.txt"
    if not fixture.exists():
        print(f"FAIL — missing fixture {fixture}", file=sys.stderr)
        return 1
    content = fixture.read_text()

    env = ansible_env()
    ok = True

    # Test 1: source count — must be > 0 on a healthy exporter.
    source_expr = extract_jinja(SMOKETEST_YML, "count configured chrony sources")
    # Strip leading/trailing {{ }} if Ansible's `>-` block preserved them.
    source_expr = source_expr.strip()
    if source_expr.startswith("{{"):
        source_expr = source_expr[2:].rsplit("}}", 1)[0].strip()
    # Put content in a var
    rendered = env.from_string("{{ " + source_expr + " | int }}").render(
        _smoketest_metrics={"content": content}
    )
    try:
        count = int(rendered)
    except ValueError:
        print(f"FAIL — source-count Jinja rendered non-int: {rendered!r}")
        return 1
    if count <= 0:
        print(f"FAIL — source-count = {count} on HEALTHY fixture (expected > 0)")
        print(f"       Jinja expr: {source_expr}")
        ok = False
    else:
        print(f"PASS — source-count = {count} on healthy fixture")

    # Test 2: offset — must parse to a small float.
    offset_expr = extract_jinja(SMOKETEST_YML, "extract chrony_tracking_last_offset")
    offset_expr = offset_expr.strip()
    if offset_expr.startswith("{{"):
        offset_expr = offset_expr[2:].rsplit("}}", 1)[0].strip()
    rendered = env.from_string("{{ " + offset_expr + " }}").render(
        _smoketest_metrics={"content": content}
    )
    try:
        offset = float(rendered)
    except ValueError:
        print(f"FAIL — offset Jinja rendered non-float: {rendered!r}")
        return 1
    if abs(offset) > 1.0:
        print(f"FAIL — offset = {offset}s out of sane range (|.| > 1.0) on healthy fixture")
        ok = False
    else:
        print(f"PASS — offset = {offset:.3g}s on healthy fixture")

    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
