#!/usr/bin/env python3
"""standup-images.py — every pinned image one member host-group deploys, one per line.

The list a standup pre-pulls on the box after phase 2 (ops/ansible/standup-image-pull.yml,
docs/design/goatlite.md §11 measure 2). It is read from the same merged pin table the
deploy reads (ops/pins/fleet-pins.yaml plus the lite sidecar ops/pins/lite-pins.yaml,
through goat_consistency.resolve), so it holds exactly the image pins rendered into
group_vars/<member>/pins.yml: components declared `absent` for the member are left out,
non-image artifacts are left out, duplicates are printed once.

  standup-images.py --member <group> [--ghcr-org <org>] [--table <fleet-pins.yaml>]

`{{ ghcr_org }}` in a pin is replaced by --ghcr-org (default: $GHCR_ORG, else usareur-af,
as group_vars/all.yml sets it). A pin carrying any other template is refused by name.
A member the table does not govern prints nothing and says so on stderr: such a goatnet
pulls its images inside its plays, as before.

Exit: 0 listed (possibly empty); 2 a pin this script cannot resolve, or a bad table.
"""
import argparse
import os
import sys

REPO = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
sys.path.insert(0, os.path.join(REPO, "ops", "consistency", "python"))

try:
    from goat_consistency import resolve  # noqa: E402
except ImportError as exc:  # pragma: no cover - dependency, not logic
    sys.exit("standup-images: %s (PyYAML + jsonschema required: pip install pyyaml jsonschema)" % exc)


def images(member, ghcr_org, table=None):
    t = resolve.load_tables(table)
    member_of = resolve.validate(t.goatnets, t.components)
    goatnet = member_of.get(member)
    if goatnet is None:
        return None
    out, bad = [], []
    for cname, spec in t.components.items():
        if (spec.get("artifact_type") or "image") != "image":
            continue
        value, _dev = resolve.resolve(spec, member, goatnet)
        if value is None:
            continue  # declared absent for this member, or no fleet value
        ref = str(value).replace("{{ ghcr_org }}", ghcr_org)
        if "{{" in ref or "{%" in ref:
            bad.append("%s: %s" % (cname, value))
            continue
        if ref not in out:
            out.append(ref)
    if bad:
        raise ValueError("a pin carries a template this script does not resolve: " + "; ".join(bad))
    return out


def main():
    ap = argparse.ArgumentParser(description="Every pinned image a member host-group deploys, one per line.")
    ap.add_argument("--member", required=True, help="the host-group, e.g. pasture_hosts")
    ap.add_argument("--ghcr-org", default=os.environ.get("GHCR_ORG") or "usareur-af")
    ap.add_argument("--table", default=None, help="the fleet table to read (default: ops/pins/fleet-pins.yaml)")
    a = ap.parse_args()
    try:
        refs = images(a.member, a.ghcr_org, a.table)
    except (ValueError, resolve.SchemaError) as e:
        print("standup-images: %s" % e, file=sys.stderr)
        return 2
    if refs is None:
        print("standup-images: %s is governed by no pin table; nothing to pre-pull" % a.member, file=sys.stderr)
        return 0
    for r in refs:
        print(r)
    return 0


if __name__ == "__main__":
    sys.exit(main())
