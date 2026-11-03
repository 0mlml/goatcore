#!/usr/bin/env python3
"""Cross-goatnet service-config parity — the fail-open-default guard.

THE FAILURE THIS CATCHES: a Class-A key declared on one goatnet and not another,
where the role default is fail-open.

  `portal_kratos_admin_url` defaults to `""`. A goatnet that omits it brings the
  portal up with its identity layer silently OFF, and
  GET /api/access/who?id=<any person email> answers "no identity matched /
  identity layer unavailable" for EVERY person. Nothing goes red. Every container
  is healthy. The service is deployed; the capability is not.

The same shape:

  * a service's data-plane keys absent -> its adapters self-disable and the page
    renders an empty shell.
  * `microscope_rbac_mode` absent -> role default `off` -> the admin fence runs
    its authn half with authz dark.
  * `audit_subscriber_id` copied from another goatnet -> surfaces key-prefix their
    audit objects as that goatnet.
  * capability flags absent from a skeleton -> services declared in
    isitup_services but never actually deployed by their roles.

`instance-variability-model.md` §4 NAMES this as the Class-A fail-open hazard.

This script is that gate.

WHAT IT CHECKS. For every service deployed on two or more governed goatnets,
every config key one goatnet declares must be declared by the others — but
ONLY when omitting it lands on a FAIL-OPEN role default. A default of `""`,
`false`, `off`, `none`, or one that hardcodes another environment's name is
fail-open: the service starts, and the capability is quietly absent or points
somewhere else. A default that is a sane universal (a port, a timeout, a
retention window) is fail-safe and is not reported, because demanding parity
on those is noise that trains people to ignore the check.

WHAT IT DELIBERATELY DOES NOT DO. It never reads a value, only whether a key
is DECLARED. Two goatnets legitimately hold different URLs, secrets and IDs —
that is the point of per-environment config. The question it asks is the one
that actually failed: "did this environment decide, or did it inherit a
silence?"

Exit: 0 ok, 1 parity gap, 2 usage/schema error.
"""

from __future__ import annotations

import os
import sys

import yaml

REPO = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
GROUP_VARS = os.path.join(REPO, "ops", "ansible", "group_vars")
ROLES = os.path.join(REPO, "ops", "ansible", "roles")
FLEET_PINS = os.path.join(REPO, "ops", "pins", "fleet-pins.yaml")
WAIVERS = os.path.join(REPO, "ops", "ci", "service-config-parity-waivers.yaml")

# A default matching one of these means "omitting the key disables or
# misdirects the capability" — the fail-open shape.
#
# The second group covers keys like `id_admin_console_audit_backend`. Its role
# default is "stderr"; a goatnet that declares nothing has its console write its
# audit trail to container stderr — ephemeral, gone on the next restart — while
# the file microscope reads stays 0 bytes.
# Nothing was empty, false, or another environment's name, so the first
# version of this check saw nothing wrong.
#
# The shape these share is not "falsy". It is: THE SERVICE STARTS AND APPEARS
# TO WORK, BUT THE DURABLE OR ENFORCING BEHAVIOUR IS ABSENT. A sink that does
# not outlive the process, a store that does not outlive a restart, a mode
# that simulates instead of acting. Those are exactly as fail-open as "" and
# strictly harder to notice, because the surface looks alive.
FAIL_OPEN_SCALARS = {
    "", "false", "off", "no", "none", "null", "0",
    # non-durable sinks / stores
    "stderr", "stdout", "memory", "inmemory", "in-memory", "tmpfs", "ephemeral",
    # no-op / simulated modes
    "noop", "no-op", "mock", "stub", "fake", "dry-run", "dryrun", "disabled",
}

# Environment tokens that must never appear in a role default that another
# environment relies on: inheriting one of these silently points a new goatnet
# at an existing one's resources.
def _foreign_env_tokens():
    """Declared in ops/goatnets/cross-env.yaml (`foreign_env_tokens`); an instance
    without that file has no other goatnet to confuse a role default with."""
    path = os.path.join(REPO, "ops", "goatnets", "cross-env.yaml")
    try:
        with open(path) as fh:
            doc = yaml.safe_load(fh) or {}
    except OSError:
        return ()
    return tuple(doc.get("foreign_env_tokens") or ())


FOREIGN_ENV_TOKENS = _foreign_env_tokens()


def load_yaml(path):
    try:
        with open(path) as fh:
            return yaml.safe_load(fh) or {}
    except Exception:
        return {}


def governed_goatnets():
    """{goatnet: [member group_vars dirs]} from the fleet pin table."""
    tbl = load_yaml(FLEET_PINS)
    out = {}
    for name, spec in (tbl.get("goatnets") or {}).items():
        members = spec.get("members") or []
        if members:
            out[name] = members
    return out


def declared_keys(member: str) -> set[str]:
    """Every top-level key declared anywhere in one member's group_vars dir."""
    d = os.path.join(GROUP_VARS, member)
    keys: set[str] = set()
    if not os.path.isdir(d):
        return keys
    for fn in sorted(os.listdir(d)):
        if fn.endswith((".yml", ".yaml")):
            data = load_yaml(os.path.join(d, fn))
            if isinstance(data, dict):
                keys.update(data.keys())
    return keys


def role_defaults() -> dict[str, tuple[object, str]]:
    """{key: (default_value, owning_role)} across every role's defaults/main.yml.

    Ownership comes from WHICH ROLE DECLARES THE DEFAULT, never from the key's
    spelling. Matching a prefix against role directory names misses real cases:
    the key `portal_kratos_admin_url` belongs to the role `goat-zenoh-portal`, so
    no prefix matches and the key is skipped entirely.

    A key defined by several roles keeps the FIRST role seen; the check only
    asks whether the default is fail-open shaped.
    """
    out: dict[str, tuple[object, str]] = {}
    if not os.path.isdir(ROLES):
        return out
    for role in sorted(os.listdir(ROLES)):
        p = os.path.join(ROLES, role, "defaults", "main.yml")
        if os.path.isfile(p):
            data = load_yaml(p)
            if isinstance(data, dict):
                for k, v in data.items():
                    out.setdefault(k, (v, role))
    return out


def fail_open(default) -> str | None:
    """Return a reason string when omitting the key is unsafe, else None."""
    if default is None:
        return "no role default at all — an undefined variable or a silent skip"
    if isinstance(default, bool):
        return "role default is false — the capability ships OFF" if default is False else None
    if isinstance(default, (int, float)):
        return None
    s = str(default).strip()
    if s.lower() in FAIL_OPEN_SCALARS:
        return f"role default is {s!r} — the capability ships disabled"
    low = s.lower()
    for tok in FOREIGN_ENV_TOKENS:
        if tok not in low:
            continue
        # FALSE-POSITIVE GUARD. A foreign token sitting inside a
        # Jinja `| default('nova')` is UNREACHABLE when the guarding variable is
        # defined — `"brief.{{ site_id | default('nova') }}.…"` renders
        # `brief.<site>.…` on any goatnet that sets site_id, which is all of
        # them. Flagging those buries an entry that matters (a LITERAL default
        # with nothing guarding it) among harmless rows — and a list that reads as
        # noise gets waived wholesale.
        #
        # So: only flag when the foreign token is reachable, i.e. it is NOT
        # inside a `| default(...)` expression. A guard that cries wolf is a
        # guard that gets silenced, and silencing it is how the real one
        # survived three days on a production goatnet.
        if _token_only_inside_jinja_default(s, tok):
            continue
        return f"role default hardcodes another environment ({s!r}) — omitting it INHERITS that environment"
    return None


def _token_only_inside_jinja_default(default: str, token: str) -> bool:
    """True when every occurrence of `token` sits inside a `| default(...)`."""
    import re as _re
    guarded_spans = [m.span() for m in _re.finditer(r"\|\s*default\([^)]*\)", default)]
    low = default.lower()
    start = 0
    while True:
        i = low.find(token, start)
        if i == -1:
            return True  # no unguarded occurrence found
        if not any(a <= i < b for a, b in guarded_spans):
            return False  # this one is reachable
        start = i + 1


def main() -> int:
    goatnets = governed_goatnets()
    if len(goatnets) < 2:
        print("ok: fewer than two governed goatnets — nothing to compare.")
        return 0

    defaults = role_defaults()
    # role -> every key that role declares a default for. This is the service
    # vocabulary: "does this goatnet run the role" is answered by whether it
    # declares ANY of that role's keys.
    role_keys: dict[str, set[str]] = {}
    for k, (_v, role) in defaults.items():
        role_keys.setdefault(role, set()).add(k)
    waivers = load_yaml(WAIVERS).get("waivers") or {}

    # goatnet -> union of every member's declared keys
    per_net: dict[str, set[str]] = {
        net: set().union(*(declared_keys(m) for m in members)) if members else set()
        for net, members in goatnets.items()
    }

    findings = []
    for key, (default, svc) in sorted(defaults.items()):
        declaring = [n for n in goatnets if key in per_net[n]]
        if not declaring:
            continue  # nobody declares it; the default governs everywhere, fine
        # A goatnet is "running" the role if it declares ANY of that role's keys.
        running = [n for n in goatnets if per_net[n] & role_keys[svc]]
        missing = [n for n in running if key not in per_net[n]]
        if not missing:
            continue
        reason = fail_open(default)
        if reason is None:
            continue  # fail-safe default; a per-env decision is not required
        for net in missing:
            if net in (waivers.get(key) or {}):
                continue
            findings.append((svc, key, net, declaring, reason))

    if not findings:
        print(f"ok: service-config parity — {len(goatnets)} goatnets, no fail-open omissions.")
        return 0

    print("SERVICE-CONFIG PARITY GAPS\n")
    print("Each row: a goatnet runs the service, another goatnet declares this key,")
    print("and omitting it lands on a fail-open role default.\n")
    last_svc = None
    for svc, key, net, declaring, reason in sorted(findings):
        if svc != last_svc:
            print(f"  [{svc}]")
            last_svc = svc
        print(f"    {key}")
        print(f"      missing on : {net}")
        print(f"      declared by: {', '.join(declaring)}")
        print(f"      why it bites: {reason}")
    print(f"\n{len(findings)} gap(s).")
    print("Fix by declaring the key for that goatnet, or waive it with a reason in")
    print(f"{os.path.relpath(WAIVERS, REPO)} if the omission is genuinely intended.")
    return 1


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as exc:  # noqa: BLE001
        print(f"error: {exc}", file=sys.stderr)
        sys.exit(2)
