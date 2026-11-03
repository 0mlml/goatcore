#!/usr/bin/env python3
"""Unit test for the per-goatnet scoping in gen-roster.py (ADR 1172).
Standalone, no pytest — same style as test_gen_roster_orgstore.py. Run:
python3 ops/operators/test_gen_roster_per_goatnet.py

What it locks — one rule, stated three ways, because every way it can break
puts one environment's people on another environment's host:

  - **A goatnet's artifact reads that goatnet's folder and nothing else.**
    `_manifest_paths(base, "zagros-prod")` returns only
    `ops/operators/zagros-prod/*.yml`. Not the sibling goatnet's folder, and not
    the flat `ops/operators/*.yml` set, which names no goatnet and whose
    inclusion would rebuild the union this split removes.
  - **The union still unions.** With no env, the gatherer keeps its old
    behaviour — flat plus one level of per-env subfolders — because
    docs/operators/roster.json remains the controller-side index and repo-side
    tooling reads it.
  - **The artifacts do not collide.** `artifact_paths` sends a goatnet's run
    to docs/operators/<goatnet>/ and only an unscoped run to the union path, so
    regenerating zagros cannot overwrite what zagros-prod's portal is served.
"""
from __future__ import annotations

import importlib.util
import sys
import tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
_spec = importlib.util.spec_from_file_location("gen_roster", HERE / "gen-roster.py")
gen_roster = importlib.util.module_from_spec(_spec)
sys.modules["gen_roster"] = gen_roster
_spec.loader.exec_module(gen_roster)

failures: list[str] = []


def check(label: str, cond: bool) -> None:
    print(f"  {'ok ' if cond else 'FAIL'} {label}")
    if not cond:
        failures.append(label)


def main() -> int:
    with tempfile.TemporaryDirectory() as td:
        base = Path(td)
        (base / "flat-person.yml").write_text("identity: {}\n")
        for env in ("zagros", "zagros-prod"):
            (base / env).mkdir()
            (base / env / f"{env}-person.yml").write_text("identity: {}\n")
        # A non-env subdir the gatherer has always skipped.
        (base / "schema").mkdir()
        (base / "schema" / "not-a-manifest.yml").write_text("identity: {}\n")

        prod = [p.name for p in gen_roster._manifest_paths(base, "zagros-prod")]
        check("a goatnet run reads only its own folder", prod == ["zagros-prod-person.yml"])
        check("the flat set is excluded from a goatnet run", "flat-person.yml" not in prod)

        zagros = [p.name for p in gen_roster._manifest_paths(base, "zagros")]
        check("the sibling goatnet's folder is excluded", zagros == ["zagros-person.yml"])

        union = sorted(p.name for p in gen_roster._manifest_paths(base))
        check(
            "the union is still flat + every per-env subfolder",
            union == sorted(["zagros-person.yml", "zagros-prod-person.yml", "flat-person.yml"]),
        )
        check("the union still skips non-env subdirs", "not-a-manifest.yml" not in union)

        # An absent goatnet folder is empty, never a silent fall-through to the
        # flat set — main() turns this into a hard error naming the sync tool.
        check("an absent goatnet folder yields nothing", gen_roster._manifest_paths(base, "zagros-dev") == [])

    md_union, json_union = gen_roster.artifact_paths(None)
    md_prod, json_prod = gen_roster.artifact_paths("zagros-prod")
    md_zagros, json_zagros = gen_roster.artifact_paths("zagros")
    check("no env writes the controller-side union", json_union.name == "roster.json" and json_union.parent.name == "operators")
    check("a goatnet writes under its own name", json_prod.parent.name == "zagros-prod" and md_prod.parent.name == "zagros-prod")
    check("two goatnets never share an artifact path", json_prod != json_zagros and md_prod != md_zagros)
    check("a goatnet never writes the union path", json_prod != json_union and md_zagros != md_union)

    if failures:
        print(f"\nFAILED ({len(failures)}): " + "; ".join(failures), file=sys.stderr)
        return 1
    print("\nall ADR 1172 per-goatnet scoping checks passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
