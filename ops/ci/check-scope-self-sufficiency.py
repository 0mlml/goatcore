#!/usr/bin/env python3
"""
Can this machine reach a substrate using ONE scope's groups, or does it need two?

ADR 1124. A goatnet can host a tactical site below it — a `SITE-` scope under
a `GOATNET-` scope, stamped on the same mgmt server. A tactical site has no
substrate of its own. Without a rule naming another scope's groups, the only way
to give a site machine its northbound path is to hand it a second, site-lying
`GOATNET-*` membership. A peer holding only the site's groups then resolves zero
rules to the host goatnet's INFRA: severed on arrival, with no rule to point at.

WHAT THE EXISTING CHECKS DO NOT ANSWER. Coverage asks "is this peer covered" —
every peer passes. The ADR 1116 §7 guard asks "is this peer's shape right".
A reach audit asks "is this peer's reach correct". None of them asks whether
the reach DEPENDS ON holding two scopes at once, which is the thing that makes
the site dimension untrue and over-grants by construction.

THE CHECK. For each machine peer: compute its reach to any `*-INFRA` group on
the fabric / DNS ports, first from all its groups together, then from each
scope's groups in isolation. If the union reaches a substrate but no single
scope does, the peer is CROSS-SCOPE DEPENDENT — it only works because it wears
two sites. That is the defect, and after the ADR 1124 rules are applied it
should be empty.

PEOPLE ARE NOT MACHINES AND ARE SKIPPED. A person legitimately holds a goatnet
role and a site role at the same time — `GOATNET-DEVELOPER` +
`SITE-PLATFORM-ADMIN` is two true statements about one human, because role and
site are orthogonal dimensions (ADR 1043). Only a MACHINE asserting two sites is
the defect. Without this exclusion the guard fires on every site admin on its
first run and gets muted, which is how a guard dies.

Usage:
  NB_API_URL=https://netbird.<zone> NB_API_TOKEN=... \\
    python3 ops/ci/check-scope-self-sufficiency.py [--strict] [--json]

Exit 0 unless --strict and cross-scope-dependent machines exist. NOT wired into
PR CI: it needs live mesh credentials and refuses to pass on an unread mesh —
same contract as check-membership-reconcile.py and check-node-class-membership.sh.
The FILE-level half of ADR 1124 (a `gear: true` scope with no substrate path,
qualified-reference well-formedness) is in check-netbird-acl-parity.py and does
gate PRs.

WHAT THIS CANNOT SEE. It grades NetBird policy, not working connectivity —
Zenoh mTLS, cert URI-SANs, DNS and route state are all outside it, so a peer can
pass here and still fail to publish. Port 443 permitted is not a service
reachable; there is no L7 visibility. And it cannot speak about a group with no
members: an empty `<SCOPE>-INFRA` is the parity guard's job, not this one.
"""
from __future__ import annotations

import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

API = os.environ.get("NB_API_URL", "").rstrip("/")
TOKEN = os.environ.get("NB_API_TOKEN", "")
STRICT = "--strict" in sys.argv
AS_JSON = "--json" in sys.argv

# Ports that mean "this peer can actually reach a substrate for work" — the
# zenoh fabric listeners and mesh DNS. A rule granting only tcp 22, or only
# icmp, is not a working path and must not count as self-sufficiency.
SUBSTRATE_PORTS = {"7447", "7448", "53", "5353", "443"}

# Suffixes that identify a MACHINE. Sourced from the taxonomy so this file does
# not become a second, drifting copy of the vocabulary (ADR 1067's principle).
TAXONOMY = Path(__file__).resolve().parents[1] / "ansible" / "netbird_acl_taxonomy.yml"

# SCOPE-NEUTRAL classes: holding one of these in another scope is NOT a site
# claim, so it must not count toward "this machine says it is at two sites".
#
# `EXTERNAL-INFRA` means "externally hosted", which is a HOSTING fact about the
# goatnet, orthogonal to which site the box sits at — the same way a person's
# role is orthogonal to their site. mesh-identity-operations.md is explicit:
# "A pod at our own site takes POD alone; a hosted service that is not a pod
# takes EXTERNAL-INFRA alone. Fusing them into an OCTOPOD class would mean
# re-deciding both facts every time either changes."
#
# A pod node can be BOTH — holding `<SITE>-POD` + `<GOATNET>-EXTERNAL-INFRA` as
# its decided profile. Without this exclusion the guard reports that operator
# decision as the defect, which is a false positive on a straddle that is correct
# by design.
SCOPE_NEUTRAL_SUFFIXES = frozenset({"EXTERNAL-INFRA"})

# PERSON ROLE suffixes. A machine holding one of these is a real defect, but it
# is ADR 1116 §7's defect (gear must not wear a person role), not ADR 1124's.
# Reporting it here with "add a HOST:INFRA rule then drop the foreign groups"
# sends the reader to the wrong fix — the rule already exists; the machine is
# simply wearing the wrong kind of group. Named separately so this guard says
# which problem it actually found. If the role was granted by the machine's own
# setup key, it is a mint-shape question, not scope drift.
ROLE_SUFFIXES = frozenset({"PLATFORM-ADMIN", "ADMIN", "MONITOR", "DEVELOPER", "OBSERVER"})


def die(msg: str, code: int = 2):
    print(f"FAIL: {msg}", file=sys.stderr)
    sys.exit(code)


if not API or not TOKEN:
    die("set NB_API_URL and NB_API_TOKEN")


def declared_prefixes() -> set[str]:
    """Every `site_prefix` the taxonomy declares — persistent sites and the
    `netbird_exercises` scopes stamped on a host site's mgmt server."""
    try:
        import yaml
    except ImportError:
        die("PyYAML not available (pip install pyyaml)")
    doc = yaml.safe_load(TAXONOMY.read_text()) or {}
    out = {
        (v or {}).get("site_prefix")
        for v in (doc.get("netbird_acl_taxonomy") or {}).values()
    }
    out |= {
        (v or {}).get("site_prefix")
        for v in (doc.get("netbird_exercises") or {}).values()
    }
    return {p for p in out if p}


def gear_scopes() -> dict[str, str]:
    """`{scope: site_prefix}` for every subordinate scope declaring `gear: true`.

    These are the scopes that hold real machines, so they are the ones where an
    empty `<PREFIX>-INFRA` has a consequence rather than being a formality.
    """
    try:
        import yaml
    except ImportError:
        die("PyYAML not available (pip install pyyaml)")
    doc = yaml.safe_load(TAXONOMY.read_text()) or {}
    out = {}
    for scope, row in (doc.get("netbird_exercises") or {}).items():
        row = row or {}
        if row.get("gear") and row.get("site_prefix"):
            out[scope] = row["site_prefix"]
    return out


def machine_class_suffixes() -> set[str]:
    """Node-class + gear-class suffixes, read from the taxonomy."""
    try:
        import yaml
    except ImportError:
        die("PyYAML not available (pip install pyyaml)")
    doc = yaml.safe_load(TAXONOMY.read_text()) or {}
    suffixes: set[str] = set()
    gear = (doc.get("netbird_exercise_gear_template") or {}).get("groups") or []
    suffixes |= {g.get("suffix") for g in gear if g.get("suffix")}
    # Infrastructure node-classes from the base template. These are machines
    # too; a person is never INFRA/NETWORK/BREAKGLASS/MOON/POD.
    suffixes |= {"INFRA", "EXTERNAL-INFRA", "BACKEND-INFRA", "NETWORK", "BREAKGLASS", "MOON", "POD"}
    return {s for s in suffixes if s}


def get(path: str):
    """GET with pagination. NetBird returns a bare list today; a future paged
    response must not silently truncate an audit, so both shapes are handled."""
    out, page, seen = [], 1, 0
    while True:
        url = f"{API}{path}"
        if page > 1:
            url += ("&" if "?" in path else "?") + urllib.parse.urlencode({"page": page})
        req = urllib.request.Request(url, headers={"Authorization": f"Token {TOKEN}", "Accept": "application/json"})
        try:
            with urllib.request.urlopen(req, timeout=30) as resp:
                body = json.loads(resp.read().decode())
        except urllib.error.HTTPError as exc:
            die(f"GET {path} -> HTTP {exc.code}")
        except Exception as exc:  # noqa: BLE001
            die(f"GET {path} -> {exc}")
        if isinstance(body, dict):
            items = body.get("data") or body.get("items") or []
            out.extend(items)
            total = body.get("total_records")
            seen += len(items)
            if not items or (total is not None and seen >= total):
                return out
            page += 1
            continue
        return body  # bare list — current NetBird behaviour, no pagination


def scope_of(group_name: str, prefixes: list[str]) -> str | None:
    """Longest declared prefix that owns this group name."""
    for p in sorted(prefixes, key=len, reverse=True):
        if group_name.startswith(p + "-"):
            return p
    return None


def main() -> int:
    groups = get("/api/groups")
    peers = get("/api/peers")
    policies = get("/api/policies")

    # Scope prefixes come from the DECLARED taxonomy, never from splitting live
    # group names on hyphens. That shortcut looks reasonable and is wrong:
    # `GOATNET-EXTERNAL-INFRA` is the `EXTERNAL-INFRA` group of scope `GOATNET`, not a
    # scope named `GOATNET-EXTERNAL`, and inferring the latter reports a
    # single-scope machine as multi-scope. Suffixes are multi-word; prefixes are
    # declared.
    prefixes = declared_prefixes()
    undeclared = sorted(
        g["name"] for g in groups
        if g.get("name") != "All" and scope_of(g.get("name", ""), list(prefixes)) is None
    )
    if undeclared:
        print(
            f"note: {len(undeclared)} live group(s) match no declared scope prefix and are "
            f"not graded: {', '.join(undeclared)}",
            file=sys.stderr,
        )

    rules = []
    for p in policies:
        if not p.get("enabled"):
            continue
        for r in p.get("rules") or []:
            if not r.get("enabled") or r.get("action", "accept") != "accept":
                continue
            rules.append(
                (
                    p.get("name", "?"),
                    {x["name"] for x in (r.get("sources") or [])},
                    {x["name"] for x in (r.get("destinations") or [])},
                    r.get("protocol"),
                    set(r.get("ports") or []) or {"ALL"},
                )
            )

    machine_suffixes = machine_class_suffixes()

    def is_machine(group_names: set[str]) -> bool:
        for gname in group_names:
            s = scope_of(gname, list(prefixes))
            suffix = gname[len(s) + 1 :] if s else gname
            if suffix in machine_suffixes:
                return True
        return False

    # A substrate group with no members is not a substrate. A `SITE-INFRA` with
    # zero peers terminates the scope's own `gear → infra` rules, so counting that
    # as self-sufficiency reports the defect as
    # healthy. This is the difference between a rule existing and a path
    # existing, and it is the whole point of grading LIVE state.
    populated = {g["name"] for g in groups if (g.get("peers_count") or 0) > 0}

    def substrate_reach(held: set[str]) -> set[str]:
        """Populated destination INFRA groups this group-set reaches on working ports."""
        reached = set()
        for _name, src, dst, proto, ports in rules:
            if not (held & src):
                continue
            usable = proto in ("all", "icmp") or bool(ports & SUBSTRATE_PORTS) or "ALL" in ports
            if proto == "icmp":
                usable = False  # ping is not a working substrate path
            if not usable:
                continue
            for d in dst:
                s = scope_of(d, list(prefixes))
                suffix = d[len(s) + 1 :] if s else d
                if suffix == "INFRA" and d in populated:
                    reached.add(d)
        return reached

    dependent, fine, skipped_people = [], 0, 0
    for peer in peers:
        held = {g["name"] for g in (peer.get("groups") or [])}
        if not is_machine(held):
            skipped_people += 1
            continue
        # A MACHINE belongs to exactly one site (ADR 1043 §1.5). Holding groups
        # in two scopes asserts it is at two sites, which cannot be true. That
        # is the defect — not "unreachable", which every peer passes today.
        per_scope = {}
        for p in prefixes:
            subset = {g for g in held if scope_of(g, list(prefixes)) == p}
            # Drop scope-neutral classes before deciding "how many sites does
            # this machine claim" — see SCOPE_NEUTRAL_SUFFIXES.
            claiming = {g for g in subset if g[len(p) + 1 :] not in SCOPE_NEUTRAL_SUFFIXES}
            if claiming:
                per_scope[p] = subset
        if len(per_scope) < 2:
            fine += 1
            continue

        # Which scopes actually carry a working substrate path on their own?
        # Naming this is what makes the finding actionable: the scopes that
        # carry nothing are pure labels, and the one that carries everything is
        # the membership the machine is really running on.
        carrying = {p: sorted(substrate_reach(g)) for p, g in per_scope.items()}
        # Which kind of defect is this? If every group outside the scope that
        # owns the most classes is a PERSON ROLE, the machine is not claiming a
        # second site — it is wearing a role it should never have (ADR 1116 §7).
        # HOME is the scope where the machine holds a NODE CLASS, because the
        # class is the site-anchored fact. Counting groups instead picks the
        # wrong scope for a node that wears several roles in one goatnet and a
        # single class in its real one: the class is what says where it is.
        def classes_in(p):
            return {g for g in per_scope[p]
                    if g[len(p) + 1:] in machine_suffixes
                    and g[len(p) + 1:] not in SCOPE_NEUTRAL_SUFFIXES}
        class_scopes = [p for p in per_scope if classes_in(p)]
        if len(class_scopes) == 1:
            home = class_scopes[0]
            foreign = {g for p, gs in per_scope.items() if p != home for g in gs
                       if g[len(p) + 1:] not in SCOPE_NEUTRAL_SUFFIXES}
        else:
            # Two scopes each claim a class — a genuine two-site assertion.
            home, foreign = None, set()
        role_only = bool(foreign) and all(
            any(g.endswith("-" + r) for r in ROLE_SUFFIXES) for g in foreign
        )
        dependent.append(
            {
                "peer": peer.get("name"),
                "scopes": sorted(per_scope.keys()),
                "groups": sorted(held),
                "carries_substrate": {p: v for p, v in carrying.items() if v},
                "carries_nothing": sorted(p for p, v in carrying.items() if not v),
                "reaches": sorted(substrate_reach(held)),
                "kind": "role-on-machine" if role_only else "multi-site",
                "foreign": sorted(foreign),
            }
        )

    # ---- substrate coverage -------------------------------------------------
    # A scope-local `infra -> gear` rule resolves against `<PREFIX>-INFRA`. If
    # that group holds no peers the rule permits nothing, and the host site's
    # machines cannot initiate to this scope at all — so every guard that runs
    # on a mgmt host reports clean about a site it never looked at.
    #
    # REPORTED, NEVER FAILED. A site legitimately has no substrate until its pod
    # lands, and a guard that goes red on a normal lifecycle state is a guard
    # somebody mutes. The number is the point: it says how many sites are in
    # this state, which is the input to whether a permanent mechanism is needed
    # for them or whether the population empties itself as pods roll out.
    peers_by_group = {g["name"]: (g.get("peers_count") or 0) for g in groups}
    substrate = []
    for scope, prefix in sorted(gear_scopes().items()):
        infra = f"{prefix}-INFRA"
        held = peers_by_group.get(infra, 0)
        if held:
            continue
        stranded = sorted(
            p.get("name", "?") for p in peers
            if any(scope_of((g or {}).get("name", ""), list(prefixes)) == prefix
                   for g in (p.get("groups") or []))
            and is_machine({(g or {}).get("name", "") for g in (p.get("groups") or [])})
        )
        substrate.append({"scope": scope, "prefix": prefix,
                          "infra_group": infra, "infra_peers": 0,
                          "machines_unreachable_from_host": stranded})

    if AS_JSON:
        print(json.dumps({"cross_scope_dependent": dependent, "self_sufficient": fine,
                          "people_skipped": skipped_people,
                          "scopes_without_substrate": substrate}, indent=2))
    else:
        print(f"scope self-sufficiency — {API}")
        print(f"  {len(peers)} peers · {skipped_people} people skipped · "
              f"{fine} machines self-sufficient · {len(dependent)} cross-scope dependent\n")

        if substrate:
            print(f"NO LOCAL SUBSTRATE — {len(substrate)} gear scope(s). Reported, not failed:")
            for s in substrate:
                n = len(s["machines_unreachable_from_host"])
                print(f"  {s['scope']:<12} {s['infra_group']}: 0 peers  -> "
                      f"{n} machine(s) unreachable from the host site")
                for m in s["machines_unreachable_from_host"]:
                    print(f"                 {m}")
            print("  Scope-local `infra -> gear` resolves against an empty group, so it")
            print("  permits nothing, and ADR 1124 refuses INFRA as a cross-scope source.")
            print("  People still reach these machines via PLATFORM-ADMIN; MACHINES do not,")
            print("  so any guard running on a mgmt host reports clean about them without")
            print("  having looked. Expected before a site's pod lands — the pod joins")
            print("  <PREFIX>-INFRA and this entry disappears on its own.\n")
        else:
            print("every gear scope has a populated INFRA group\n")
        for d in dependent:
            print(f"MULTI-SCOPE {d['peer']}")
            print(f"      scopes    {', '.join(d['scopes'])} — a machine is at ONE site")
            print(f"      groups    {', '.join(d['groups'])}")
            for scope, reached in sorted(d["carries_substrate"].items()):
                print(f"      {scope:<9} carries the substrate path -> {', '.join(reached)}")
            for scope in d["carries_nothing"]:
                print(f"      {scope:<9} carries NO substrate path — a label only")
            if d.get("kind") == "role-on-machine":
                print(f"      NOTE      the foreign group(s) are PERSON ROLES: {', '.join(d['foreign'])}")
                print("      fix       this is ADR 1116 §7 (gear must not wear a person role), NOT")
                print("                a scope problem — the HOST:INFRA rule already exists. Re-mint")
                print("                or re-stamp the machine with a node class instead.\n")
            else:
                print("      fix       give the scope that should own this machine a `HOST:INFRA`")
                print("                rule (ADR 1124), then drop the foreign-scope groups.\n")
        multi = [d for d in dependent if d.get("kind") != "role-on-machine"]
        roles = [d for d in dependent if d.get("kind") == "role-on-machine"]
        if multi:
            print(f"{len(multi)} machine(s) assert more than one SITE. The site dimension is")
            print("untrue for each, and each is over-granted by construction — it holds a")
            print("whole foreign scope's group to obtain one path.")
        if roles:
            print(f"{len(roles)} machine(s) wear a PERSON ROLE from another scope. That is ADR")
            print("1116 §7, not ADR 1124 — the cross-scope rule they need already exists.")
            print("Listed here because this guard reads the same membership, not because it")
            print("owns the fix.")
        else:
            print("Every machine belongs to exactly one scope. ADR 1124 target state.")

    return 1 if (STRICT and dependent) else 0


if __name__ == "__main__":
    sys.exit(main())
