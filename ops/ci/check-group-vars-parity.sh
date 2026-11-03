#!/usr/bin/env bash
# ops/ci/check-group-vars-parity.sh — the "must equal" group_vars fence.
#
# Several config values are duplicated across two roles because the two halves
# of one mechanism are deployed by two different roles. Today the duplication is
# enforced only by a comment, and every one of these pairs fails SILENTLY when
# it drifts (identity fragility audit §2, "split-SoT ledger"):
#
#   slot-pool salt   — anchor = sha256("<salt>:<k>")[:32] on both sides
#                      (roles/goat-zenoh/templates/config.json5.j2:329,
#                      portal internal/... via PORTAL_SLOT_POOL_SALT). A
#                      differing salt mints certs that match no router rule, so
#                      the tenant writes NOTHING and is told nothing.
#   slot-pool size   — the portal hands out slot k; the router baked slots
#                      0..size-1. portal size > router size = a cert for a slot
#                      the router never baked.
#   budget self-tier — fabric_crawler_self_tier labels a tenant's usage against
#                      the allowance zenoh_topic_budget_self_block actually
#                      grants; a skew mislabels "self" as "request" silently.
#   identity model   — roles/goat-zenoh-portal/defaults/main.yml:"MUST agree
#                      with the router role's zenoh_identity_model".
#
# Existing asserts (roles/goat-zenoh-portal/tasks/main.yml:35,
# roles/goat-zenoh/tasks/main.yml:59) only check the salt is NON-EMPTY —
# they cannot see the other role's value. This check can: it reads both halves
# out of the same group_vars scope. Shape follows check-group-permissions-parity.sh.
#
# Fail-closed ratchet: SELF_AUDIT_KEYS below is the set of key patterns that
# must be covered by some pair. A new half added to group_vars without a pair
# here fails this script, so coverage may only grow.
set -euo pipefail

ANSIBLE_DIR="${1:-ops/ansible}"

python3 - "$ANSIBLE_DIR" <<'PY'
import glob, os, re, sys

try:
    import yaml
except ImportError:
    yaml = None

ansible_dir = sys.argv[1]


def portal_absent_scopes():
    """group_vars scopes that run NO portal — the `portal` component is declared
    `absent` for them in the pins SoT (ops/pins/fleet-pins.yaml).

    A scope may legitimately run the Zenoh ROUTER without the portal (topology #2
    dormant standby: zagros-b runs the router isolated, the portal stays
    single-homed on zagros-a). For such a scope the portal-side half of every
    router-vs-portal pair below CORRECTLY rides its off-default — that is not the
    drift `require_both` guards against, so those pairs are skipped. The gate is
    the pins SoT: re-add a portal to the scope (drop its `absent`) and the pairs
    re-engage automatically. Fail-open (empty set) if the table is unreadable —
    the pairs then FAIL loudly rather than silently skip."""
    table = os.path.join(ansible_dir, "..", "pins", "fleet-pins.yaml")
    if yaml is None or not os.path.exists(table):
        return set()
    try:
        with open(table, encoding="utf-8") as fh:
            data = yaml.safe_load(fh) or {}
    except Exception:
        return set()
    out = set()
    for spec in (data.get("components") or {}).values():
        if not isinstance(spec, dict) or spec.get("var") != "portal_image":
            continue
        for scope, dev in (spec.get("deviations") or {}).items():
            if isinstance(dev, dict) and dev.get("absent") is True:
                out.add(scope)
    return out

# ---------------------------------------------------------------------------
# The pairs. `a`/`b` are group_vars key names; `a_default`/`b_default` name the
# role defaults file the key falls back to when a scope leaves it unset (None =
# no role default exists, so unset means unset).
#
# require_both:  True  — the scope must set BOTH halves. Used where the role
#                        defaults are the OFF posture (portal size 0 / salt ""
#                        vs router size 256), so inheriting one half is itself
#                        the drift.
#                False — one half legitimately rides its role default; compare
#                        the EFFECTIVE values (group_vars override else default).
# severity:      "fail" or "warn". A warn pair is one the tree is known to be
#                skewed on today; the script tells you to promote it once clean.
# ---------------------------------------------------------------------------
PAIRS = [
    {
        "name": "slot-pool salt (ADR 1025)",
        "a": "zenoh_slot_pool_salt",
        "a_default": "roles/goat-zenoh/defaults/main.yml",
        "b": "portal_slot_pool_salt",
        "b_default": "roles/goat-zenoh-portal/defaults/main.yml",
        "require_both": True,
        "severity": "fail",
        "why": "the router bakes anchors from one salt and the portal mints CNs from the other; a skew writes nothing, silently",
    },
    {
        "name": "slot-pool size (ADR 1025)",
        "a": "zenoh_slot_pool_size",
        "a_default": "roles/goat-zenoh/defaults/main.yml",
        "b": "portal_slot_pool_size",
        "b_default": "roles/goat-zenoh-portal/defaults/main.yml",
        "require_both": True,
        "severity": "fail",
        "why": "the portal would hand out a slot index the router never baked",
    },
    {
        "name": "identity model (ADR 1025)",
        "a": "zenoh_identity_model",
        "a_default": "roles/goat-zenoh/defaults/main.yml",
        "b": "portal_identity_model",
        "b_default": "roles/goat-zenoh-portal/defaults/main.yml",
        "require_both": True,
        "severity": "fail",
        "why": "per-vendor onboarding edits the ACL and SIGHUPs; per-uuid-slot-pool does neither — the two halves must run the same onboarding contract",
    },
    {
        "name": "topic-budget self tier (ADR 1075)",
        # The grant record is the goatnet's; the crawler's tier is a LABEL that
        # must track it. The crawler half legitimately rides its role default,
        # so this pair compares effective values.
        "a": "zenoh_topic_budget_self_block",
        "a_default": None,
        "b": "fabric_crawler_self_tier",
        "b_default": "roles/fabric-crawler/defaults/main.yml",
        "require_both": False,
        "anchor": "a",  # only scopes that set `a` are in scope
        "severity": "fail",
        "why": "the crawler would label a within-allowance tenant as over-block (or the reverse) with no error anywhere",
    },
    {
        "name": "lazy slot storage (ADR 1101)",
        # A goatnet with the router half true and the portal half unset runs the
        # portal's role default false — the router stops pre-cutting stores and
        # the portal does not live-add one at mint (handlers.go gates on it).
        "a": "zenoh_lazy_slot_storage",
        "a_default": "roles/goat-zenoh/defaults/main.yml",
        "b": "portal_lazy_slot_storage",
        "b_default": "roles/goat-zenoh-portal/defaults/main.yml",
        "require_both": False,
        "anchor": "a",
        # Fail, not warn: assigned slots end up with no store, and every query
        # against them answers EMPTY with no error.
        "severity": "fail",
        "why": "router-true + portal-false means a freshly minted slot gets NO store — data silently unpersisted",
    },
]

# Any group_vars key matching one of these must be covered by a pair above.
SELF_AUDIT_KEYS = [
    r"^(zenoh|portal)_slot_pool_[a-z0-9_]+$",
    r"^(zenoh|portal)_lazy_slot_storage$",
    r"^fabric_crawler_self_tier$",
    r"^zenoh_topic_budget_self_block$",
]

# ---------------------------------------------------------------------------
# Scanning. Regex, not yaml.safe_load: these files carry Jinja values that must
# be compared as WRITTEN ("{{ lookup('env','SITE_SLOT_POOL_SALT') }}" is only
# byte-identical to its twin as a literal — CI cannot resolve the env).
# ---------------------------------------------------------------------------
ASSIGN = re.compile(r"^([a-z][a-z0-9_]*)\s*:\s*(.*?)\s*$")


def strip_value(raw):
    """Drop a trailing comment and one layer of matching quotes."""
    v = raw
    if v.startswith(("'", '"')):
        q = v[0]
        end = v.find(q, 1)
        if end != -1:
            return v[1:end]
    v = re.split(r"\s+#", v, maxsplit=1)[0].strip()
    return v


def scan(path):
    """Top-level scalar assignments in one group_vars file: key -> (line, value)."""
    out = {}
    with open(path, encoding="utf-8") as fh:
        for n, line in enumerate(fh, 1):
            if line.startswith((" ", "\t", "#", "-")) or not line.strip():
                continue
            m = ASSIGN.match(line.rstrip("\n"))
            if not m:
                continue
            key, raw = m.group(1), m.group(2)
            if raw == "" or raw.startswith("#"):
                continue  # block/mapping opener or a bare comment line
            out[key] = (n, strip_value(raw))
    return out


def scopes():
    """Deployment scope -> {key: (path, line, value)}.

    A scope is one group_vars directory (ops/ansible/group_vars/<group>/) — a
    key may live in any file inside it — or one flat group_vars file.
    """
    found = {}
    for d in sorted(glob.glob(f"{ansible_dir}/group_vars/*/")):
        name = os.path.basename(d.rstrip("/"))
        if name == "_examples":
            continue
        for p in sorted(glob.glob(f"{d}*.yml")):
            for k, (line, val) in scan(p).items():
                found.setdefault(name, {})[k] = (p, line, val)
    for p in sorted(glob.glob(f"{ansible_dir}/inventory/group_vars/*.yml")):
        name = f"inventory:{os.path.basename(p)[:-4]}"
        for k, (line, val) in scan(p).items():
            found.setdefault(name, {})[k] = (p, line, val)
    return found


def role_default(rel_path, key):
    """(path, line, value) for a key in a role defaults file, or None."""
    if not rel_path:
        return None
    p = os.path.join(ansible_dir, rel_path)
    hit = scan(p).get(key)
    return None if hit is None else (p, hit[0], hit[1])


def show(where):
    if where is None:
        return "<unset>"
    path, line, val = where
    return f"{path}:{line} = {val!r}"


def env_var(val):
    m = re.search(r"lookup\(\s*'env'\s*,\s*'([A-Z0-9_]+)'", val or "")
    return m.group(1) if m else None


GV = scopes()
PORTAL_ABSENT = portal_absent_scopes()
skipped_portal_absent = set()
ok = True
warned = []
checked = 0

for pair in PAIRS:
    a_key, b_key = pair["a"], pair["b"]
    a_def = role_default(pair["a_default"], a_key)
    b_def = role_default(pair["b_default"], b_key)
    fail = pair["severity"] == "fail"
    portal_paired = b_key.startswith("portal_")
    clean = True

    for scope in sorted(GV):
        # A scope that runs the router but no portal (portal `absent` in the pins
        # SoT) legitimately leaves every portal_* half on its off-default; the
        # router-vs-portal pairs do not apply. Router-vs-router parity across the
        # two nodes rides the shared env-var lookup + the node-parity rule, not
        # this check.
        if portal_paired and scope in PORTAL_ABSENT:
            skipped_portal_absent.add(scope)
            continue
        a_set = GV[scope].get(a_key)
        b_set = GV[scope].get(b_key)
        a_set = None if a_set is None else (a_set[0], a_set[1], a_set[2])
        b_set = None if b_set is None else (b_set[0], b_set[1], b_set[2])

        if pair["require_both"]:
            if a_set is None and b_set is None:
                continue
            if a_set is None or b_set is None:
                missing, present = (a_key, b_key) if a_set is None else (b_key, a_key)
                where = show(b_set if a_set is None else a_set)
                dflt = show(a_def if a_set is None else b_def)
                msg = (
                    f"{'FAIL' if fail else 'warn'}: {pair['name']}: group_vars/{scope} sets "
                    f"{present} ({where}) but never sets {missing} — it would silently inherit "
                    f"the role default {dflt}. {pair['why']}"
                )
                print(msg)
                clean = False
                if fail:
                    ok = False
                continue
            a_eff, b_eff = a_set, b_set
        else:
            anchor = pair.get("anchor", "a")
            anchor_set = a_set if anchor == "a" else b_set
            if anchor_set is None:
                continue  # goatnet does not run this mechanism
            a_eff = a_set or a_def
            b_eff = b_set or b_def
            if a_eff is None or b_eff is None:
                print(
                    f"{'FAIL' if fail else 'warn'}: {pair['name']}: group_vars/{scope} sets "
                    f"{a_key if a_set else b_key} but the other half "
                    f"({b_key if a_set else a_key}) has neither an override nor a role default"
                )
                clean = False
                if fail:
                    ok = False
                continue

        checked += 1
        if a_eff[2] != b_eff[2]:
            print(
                f"{'FAIL' if fail else 'warn'}: {pair['name']}: group_vars/{scope} — "
                f"{a_key} {show(a_eff)} != {b_key} {show(b_eff)}. {pair['why']}"
            )
            ea, eb = env_var(a_eff[2]), env_var(b_eff[2])
            if ea or eb:
                print(f"      (env vars read: {a_key} -> {ea or 'literal'}, {b_key} -> {eb or 'literal'})")
            clean = False
            if fail:
                ok = False

    if not fail:
        if clean:
            print(
                f"FAIL: {pair['name']} is declared severity=warn but no scope is skewed — "
                "promote it to severity=\"fail\" (the ratchet only tightens)"
            )
            ok = False
        else:
            warned.append(pair["name"])

# Self-audit: a key that looks like half of one of these mechanisms but is
# covered by no pair is an unfenced half.
covered = {p["a"] for p in PAIRS} | {p["b"] for p in PAIRS}
uncovered = set()
for scope, keys in GV.items():
    for k in keys:
        if k in covered:
            continue
        if any(re.match(pat, k) for pat in SELF_AUDIT_KEYS):
            uncovered.add(f"{k} (group_vars/{scope})")
if uncovered:
    print("FAIL: group_vars keys that look like half of a fenced pair but are covered by none:")
    for u in sorted(uncovered):
        print(f"  {u}")
    ok = False

if not ok:
    sys.exit(1)
tail = f"; {len(warned)} known-skew pair(s) reported as warn: {', '.join(warned)}" if warned else ""
if skipped_portal_absent:
    tail += (f"; portal-paired pairs skipped for portal-absent scope(s): "
             f"{', '.join(sorted(skipped_portal_absent))} (router runs, no portal)")
print(f"ok: {checked} group_vars pair comparison(s) across {len(GV)} scopes agree{tail}")
PY
