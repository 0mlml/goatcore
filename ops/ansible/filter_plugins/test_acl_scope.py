#!/usr/bin/env python3
"""Unit test for the ADR 1124 scope-qualified reference resolver.

Standalone (no pytest dependency — the repo's CI runs bats and plain python);
exits non-zero on any failed assertion. Run:

    python3 ops/ansible/filter_plugins/test_acl_scope.py

What it locks — every one of these is a constraint ADR 1124 decided, and a
regression in any of them silently widens a cross-scope grant:

  - bare suffixes still resolve in the rule's own scope (no behaviour change),
  - HOST: resolves via host_site; a concrete scope name resolves via the map,
  - an unresolvable HOST / unknown scope FAILS rather than falling back local,
  - bidirectional + qualified is refused (the reverse-grant hole),
  - qualified on both sides is refused,
  - UPWARD destinations are limited to INFRA and must be port-scoped,
  - DOWNWARD sources are limited to PLATFORM-ADMIN and MAY be any-port,
  - PLATFORM-ADMIN -> HOST:INFRA (a site admin administering the goatnet
    substrate) is refused — the boundary the tactical-site scope exists to hold,
  - template rules may use HOST: but never a concrete scope name.
"""
from __future__ import annotations

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from acl_scope import (  # noqa: E402
    AclScopeError,
    resolve_ref,
    resolve_refs,
    rule_uses_qualified,
    validate_rule,
)

FAILS = []
SCOPES = {"zagros-prod": "ZAGROS-PROD", "site-one": "SITE_ONE", "zagros": "ZAGROS"}


def check(cond, msg):
    if not cond:
        FAILS.append(msg)


def check_raises(fn, msg):
    try:
        fn()
    except AclScopeError:
        return
    except Exception as exc:  # noqa: BLE001
        FAILS.append("{} (raised {} instead of AclScopeError)".format(msg, type(exc).__name__))
        return
    FAILS.append(msg)


# --- resolution ------------------------------------------------------------
check(resolve_ref("INFRA", "SITE_ONE") == "SITE_ONE-INFRA", "bare suffix must resolve in its own scope")
check(
    resolve_ref("HOST:INFRA", "SITE_ONE", "ZAGROS-PROD") == "ZAGROS-PROD-INFRA",
    "HOST: must resolve via the host prefix",
)
check(
    resolve_ref("zagros-prod:INFRA", "SITE_ONE", None, SCOPES) == "ZAGROS-PROD-INFRA",
    "a concrete scope name must resolve via the scope map",
)
check(
    resolve_refs(["KID", "HOST:INFRA"], "SITE_ONE", "ZAGROS-PROD") == ["SITE_ONE-KID", "ZAGROS-PROD-INFRA"],
    "mixed bare + qualified lists must resolve elementwise",
)
check(resolve_refs([], "SITE_ONE") == [], "an empty list resolves to an empty list")

# Fail closed — never silently resolve to the local scope.
check_raises(lambda: resolve_ref("HOST:INFRA", "SITE_ONE"), "HOST: with no host_prefix must raise")
check_raises(
    lambda: resolve_ref("nosuchscope:INFRA", "SITE_ONE", None, SCOPES),
    "an undeclared scope name must raise",
)
check_raises(lambda: resolve_ref("HOST:", "SITE_ONE", "ZAGROS-PROD"), "an empty suffix must raise")

# --- detection -------------------------------------------------------------
check(
    rule_uses_qualified({"sources": ["KID"], "destinations": ["HOST:INFRA"]}),
    "a qualified destination must be detected",
)
check(
    not rule_uses_qualified({"sources": ["KID"], "destinations": ["INFRA"]}),
    "an all-bare rule must not be flagged as qualified",
)


def upward(**kw):
    rule = {
        "name": "gear -> host-site infra",
        "sources": ["KID"],
        "destinations": ["HOST:INFRA"],
        "protocol": "tcp",
        "ports": ["7447"],
        "bidirectional": False,
    }
    rule.update(kw)
    return rule


def downward(**kw):
    rule = {
        "name": "host-site platform-admin -> gear",
        "sources": ["HOST:PLATFORM-ADMIN"],
        "destinations": ["KID"],
        "protocol": "all",
        "bidirectional": False,
    }
    rule.update(kw)
    return rule


# --- the rules that must pass ---------------------------------------------
check(validate_rule(upward()) == [], "the canonical upward rule must be legal")
check(validate_rule(upward(protocol="icmp", ports=None)) == [], "icmp upward needs no port list")
check(
    validate_rule(downward()) == [],
    "the canonical downward admin rule must be legal, including protocol: all",
)
check(
    validate_rule({"name": "intra", "sources": ["P2P"], "destinations": ["KID"], "protocol": "all"}) == [],
    "an unqualified rule must be untouched by these constraints",
)
check(validate_rule(upward(), template=True) == [], "HOST: is legal inside a template")

# --- the rules that must fail ---------------------------------------------
check(validate_rule(upward(bidirectional=True)), "bidirectional + qualified must be refused")
check(
    validate_rule(upward(destinations=["HOST:PLATFORM-ADMIN"])),
    "an upward destination outside {INFRA} must be refused",
)
check(
    validate_rule(upward(destinations=["HOST:BACKEND-INFRA"])),
    "reaching the hidden backend tier of another scope must be refused",
)
check(validate_rule(upward(protocol="all", ports=None)), "protocol: all upward must be refused")
check(validate_rule(upward(ports=None)), "an upward tcp rule with no ports must be refused")
check(
    validate_rule(downward(sources=["HOST:INFRA"])),
    "INFRA as a qualified source must be refused — machines get function, not any-port",
)
check(
    validate_rule({"name": "both", "sources": ["HOST:PLATFORM-ADMIN"],
                   "destinations": ["HOST:INFRA"], "protocol": "tcp", "ports": ["443"],
                   "bidirectional": False}),
    "qualified on both sides must be refused",
)
check(
    validate_rule(upward(destinations=["zagros-prod:INFRA"]), template=True),
    "a concrete scope name inside a template must be refused",
)

# THE boundary case, stated as its own test because it is the one the tactical
# scope exists to hold: a site admin must never gain any-port administrative
# reach to the goatnet substrate.
site_admin_up = {
    "name": "platform-admin -> host-site infra (any port)",
    "sources": ["PLATFORM-ADMIN"],
    "destinations": ["HOST:INFRA"],
    "protocol": "all",
    "bidirectional": False,
}
check(
    validate_rule(site_admin_up),
    "a site admin reaching the host substrate on any port MUST be refused (ADR 1124)",
)

if FAILS:
    print("FAIL — {} assertion(s):".format(len(FAILS)), file=sys.stderr)
    for f in FAILS:
        print("  - {}".format(f), file=sys.stderr)
    sys.exit(1)
print("ok: acl_scope — resolution, fail-closed behaviour and all ADR 1124 constraints hold")
