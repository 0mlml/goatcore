#!/usr/bin/env python3
"""affected-flows.py — which end-to-end flows does a change touch?

Reads ops/validation/flow-map.yaml and a git diff range, prints the flows whose
`paths` intersect the changed files. Used by the rc-validate workflow on every
release-candidate tag, and by hand before opening one.

Usage:
  affected-flows.py <base>..<head>            # e.g. sha-3249f5b..goat-id-console/v1.4.0-rc.1
  affected-flows.py --component id-admin-console --head <ref>
                                              # base = the commit fleet-pins says the
                                              # fleet runs for that component (its sha-<short>)
  affected-flows.py --files a b c             # no git; classify an explicit list
  affected-flows.py --self-test               # offline: proves a hit and a miss

Options: --json (machine output), --all (list every flow, marking affected).
Exit 0 always on success; the caller decides what to run. A change that touches
NO mapped path prints nothing under --json's "affected" — that is a statement
about the map's coverage, so the workflow also prints the unmapped changed
files for a human to judge.
"""
from __future__ import annotations
import argparse
import fnmatch
import json
import re
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent
MAP = HERE / "flow-map.yaml"
PINS = ROOT / "ops" / "pins" / "fleet-pins.yaml"

try:
    import yaml
except ImportError:  # pragma: no cover
    sys.exit("affected-flows.py needs PyYAML (pip install pyyaml)")


def load_map(path: Path = MAP) -> list[dict]:
    d = yaml.safe_load(path.read_text())
    if not d or d.get("schema") != "flow-map/v0":
        sys.exit(f"{path}: not a flow-map/v0 document")
    return d["flows"]


def glob_hit(pattern: str, path: str) -> bool:
    # `dir/**` matches anything under dir (fnmatch's * does not cross '/', but
    # ** here is a deliberate "everything below").
    if pattern.endswith("/**"):
        return path.startswith(pattern[:-3] + "/")
    return fnmatch.fnmatchcase(path, pattern)


def affected(flows: list[dict], files: list[str]) -> tuple[list[dict], list[str]]:
    hit, mapped = [], set()
    for f in flows:
        why = sorted({p for p in files for pat in f.get("paths", []) if glob_hit(pat, p)})
        if why:
            hit.append({**f, "changed": why})
            mapped.update(why)
    unmapped = sorted(set(files) - mapped)
    return hit, unmapped


def changed_files(rng: str) -> list[str]:
    out = subprocess.run(["git", "-C", str(ROOT), "diff", "--name-only", rng],
                         capture_output=True, text=True)
    if out.returncode != 0:
        sys.exit(f"git diff {rng}: {out.stderr.strip()}")
    return [l for l in out.stdout.split("\n") if l.strip()]


def base_from_ref(ref: str) -> str | None:
    """The git ref an image reference was built from, or None.

    `…/<image>:sha-<short>@…` is the image workflow's short-sha tag (type=sha,
    format=short,prefix=sha-), which IS the git short sha. `…/<image>:vX.Y.Z[-rc.N]@…`
    is a release-candidate or release image, built from the git tag
    `<image>/vX.Y.Z[-rc.N]` (ADR 1173). Once a candidate graduates, the fleet
    value carries the version tag, so both shapes must resolve: an unresolved
    base made the release driver skip every live row of the next candidate."""
    m = re.search(r":sha-([0-9a-f]{7,})@", ref)
    if m:
        return m.group(1)
    m = re.search(r"/([a-z0-9][a-z0-9._-]*):(v\d+\.\d+\.\d+(?:-rc\.\d+)?)@", ref)
    if m:
        return f"{m.group(1)}/{m.group(2)}"
    return None


def fleet_base(component: str) -> str:
    """The commit the fleet runs for a component, from fleet-pins' `fleet:` value
    (see base_from_ref for the two tag shapes)."""
    pins = yaml.safe_load(PINS.read_text())
    comp = (pins.get("components") or {}).get(component)
    if not comp:
        sys.exit(f"{PINS}: no component {component!r}")
    ref = str(comp.get("fleet", ""))
    base = base_from_ref(ref)
    if not base:
        sys.exit(f"{component}: fleet value carries neither a sha-<short> nor a vX.Y.Z[-rc.N] tag, cannot derive a base commit: {ref}")
    if subprocess.run(["git", "-C", str(ROOT), "rev-parse", "--verify", "--quiet", base + "^{commit}"],
                      capture_output=True).returncode != 0:
        sys.exit(f"{component}: fleet base {base!r} is not a commit or tag in this checkout (fetch tags: git fetch --tags)")
    return base


def lint() -> int:
    """Every `cheap.dir` must exist and every `live` row must be a manifest row.
    A flow that points at a suite that is not there, or a row that is not there,
    is coverage that does not exist — say so in CI, not at release time."""
    flows = load_map()
    manifest = yaml.safe_load((HERE / "acceptance-tests.yaml").read_text())
    rows = {t["id"] for t in manifest.get("tests", [])}
    bad = 0
    for f in flows:
        for c in f.get("cheap", []) or []:
            if not (ROOT / c["dir"]).is_dir():
                print(f"{f['id']}: cheap suite dir missing: {c['dir']}"); bad += 1
        for r in f.get("live", []) or []:
            if r not in rows:
                print(f"{f['id']}: live row {r!r} is not in acceptance-tests.yaml"); bad += 1
        if not f.get("paths"):
            print(f"{f['id']}: no paths — the flow can never be affected"); bad += 1
    print("flow-map lint:", "OK" if not bad else f"{bad} problem(s)")
    return 1 if bad else 0


def self_test() -> int:
    flows = [{"id": "x", "paths": ["a/b/**", "c/d.html"], "cheap": [], "live": ["row-x"]},
             {"id": "y", "paths": ["e/**"], "cheap": [], "live": []}]
    hit, unmapped = affected(flows, ["a/b/c/d.go", "c/d.html", "z/unrelated.md"])
    ok = [h["id"] for h in hit] == ["x"] and unmapped == ["z/unrelated.md"]
    ok = ok and affected(flows, ["c/d.htmlx"])[0] == []          # no prefix sloppiness
    ok = ok and affected(flows, ["e/f"])[0][0]["id"] == "y"
    ok = ok and base_from_ref("ghcr.io/x/onboard-runner:sha-79827b0@sha256:aa") == "79827b0"
    ok = ok and base_from_ref("ghcr.io/x/goat-id-console:v1.1.0-rc.1@sha256:aa") == "goat-id-console/v1.1.0-rc.1"
    ok = ok and base_from_ref("ghcr.io/x/goat-id-console:v1.2.0@sha256:aa") == "goat-id-console/v1.2.0"
    ok = ok and base_from_ref("ghcr.io/x/goat-id-console:latest@sha256:aa") is None
    print("self-test:", "PASS" if ok else "FAIL")
    return 0 if ok else 1


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("range", nargs="?", help="<base>..<head> git range")
    ap.add_argument("--component", help="fleet-pins component; base = its fleet sha")
    ap.add_argument("--head", help="head ref when --component is given (default HEAD)")
    ap.add_argument("--files", nargs="*", help="classify these paths instead of a git range")
    ap.add_argument("--json", action="store_true")
    ap.add_argument("--all", action="store_true", help="list every flow, marking the affected ones")
    ap.add_argument("--self-test", action="store_true")
    ap.add_argument("--lint", action="store_true", help="check the map against the repo and the manifest")
    a = ap.parse_args()
    if a.self_test:
        return self_test()
    if a.lint:
        return lint()

    if a.files is not None:
        files = a.files
        rng = "(explicit file list)"
    elif a.component:
        rng = f"{fleet_base(a.component)}..{a.head or 'HEAD'}"
        files = changed_files(rng)
    elif a.range:
        rng = a.range
        files = changed_files(rng)
    else:
        return ap.error("give a <base>..<head> range, --component, or --files")

    flows = load_map()
    hit, unmapped = affected(flows, files)
    if a.json:
        print(json.dumps({"range": rng, "changed": len(files), "affected": hit, "unmapped": unmapped,
                          "live_rows": sorted({r for h in hit for r in h.get("live", [])})}, indent=2))
        return 0
    print(f"range: {rng}  changed files: {len(files)}")
    shown = flows if a.all else hit
    for f in shown:
        mark = "AFFECTED" if any(h["id"] == f["id"] for h in hit) else "-"
        print(f"  {mark:8} {f['id']}: {f.get('what','')}")
        for h in hit:
            if h["id"] == f["id"]:
                for c in h["changed"]:
                    print(f"             ~ {c}")
        if f.get("live"):
            print(f"             live rows: {', '.join(f['live'])}")
        else:
            print("             live rows: none yet")
    if not hit:
        print("  no mapped flow is affected")
    if unmapped:
        print(f"  changed files matching no flow ({len(unmapped)}):")
        for u in unmapped[:40]:
            print(f"    ? {u}")
        if len(unmapped) > 40:
            print(f"    … and {len(unmapped) - 40} more")
    return 0


if __name__ == "__main__":
    sys.exit(main())
