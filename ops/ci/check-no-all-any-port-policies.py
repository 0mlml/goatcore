#!/usr/bin/env python3
"""Refuse a NetBird policy that opens every port from everyone to one peer.

    check-no-all-any-port-policies.py policies.json
    curl -s -H "Authorization: Token $T" "$NB/api/policies" | check-no-all-any-port-policies.py
    check-no-all-any-port-policies.py --self-test

ADR 1216. A rule whose source is the group `All`, whose protocol is `all`, and
whose destination is either a single device (a `destinationResource` of type
`peer`, or of type `host`: one routed address) or the group `All` grants every
enrolled machine on the goatnet every port on that destination. A
bidirectional rule is graded in both directions, so writing the same thing
as "device group -> All, both ways" is the same violation. It is how a device ends up manageable in a hurry,
and it bypasses everything the warrant registry declares. The declared path
for a network engineer is the NET-ENGINEER group on the `device-admin` port
set (ops/warrant/port-sets.yaml).

FAILS (exit 1) on any rule that is ENABLED, inside an ENABLED policy, and has,
in either direction it permits:
  - `All` among its source groups,
  - protocol `all`,
  - a destination that is a single peer or host resource, or the group `All`.

A rule that names ports, a protocol other than `all`, a narrower source group,
or a group destination other than `All` passes: those are declared shapes the
ACL matrix and its assertions already cover. A `subnet` or `domain` resource
is a network route, not one device, and is outside this guard. A disabled
policy or rule passes.

READ-ONLY. It reads a JSON document and writes nothing anywhere. Input is the
NetBird `GET /api/policies` response: a list of policies, or an object with a
`policies` list. Source and destination entries may be group objects (with
`name`) or bare ids; a bare id is resolved through `--groups FILE` (the
`GET /api/groups` response) when one is given, and otherwise only the literal
`All` matches.

Exit 0 clean, 1 on a violation, 2 on input that is not a policy list.
"""

from __future__ import annotations

import argparse
import json
import sys
import tempfile
from pathlib import Path

ALL = "All"


class InputError(ValueError):
    pass


def _policies(doc):
    if isinstance(doc, dict) and isinstance(doc.get("policies"), list):
        doc = doc["policies"]
    if not isinstance(doc, list):
        raise InputError("expected a list of NetBird policies (GET /api/policies)")
    for p in doc:
        if not isinstance(p, dict):
            raise InputError("a policy entry is not an object")
    return doc


def _group_names(entries, by_id):
    names = set()
    for e in entries or []:
        if isinstance(e, dict):
            n = e.get("name") or by_id.get(str(e.get("id", "")), "")
        else:
            n = by_id.get(str(e), str(e))
        if n:
            names.add(n)
    return names


def _resource(rule, key):
    r = rule.get(key)
    if r is None:
        # snake_case, as some API clients and exports spell it
        snake = "source_resource" if key == "sourceResource" else "destination_resource"
        r = rule.get(snake)
    return r if isinstance(r, dict) else None


ONE_DEVICE = ("peer", "host")


def _directions(rule):
    """(sources, destinations, destination resource) for each direction the
    rule permits: one, or two when it is bidirectional."""
    out = [(rule.get("sources"), rule.get("destinations"), _resource(rule, "destinationResource"))]
    if rule.get("bidirectional"):
        out.append((rule.get("destinations"), rule.get("sources"), _resource(rule, "sourceResource")))
    return out


def violations(doc, groups=None):
    by_id = {}
    for g in groups or []:
        if isinstance(g, dict) and g.get("id") is not None:
            by_id[str(g["id"])] = g.get("name", "")
    found = []
    for pol in _policies(doc):
        if pol.get("enabled") is False:
            continue
        for rule in pol.get("rules") or []:
            if not isinstance(rule, dict) or rule.get("enabled") is False:
                continue
            if str(rule.get("protocol", "")).lower() != "all":
                continue
            targets = []
            for sources, destinations, dres in _directions(rule):
                if ALL not in _group_names(sources, by_id):
                    continue
                dtype = str(dres.get("type", "")).lower() if dres else ""
                one_device = dtype in ONE_DEVICE
                to_all = ALL in _group_names(destinations, by_id)
                if not (one_device or to_all):
                    continue
                target = ("%s %s" % (dtype, dres.get("id"))) if one_device else "group All"
                if target not in targets:
                    targets.append(target)
            for target in targets:
                found.append({
                    "policy": pol.get("name") or pol.get("id") or "<unnamed>",
                    "policy_id": pol.get("id"),
                    "rule": rule.get("name") or rule.get("id") or "<unnamed>",
                    "target": target,
                })
    return found


def report(found):
    if not found:
        print("no-all-any-port policies: PASS — no enabled rule opens every port "
              "from All to a single peer or to All")
        return 0
    print("ALL -> ANY-PORT POLICIES (ADR 1216) — %d enabled rule(s):\n" % len(found))
    for f in found:
        print("  policy %r (id %s), rule %r: source All, protocol all, destination %s"
              % (f["policy"], f["policy_id"], f["rule"], f["target"]))
    print(
        "\nEvery machine on the goatnet reaches that destination on every port.\n"
        "Declare the access instead: a network engineer reaches NETWORK, CONTROL\n"
        "and EDGE-SERVER through the NET-ENGINEER group on the device-admin port\n"
        "set (ops/warrant/rule-intents/netbird-acl.yaml, ADR 1216), then disable\n"
        "the hand-made policy."
    )
    return 1


# ── self-test ───────────────────────────────────────────────────────────────

def _rule(**kw):
    r = {"name": "r", "enabled": True, "action": "accept", "bidirectional": False,
         "protocol": "all", "sources": [{"id": "gAll", "name": "All"}],
         "destinations": [], "destinationResource": {"id": "peer-1", "type": "peer"}}
    r.update(kw)
    return r


FIXTURES = {
    # name: (policies document, expected violation count)
    "hand-made All -> one peer, any port": (
        [{"id": "p1", "name": "switch access", "enabled": True, "rules": [_rule()]}], 1),
    "All -> All, any port": (
        [{"id": "p2", "name": "Default", "enabled": True,
          "rules": [_rule(destinations=[{"id": "gAll", "name": "All"}], destinationResource=None)]}], 1),
    "wrapped in {policies: [...]}, snake_case resource": (
        {"policies": [{"id": "p3", "name": "x", "enabled": True,
                       "rules": [_rule(destinationResource=None,
                                       destination_resource={"id": "peer-9", "type": "peer"})]}]}, 1),
    "bare ids resolved through --groups": (
        [{"id": "p4", "name": "ids", "enabled": True,
          "rules": [_rule(sources=["gAll"])]}], 1),
    "disabled policy passes": (
        [{"id": "p5", "name": "off", "enabled": False, "rules": [_rule()]}], 0),
    "disabled rule passes": (
        [{"id": "p6", "name": "x", "enabled": True, "rules": [_rule(enabled=False)]}], 0),
    "port-scoped tcp to one peer passes": (
        [{"id": "p7", "name": "SSH Access", "enabled": True,
          "rules": [_rule(protocol="tcp", ports=["22"])]}], 0),
    "narrower source group passes": (
        [{"id": "p8", "name": "x", "enabled": True,
          "rules": [_rule(sources=[{"id": "g1", "name": "SITE_ONE-NET-ENGINEER"}])]}], 0),
    "All -> a named group passes": (
        [{"id": "p9", "name": "x", "enabled": True,
          "rules": [_rule(destinationResource=None,
                          destinations=[{"id": "g2", "name": "ZAGROS-PROD-INFRA"}])]}], 0),
    "a host resource is one device: fails": (
        [{"id": "p10", "name": "x", "enabled": True,
          "rules": [_rule(destinationResource={"id": "h1", "type": "host"},
                          destinations=[{"id": "g3", "name": "ZAGROS-PROD-NETWORK"}])]}], 1),
    "a subnet resource is a route, not a device: passes": (
        [{"id": "p11", "name": "x", "enabled": True,
          "rules": [_rule(destinationResource={"id": "s1", "type": "subnet"},
                          destinations=[{"id": "g3", "name": "ZAGROS-PROD-NETWORK"}])]}], 0),
    "bidirectional one peer <-> All, any port, is the same exposure: fails": (
        [{"id": "p12", "name": "x", "enabled": True,
          "rules": [_rule(bidirectional=True, destinationResource=None, sources=[],
                          sourceResource={"id": "peer-7", "type": "peer"},
                          destinations=[{"id": "gAll", "name": "All"}])]}], 1),
    "one-way one peer -> All is not this guard's shape: passes": (
        [{"id": "p13", "name": "x", "enabled": True,
          "rules": [_rule(destinationResource=None, sources=[],
                          sourceResource={"id": "peer-7", "type": "peer"},
                          destinations=[{"id": "gAll", "name": "All"}])]}], 0),
    "bidirectional All -> All counts once": (
        [{"id": "p14", "name": "x", "enabled": True,
          "rules": [_rule(bidirectional=True, destinationResource=None,
                          destinations=[{"id": "gAll", "name": "All"}])]}], 1),
}
GROUPS_FIXTURE = [{"id": "gAll", "name": "All"}, {"id": "g1", "name": "SITE_ONE-NET-ENGINEER"}]


def self_test():
    failed = 0
    for name, (doc, want) in FIXTURES.items():
        got = len(violations(doc, GROUPS_FIXTURE))
        ok = got == want
        failed += not ok
        print("  %s  %s (want %d, got %d)" % ("ok  " if ok else "FAIL", name, want, got))
    # End to end through main(): file argument, exit codes, malformed input.
    with tempfile.TemporaryDirectory() as d:
        bad = Path(d) / "bad.json"
        bad.write_text(json.dumps(FIXTURES["hand-made All -> one peer, any port"][0]))
        good = Path(d) / "good.json"
        good.write_text(json.dumps(FIXTURES["port-scoped tcp to one peer passes"][0]))
        junk = Path(d) / "junk.json"
        junk.write_text('{"not": "policies"}')
        for label, path, want in (("violating file exits 1", bad, 1),
                                  ("clean file exits 0", good, 0),
                                  ("non-policy input exits 2", junk, 2)):
            got = main([str(path)], quiet=True)
            ok = got == want
            failed += not ok
            print("  %s  %s (want %d, got %d)" % ("ok  " if ok else "FAIL", label, want, got))
    if failed:
        print("\nno-all-any-port policies self-test: %d case(s) FAILED" % failed)
        return 1
    print("\nno-all-any-port policies self-test: every case grades correctly")
    return 0


def main(argv=None, quiet=False):
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[1])
    ap.add_argument("policies", nargs="?", help="GET /api/policies JSON; stdin if omitted")
    ap.add_argument("--groups", help="GET /api/groups JSON, to resolve bare group ids")
    ap.add_argument("--self-test", action="store_true")
    a = ap.parse_args(argv)
    if a.self_test:
        return self_test()
    try:
        raw = Path(a.policies).read_text() if a.policies else sys.stdin.read()
        doc = json.loads(raw)
        groups = json.loads(Path(a.groups).read_text()) if a.groups else None
        if isinstance(groups, dict) and isinstance(groups.get("groups"), list):
            groups = groups["groups"]
        found = violations(doc, groups)
    except (OSError, json.JSONDecodeError, InputError) as exc:
        if not quiet:
            print("REFUSING: %s" % exc, file=sys.stderr)
        return 2
    if quiet:
        return 1 if found else 0
    return report(found)


if __name__ == "__main__":
    sys.exit(main())
