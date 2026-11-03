#!/usr/bin/env python3
"""Unit test for the identity-grammar anti-regression gate in validate.py
(ADR 1043 Phase C). Standalone (no pytest dependency — the repo's main CI runs
bats, not pytest); exits non-zero on any failed assertion so a CI step can gate
on it. Run: python3 ops/operators/test_validate_grammar.py

What it locks:
  - manifests added BEFORE the cutover are grandfathered (no grammar errors),
  - manifests added ON/AFTER the cutover must declare an `affiliation` block,
  - the deprecated VENDOR / OFFSITE groups fail on new manifests,
  - OPERATOR is NOT flagged (the grammar redefined it as the valid tactical role),
  - the end-to-end validate_manifest() surfaces the gate on a real temp file.
"""
from __future__ import annotations

import os
import sys
import tempfile
from pathlib import Path

# Pin the cutover so the test is independent of the wall clock / the default.
os.environ["GOAT_GRAMMAR_CUTOVER"] = "2026-06-16"

sys.path.insert(0, str(Path(__file__).parent))
import validate  # noqa: E402  (after the env pin + path insert)

FAILS: list[str] = []


def check(name: str, cond: bool, detail: str = "") -> None:
    if cond:
        print(f"ok   - {name}")
    else:
        print(f"FAIL - {name}  {detail}")
        FAILS.append(name)


def errs(data: dict) -> list[str]:
    return validate._grammar_regression_errors(data)


# --- _grammar_regression_errors unit cases ---------------------------------

check(
    "legacy manifest grandfathered (added < cutover, no affiliation, VENDOR)",
    errs({"identity": {"added": "2026-05-20"}, "netbird": {"auto_groups": ["VENDOR"]}}) == [],
)
check(
    "new compliant manifest passes",
    errs({
        "identity": {"added": "2026-06-16"},
        "affiliation": {"id": "acme", "kind": "vendor"},
        "netbird": {"auto_groups": ["DEVELOPER"]},
    }) == [],
)
check(
    "new manifest missing affiliation -> 1 error",
    len(errs({"identity": {"added": "2026-06-20"}, "netbird": {"auto_groups": ["DEVELOPER"]}})) == 1,
)
dep = errs({
    "identity": {"added": "2026-06-20"},
    "affiliation": {"id": "acme", "kind": "vendor"},
    "netbird": {"auto_groups": ["VENDOR", "OFFSITE", "OBSERVER"]},
})
check("new manifest flags both VENDOR and OFFSITE (not OBSERVER)", len(dep) == 2,
      detail=f"got {len(dep)}: {dep}")
check(
    "OPERATOR is NOT flagged (valid tactical role under the grammar)",
    errs({
        "identity": {"added": "2026-06-20"},
        "affiliation": {"id": "unit", "kind": "organic"},
        "netbird": {"auto_groups": ["OPERATOR"]},
    }) == [],
)
check(
    "boundary: added == cutover is gated (>= cutover)",
    len(errs({"identity": {"added": "2026-06-16"}, "netbird": {"auto_groups": []}})) == 1,
)
check(
    "blank/missing added treated as legacy (schema catches the real gap)",
    errs({"identity": {}, "netbird": {"auto_groups": ["VENDOR"]}}) == [],
)

# --- end-to-end through validate_manifest() on a real temp file ------------

SCHEMA = validate.load_schema()
from jsonschema import Draft7Validator  # noqa: E402
VALIDATOR = Draft7Validator(SCHEMA)

# Fixture 1: schema-VALID (OBSERVER, no vendor block needed) but new + no
# affiliation -> the gate must fire on the missing affiliation block.
NEW_NO_AFFILIATION = """\
identity:
  handle: zz-grammar-probe-noaff
  email: probe@notional.example
  name: Grammar Probe NoAff
  type: system
  status: active
  added: 2026-06-20
  added_by: ci
netbird:
  enabled: true
  sites: [zagros]
  role: peer
  auto_groups: [OBSERVER]
ory_site: {enabled: false, sites: [], groups: []}
ory_idp: {enabled: false, sites: [], groups: []}
zenoh:
  enabled: true
  trial:
    vendor: zz-grammar-probe-noaff
"""

# Fixture 2: schema-VALID vendor-primary WITH affiliation, but uses the
# deprecated VENDOR group -> the gate must fire on VENDOR (and ONLY that).
NEW_DEPRECATED_VENDOR = """\
identity:
  handle: zz-grammar-probe-vendor
  email: probe2@notional.example
  name: Grammar Probe Vendor
  type: person
  status: active
  added: 2026-06-20
  added_by: ci
vendor:
  display_name: Notional Probe Co
  short_tag: zz-grammar-probe-vendor
affiliation:
  id: zz-probe-org
  kind: vendor
netbird:
  enabled: true
  sites: [zagros]
  role: peer
  auto_groups: [VENDOR]
ory_site: {enabled: false, sites: [], groups: []}
ory_idp: {enabled: false, sites: [], groups: []}
zenoh:
  enabled: true
  trial:
    vendor: zz-grammar-probe-vendor
"""

with tempfile.TemporaryDirectory() as d:
    p1 = Path(d) / "zz-grammar-probe-noaff.yml"
    p1.write_text(NEW_NO_AFFILIATION)
    e1 = validate.validate_manifest(p1, VALIDATOR)
    check("e2e: new no-affiliation manifest is schema-valid but grammar-gated",
          len(e1) == 1 and any("affiliation" in x for x in e1), detail=f"errors: {e1}")

    p2 = Path(d) / "zz-grammar-probe-vendor.yml"
    p2.write_text(NEW_DEPRECATED_VENDOR)
    e2 = validate.validate_manifest(p2, VALIDATOR)
    check("e2e: new manifest with deprecated VENDOR group is grammar-gated",
          len(e2) == 1 and any("VENDOR" in x for x in e2), detail=f"errors: {e2}")

# Missing dependencies retain CLI diagnostics/exit 2, but importing this library
# raises ImportError without printing or terminating the importing process.
import builtins
import contextlib
import io
from unittest.mock import patch

source = (Path(__file__).parent / "validate.py").read_text()
for dependency in ("yaml", "jsonschema"):
    original_import = builtins.__import__
    def import_without(name, *args, **kwargs):
        if name == dependency:
            raise ImportError("injected missing " + name)
        return original_import(name, *args, **kwargs)
    for module_name in ("__main__", "imported_validator"):
        stderr = io.StringIO()
        with patch("builtins.__import__", side_effect=import_without), contextlib.redirect_stderr(stderr):
            try:
                exec(compile(source, "validate.py", "exec"),
                     {"__name__": module_name, "__file__": str(Path(__file__).parent / "validate.py")})
            except (SystemExit, ImportError) as error:
                if module_name == "__main__":
                    check(dependency + " CLI dependency error", isinstance(error, SystemExit)
                          and error.code == 2 and "ERROR:" in stderr.getvalue())
                else:
                    check(dependency + " library dependency error", isinstance(error, ImportError)
                          and not stderr.getvalue())
            else:
                check(dependency + " missing dependency must fail", False)

if FAILS:
    print(f"\n{len(FAILS)} assertion(s) failed.")
    sys.exit(1)
print("\nall grammar-gate assertions passed.")
