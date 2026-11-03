#!/usr/bin/env python3
"""Unit test for the org-catalog overlay in gen-roster.py (ADR 1112 increment
1, docs/design/org-hierarchy-and-dedup.md §13.1). Standalone, no pytest —
same style as test_gen_roster_collab.py. Run:
python3 ops/operators/test_gen_roster_orgstore.py

What it locks:

  - **The parity gate.** With an EMPTY catalog, apply_org_catalog is a strict
    no-op: the registry (and its content digest) is byte-identical to the
    manifests-only build. This is the proof the increment breaks nothing
    while dormant (design §13.1's byte-identical golden test).
  - **Catalog wins, loudly.** A catalog parent/kind/nation overrides the
    manifest-derived value AND emits a warning naming both values — never a
    silent override.
  - **Member-less ancestors become real org rows** (roots of the chain the
    portal resolver's parent walk must pass through), sorted into place, with
    empty members — and they do NOT appear in the flat member list.
  - **The overlay never touches members** (nested or flat).
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


def manifest(handle: str, email: str, org_id: str | None, parent: str | None = None) -> dict:
    m = {
        "identity": {
            "handle": handle,
            "email": email,
            "type": "person",
            "status": "active",
            "added": "2026-08-04",
        }
    }
    if org_id:
        aff = {"id": org_id, "kind": "partner", "nation": "SVN"}
        if parent:
            aff["parent"] = parent
        m["affiliation"] = aff
    return m


def main() -> int:
    manifests = [
        manifest("user228x", "a@example.test", "org123x"),
        manifest("pat-two", "b@example.test", "org130x"),
        manifest("pat-three", "c@example.test", "org130x"),
    ]

    # --- parity gate: empty catalog is a strict no-op --------------------
    baseline = gen_roster.build_registry(manifests)
    baseline_digest = gen_roster.content_digest(baseline)

    overlaid = gen_roster.apply_org_catalog(
        gen_roster.build_registry(manifests), {"orgs": []}
    )
    check(
        "empty catalog: registry identical",
        overlaid == baseline,
        "apply_org_catalog mutated the registry with an empty catalog",
    )
    check(
        "empty catalog: content digest identical",
        gen_roster.content_digest(overlaid) == baseline_digest,
    )

    # --- overlay: ancestors, parent attach, catalog-wins -----------------
    catalog = {
        "orgs": [
            {"id": "org125x", "kind": "partner", "nation": "SVN"},
            {"id": "org123x", "parent": "org125x"},
            {"id": "org130x", "parent": "org125x"},
        ]
    }
    reg = gen_roster.apply_org_catalog(gen_roster.build_registry(manifests), catalog)
    by_id = {o["id"]: o for o in reg["orgs"]}

    check(
        "member-less ancestor added as an org row",
        "org125x" in by_id and by_id["org125x"]["members"] == [],
        f"orgs: {sorted(by_id)}",
    )
    check(
        "children gained the catalog parent",
        by_id["org123x"]["parent"] == "org125x"
        and by_id["org130x"]["parent"] == "org125x",
    )
    check(
        "orgs stay sorted after ancestor insertion",
        [o["id"] for o in reg["orgs"]] == sorted(o["id"] for o in reg["orgs"]),
    )
    check(
        "flat member list untouched (no ancestor members, same size)",
        len(reg["members"]) == 3
        and all(m["org"] != "org125x" for m in reg["members"]),
    )
    check(
        "nested members untouched",
        len(by_id["org130x"]["members"]) == 2,
    )

    # --- catalog-wins is loud, never silent ------------------------------
    conflict_catalog = {"orgs": [{"id": "org123x", "nation": "USA"}]}
    reg2 = gen_roster.apply_org_catalog(gen_roster.build_registry(manifests), conflict_catalog)
    by_id2 = {o["id"]: o for o in reg2["orgs"]}
    check(
        "catalog value wins on conflict",
        by_id2["org123x"]["nation"] == "USA",
    )
    check(
        "conflict override emits a warning naming both values",
        any("catalog nation 'USA' overrides" in w and "SVN" in w for w in reg2["warnings"]),
        f"warnings: {reg2['warnings']}",
    )

    # --- rendering survives member-less orgs ------------------------------
    md = gen_roster.render_markdown(reg)
    check("markdown renders the ancestor section", "## org125x" in md)

    # --- composition survives the overlay (ADR 1171) -----------------------
    # It reached the live registry, the catalog dropped it, and the wrap
    # intersection then read every multinational org as national and refused
    # its members. Three places had to carry it; this pins the last one.
    reg_c = {
        "orgs": [{"id": "org79x", "kind": "partner", "nation": None, "parent": None,
                  "members": [{"handle": "bel", "state": "active"}]}],
        "members": [{"handle": "bel", "org": "org79x"}],
        "warnings": [],
    }
    out_c = gen_roster.apply_org_catalog(reg_c, {"orgs": [
        {"id": "org79x", "nation": "NATO", "composition": "multinational"},
        {"id": "ancestor-only", "nation": "USA", "composition": "national"},
    ]})
    lc = next(o for o in out_c["orgs"] if o["id"] == "org79x")
    check("composition reaches an org with members", lc.get("composition") == "multinational", f"got {lc.get('composition')}")
    anc = next(o for o in out_c["orgs"] if o["id"] == "ancestor-only")
    check("composition reaches a member-less ancestor row", anc.get("composition") == "national", f"got {anc.get('composition')}")

    reg_d = {"orgs": [{"id": "plain", "kind": "vendor", "nation": None, "parent": None, "members": []}],
             "members": [], "warnings": []}
    out_d = gen_roster.apply_org_catalog(reg_d, {"orgs": [{"id": "plain", "nation": "USA"}]})
    check("an undeclared composition is omitted, not emitted empty",
          "composition" not in next(o for o in out_d["orgs"] if o["id"] == "plain"))

    # --- alias folding (ADR 1112 org tidy) ---------------------------------
    # Until this landed, gen-roster.py never read `aliases`, so every alias the
    # registry recorded was inert in the projection the portal consumes and the
    # people stayed under a per-person slug carrying no nation.
    reg_a = {
        "orgs": [
            {"id": "org63x", "kind": "vendor", "nation": None, "parent": None,
             "members": [{"handle": "real-one", "state": "active"}]},
            {"id": "org65x", "kind": "vendor", "nation": None, "parent": None,
             "members": [{"handle": "user903x", "state": "active"}]},
            {"id": "orphan-alias", "kind": "vendor", "nation": "POL", "parent": None,
             "members": [{"handle": "solo", "state": "active"}]},
        ],
        "members": [
            {"handle": "real-one", "org": "org63x"},
            {"handle": "user903x", "org": "org65x"},
            {"handle": "solo", "org": "orphan-alias"},
        ],
        "warnings": [],
    }
    cat_a = {"orgs": [
        {"id": "org63x", "nation": "USA", "aliases": ["org65x"]},
        {"id": "no-manifest-members", "nation": "POL", "aliases": ["orphan-alias"]},
    ]}
    out = gen_roster.apply_org_catalog(reg_a, cat_a)
    ids = {o["id"] for o in out["orgs"]}
    check("the alias org is gone", "org65x" not in ids, f"got {sorted(ids)}")
    org63x = next(o for o in out["orgs"] if o["id"] == "org63x")
    check("its member moved to the canonical org",
          sorted(m["handle"] for m in org63x["members"]) == sorted(["user903x", "real-one"]),
          f"got {[m['handle'] for m in org63x['members']]}")
    check("the flat list follows", {m["handle"]: m["org"] for m in out["members"]}["user903x"] == "org63x")
    check("the folded member inherits the canonical nation", org63x["nation"] == "USA")
    check("an alias whose canonical org has no manifest members becomes it",
          "no-manifest-members" in ids and "orphan-alias" not in ids, f"got {sorted(ids)}")
    check("nobody is lost", len(out["members"]) == 3, f"got {len(out['members'])}")
    check("a sole member of an alias that becomes its canonical org follows it in the flat list",
          {m["handle"]: m["org"] for m in out["members"]}["solo"] == "no-manifest-members",
          f"got {[(m['handle'], m['org']) for m in out['members']]}")

    reg_b = {
        "orgs": [
            {"id": "acme-partner", "kind": "partner", "nation": "POL", "parent": None, "members": []},
            {"id": "acme-runner", "kind": "vendor", "nation": "POL", "parent": None, "members": []},
        ],
        "members": [], "warnings": [],
    }
    out_b = gen_roster.apply_org_catalog(
        reg_b, {"orgs": [{"id": "acme-partner", "kind": "partner", "aliases": ["acme-runner"]}]}
    )
    check("a fact asserted on an alias warns and loses",
          any("an alias is not a separate organisation" in w for w in out_b["warnings"])
          and next(o for o in out_b["orgs"] if o["id"] == "acme-partner")["kind"] == "partner",
          f"warnings: {out_b['warnings']}")

    # --- catalog UNION across goatnets (roster.json is one artifact) -------
    # `--env` used to default to zagros, so store mode read ONE goatnet's catalog
    # and shipped it to every goatnet. These assert the union and that a real
    # disagreement is reported rather than silently resolved.
    import tempfile, pathlib
    d = pathlib.Path(tempfile.mkdtemp())
    (d / "alpha.yaml").write_text(
        "orgs:\n  - id: shared\n    nation: USA\n  - id: only-alpha\n    nation: POL\n"
    )
    (d / "beta.yaml").write_text(
        "orgs:\n  - id: shared\n    nation: USA\n  - id: only-beta\n    nation: SVK\n"
    )
    warns = []
    merged = gen_roster.merge_org_catalogs(
        [d / "alpha.yaml", d / "beta.yaml"], warns
    )
    ids = {o["id"] for o in merged["orgs"]}
    check("union carries orgs from every catalog", ids == {"shared", "only-alpha", "only-beta"}, f"got {sorted(ids)}")
    check("an org in two catalogs appears once", len(merged["orgs"]) == 3, f"got {len(merged['orgs'])}")
    check("agreeing catalogs produce no warning", warns == [], f"warnings: {warns}")

    (d / "gamma.yaml").write_text("orgs:\n  - id: shared\n    nation: CAN\n")
    warns2 = []
    merged2 = gen_roster.merge_org_catalogs(
        [d / "alpha.yaml", d / "gamma.yaml"], warns2
    )
    check(
        "disagreeing catalogs warn and keep the first",
        any("catalogs disagree on nation" in w for w in warns2)
        and next(o for o in merged2["orgs"] if o["id"] == "shared")["nation"] == "USA",
        f"warnings: {warns2}",
    )

    print()
    if FAILS:
        print(f"{len(FAILS)} FAILURE(S): {', '.join(FAILS)}")
        return 1
    print("all org-catalog overlay checks passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
