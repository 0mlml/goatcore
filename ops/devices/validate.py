#!/usr/bin/env python3
"""Validate a device manifest against the Phase F.4 jsonschema.

Usage:
    ./validate.py <manifest.yml> [<manifest.yml> ...]
    ./validate.py --report [<directory>]

Exit codes:
    0 — all manifests validate cleanly
    1 — at least one manifest failed validation
    2 — usage / file-not-found / jsonschema-not-installed error

`goat admin client onboard <handle>` calls this in pre-flight via
ops/devices/validate.py; failures halt before bundle-create runs.
"""
from __future__ import annotations

import json
import sys
from pathlib import Path

try:
    import yaml
except ImportError:
    print("ERROR: PyYAML is required. Install via: pip install pyyaml", file=sys.stderr)
    sys.exit(2)

try:
    from jsonschema import Draft7Validator
except ImportError:
    print("ERROR: jsonschema is required. Install via: pip install jsonschema", file=sys.stderr)
    sys.exit(2)


SCHEMA_PATH = Path(__file__).parent / "schema" / "device-manifest.schema.json"


def load_schema() -> dict:
    if not SCHEMA_PATH.exists():
        print(f"ERROR: schema not found at {SCHEMA_PATH}", file=sys.stderr)
        sys.exit(2)
    with SCHEMA_PATH.open() as f:
        return json.load(f)


def validate_manifest(path: Path, validator: Draft7Validator) -> list[str]:
    if not path.exists():
        return [f"file not found: {path}"]
    try:
        with path.open() as f:
            data = yaml.safe_load(f)
    except yaml.YAMLError as e:
        return [f"YAML parse error: {e}"]

    # PyYAML auto-parses ISO dates into datetime.date; coerce back to
    # ISO string before jsonschema's format=date check.
    import datetime as _dt

    def _coerce(node):
        if isinstance(node, dict):
            return {k: _coerce(v) for k, v in node.items()}
        if isinstance(node, list):
            return [_coerce(x) for x in node]
        if isinstance(node, (_dt.date, _dt.datetime)):
            return node.isoformat()
        return node

    data = _coerce(data)
    if data is None:
        return ["empty manifest"]

    errors = []
    for err in sorted(validator.iter_errors(data), key=lambda e: list(e.absolute_path)):
        loc = ".".join(str(p) for p in err.absolute_path) or "<root>"
        errors.append(f"{loc}: {err.message}")

    # Cross-field: filename stem must match identity.handle.
    if not errors and not str(path).endswith(".yml.example"):
        identity = data.get("identity", {})
        handle = identity.get("handle")
        stem = path.stem
        if handle and stem != handle:
            errors.append(
                f"<filename>: handle '{handle}' does not match filename stem '{stem}'. "
                f"Rename to '{handle}.yml' or fix identity.handle."
            )

    return errors


def run_report(manifests_dir: Path | None = None) -> int:
    schema = load_schema()
    Draft7Validator.check_schema(schema)
    validator = Draft7Validator(schema)

    if manifests_dir is None:
        manifests_dir = Path(__file__).parent
    files = sorted(
        p for p in manifests_dir.glob("*.yml")
        if not p.name.endswith(".yml.example")
    )

    if not files:
        print(f"(no device manifests at {manifests_dir})")
        return 0

    rows: list[tuple[str, str, str]] = []
    for path in files:
        errors = validate_manifest(path, validator)
        if errors:
            rows.append((path.name, "INVALID", "; ".join(errors[:1])))
            continue
        try:
            with path.open() as f:
                data = yaml.safe_load(f)
        except yaml.YAMLError as e:
            rows.append((path.name, "INVALID", f"YAML parse error: {e}"))
            continue
        role = data.get("identity", {}).get("role", "?")
        site = data.get("device", {}).get("site", "?")
        status = data.get("identity", {}).get("status", "?")
        rows.append((path.name, status, f"role={role} site={site}"))

    name_w = max(len(r[0]) for r in rows)
    status_w = max(len(r[1]) for r in rows)
    print(f"{'manifest'.ljust(name_w)}  {'status'.ljust(status_w)}  detail")
    print(f"{'-' * name_w}  {'-' * status_w}  {'-' * 40}")
    for name, status, note in rows:
        print(f"{name.ljust(name_w)}  {status.ljust(status_w)}  {note}")

    any_failed = any(s == "INVALID" for _, s, _ in rows)
    return 1 if any_failed else 0


def main(argv: list[str]) -> int:
    if len(argv) >= 2 and argv[1] == "--report":
        manifests_dir = Path(argv[2]).resolve() if len(argv) >= 3 else None
        if manifests_dir is not None and not manifests_dir.is_dir():
            print(f"ERROR: --report directory does not exist: {manifests_dir}", file=sys.stderr)
            return 2
        return run_report(manifests_dir)

    if len(argv) < 2:
        print("Usage: validate.py <manifest.yml> [<manifest.yml> ...]", file=sys.stderr)
        print("       validate.py --report", file=sys.stderr)
        return 2

    schema = load_schema()
    Draft7Validator.check_schema(schema)
    validator = Draft7Validator(schema)

    any_failed = False
    for raw in argv[1:]:
        path = Path(raw)
        errors = validate_manifest(path, validator)
        if errors:
            any_failed = True
            print(f"FAIL: {path}")
            for err in errors:
                print(f"  - {err}")
        else:
            print(f"OK:   {path}")

    return 1 if any_failed else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
