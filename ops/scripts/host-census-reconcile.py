#!/usr/bin/env python3
"""
Does every infrastructure host on the mesh appear in the inventory, and vice versa?

A census has to answer "which physical machines are at this site" from the
repository, and a host can exist without being *declared* anywhere a tool can
see: only as a ProxyJump STRING inside other hosts' `ansible_ssh_common_args`,
or only as a row in a markdown table.

A host that exists only as a substring of another host's connection arguments is
invisible to every tool that enumerates inventory, and to every person who does.
Both are declared now. Nothing stops the next one. That census was corrected
because a person noticed absences, which is not a control.

WHAT THIS CHECKS, IN BOTH DIRECTIONS

  drift: on-mesh, undeclared   a peer in the goatnet's infra group with no
                              matching host in the ansible inventory. This is
                              the lab-node shape and the reason the check
                              exists.
  drift: declared, off-mesh    an inventory host in a mesh-bearing group with no
                              matching peer. Either it is gone, or it never
                              enrolled, or it is named differently in the two
                              places — all three are worth a look, none is
                              automatically a fault.

WHY THE INFRA GROUP AND NOT EVERY PEER. Most mesh peers are people and vendor
workloads; they have no business in an ansible inventory and comparing against
them would bury the signal. The infra group is the set that is supposed to be
managed, so it is the set where absence means something. Pass --infra-group to
name it; the default follows the ADR 1158 convention of `<SCOPE>-INFRA`.

WHERE THIS RUNS. On a mgmt host, on a timer — never GitHub Actions. It needs an
admin-scoped NetBird PAT, and Actions secrets are readable by anyone with push
access; a check that cannot reach the mesh from a hosted runner would report
"clean" by measuring nothing, which is worse than no check. See
docs/design/live-state-guard-scheduling.md.

EXIT CODES — each means exactly one thing.

  0  the mesh and the inventory were both read, and they agree.
  1  they were both read and they disagree. The drift is listed.
  2  the mesh could not be read (no token, API error, network). NOT clean —
     nothing was compared. Never collapse this into 0.
  3  the inventory could not be read or parsed. Same: nothing was compared.

Read-only. It issues GET requests and parses files; it changes no mesh state,
no inventory, and nothing on disk.

USAGE

  host-census-reconcile.py --goatnet site-a \\
      --inventory ops/ansible/inventory/mgmt-hosts.ini \\
      [--inventory ops/ansible/inventory/site-a-breakglass.ini] \\
      [--infra-group SITE-A-INFRA] [--json]

  Token from $<GOATNET>_NB_API_TOKEN (site-a -> $SITE_A_NB_API_TOKEN),
  matching the convention in peer-attribution.py and operator-onboard.yml.
"""

import argparse
import configparser
import json
import os
import sys
import urllib.error
import urllib.request

EXIT_OK, EXIT_DRIFT, EXIT_MESH_UNREADABLE, EXIT_INVENTORY_UNREADABLE = 0, 1, 2, 3

# A NetBird peer's DNS label and an ansible inventory hostname are the same
# string in this fleet, but the peer carries a domain suffix. Compare on the
# label so `mgmt-site-a-01.netbird.selfhosted` matches the inventory's
# `mgmt-site-a-01` without a fuzzy match that could pair the wrong hosts.
def label(name: str) -> str:
    return (name or "").strip().split(".")[0].lower()


def read_inventory(paths, group_prefix=""):
    """Every host declared across the given inventory files, as {label: [groups]}.

    configparser is used rather than ansible-inventory on purpose: this runs on a
    mgmt host inside a timer, and shelling out to ansible would make the check
    depend on a working ansible install to answer a question about text files.
    """
    hosts = {}
    cp = configparser.ConfigParser(allow_no_value=True, delimiters=("=",),
                                   strict=False, interpolation=None)
    read = cp.read(paths)
    if not read:
        raise FileNotFoundError(f"no inventory file could be read from {paths}")
    for section in cp.sections():
        # `[grp:vars]` and `[grp:children]` describe a group, they do not
        # declare hosts. Skipping them keeps a var value from being read as a
        # hostname.
        if ":" in section:
            continue
        # Scope to this goatnet. One inventory file declares every goatnet's
        # hosts, so comparing all of them against ONE goatnet's mesh reports
        # every other goatnet as "declared, off-mesh". Noise on that scale
        # trains people to ignore the check.
        if group_prefix and not section.startswith(group_prefix):
            continue
        for entry in cp[section]:
            host = entry.split()[0] if entry.split() else ""
            if not host or host.startswith("#"):
                continue
            hosts.setdefault(label(host), set()).add(section)
    return {h: sorted(g) for h, g in hosts.items()}


def read_mesh(api_url, token, infra_group):
    """Two maps: peers in the infra group, and every peer on the mesh.

    Both are needed to tell three different states apart. A host declared in the
    inventory but absent from the infra group is not necessarily missing — the
    breakglass hosts are deliberately outside it, because a breakglass box runs
    no mgmt-stack. Reporting those as "not on the mesh" when they are sitting
    right there, enrolled and reachable, is a false alarm that costs the check
    its credibility on the first run.
    """
    req = urllib.request.Request(
        api_url.rstrip("/") + "/api/peers",
        headers={"Authorization": f"Token {token}", "Accept": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=30) as r:
        peers = json.load(r)
    out, everything = {}, {}
    for p in peers:
        groups = [g.get("name", "") for g in (p.get("groups") or [])]
        in_infra = (not infra_group) or (infra_group in groups)
        # A peer carries several names and they are NOT interchangeable.
        # `dns_label` is what NetBird publishes in the mesh zone and it can carry
        # a disambiguating suffix — a peer named
        # `mgmt-site-a-01` can have dns_label `mgmt-site-a-01-210-9`.
        # Matching on dns_label alone reports that host as undeclared when it is
        # declared, which is a false positive in the exact direction this check
        # exists to report. So collect every candidate and match on ANY of them.
        cands = {label(p.get(k) or "") for k in ("name", "hostname", "dns_label")}
        cands.discard("")
        if not cands:
            continue
        display = p.get("name") or p.get("dns_label") or p.get("hostname")
        for c in cands:
            everything.setdefault(c, display)
            if in_infra:
                out.setdefault(c, display)
    return out, everything


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__.strip().splitlines()[0])
    ap.add_argument("--goatnet", required=True,
                    help="goatnet id, e.g. zagros-prod. Selects the token env var.")
    ap.add_argument("--inventory", action="append", required=True,
                    help="inventory .ini path; repeatable")
    ap.add_argument("--group-prefix", default="",
                    help="inventory group prefix for this goatnet "
                         "(default: goatnet with dashes as underscores, e.g. zagros_prod)")
    ap.add_argument("--infra-group", default="",
                    help="NetBird group holding managed infrastructure "
                         "(default: <GOATNET>-INFRA)")
    ap.add_argument("--api-url", default="",
                    help="NetBird management URL (default: https://netbird.<goatnet>.net)")
    ap.add_argument("--json", action="store_true", help="emit JSON")
    a = ap.parse_args(argv)

    infra_group = a.infra_group or (a.goatnet.upper().replace("-", "_")
                                    .replace("_", "-") + "-INFRA")
    api_url = a.api_url or f"https://netbird.{a.goatnet}.net"
    tokvar = a.goatnet.upper().replace("-", "_") + "_NB_API_TOKEN"
    token = os.environ.get(tokvar, "")

    # Inventory first: if the declared side cannot be read there is nothing to
    # compare the mesh against, and saying so is cheaper than an API round trip.
    try:
        group_prefix = a.group_prefix or (a.goatnet.replace("-", "_") + "_")
        inventory = read_inventory(a.inventory, group_prefix)
    except Exception as e:
        print(f"FAIL: inventory unreadable — {e}", file=sys.stderr)
        print("Nothing was compared. This is not a clean result.", file=sys.stderr)
        return EXIT_INVENTORY_UNREADABLE

    if not token:
        print(f"FAIL: ${tokvar} is not set, so the mesh was not read.", file=sys.stderr)
        print("Nothing was compared. This is not a clean result — a census that "
              "cannot see the mesh has not checked anything.", file=sys.stderr)
        return EXIT_MESH_UNREADABLE
    try:
        mesh, all_peers = read_mesh(api_url, token, infra_group)
    except (urllib.error.URLError, urllib.error.HTTPError, ValueError, OSError) as e:
        print(f"FAIL: mesh unreadable at {api_url} — {e}", file=sys.stderr)
        print("Nothing was compared. This is not a clean result.", file=sys.stderr)
        return EXIT_MESH_UNREADABLE

    # One peer contributes several candidate labels, so collapse by the display
    # name before reporting or a single host appears as three findings.
    undeclared_peers = {}
    for lbl, display in mesh.items():
        if lbl in inventory:
            continue
        undeclared_peers.setdefault(display, lbl)
    undeclared = sorted(
        lbl for display, lbl in undeclared_peers.items()
        # A peer is only undeclared if NONE of its names is in the inventory.
        if not any(c in inventory for c, d in mesh.items() if d == display)
    )
    # Genuinely absent: not on the mesh under ANY name, in any group.
    absent = sorted(set(inventory) - set(all_peers))
    # Present and enrolled, just outside the infra group. Expected for
    # breakglass; informational, never a fault on its own.
    outside = sorted((set(inventory) - set(mesh)) & set(all_peers))

    if a.json:
        print(json.dumps({
            "goatnet": a.goatnet, "infra_group": infra_group,
            "mesh_peers": len(mesh), "inventory_hosts": len(inventory),
            "on_mesh_undeclared": [{"label": h, "peer": mesh[h]} for h in undeclared],
            "declared_absent": [{"label": h, "groups": inventory[h]} for h in absent],
            "declared_outside_infra_group": [{"label": h, "groups": inventory[h]} for h in outside],
        }, indent=2))
        return EXIT_DRIFT if (undeclared or absent) else EXIT_OK

    # Visible when it is working, not only when it fails: the counts print on
    # every run so a silent job and a healthy one do not look the same.
    print(f"census {a.goatnet}: {len(mesh)} peer(s) in {infra_group}, "
          f"{len(inventory)} host(s) declared in {group_prefix}* "
          f"across {len(a.inventory)} inventory file(s)")

    if undeclared:
        print(f"\nDRIFT — on the mesh, declared nowhere ({len(undeclared)}):")
        for h in undeclared:
            print(f"  {h}   (peer: {mesh[h]})")
        print("  These are reachable and managed by nothing. This is the F-441 "
              "shape: a host only a person knows about.")
    if absent:
        print(f"\nDRIFT — declared, on the mesh under no name ({len(absent)}):")
        for h in absent:
            print(f"  {h}   (groups: {', '.join(inventory[h])})")
        print("  Gone, never enrolled, or named differently in the two places. "
              "None of those is automatically a fault; all three are worth a look.")

    if outside:
        print(f"\nnote — declared and enrolled, outside {infra_group} ({len(outside)}):")
        for h in outside:
            print(f"  {h}   (groups: {', '.join(inventory[h])})")
        print("  Expected for breakglass hosts, which run no mgmt-stack and are "
              "deliberately not infra-group members. Not counted as drift.")

    if not undeclared and not absent:
        print("OK: every infra peer is declared and every declared host is on the mesh.")
        return EXIT_OK
    return EXIT_DRIFT


if __name__ == "__main__":
    sys.exit(main())
