#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""check-sovereign-pkis.py — the sovereign PKI registry is well-formed and every
artefact generated from it is current.

Registry: ops/identity/sovereign-pkis.yaml. Decision: docs/adr/1209. Library:
ops/identity/sovereign_pkis.py (the rules are there; this file only runs them).

WHAT IT REFUSES
  1. a registry that fails sovereign_pkis.validate(): a malformed profile, a live
     profile with unverified fields or a missing trust anchor / revocation /
     example, an identity rule or issuer_match that does not compile, an
     identifier_regex without exactly one capture group, an example card the
     profile's own rule does not resolve to its identifier, a source_scheme that
     grammar.yaml does not know;
  2. a committed generated artefact that differs from what the registry renders:
     ops/identity/claim-maps/sovereign-pki.jsonnet;
  3. a live profile whose nginx maps the role would not be able to render (the
     filter plugin raises).

Usage:
  ops/ci/check-sovereign-pkis.py [repo-root]
  ops/ci/check-sovereign-pkis.py --self-test

Exit: 0 clean · 1 a violation (each listed) · 2 self-test failure
"""
import copy
import pathlib
import sys

import yaml

ROOT = pathlib.Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "ops/identity"))
import sovereign_pkis as sp  # noqa: E402

GENERATED = {"ops/identity/claim-maps/sovereign-pki.jsonnet": sp.claim_map}


def grammar_schemes(root):
    g = yaml.safe_load((root / "ops/identity/grammar.yaml").read_text(encoding="utf-8")) or {}
    dim = (g.get("dimensions") or {}).get("identity.source_scheme") or {}
    return set((dim.get("values") or {}).keys())


def check(root):
    root = pathlib.Path(root)
    problems = []
    reg = sp.load(root / "ops/identity/sovereign-pkis.yaml")
    problems += [f"registry: {p}" for p in sp.validate(reg, grammar_schemes(root))]
    if problems:
        return problems
    live = sp.live(reg)
    for rel, render in GENERATED.items():
        path = root / rel
        if not path.exists():
            problems.append(f"{rel}: missing; run ops/identity/sovereign-pkis-render.py {rel.split('/')[-1].split('.')[0]}")
            continue
        if path.read_text(encoding="utf-8") != render(live):
            problems.append(f"{rel}: differs from the registry; regenerate with ops/identity/sovereign-pkis-render.py claim-map > {rel}")
    try:
        sp.nginx_maps(live)
    except Exception as e:  # noqa: BLE001 - any render failure is a violation
        problems.append(f"nginx maps do not render for the live profiles: {e}")
    return problems


def self_test():
    reg = sp.load(ROOT / "ops/identity/sovereign-pkis.yaml")
    schemes = grammar_schemes(ROOT)
    assert not sp.validate(reg, schemes), "the committed registry must be clean for the self-test"
    bad = copy.deepcopy(reg)
    bad["pkis"][0]["identity"]["identifier_regex"] = r"^\d{10}@mil$"   # no capture group
    assert any("capture group" in p for p in sp.validate(bad, schemes)), "no-group regex must be refused"
    bad = copy.deepcopy(reg)
    bad["pkis"][0]["example"]["san_upn"] = "notanumber@mil"
    assert any("yields" in p for p in sp.validate(bad, schemes)), "an example the rule cannot resolve must be refused"
    bad = copy.deepcopy(reg)
    bad["pkis"][0]["unverified"] = ["issuer_match"]
    assert any("unverified" in p for p in sp.validate(bad, schemes)), "a live profile with unverified fields must be refused"
    bad = copy.deepcopy(reg)
    bad["pkis"][0]["nation"] = "US"
    assert any("alpha-3" in p for p in sp.validate(bad, schemes)), "a two-letter nation must be refused"
    bad = copy.deepcopy(reg)
    bad["pkis"][0]["source_scheme"] = "made-up"
    assert any("source_scheme" in p for p in sp.validate(bad, schemes)), "an unknown source_scheme must be refused"
    return 0


def main(argv):
    if argv and argv[0] == "--self-test":
        try:
            self_test()
        except AssertionError as e:
            print(f"self-test FAILED: {e}")
            return 2
        print("self-test ok")
        return 0
    root = argv[0] if argv else str(ROOT)
    problems = check(root)
    for p in problems:
        print("check-sovereign-pkis:", p)
    if problems:
        return 1
    print("check-sovereign-pkis: registry and generated artefacts agree")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
