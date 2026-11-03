#!/usr/bin/env python3
"""Every play reads a site's leaf certificate or key through goat_leaf (#2165).

A leaf read straight from the checkout, `{{ playbook_dir }}/../enrollment/<site>/
<name>-{cert,key}.pem`, exists only in the checkout someone staged it into, so a
deploy from a fresh worktree (C3, #2124) refuses until the leaf is re-staged.
goat_leaf reads the site manifest's out_dir first and keeps the checkout copy as
a fallback. This guard fails on any task, var or default in ops/ansible that
names a leaf pem under ops/enrollment/<site>/ without the filter.

    python3 ops/ci/check-leaf-paths-through-goat-leaf.py [--self-test]

CA roots and public keys (ops/enrollment/ca/, public-keys/) are not leaves and
are not judged. Retired-goatnet roles may stay on the old path with
`# leaf-path: <reason>` on the line.
"""

import re
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
ANSIBLE = ROOT / "ops" / "ansible"

LEAF = re.compile(
    r"enrollment/(?!ca/|public-keys/|manifests/|cmd/)"
    r"(\{\{[^}]*\}\}|[A-Za-z0-9_.-]+)/[^\"'\s]*-(cert|key)\.pem"
)


def offenders(base):
    out = []
    for path in sorted(base.rglob("*.yml")):
        for n, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
            code = line.split("#", 1)[0] if not line.lstrip().startswith("#") else ""
            if not code or "goat_leaf" in code or "# leaf-path:" in line:
                continue
            if LEAF.search(code):
                out.append(f"{path.relative_to(base.parent.parent) if base == ANSIBLE else path.name}:{n}: {line.strip()}")
    return out


def self_test():
    with tempfile.TemporaryDirectory() as d:
        base = Path(d)
        (base / "bad.yml").write_text(
            'x: "{{ playbook_dir }}/../enrollment/{{ site_id }}/router-cert.pem"\n'
            'y: "{{ playbook_dir }}/../enrollment/zagros/mgmt-identity-key.pem"\n')
        (base / "good.yml").write_text(
            "x: \"{{ 'router-cert.pem' | goat_leaf(site_id) }}\"\n"
            'r: "{{ playbook_dir }}/../enrollment/ca/zagros/root-cert.pem"\n'
            'k: "{{ playbook_dir }}/../enrollment/public-keys/a.pem"\n'
            '# a comment naming ops/enrollment/zagros/router-cert.pem is fine\n'
            'z: "ops/enrollment/nova-lan/router-cert.pem"   # leaf-path: retired goatnet\n')
        bad = offenders(base)
        names = {o.split(":", 1)[0] for o in bad}
        ok = len(bad) == 2 and names == {"bad.yml"}
        print(("self-test ok" if ok else "self-test FAILED") + f": {bad}")
        return ok


def main():
    if "--self-test" in sys.argv:
        return 0 if self_test() else 1
    bad = offenders(ANSIBLE)
    if bad:
        print("FAIL: a leaf is read from the checkout, not through goat_leaf (#2165):")
        for b in bad:
            print("  " + b)
        print("Use: \"{{ '<name>-cert.pem' | goat_leaf(site_id) }}\" (ops/ansible/filter_plugins/goat_leaf.py).")
        return 1
    print("ok: every leaf read goes through goat_leaf")
    return 0


if __name__ == "__main__":
    sys.exit(main())
