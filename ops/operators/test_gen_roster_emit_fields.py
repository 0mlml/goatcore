#!/usr/bin/env python3
"""gen-roster must EMIT the affiliation fields operators enter.

Regression for the A12 gap: manifests carry `sector`, `display_name`, and
`coalition`/`alliance_standing`, but the generator dropped all three — they
collected and silently discarded, so the roster looked emptier than the data.
This asserts they reach the org record, and that `alliance_standing` dual-reads
the legacy `standing`/`coalition` keys (ADR 1151), preferring the new key.
"""
from __future__ import annotations

import importlib.util
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
_spec = importlib.util.spec_from_file_location("gen_roster", HERE / "gen-roster.py")
gen_roster = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(gen_roster)

FAILS: list[str] = []


def check(name: str, cond: bool, detail: str = "") -> None:
    if cond:
        print(f"  ok  {name}")
    else:
        FAILS.append(name)
        print(f"FAIL  {name}  {detail}")


def person(handle: str, aff: dict) -> dict:
    return {
        "identity": {"handle": handle, "email": f"{handle}@x", "type": "person",
                     "status": "active", "added": "2026-09-02"},
        "affiliation": aff,
    }


def org_of(reg: dict, org_id: str) -> dict:
    for o in reg["orgs"]:
        if o["id"] == org_id:
            return o
    raise AssertionError(f"org {org_id} not in registry")


# 1. all three fields emit
reg = gen_roster.build_registry([
    person("a", {"id": "acme", "kind": "vendor", "nation": "USA",
                 "sector": "commercial", "display_name": "ACME Corp"}),
])
o = org_of(reg, "acme")
check("sector emitted", o.get("sector") == "commercial", repr(o))
check("display_name emitted", o.get("display_name") == "ACME Corp", repr(o))

# 2. alliance_standing dual-reads the legacy coalition key
reg = gen_roster.build_registry([
    person("b", {"id": "demo-nat-unit", "kind": "partner", "nation": "SVK", "coalition": "member"}),
])
o = org_of(reg, "demo-nat-unit")
check("alliance_standing dual-reads coalition", o.get("alliance_standing") == "member", repr(o))

# 3. alliance_standing wins over the legacy standing/coalition keys
reg = gen_roster.build_registry([
    person("c", {"id": "dual", "kind": "partner", "nation": "EST",
                 "alliance_standing": "member", "standing": "partner", "coalition": "partner"}),
])
o = org_of(reg, "dual")
check("alliance_standing wins over legacy keys", o.get("alliance_standing") == "member", repr(o))

# 4. absent fields are dropped, not emitted as null (roster stays clean)
reg = gen_roster.build_registry([
    person("d", {"id": "bare", "kind": "organic", "nation": "USA"}),
])
o = org_of(reg, "bare")
check("absent sector not emitted as key", "sector" not in o, repr(o))
check("absent display_name not emitted", "display_name" not in o, repr(o))
check("absent alliance_standing not emitted", "alliance_standing" not in o, repr(o))

# 5. a member nationality is alpha-3 or undeclared, never free text
def with_nationality(handle: str, nat: str) -> dict:
    m = person(handle, {"id": "natorg", "kind": "vendor", "nation": "ISL"})
    m["identity"]["nationality"] = nat
    return m


reg = gen_roster.build_registry([
    with_nationality("n1", "ISL"),
    with_nationality("n2", "gb"),  # oss-allow: public ISO table
    with_nationality("n3", "BRITISH"),
])
nat = {m["handle"]: m["nationality"] for m in reg["members"]}
check("alpha-3 nationality kept", nat["n1"] == "ISL", repr(nat))
check("alpha-2 nationality folded", nat["n2"] == "GBR", repr(nat))  # oss-allow: public ISO table
check("free-text nationality emitted as undeclared", nat["n3"] is None, repr(nat))
check("free-text nationality named in a warning",
      any("'n3'" in w and "BRITISH" in w for w in reg["warnings"]), repr(reg["warnings"]))

if FAILS:
    print(f"\nFAIL: {len(FAILS)} check(s) failed")
    sys.exit(1)
print("\nall gen-roster field-emission checks passed")
