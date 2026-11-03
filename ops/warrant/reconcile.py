#!/usr/bin/env python3
"""warrant reconcile — read the LIVE ACL back and say what the registry never declared.

    live NetBird policies ──┐
                            ├─→ reconcile.py ─→ classification + adoption stanzas
    rule-intents/ ──────────┘

WHY THIS EXISTS. warrant has been WRITE-ONLY. `generate.py` renders the matrix
and the cutover playbook applies it, and the only read-back anywhere is the
cutover's orphan check — which filters on `description` containing
`[L4-cutover via netbird-acl-cutover.yml]`, the tag warrant stamps on its own
output. A policy created in the NetBird web console has an EMPTY description.
It therefore matches no filter, appears in no report, is skipped by the orphan
reaper, and survives every deploy.

Nothing in the repo compared the live policy set against the registry. That is
not a gap in coverage, it is a gap in KIND: every check we had asks "is what we
declared applied correctly", and none asks "is what is applied something we
declared".

A mesh can accumulate hand-written policies granting the built-in `All` group —
every peer on the mesh — every protocol on every port to production control
hosts, and nothing notices them. This is the guard that does.

WHAT IT CLASSIFIES. Every live policy lands in exactly one bucket:

  generated   carries warrant's tag AND its name appears in the matrix.
              The healthy case.
  orphaned    carries the tag, name NOT in the matrix. warrant made it and the
              registry has since dropped the rule. The cutover already finds
              these; repeated here so one command gives the whole picture.
  undeclared  does NOT carry the tag. Nobody generated it. This is the bucket
              that had no watcher, and the one this tool exists for.
  default     NetBird's built-in `Default` All<->All policy, reported by state
              rather than as drift — disabled is the post-cutover expectation.

WHAT IT DOES NOT DO. It changes nothing. No writes, no deletes, no API mutation
of any kind — this is a reader, and a reader is what was missing. Removing an
undeclared policy is a human decision because some of them are legitimate work
that simply took the wrong road, and deleting those silently would teach people
that the console is where real work gets undone.

`--adopt` prints each undeclared policy as a rule stanza shaped for
`rule-intents/`, with the annotation fields left EMPTY and required. That is the
retroactive-legitimisation path: the person who made the change in the console
says why, the stanza goes through review like any other rule, and the next
generate makes it real. The tool deliberately cannot fill `justification` in —
a machine-written reason is exactly the thing review must not be given.

Exit codes follow the live-state-guard contract
(docs/design/live-state-guard-scheduling.md §4):
  0  read + nothing undeclared
  1  undeclared policies exist — the condition this exists to catch
  2  a source could not be read, which says NOTHING and must never be
     mistaken for clean

NETWORK MODE (docs/design/mesh-change-control.md §5). `--network --scope S`
reads the live nameserver groups (`/api/dns/nameservers`) and routes
(`/api/routes`) and classifies each against ops/warrant/network/<S>.yaml:

  declared    its name (nameserver group) or `network_id` (route) is listed
  undeclared  it is live and the file does not list it
  missing     the file lists it and nothing live carries it

`--live-json FILE` reads both arrays from a file shaped
`{"nameservers": [...], "routes": [...]}` instead of the API, so the
classification is testable offline and a captured snapshot can be graded
later. Same exit contract: 1 when anything is undeclared or missing.

Usage:
    NB_API_URL=https://netbird.example.net NB_API_TOKEN=... \\
      reconcile.py --site site-a
    ... --adopt          # print registry stanzas for the undeclared set
    ... --json           # machine-readable, for a guard or a dashboard
    reconcile.py --network --scope site-a --live-json snapshot.json
    NB_API_TOKEN=... reconcile.py --network --scope site-a --mgmt-url https://mgmt.<site>
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.error
import urllib.request
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
MATRIX = REPO / "ops/ansible/netbird_acl_matrix.yml"

# The tags warrant has stamped into policies it creates. SUBSTRING tests,
# matching the playbook's own `search` filter, so the exercise variant
# `[L4-exercise <site> via ...]` is recognised alongside `[L4-cutover via ...]`.
#
# TWO ENTRIES, AND THE SECOND ONE IS A LIVE DEFECT, not backwards-compatibility
# politeness. A renamed playbook changes the tag it WRITES, and the old name's
# prefix was first-instance inheritance. Nothing rewrites the tag on policies
# already carrying the old one.
#
# A plane last cut over before the rename carries only the old tag, so
# partitioning by the current tag alone would treat every one of its own
# policies as foreign.
#
# Recognising the old tag here is how this tool tells the truth about what is
# generated. It does NOT fix the underlying condition — that needs the live
# descriptions rewritten, or the playbook taught its own history — and
# `stale_tag` below reports it rather than absorbing it silently.
WARRANT_TAGS = [
    "via netbird-acl-cutover.yml",
    "via nova-netbird-acl-cutover.yml",
]
CURRENT_TAG = WARRANT_TAGS[0]


def api(url: str, token: str, path: str):
    req = urllib.request.Request(
        f"{url.rstrip('/')}{path}",
        headers={"Authorization": f"Token {token}", "Accept": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=30) as r:
        return json.loads(r.read().decode())


def declared_names() -> set[str] | None:
    """Every rule name the generated matrix carries, across ALL rule-bearing keys.

    Four top-level keys carry rules, not one. Parsing only `netbird_acl_matrix`
    reads about half of them and silently calls the rest undeclared — the exact
    miscount that has already produced a wrong answer about this file once.
    """
    try:
        import yaml
    except ImportError:
        print("reconcile: PyYAML not available", file=sys.stderr)
        return None
    try:
        doc = yaml.safe_load(MATRIX.read_text())
    except (OSError, Exception) as e:  # noqa: BLE001 - any parse failure is "unread"
        print(f"reconcile: cannot read {MATRIX}: {e}", file=sys.stderr)
        return None

    names: set[str] = set()

    def harvest(node):
        if isinstance(node, dict):
            for k, v in node.items():
                if k == "rules" and isinstance(v, list):
                    for r in v:
                        if isinstance(r, dict) and r.get("name"):
                            names.add(str(r["name"]))
                else:
                    harvest(v)
        elif isinstance(node, list):
            for v in node:
                harvest(v)

    harvest(doc)
    return names


def declared_match(live_name: str, declared: set[str]) -> bool:
    """Is this live policy name one the registry declares?

    The cutover PREFIXES a scope label onto the policy name for subordinate
    scopes — the registry rule `p2p <-> p2p (the clique)` is applied as
    `SITE_ONE - p2p <-> p2p (the clique)`. Comparing the live name directly
    therefore misses every subordinate-scope policy.

    That is not a cosmetic bug in a reporting tool. On the first live run this
    reported 51 legitimate SITE_ONE policies as ORPHANED — i.e. as candidates for
    deletion. A drift tool whose false-positive rate is 51 is not a tool anyone
    will read twice, and the failure mode of "delete what it says is orphaned"
    is an outage. Strip the prefix and compare on the declared name.

    The separator is a MIDDLE DOT with spaces, written by the playbook. Split
    on the first occurrence only: a rule name may legitimately contain the
    character (an arrow-and-dot rule name would), and splitting greedily would
    truncate it.
    """
    if live_name in declared:
        return True
    for sep in (" \u00b7 ", " - "):
        if sep in live_name:
            _, _, tail = live_name.partition(sep)
            if tail in declared:
                return True
    return False


def rule_shape(pol: dict) -> dict:
    """Reduce a live policy to the fields a registry stanza needs."""
    rules = pol.get("rules") or [{}]
    r = rules[0]
    srcs = [g.get("name", "") for g in (r.get("sources") or [])]
    dsts = [g.get("name", "") for g in (r.get("destinations") or [])]
    sres = (r.get("sourceResource") or {}).get("id")
    dres = (r.get("destinationResource") or {}).get("id")
    ports = list(r.get("ports") or [])
    for pr in r.get("port_ranges") or r.get("portRanges") or []:
        ports.append(f"{pr.get('start')}-{pr.get('end')}")
    proto = r.get("protocol") or "all"
    return {
        "sources": srcs,
        "destinations": dsts,
        "source_resource": sres,
        "destination_resource": dres,
        "protocol": proto,
        "ports": ports,
        "bidirectional": bool(r.get("bidirectional")),
        "action": r.get("action") or "accept",
        # Unrestricted = every protocol AND every port. Surfaced because an
        # undeclared policy that limits nothing is a different severity from
        # one scoped to a port, and the reader should not have to derive it.
        "unrestricted": proto in ("", "all") and not ports,
    }


def adoption_stanza(pol: dict, shape: dict) -> str:
    """A rule stanza for rule-intents/, with the human fields left blank.

    `justification` and `owner` are REQUIRED and EMPTY on purpose. The whole
    value of bringing a console change back through review is the sentence
    saying why it was made, and a generated reason would be worse than none —
    it would give a reviewer something plausible to approve.

    A resource-targeted rule is emitted COMMENTED OUT, because the registry
    speaks in groups: a rule pinned to one peer id cannot survive that peer
    being re-enrolled (the id changes), so adopting it verbatim would encode a
    fact with a short life. The right adoption is a group, and that is a
    decision for the person, not a transform.
    """
    lines = [f'      - name: "{pol.get("name", "")}"']
    lines.append('        description: ""          # REQUIRED: what this permits, in one sentence')
    lines.append('        justification: ""        # REQUIRED: why it exists. Written by the person who made it.')
    lines.append('        owner: ""                # REQUIRED: who answers for it')
    if shape["source_resource"] or shape["destination_resource"]:
        lines.append("        # ADOPT AS GROUPS, NOT AS THIS. The live rule targets a PEER ID:")
        if shape["source_resource"]:
            lines.append(f"        #   sourceResource:      {shape['source_resource']}")
        if shape["destination_resource"]:
            lines.append(f"        #   destinationResource: {shape['destination_resource']}")
        lines.append("        # A peer id changes on re-enrolment, so a rule pinned to one cannot")
        lines.append("        # outlive it. Name the CLASS the peer belongs to instead.")
    # Emit an EMPTY list plus a trailing comment, never `[# REQUIRED]` — that
    # renders a comment inside a flow sequence, which is not valid YAML, so the
    # stanza would fail to parse the moment anyone pasted it. An adoption path
    # that hands you something broken is not an adoption path.
    for field in ("sources", "destinations"):
        vals = shape[field]
        if vals:
            lines.append(f"        {field}: [{', '.join(vals)}]")
        else:
            lines.append(f"        {field}: []   # REQUIRED: the rule targets a peer id, name its CLASS")
    lines.append(f"        protocol: {shape['protocol']}")
    if shape["ports"]:
        lines.append(f"        # live ports: {', '.join(shape['ports'])} — replace with a named set from port-sets.yaml")
        lines.append("        port_set: \"\"            # REQUIRED: rules name a SET, never literals")
    elif shape["unrestricted"]:
        lines.append("        # NO PORT LIMIT AND NO PROTOCOL LIMIT as applied. Narrowing this is")
        lines.append("        # the point of adoption: find what actually listens before declaring it.")
    lines.append(f"        action: {shape['action']}")
    lines.append(f"        bidirectional: {str(shape['bidirectional']).lower()}")
    return "\n".join(lines)


NETWORK_DIR = REPO / "ops/warrant/network"


def classify_network(declared: dict, nameservers: list, routes: list) -> dict:
    """Bucket every live nameserver group and route against the declaration."""
    decl_ns = list(declared.get("nameserver_groups") or [])
    decl_rt = list(declared.get("routes") or [])
    out: dict[str, dict[str, list]] = {
        "nameserver_groups": {"declared": [], "undeclared": [], "missing": []},
        "routes": {"declared": [], "undeclared": [], "missing": []},
    }
    live_ns = set()
    for ns in nameservers or []:
        name = str(ns.get("name") or "")
        live_ns.add(name)
        row = {"id": ns.get("id"), "name": name, "enabled": bool(ns.get("enabled", True)),
               "groups": list(ns.get("groups") or []),
               "domains": list(ns.get("domains") or [])}
        out["nameserver_groups"]["declared" if name in decl_ns else "undeclared"].append(row)
    out["nameserver_groups"]["missing"] = [n for n in decl_ns if n not in live_ns]
    live_rt = set()
    for rt in routes or []:
        nid = str(rt.get("network_id") or "")
        live_rt.add(nid)
        row = {"id": rt.get("id"), "network_id": nid, "enabled": bool(rt.get("enabled", True)),
               "network": rt.get("network") or ",".join(rt.get("domains") or []),
               "peer": rt.get("peer") or "", "peer_groups": list(rt.get("peer_groups") or []),
               "groups": list(rt.get("groups") or [])}
        out["routes"]["declared" if nid in decl_rt else "undeclared"].append(row)
    out["routes"]["missing"] = [n for n in decl_rt if n not in live_rt]
    return out


def network_drift(buckets: dict) -> bool:
    return any(buckets[k]["undeclared"] or buckets[k]["missing"] for k in buckets)


def main_network(args) -> int:
    sys.path.insert(0, str(Path(__file__).resolve().parent))
    import membership as mem

    decl_path = Path(args.network_file) if args.network_file else NETWORK_DIR / f"{args.scope}.yaml"
    try:
        declared = mem.load(decl_path)
    except mem.SourceError as e:
        print(f"reconcile: {e}", file=sys.stderr)
        print("  This is exit 2, not a pass: an unread source says nothing.", file=sys.stderr)
        return 2
    errs = mem.validate_network(declared, stem=decl_path.stem)
    if errs:
        print(f"reconcile: {decl_path} is not a valid declaration:", file=sys.stderr)
        for e in errs:
            print(f"  - {e}", file=sys.stderr)
        return 2

    if args.live_json:
        try:
            live = json.loads(Path(args.live_json).read_text())
            nameservers, routes = live["nameservers"], live["routes"]
        except (OSError, ValueError, KeyError, TypeError) as e:
            print(f"reconcile: cannot read {args.live_json} (want {{\"nameservers\": [...], "
                  f"\"routes\": [...]}}): {e}", file=sys.stderr)
            return 2
    else:
        url = args.mgmt_url or os.environ.get("NB_API_URL", "")
        token = os.environ.get("NB_API_TOKEN", "")
        if not url or not token:
            print("reconcile: --mgmt-url (or NB_API_URL) / NB_API_TOKEN unset — cannot read live state.",
                  file=sys.stderr)
            print("  This is exit 2, not a pass: an unread source says nothing.", file=sys.stderr)
            return 2
        try:
            nameservers = api(url, token, "/api/dns/nameservers")
            routes = api(url, token, "/api/routes")
        except (urllib.error.URLError, urllib.error.HTTPError, OSError, ValueError) as e:
            print(f"reconcile: cannot read live nameservers/routes: {e}", file=sys.stderr)
            return 2
    if not isinstance(nameservers, list) or not isinstance(routes, list):
        print("reconcile: nameservers and routes must both be JSON arrays", file=sys.stderr)
        return 2

    buckets = classify_network(declared, nameservers, routes)
    drift = network_drift(buckets)
    if args.as_json:
        print(json.dumps({"scope": args.scope, "buckets": buckets}, indent=2))
        return 1 if drift else 0

    print(f"warrant reconcile --network — {args.scope}: "
          f"{len(nameservers)} nameserver group(s), {len(routes)} route(s) live")
    for kind, label, key in (("nameserver_groups", "nameserver groups", "name"), ("routes", "routes", "network_id")):
        b = buckets[kind]
        print(f"  {label}: declared {len(b['declared'])}, undeclared {len(b['undeclared'])}, "
              f"missing {len(b['missing'])}")
        for row in b["undeclared"]:
            state = "" if row["enabled"] else " (disabled)"
            extra = (", ".join(row["domains"]) or "all domains") if kind == "nameserver_groups" else row["network"]
            print(f"    undeclared  {row[key]:<40} {extra}{state}")
        for name in b["missing"]:
            print(f"    missing     {name:<40} declared in {decl_path.name}, not live")
    print()
    if not drift:
        print("Everything live is declared, and everything declared is live.")
        return 0
    print(f"Undeclared items are live and in no declaration: add them to {decl_path.relative_to(REPO) if decl_path.is_relative_to(REPO) else decl_path}")
    print("through review if they are legitimate, or remove them with the bootstrap play that owns them.")
    return 1


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--site", help="label for the report (e.g. zagros-prod); required for policies")
    ap.add_argument("--adopt", action="store_true", help="print registry stanzas for undeclared policies")
    ap.add_argument("--json", dest="as_json", action="store_true")
    ap.add_argument("--network", action="store_true",
                    help="read back nameserver groups and routes against ops/warrant/network/<scope>.yaml")
    ap.add_argument("--scope", help="taxonomy scope for --network")
    ap.add_argument("--live-json", help="--network: read live arrays from this file instead of the API")
    ap.add_argument("--mgmt-url", help="--network: management API base URL (default NB_API_URL)")
    ap.add_argument("--network-file", help="--network: declaration path (default ops/warrant/network/<scope>.yaml)")
    args = ap.parse_args()

    if args.network:
        if not args.scope:
            ap.error("--network requires --scope")
        return main_network(args)
    if not args.site:
        ap.error("--site is required")

    url = os.environ.get("NB_API_URL", "")
    token = os.environ.get("NB_API_TOKEN", "")
    if not url or not token:
        print("reconcile: NB_API_URL / NB_API_TOKEN unset — cannot read live state.", file=sys.stderr)
        print("  This is exit 2, not a pass: an unread source says nothing.", file=sys.stderr)
        return 2

    try:
        policies = api(url, token, "/api/policies")
    except (urllib.error.URLError, urllib.error.HTTPError, OSError, ValueError) as e:
        print(f"reconcile: cannot read live policies: {e}", file=sys.stderr)
        return 2

    declared = declared_names()
    if declared is None:
        return 2

    buckets: dict[str, list] = {"generated": [], "orphaned": [], "undeclared": [], "default": []}
    for pol in policies:
        name = pol.get("name", "")
        desc = pol.get("description") or ""
        shape = rule_shape(pol)
        row = {
            "id": pol.get("id"),
            "name": name,
            "enabled": bool(pol.get("enabled")),
            **shape,
        }
        if pol.get("id", "").startswith("pol_") and name == "Default":
            buckets["default"].append(row)
        elif any(t in desc for t in WARRANT_TAGS):
            row["stale_tag"] = CURRENT_TAG not in desc
            buckets["generated" if declared_match(name, declared) else "orphaned"].append(row)
        else:
            buckets["undeclared"].append(row)

    if args.as_json:
        print(json.dumps({"site": args.site, "buckets": buckets}, indent=2))
        return 1 if buckets["undeclared"] else 0

    print(f"warrant reconcile — {args.site}: {len(policies)} live policies")
    print(f"  generated  {len(buckets['generated']):>3}  declared in the registry and tagged by warrant")
    print(f"  orphaned   {len(buckets['orphaned']):>3}  warrant made them; the registry no longer declares them")
    print(f"  undeclared {len(buckets['undeclared']):>3}  NOBODY GENERATED THESE")
    stale = [p for p in buckets["generated"] + buckets["orphaned"] if p.get("stale_tag")]
    if stale:
        print(f"  STALE TAG  {len(stale):>3}  generated by warrant, but tagged with a playbook NAME")
        print(f"             that no longer writes it. Phase 4.1a partitions on the CURRENT tag,")
        print(f"             so these read as FOREIGN: the cutover fails loud on name collision")
        print(f"             and the orphan reaper sees none of its own work.")
    for d in buckets["default"]:
        state = "ENABLED — the cutover has not been enforced" if d["enabled"] else "disabled, as expected post-cutover"
        print(f"  default        NetBird built-in Default policy: {state}")
    print()

    if not buckets["undeclared"]:
        print("No undeclared policies. Everything applied is something the registry declares.")
        return 0

    unrestricted = [p for p in buckets["undeclared"] if p["unrestricted"] and p["enabled"]]
    print("UNDECLARED POLICIES — applied, and in no registry:")
    for p in sorted(buckets["undeclared"], key=lambda x: (not (x["unrestricted"] and x["enabled"]), x["name"])):
        flag = "  <== UNRESTRICTED" if p["unrestricted"] and p["enabled"] else ""
        state = "" if p["enabled"] else " (disabled)"
        src = ", ".join(p["sources"]) or (f"peer {p['source_resource']}" if p["source_resource"] else "(none)")
        dst = ", ".join(p["destinations"]) or (f"peer {p['destination_resource']}" if p["destination_resource"] else "(none)")
        ports = ", ".join(p["ports"]) or "all ports"
        print(f"  {p['id']:<24} {p['name'][:44]:<46}{state}{flag}")
        print(f"      {src}  ->  {dst}   [{p['protocol']} {ports}]")
    print()
    if unrestricted:
        print(f"{len(unrestricted)} of them limit NEITHER protocol NOR port while enabled.")
        print("Under a default-deny cutover that is the posture removed for those destinations.")
        print()

    if args.adopt:
        print("=" * 72)
        print("ADOPTION STANZAS — paste into ops/warrant/rule-intents/, fill the")
        print("REQUIRED fields, and send it through review like any other rule.")
        print("The justification is deliberately blank: it is the whole reason the")
        print("change is worth reviewing, and a generated one would be worse than none.")
        print("=" * 72)
        for p in buckets["undeclared"]:
            print()
            print(adoption_stanza({"name": p["name"]}, p))
    else:
        print("Re-run with --adopt to print registry stanzas for these.")

    return 1


if __name__ == "__main__":
    sys.exit(main())
