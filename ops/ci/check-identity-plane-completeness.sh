#!/usr/bin/env bash
# ops/ci/check-identity-plane-completeness.sh — the fail-closed fence for a
# goatnet's LOGIN plane group_vars.
#
# WHY THIS EXISTS:
#
# A goatnet's group_vars are authored fresh rather than copied from a working
# goatnet — correct per instance-variability-model.md, since the Ory secrets are
# Class-B and MUST NOT be shared. But "authored fresh" means every var that is
# only a COMMENT in the reference site can be silently omitted:
#
#   ory_recovery_enabled              omitted -> role default false -> EVERY
#                                     welcome-email "set your password" link
#                                     400s "Recovery is not allowed because it
#                                     was disabled". The link mints fine via the
#                                     admin API, the email looks healthy, and the
#                                     failure only appears when the human clicks.
#                                     The onboardee is stranded.
#   mgmt_host_compose_watch_services  omitted -> empty -> "revive everything" ->
#                                     the watcher restarts kratos-migrate /
#                                     hydra-migrate (exit-0 by design) every poll
#                                     cycle.
#   kratos/hydra/ui secrets           omitted -> ory-deploy.yml aborts on its
#                                     Require-secrets assert. This one at least
#                                     fails LOUDLY at deploy time; the other two
#                                     do not fail at all.
#
# The common shape: a var whose absence is INDISTINGUISHABLE from a deliberate
# choice, on a plane where the symptom surfaces days later in someone else's
# inbox. This check makes absence a build failure instead.
#
# THE RULE: every group_vars scope declaring `mgmt_stack_profile: full` runs
# Kratos + Hydra + selfservice-ui — a human login plane — and must therefore
# declare each key below EXPLICITLY. `ory_recovery_enabled: false` is a valid
# answer (the prov_* standbys mint no admin recovery links); silence
# is not.
#
# Deliberately keyed on mgmt_stack_profile, NOT on id_admin_console_enabled: a
# site can run the console with zero id_admin_console_* keys in its group_vars,
# so a console-keyed rule would miss it.
#
# Fail-closed ratchet: a new full-profile scope inherits every requirement here
# automatically, so coverage may only grow.
set -euo pipefail

ANSIBLE_DIR="${1:-ops/ansible}"

python3 - "$ANSIBLE_DIR" <<'PY'
import glob
import os
import re
import sys

ansible_dir = sys.argv[1]

# Keys every full-profile (Kratos-bearing) scope must declare explicitly.
# value_check: None = presence is enough; callable = extra validation on the
# raw right-hand side of the assignment.
def _nonempty(v):
    v = v.strip().strip('"').strip("'")
    return (True, "") if v else (False, "declared but EMPTY")


def _bool_literal(v):
    v = v.strip().strip('"').strip("'").lower()
    if v in ("true", "false"):
        return True, ""
    return False, f"must be a literal true/false, got {v!r}"


def _watch_allowlist(v):
    v = v.strip().strip('"').strip("'")
    if not v:
        return False, (
            "declared but EMPTY — an empty allowlist means REVIVE EVERYTHING, "
            "which restarts kratos-migrate/hydra-migrate every poll cycle"
        )
    missing = [s for s in ("kratos", "hydra") if s not in v.split()]
    if missing:
        return False, (
            f"omits {'+'.join(missing)} — the Ory containers would not be revived "
            "after a host restart"
        )
    return True, ""


def _declared(v):
    """Presence is the whole requirement; an explicit empty value is a valid answer.

    Used for keys where "this goatnet deliberately does not do X" is legitimate
    but must be WRITTEN DOWN, because absence and deliberate-off are otherwise
    indistinguishable in a fresh-authored scope.
    """
    return True, ""


REQUIRED = {
    "kratos_cookie_secret": _nonempty,
    "kratos_cipher_secret": _nonempty,
    "hydra_secrets_system": _nonempty,
    "hydra_secrets_cookie": _nonempty,
    "ui_cookie_secret": _nonempty,
    "ui_csrf_secret": _nonempty,
    "mgmt_host_compose_watch_services": _watch_allowlist,
    "ory_recovery_enabled": _bool_literal,
    # A list header (`site_admins:` followed by `- email:` items) has an empty
    # right-hand side, so presence is all this can check here. That is still the
    # whole failure mode: a site that omits the key entirely.
    "site_admins": _declared,
    "netbird_pkce_client_id": _declared,
}

WHY = {
    "ory_recovery_enabled": (
        "absent -> role default false -> every admin-minted password link 400s "
        "'Recovery is not allowed because it was disabled' (silent until a human clicks)"
    ),
    "mgmt_host_compose_watch_services": (
        "absent -> empty -> watcher revives kratos-migrate/hydra-migrate forever"
    ),
    "site_admins": (
        "absent -> EVERY identity task in dashboard-deploy.yml is gated "
        "`when: site_admins | length > 0` and silently no-ops -> the F-238 admin "
        "reconciler never runs -> people sign in fine and land as role `user`, so "
        "the failure looks like broken auth and is actually zero authorization. "
        "zagros-prod shipped this way; declare `site_admins: []` if a goatnet "
        "genuinely has none"
    ),
    "netbird_pkce_client_id": (
        "absent -> management.json gets no PKCEAuthorizationFlow and no "
        "DeviceAuthorizationFlow -> management answers NotFound for both and every "
        "client dies with 'no SSO provider returned from management' -> the goatnet "
        "is setup-key-only and `netbird ssh` cannot obtain its JWT. Declare "
        "`netbird_pkce_client_id: \"\"` to say setup-key-only on purpose"
    ),
}

# Top-level `key: value` only — indented keys are nested under some other
# mapping and are NOT the scope-level declaration this check is about.
ASSIGN = re.compile(r"^([a-z_][a-z0-9_]*):(.*)$")


def scope_vars(path):
    """Map top-level key -> raw value string, plus any key declared twice.

    Duplicates matter here and are otherwise invisible: YAML silently keeps the
    LAST occurrence, so two concurrent branches each adding the same identity
    key merge into a file that parses fine and passes every completeness check
    while carrying two sources of truth. That happened for real on
    zagros_prod_hosts (2026-08-05, PR #2127 vs the standup track — 8 duplicated
    Ory keys). The values agreed that time; the next collision is where one
    branch's value silently wins.
    """
    out = {}
    dupes = {}
    with open(path, encoding="utf-8") as fh:
        for lineno, line in enumerate(fh, 1):
            if line.startswith("#"):
                continue
            m = ASSIGN.match(line.rstrip("\n"))
            if not m:
                continue
            key = m.group(1)
            if key in out:
                dupes.setdefault(key, []).append(lineno)
            else:
                out[key] = m.group(2)
    return out, dupes


failures = []
checked = []

pattern = os.path.join(ansible_dir, "group_vars", "*", "vars.yml")
for path in sorted(glob.glob(pattern)):
    scope = os.path.basename(os.path.dirname(path))
    kv, dupes = scope_vars(path)

    profile = kv.get("mgmt_stack_profile", "")
    if profile.strip().strip('"').strip("'") != "full":
        continue

    checked.append(scope)

    # Any duplicated key in an identity-plane scope is a merge collision:
    # YAML keeps the last one, so the file looks healthy either way.
    for key, extra_lines in sorted(dupes.items()):
        if key in REQUIRED:
            failures.append(
                f"{scope}: `{key}` declared MORE THAN ONCE (also at line(s) "
                f"{', '.join(str(n) for n in extra_lines)}) — YAML silently keeps "
                "the last, so two sources of truth parse clean. Merge them."
            )
    for key, value_check in REQUIRED.items():
        if key not in kv:
            why = WHY.get(key)
            msg = f"{scope}: MISSING `{key}`"
            if why:
                msg += f"\n      {why}"
            failures.append(msg)
            continue
        ok, detail = value_check(kv[key])
        if not ok:
            failures.append(f"{scope}: `{key}` {detail}")

if not checked:
    print(
        "check-identity-plane-completeness: no full-profile scopes found — "
        f"is {ansible_dir}/group_vars present?",
        file=sys.stderr,
    )
    sys.exit(1)

print(f"checked {len(checked)} full-profile scope(s): {', '.join(checked)}")

if failures:
    print("\nIdentity-plane group_vars are incomplete:\n", file=sys.stderr)
    for f in failures:
        print(f"  - {f}", file=sys.stderr)
    print(
        "\nEvery `mgmt_stack_profile: full` scope runs Kratos + Hydra +\n"
        "selfservice-ui and must declare each key EXPLICITLY. Declaring\n"
        "`ory_recovery_enabled: false` is a valid answer for a goatnet that\n"
        "mints no admin recovery links — silence is not, because silence is\n"
        "how zagros-prod shipped with every password link dead.\n",
        file=sys.stderr,
    )
    sys.exit(1)

print("identity-plane group_vars complete for every full-profile scope.")
PY
