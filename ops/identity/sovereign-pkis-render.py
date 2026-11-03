#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""sovereign-pkis-render.py — render the one PKI registry into every door's shape.

  sovereign-pkis-render.py check                      validate the registry (what CI runs)
  sovereign-pkis-render.py list                       id, status, nation, identity rule
  sovereign-pkis-render.py terraform                  collab-services var.sovereign_pkis default block
  sovereign-pkis-render.py openbao-dir --out DIR [--bundle ID=PEM ...]
                                                      the store's OPENBAO_SOVEREIGN_PKI_DIR tree
  sovereign-pkis-render.py nginx-maps [--pki ID ...]  the icam-mtls-proxy map blocks
  sovereign-pkis-render.py nginx-js [--pki ID ...]    pki-profiles.js (san_othername profiles only)
  sovereign-pkis-render.py claim-map                  ops/identity/claim-maps/sovereign-pki.jsonnet
  sovereign-pkis-render.py openssl-cnf ID [--identifier X] [--email E] [--ca-subj | --print-identifier]
                                                      an openssl req config for a synthetic card

Only `status: live` profiles render unless --include-sketch is given (nginx-maps and
openssl-cnf accept a sketch profile by id for the proofs). Registry:
ops/identity/sovereign-pkis.yaml. Design: docs/adr/1209.
"""
import argparse
import pathlib
import shutil
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import sovereign_pkis as sp  # noqa: E402

GRAMMAR = pathlib.Path(__file__).resolve().parent / "grammar.yaml"


def grammar_schemes():
    import yaml
    g = yaml.safe_load(GRAMMAR.read_text(encoding="utf-8")) or {}
    dim = (g.get("dimensions") or {}).get("identity.source_scheme") or {}
    return set((dim.get("values") or {}).keys()) or None


def select(reg, ids, include_sketch):
    pool = sp.pkis(reg) if include_sketch else sp.live(reg)
    if not ids:
        return pool
    chosen = []
    for i in ids:
        p = sp.by_id(reg, i)
        if p["status"] != "live" and not include_sketch:
            sys.exit(f"{i} is a sketch profile; pass --include-sketch to render it")
        chosen.append(p)
    return chosen


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--registry", default=str(sp.REGISTRY))
    ap.add_argument("--include-sketch", action="store_true")
    sub = ap.add_subparsers(dest="cmd", required=True)
    sub.add_parser("check")
    sub.add_parser("list")
    sub.add_parser("terraform")
    p = sub.add_parser("openbao-dir"); p.add_argument("--out", required=True); p.add_argument("--bundle", action="append", default=[])
    p.add_argument("--allow-missing", action="store_true")
    p = sub.add_parser("nginx-maps"); p.add_argument("--pki", action="append", default=[])
    p = sub.add_parser("nginx-js"); p.add_argument("--pki", action="append", default=[])
    sub.add_parser("claim-map")
    p = sub.add_parser("openssl-cnf"); p.add_argument("id"); p.add_argument("--identifier"); p.add_argument("--email")
    p.add_argument("--ca-subj", action="store_true", help="print the test CA -subj instead of the leaf config")
    p.add_argument("--print-identifier", action="store_true", help="print the identifier the card will carry")
    a = ap.parse_args(argv)

    reg = sp.load(a.registry)
    problems = sp.validate(reg, grammar_schemes())
    if a.cmd == "check":
        for pr in problems:
            print("sovereign-pkis:", pr)
        print(f"sovereign-pkis: {len(sp.pkis(reg))} profiles, {len(sp.live(reg))} live, {len(problems)} problem(s)")
        return 1 if problems else 0
    if problems:
        for pr in problems:
            print("sovereign-pkis:", pr, file=sys.stderr)
        sys.exit("registry has problems; fix them before rendering")

    if a.cmd == "list":
        for p in sp.pkis(reg):
            ident = p.get("identity") or {}
            rule = ident.get("source", "-") + (f" {ident['identifier_regex']}" if ident.get("identifier_regex") else "")
            print(f"{p['id']:<12} {p['status']:<7} {p['nation']:<5} {p['identifier_name']:<40} {rule}")
        return 0
    if a.cmd == "terraform":
        sys.stdout.write(sp.terraform_value(sp.live(reg)))
        return 0
    if a.cmd == "openbao-dir":
        out = pathlib.Path(a.out)
        bundles = dict(b.split("=", 1) for b in a.bundle)
        missing = []
        for rel, content in sp.openbao_layout(sp.live(reg)).items():
            dest = out / rel
            dest.parent.mkdir(parents=True, exist_ok=True)
            if content is None:
                pki_id = rel.split("/")[0]
                if pki_id in bundles:
                    shutil.copyfile(bundles[pki_id], dest)
                elif not dest.exists():
                    missing.append(rel)
            else:
                dest.write_text(content, encoding="utf-8")
        for m in missing:
            print(f"openbao-dir: {out / m} is missing; place the PKI's CA bundle there (see its SOURCE file)", file=sys.stderr)
        if missing and not a.allow_missing:
            return 1
        print(f"openbao-dir: {out} holds {len(sp.live(reg))} profile(s); export OPENBAO_SOVEREIGN_PKI_DIR={out}")
        return 0
    if a.cmd == "nginx-maps":
        sys.stdout.write(sp.nginx_maps(select(reg, a.pki, a.include_sketch)))
        return 0
    if a.cmd == "nginx-js":
        js = sp.nginx_profiles_js(select(reg, a.pki, a.include_sketch))
        if js:
            sys.stdout.write(js)
        return 0
    if a.cmd == "claim-map":
        sys.stdout.write(sp.claim_map(sp.live(reg)))
        return 0
    if a.cmd == "openssl-cnf":
        pki = sp.by_id(reg, a.id)
        if pki.get("example") is None:
            sys.exit(f"{a.id} has no example card in the registry")
        ca_subj, cnf, info = sp.openssl_request(pki, a.identifier, a.email)
        if a.print_identifier:
            print(info["identifier"])
        else:
            sys.stdout.write(ca_subj + "\n" if a.ca_subj else cnf)
        return 0
    return 2


if __name__ == "__main__":
    sys.exit(main())
