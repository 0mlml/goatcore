#!/usr/bin/env python3
"""ops/identity/generate.py — the identity-grammar SoT generator (ADR 1067).

Reads ops/identity/grammar.yaml and derives enforced artifacts + human tables
from it, one-way. This is the increment-1/2 core: it emits the manifest-schema
enum blocks and proves — via `--check` — that they reproduce the CURRENT schema's
{enum, description} byte-for-byte, before any target is flipped to generated.

Usage:
  generate.py --check              # gate: generated blocks == current schema blocks
  generate.py --emit <dimension>   # print the generated {enum, description} block
  generate.py --list               # list generatable dimensions

Increments 3-4 extend this to write the schema/Kratos/taxonomy/cert value-sets +
the descriptor schema + the human tables, with a git-diff regeneration gate.
"""
import argparse
import json
import sys
from pathlib import Path

try:
    import yaml
except ImportError:
    sys.exit("FAIL: PyYAML required (pip install pyyaml)")

HERE = Path(__file__).resolve().parent          # ops/identity/
REPO = HERE.parent.parent                        # repo root
GRAMMAR = HERE / "grammar.yaml"
MANIFEST_SCHEMA = REPO / "ops/operators/schema/operator-manifest.schema.json"
COALITION_FRAMEWORKS = HERE / "coalition-frameworks.yaml"

# dimension name -> JSON path (property chain) in the manifest schema whose
# {enum, description} that dimension owns. Only the clean per-field enums are
# mapped; composite dimensions (role_family/node_class/echelon = the auto_groups
# union) and reserved edge_types are generated in increment 3, not here.
MANIFEST_PATHS = {
    "affiliation.kind":             ["affiliation", "kind"],
    "affiliation.coalition":        ["affiliation", "coalition"],
    "affiliation.alliance_standing": ["affiliation", "alliance_standing"],
    "affiliation.sector":           ["affiliation", "sector"],
    "affiliation.gov_branch":       ["affiliation", "gov_branch"],
    # ADR 1171. Mapped from the start deliberately: a closed enum with no parity
    # gate is how a vocabulary and its schema drift apart in silence, and this
    # one governs whether a person's nation is allowed to change their org's.
    "affiliation.relationship":     ["affiliation", "relationship"],
    "affiliation.unit_code.scheme": ["affiliation", "unit_code", "scheme"],
    "affiliation.unit_code.source": ["affiliation", "unit_code", "source"],
    "identity.type":                ["identity", "type"],
    "identity.status":              ["identity", "status"],
    "identity.autonomy":            ["identity", "autonomy"],
    "netbird.role":                 ["netbird", "role"],
}


def load_grammar():
    with open(GRAMMAR) as f:
        return yaml.safe_load(f)


def emit_block(grammar, dim):
    """The generated {enum, description} block for a dimension (the SoT truth)."""
    d = grammar["dimensions"][dim]
    # Enum = the always-live values, then any demand-gated values (part of the
    # closed vocabulary + the manifest schema so an enabling goatnet's manifests
    # validate; the per-goatnet console gate governs exposure/acceptance, not the
    # schema — ADR 1077 affiliation.sector non-gov cluster). Declaration order.
    enum = list(d["values"].keys()) + list(d.get("gated_values", {}).keys())
    return {
        "enum": enum,
        "description": d["schema_description"],
    }


def schema_node(schema, chain):
    node = schema["properties"]
    for i, key in enumerate(chain):
        node = node[key]
        if i < len(chain) - 1:
            node = node["properties"]
    return node


def check():
    grammar = load_grammar()
    with open(MANIFEST_SCHEMA) as source_file:
        schema = json.load(source_file)
    ok = True
    for dim, chain in MANIFEST_PATHS.items():
        gen = emit_block(grammar, dim)
        cur = schema_node(schema, chain)
        cur_block = {"enum": cur.get("enum"), "description": cur.get("description")}
        if gen["enum"] != cur_block["enum"]:
            ok = False
            print(f"FAIL {dim}: enum drift\n  SoT:    {gen['enum']}\n  schema: {cur_block['enum']}")
        elif gen["description"] != cur_block["description"]:
            ok = False
            print(f"FAIL {dim}: description drift\n  SoT:    {gen['description']!r}\n  schema: {cur_block['description']!r}")
        else:
            print(f"OK   {dim}: {len(gen['enum'])} values reproduce byte-for-byte")
    ok = check_coalition_frameworks() and ok
    if not ok:
        print("\nFAIL: grammar.yaml/coalition-frameworks.yaml do not reproduce the current manifest schema.")
        return 1
    print(f"\nOK: all {len(MANIFEST_PATHS)} mapped dimensions reproduce the manifest schema byte-for-byte "
          f"(grammar v{grammar['version']}); coalition-frameworks reference valid.")
    return 0


def catalog():
    """Human-legible dump of the ENTIRE vocabulary — every dimension, value,
    authority, and definition in one read. The illustrative aggregate view."""
    grammar = load_grammar()
    dims = dict(grammar["dimensions"])
    if "edge_types" in grammar:
        dims["edge_types"] = grammar["edge_types"]
    print(f"identity grammar v{grammar['version']} — {len(dims)} vocabularies\n")
    for dim, d in dims.items():
        auth = d.get("authority", "?")
        print(f"### {dim}  [authority: {auth}]")
        for val, meta in d["values"].items():
            m = meta if isinstance(meta, dict) else {}
            a = m.get("authority", auth)
            flags = []
            if m.get("deprecated"):
                flags.append("DEPRECATED")
            if m.get("scope"):
                flags.append(f"scope={m['scope']}")
            if m.get("status") and m["status"] != "built":
                flags.append(m["status"].upper())
            tag = f" ({', '.join(flags)})" if flags else ""
            defn = " ".join(str(m.get("definition", "")).split())
            print(f"  - {val}  [{a}]{tag}\n      {defn}")
        print()
    return 0


def load_coalition_frameworks():
    with open(COALITION_FRAMEWORKS) as f:
        return yaml.safe_load(f)


def resolve_coalition(nation, framework=None, as_of=None):
    """Derive affiliation.coalition standing (member|partner) for a nation, as-of a
    date (default today), RELATIVE TO a framework (default = the registry's
    default_framework), from the effective-dated coalition-frameworks.yaml registry
    (ADR 1077). Unknown nation, a date before membership, or an unknown framework =>
    partner. This is the registry's canonical consumer — the same logic the goatnet
    registry resolver reuses with the goatnet's active framework."""
    from datetime import date
    if as_of is None:
        as_of = date.today()
    elif isinstance(as_of, str):
        as_of = date.fromisoformat(as_of)
    n = (nation or "").strip().upper()
    ref = load_coalition_frameworks()
    fw_id = (framework or ref.get("default_framework") or "").strip()
    fw = (ref.get("frameworks") or {}).get(fw_id)
    if not fw:
        return "partner"
    if n in {m.strip().upper() for m in fw.get("member_markers", [])}:
        return "member"
    for m in fw.get("members", []):
        if m["nation"].strip().upper() == n:
            since = m["member_since"]
            since = since if isinstance(since, date) else date.fromisoformat(str(since))
            if as_of < since:
                return "partner"                 # not yet a member as-of the date
            until = m.get("member_until")        # membership is an effective-dated
            if until:                            # RANGE — an ally that leaves the
                until = until if isinstance(until, date) else date.fromisoformat(str(until))
                if as_of >= until:               # framework flips back to partner
                    return "partner"             # from member_until onward.
            return "member"
    return "partner"


def check_coalition_frameworks():
    """Validate the framework registry: default_framework exists; each framework's
    members carry valid alpha-3 uppercase codes, ISO member_since dates, no dup
    nations within a framework."""
    from datetime import date
    ref = load_coalition_frameworks()
    frameworks = ref.get("frameworks") or {}
    ok = True
    if (ref.get("default_framework") or "") not in frameworks:
        ok = False; print(f"FAIL coalition-frameworks: default_framework {ref.get('default_framework')!r} not defined")
    total = 0
    for fw_id, fw in frameworks.items():
        seen = set()
        for m in fw.get("members", []):
            total += 1
            code = m.get("nation", "")
            if not (isinstance(code, str) and code.isupper() and code.isalpha() and 2 <= len(code) <= 3):
                ok = False; print(f"FAIL coalition-frameworks[{fw_id}]: bad nation code {code!r}")
            if code in seen:
                ok = False; print(f"FAIL coalition-frameworks[{fw_id}]: duplicate nation {code!r}")
            seen.add(code)
            since = m.get("member_since")
            since_d = None
            try:
                since_d = since if isinstance(since, date) else date.fromisoformat(str(since))
            except (ValueError, TypeError):
                ok = False; print(f"FAIL coalition-frameworks[{fw_id}]: bad member_since for {code!r}: {since!r}")
            until = m.get("member_until")   # optional — membership end (ally leaves)
            if until is not None:
                try:
                    until_d = until if isinstance(until, date) else date.fromisoformat(str(until))
                    if since_d and until_d < since_d:
                        ok = False; print(f"FAIL coalition-frameworks[{fw_id}]: member_until < member_since for {code!r}")
                except (ValueError, TypeError):
                    ok = False; print(f"FAIL coalition-frameworks[{fw_id}]: bad member_until for {code!r}: {until!r}")
    if ok:
        print(f"OK   coalition-frameworks: {len(frameworks)} framework(s), {total} members, valid codes + ISO dates")
    return ok


def main():
    ap = argparse.ArgumentParser()
    g = ap.add_mutually_exclusive_group(required=True)
    g.add_argument("--check", action="store_true")
    g.add_argument("--emit", metavar="DIMENSION")
    g.add_argument("--catalog", action="store_true", help="dump the whole vocabulary")
    g.add_argument("--list", action="store_true")
    g.add_argument("--resolve-coalition", metavar="NATION",
                   help="derive affiliation.coalition standing for a nation from coalition-frameworks.yaml")
    ap.add_argument("--framework", metavar="ID",
                    help="framework for --resolve-coalition (default = registry default_framework)")
    ap.add_argument("--as-of", metavar="YYYY-MM-DD",
                    help="effective date for --resolve-coalition (default today)")
    args = ap.parse_args()

    if args.resolve_coalition:
        print(resolve_coalition(args.resolve_coalition, args.framework, args.as_of))
        return 0
    if args.catalog:
        return catalog()
    if args.list:
        for d in MANIFEST_PATHS:
            print(d)
        return 0
    if args.emit:
        grammar = load_grammar()
        if args.emit not in grammar["dimensions"]:
            sys.exit(f"unknown dimension: {args.emit}")
        print(json.dumps(emit_block(grammar, args.emit), indent=2, ensure_ascii=False))
        return 0
    if args.check:
        return check()
    return None


if __name__ == "__main__":
    sys.exit(main())
