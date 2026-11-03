#!/usr/bin/env python3
"""Check committed org catalogs against their generator, without network or writes.

This checks generated form, not freshness against the live organization store.
Exit 0 means every catalog matches; exit 1 means drift or an unreadable input.
"""

import argparse
import difflib
import importlib.util
import sys
from pathlib import Path

import yaml

REPO = Path(__file__).resolve().parents[2]
CATALOG_DIR = REPO / "ops/identity/orgs"
SPEC = importlib.util.spec_from_file_location(
    "sync_org_catalog", REPO / "ops/scripts/sync-org-catalog.py")
SYNC = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SYNC)


def check_catalog(path):
    """Return whether one catalog exactly matches render_catalog's output."""
    try:
        current = path.read_bytes().decode("utf-8")
        try:
            catalog = yaml.safe_load(current)
        except Exception as exc:  # SafeLoader constructors also raise built-in exceptions.
            raise ValueError(f"invalid YAML catalog: {exc}") from exc
        if not isinstance(catalog, dict) or not isinstance(catalog.get("orgs"), list):
            raise ValueError("expected a mapping with an orgs list")
        for row in catalog["orgs"]:
            if not isinstance(row, dict) or not isinstance(row.get("id"), str) or not row["id"].strip():
                raise ValueError("each org must be a mapping with a non-empty string id")
            aliases = row.get("aliases")
            if aliases is not None and (
                not isinstance(aliases, list) or any(not isinstance(alias, str) for alias in aliases)
            ):
                raise ValueError("org aliases must be a list of strings")
        rendered = SYNC.render_catalog(catalog, path.stem)
    except (OSError, UnicodeError, yaml.YAMLError, ValueError) as exc:
        print(f"ERROR: {path}: {exc}", file=sys.stderr)
        return False
    if current != rendered:
        print(f"DRIFT: {path}: differs from sync-org-catalog.render_catalog", file=sys.stderr)
        diff = difflib.unified_diff(
            current.splitlines(keepends=True), rendered.splitlines(keepends=True),
            fromfile=str(path), tofile=f"{path} (generated)")
        sys.stderr.writelines(diff)
        return False
    print(f"OK: {path.name} matches generated form ({len(catalog['orgs'])} orgs)")
    return True


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--catalog-dir", type=Path, default=CATALOG_DIR,
                        help="directory containing committed YAML catalogs")
    args = parser.parse_args(argv)
    try:
        paths = sorted(path for path in args.catalog_dir.iterdir() if path.suffix in (".yaml", ".yml"))
    except OSError as exc:
        print(f"ERROR: {args.catalog_dir}: {exc}", file=sys.stderr)
        return 1
    if not paths:
        print(f"ERROR: {args.catalog_dir}: no YAML catalogs found", file=sys.stderr)
        return 1
    results = [check_catalog(path) for path in paths]
    return 0 if all(results) else 1


if __name__ == "__main__":
    sys.exit(main())
