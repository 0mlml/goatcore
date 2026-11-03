#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Validate a hold-record file (JSON Lines) against schema/hold-record.v1.schema.json.

    validate-hold-records.py <file.jsonl>      exit 0 = every line valid
    pytest ops/lifecycle/validate-hold-records.py  runs the same checks on the fixture

Checks beyond the schema, because a schema cannot see across lines:
  * one record per peer_id (a second hold for the same peer is a re-entry the
    writer must express by removing the first line, never by appending);
  * the site prefix in hold_group matches `site` upper-cased and is followed
    directly by the lane group (ZAGROS- must not admit ZAGROS-PROD-* groups).

Uses `jsonschema` when installed. Without it, a built-in checker walks the
same schema file (the draft-7 subset this schema uses: type, const, enum,
pattern, min/maxLength, required, additionalProperties, items, uniqueItems,
allOf/if/then on a const). Same file, same verdicts — the CI runner has no
jsonschema, and a check that skips is not a check.
"""
import json
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
SCHEMA_PATH = os.path.join(HERE, "schema", "hold-record.v1.schema.json")
FIXTURE_PATH = os.path.join(HERE, "fixtures", "hold-records.jsonl")


def load_schema():
    with open(SCHEMA_PATH) as fh:
        return json.load(fh)


_TYPES = {"object": dict, "array": list, "string": str, "integer": int,
          "number": (int, float), "boolean": bool, "null": type(None)}


def _json_eq(a, b):
    """JSON equality: `true` is not `1` (Python says True == 1; draft-7 does not)."""
    if isinstance(a, bool) != isinstance(b, bool):
        return False
    return a == b


def _check(node, value, path, out):
    """Minimal draft-7 walker for the subset hold-record.v1 uses."""
    def bad(msg):
        out.append(("/".join(path) or "<record>", msg))
    t = node.get("type")
    if t is not None:
        types = t if isinstance(t, list) else [t]
        ok = any(isinstance(value, _TYPES[x]) and not
                 (x in ("integer", "number") and isinstance(value, bool))
                 for x in types)
        if not ok:
            bad("%r is not of type %s" % (value, ", ".join("'%s'" % x for x in types)))
            return
    if "const" in node and not _json_eq(value, node["const"]):
        bad("%r was expected" % node["const"])
    if "enum" in node and not any(_json_eq(value, e) for e in node["enum"]):
        bad("%r is not one of %r" % (value, node["enum"]))
    if isinstance(value, str):
        if "pattern" in node and not re.search(node["pattern"], value):
            bad("%r does not match %r" % (value, node["pattern"]))
        if len(value) < node.get("minLength", 0):
            bad("%r is too short" % value)
        if len(value) > node.get("maxLength", 10 ** 9):
            bad("%r is too long" % value)
    if isinstance(value, dict):
        for k in node.get("required", []):
            if k not in value:
                bad("%r is a required property" % k)
        props = node.get("properties", {})
        if node.get("additionalProperties") is False:
            for k in value:
                if k not in props:
                    bad("Additional properties are not allowed (%r was unexpected)" % k)
        for k, sub in props.items():
            if k in value:
                _check(sub, value[k], path + [k], out)
    if isinstance(value, list):
        if "items" in node:
            for i, item in enumerate(value):
                _check(node["items"], item, path + [str(i)], out)
        if node.get("uniqueItems") and len(set(map(json.dumps, value))) != len(value):
            bad("%r has non-unique elements" % (value,))
    for branch in node.get("allOf", []):
        cond = branch.get("if")
        if cond is None:
            _check(branch, value, path, out)
            continue
        probe = []
        _check(cond, value, path, probe)
        if not probe and "then" in branch:
            _check(branch["then"], value, path, out)


def _builtin_errors(schema, rec):
    out = []
    _check(schema, rec, [], out)
    return out


def _errors_for(schema, rec, force_builtin=False):
    if not force_builtin:
        try:
            import jsonschema
            v = jsonschema.Draft7Validator(schema)
            return [("/".join(str(p) for p in e.path) or "<record>", e.message)
                    for e in sorted(v.iter_errors(rec), key=lambda e: list(e.path))]
        except ImportError:
            pass  # jsonschema unavailable: use the built-in validator below
    return _builtin_errors(schema, rec)


def validate_lines(lines, schema, force_builtin=False):
    """Return a list of (line_no, message). Empty list = valid."""
    errors = []
    seen = {}
    for n, raw in enumerate(lines, 1):
        raw = raw.strip()
        if not raw:
            continue
        try:
            rec = json.loads(raw)
        except ValueError as exc:
            errors.append((n, "not JSON: %s" % exc))
            continue
        for where, msg in _errors_for(schema, rec, force_builtin):
            errors.append((n, "%s: %s" % (where, msg)))
        if not isinstance(rec, dict):
            continue
        pid = rec.get("peer_id")
        if pid in seen:
            errors.append((n, "peer_id %r already recorded on line %d" % (pid, seen[pid])))
        elif pid is not None:
            seen[pid] = n
        # The remainder after the prefix must BE the lane group, so a longer
        # sibling prefix (SITE- vs SITE-PROD-) cannot slip through.
        site, group = rec.get("site"), rec.get("hold_group")
        if isinstance(site, str) and isinstance(group, str):
            prefix = site.upper() + "-"
            rest = group[len(prefix):] if group.startswith(prefix) else None
            if rest is None or not re.match(r"(PROBATION-|QUARANTINED$)", rest):
                errors.append((n, "hold_group %r does not carry site prefix %r "
                                  "directly followed by PROBATION-<role> or QUARANTINED"
                               % (group, prefix)))
    return errors


def validate_file(path):
    with open(path) as fh:
        return validate_lines(fh.readlines(), load_schema())


# ---- pytest surface --------------------------------------------------------

def test_fixture_is_valid():
    assert validate_file(FIXTURE_PATH) == []


def test_fixture_covers_both_lanes():
    with open(FIXTURE_PATH) as source_file:
        lanes = {json.loads(l)["lane"] for l in source_file if l.strip()}
    assert lanes == {"dormancy", "security"}


def _one(**over):
    with open(FIXTURE_PATH) as source_file:
        rec = json.loads(source_file.readline())
    rec.update(over)
    return [json.dumps(rec)]


def _both(lines):
    """Run the library validator (if present) and the built-in one; both must agree."""
    a = validate_lines(lines, load_schema())
    b = validate_lines(lines, load_schema(), force_builtin=True)
    assert bool(a) == bool(b), (a, b)
    return b


def test_builtin_accepts_fixture():
    assert validate_file(FIXTURE_PATH) == []
    with open(FIXTURE_PATH) as source_file:
        lines = source_file.readlines()
    errors = validate_lines(lines, load_schema(), force_builtin=True)
    assert errors == []


def test_dormancy_must_park_in_probation():
    errs = _both(_one(hold_group="ZAGROS-QUARANTINED"))
    assert any("hold_group" in m for _, m in errs), errs


def test_security_must_park_in_quarantined():
    errs = _both(_one(lane="security", hold_group="ZAGROS-PROBATION-DEVELOPER"))
    assert any("hold_group" in m for _, m in errs), errs


def test_unknown_field_and_version_refused():
    assert _both(_one(email="x@example.org"))
    assert _both(_one(v=2))


def test_hyphenated_goatnet_prefix_accepted():
    # A hyphenated goatnet's taxonomy site_prefix is its full name; the sweep runs there too.
    assert _both(_one(site="zagros-prod", hold_group="ZAGROS-PROD-PROBATION-DEVELOPER")) == []
    assert _both(_one(site="zagros-prod", lane="security", actor="ops",
                      hold_group="ZAGROS-PROD-QUARANTINED")) == []
    assert _both(_one(site="zagros-prod", hold_group="ZAGROS-PROD-QUARANTINED"))
    assert _both(_one(site="zagros-prod", hold_group="ZAGROS-PROBATION-DEVELOPER"))


def test_bool_is_not_the_version_number():
    assert _both(_one(v=True))


def test_missing_required_and_bad_types_refused():
    with open(FIXTURE_PATH) as source_file:
        rec = json.loads(source_file.readline())
    del rec["prior_groups"]
    assert _both([json.dumps(rec)])
    assert _both(_one(prior_groups="ZAGROS-DEVELOPER"))
    assert _both(_one(prior_groups=["ZAGROS-DEVELOPER", "ZAGROS-DEVELOPER"]))
    assert _both(_one(lane="dormant"))
    assert _both(_one(entered_at="2026-09-01 10:23"))
    assert _both(_one(handle=42))


def test_duplicate_peer_refused():
    with open(FIXTURE_PATH) as source_file:
        line = source_file.readline()
    errs = validate_lines([line, line], load_schema())
    assert any("already recorded" in m for _, m in errs), errs


def test_site_prefix_must_match():
    errs = validate_lines(_one(site="site-one"), load_schema())
    assert any("site prefix" in m for _, m in errs), errs


def test_longer_sibling_site_prefix_refused():
    # SITE- is a string prefix of SITE-PROD-: a site must not accept its
    # sibling's groups in either lane.
    errs = _both(_one(site="zagros", hold_group="ZAGROS-PROD-PROBATION-DEVELOPER"))
    assert any("site prefix" in m for _, m in errs), errs
    errs = _both(_one(site="zagros", lane="security",
                      hold_group="ZAGROS-PROD-QUARANTINED"))
    assert any("site prefix" in m for _, m in errs), errs


# ---- CLI -------------------------------------------------------------------

def main(argv):
    if len(argv) != 2:
        print(__doc__.strip().splitlines()[0])
        print("usage: validate-hold-records.py <file.jsonl>")
        return 2
    errs = validate_file(argv[1])
    for n, msg in errs:
        print("%s:%d: %s" % (argv[1], n, msg))
    if errs:
        print("%d problem(s)" % len(errs))
        return 1
    print("%s: ok" % argv[1])
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
