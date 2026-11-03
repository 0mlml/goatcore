#!/usr/bin/env python3
"""Validate an operator manifest against the Block 83 jsonschema.

Usage:
    ./validate.py <manifest.yml> [<manifest.yml> ...]
    ./validate.py --json <manifest.yml> [...]      # machine-readable JSON array
                                                   # ({file, ok, errors:[{loc,message}],
                                                   # shape, detail}); the linter-as-a-
                                                   # service (portal /api/onboard/lint) +
                                                   # batch upload consume this, so the
                                                   # webform runs the SAME validation as
                                                   # the onboard Phase-0 gate.
    ./validate.py --report [<directory>]           # walk every manifest in this directory,
                                                   # validate, classify each by canonical
                                                   # shape, and print an aggregate table.
                                                   # Default directory: dirname of this
                                                   # script. Exits non-zero if any manifest
                                                   # fails validation or is unclassifiable.

Exit codes:
    0 — all manifests validate cleanly (and classify in --report mode)
    1 — at least one manifest failed validation or was unclassifiable
    2 — usage / file-not-found / jsonschema-not-installed error

The orchestrator at ops/ansible/operator-onboard.yml calls this in Phase 0
pre-flight; failures halt before any mint hits a backend.

Three canonical shapes (see ops/operators/README.md §"Canonical shapes"):
    - vendor-primary  — VENDOR group + vendor.display_name + zenoh.enabled
    - vendor-sibling  — VENDOR group + vendor.display_name + zenoh disabled
                        (cert minted via the vendor's primary manifest)
    - operator        — ADMIN/OPERATOR group, no vendor block

Any manifest whose shape doesn't match one of the three is flagged as
'unclassifiable' in --report mode. Schema-level violations (missing
required fields, wrong types, disabled-block stub content) are caught
at validate-time and never reach the classifier.
"""
from __future__ import annotations

import json
import os
import sys
from pathlib import Path

try:
    import yaml
except ImportError:
    if __name__ == "__main__":
        print("ERROR: PyYAML is required. Install via: pip install pyyaml", file=sys.stderr)
        sys.exit(2)
    raise

try:
    from jsonschema import Draft7Validator
except ImportError:
    if __name__ == "__main__":
        print("ERROR: jsonschema is required. Install via: pip install jsonschema", file=sys.stderr)
        sys.exit(2)
    raise


SCHEMA_PATH = Path(__file__).parent / "schema" / "operator-manifest.schema.json"


def load_schema() -> dict:
    if not SCHEMA_PATH.exists():
        print(f"ERROR: schema not found at {SCHEMA_PATH}", file=sys.stderr)
        sys.exit(2)
    with SCHEMA_PATH.open() as f:
        return json.load(f)


# Identity-grammar anti-regression cutover (Phase C, ADR 1043). Manifests whose
# `identity.added` is ON OR AFTER this date must comply with the grammar:
# declare an `affiliation` block (the schema requires `kind` inside it) and not
# use the deprecated role/echelon groups. Manifests added BEFORE it are
# grandfathered (the pre-grammar fleet; backfill is a separate sweep). Override
# for tests via GOAT_GRAMMAR_CUTOVER. Date-based grandfather, not an allowlist,
# so it is self-maintaining; the escape hatch (back-dating `added` on a new
# manifest) is a deliberate evasion, out of scope.
GRAMMAR_CUTOVER = os.environ.get("GOAT_GRAMMAR_CUTOVER", "2026-06-16")

# auto_groups tokens retired by the grammar (ADR 1043). VENDOR -> DEVELOPER
# (the role-sense of vendor); OFFSITE -> a real command echelon. NOTE: OPERATOR
# is deliberately NOT here — the grammar REDEFINED it as the tactical-actor role
# (kept valid); its old observability sense moved to MONITOR, but the manifest
# carries only the group token, so tactical-OPERATOR and legacy-OPERATOR are
# indistinguishable here and we cannot fail-closed on it. Operators choosing
# MONITOR for observability is guidance, not a gate.
_DEPRECATED_GROUPS = {
    "VENDOR": "DEVELOPER (the role-sense of vendor; affiliation 'vendor' rides the affiliation block, not a group)",
    "OFFSITE": "a real command echelon (COMPONENT/COMMAND/…); OFFSITE is retired",
    "OPSYSTEM": "retired from the ACL taxonomy (ADR 1116); do not onboard into OPSYSTEM",
}


def _grammar_regression_errors(data: dict) -> list[str]:
    """Anti-regression gate (ADR 1043 Phase C): for manifests added on/after
    GRAMMAR_CUTOVER, require an affiliation block and forbid deprecated groups.
    Returns error strings (empty = OK or grandfathered). Schema validation must
    have passed before this is called (it reads identity.added / netbird)."""
    added = str(data.get("identity", {}).get("added", ""))
    # Grandfather anything added before the cutover (lexical compare is correct
    # for zero-padded ISO 8601 dates). Missing/blank added -> treat as legacy
    # (schema already requires `added`, so a blank one fails earlier).
    if not added or added < GRAMMAR_CUTOVER:
        return []

    errors: list[str] = []
    if "affiliation" not in data:
        errors.append(
            f"identity-grammar: manifest added {added} (>= cutover {GRAMMAR_CUTOVER}) must "
            f"declare an 'affiliation' block (id + kind) per ADR 1043 — capability is never "
            f"named after affiliation. Legacy manifests are grandfathered."
        )
    auto_groups = set(data.get("netbird", {}).get("auto_groups", []))
    for grp in sorted(auto_groups & set(_DEPRECATED_GROUPS)):
        errors.append(
            f"identity-grammar: deprecated netbird.auto_groups entry '{grp}' on a manifest "
            f"added {added} — use {_DEPRECATED_GROUPS[grp]} (ADR 1043)."
        )
    return errors


def validate_manifest(path: Path, validator: Draft7Validator) -> list[str]:
    """Return list of validation error messages; empty list = OK."""
    if not path.exists():
        return [f"file not found: {path}"]

    try:
        with path.open() as f:
            data = yaml.safe_load(f)
    except yaml.YAMLError as e:
        return [f"YAML parse error: {e}"]

    # PyYAML auto-parses ISO dates (e.g.) into datetime.date objects.
    # JSON Schema validates the wire-format (string with format=date), so coerce
    # back to ISO 8601 string before validation. Same for datetime.datetime.
    import datetime as _dt

    def _coerce(node):
        if isinstance(node, dict):
            return {k: _coerce(v) for k, v in node.items()}
        if isinstance(node, list):
            return [_coerce(x) for x in node]
        if isinstance(node, (_dt.date, _dt.datetime)):
            return node.isoformat()
        return node

    data = _coerce(data)

    if data is None:
        return ["empty manifest"]

    errors = []
    for err in sorted(validator.iter_errors(data), key=lambda e: list(e.absolute_path)):
        loc = ".".join(str(p) for p in err.absolute_path) or "<root>"
        errors.append(f"{loc}: {err.message}")

    # Cross-field check: filename stem must match identity.handle.
    # Skip for template files (paths ending in `.yml.example`) — those are
    # documentation, not real operator manifests; the README workflow copies
    # them to <handle>.yml at which point the check applies.
    if not errors and not str(path).endswith(".yml.example"):
        identity = data.get("identity", {})
        handle = identity.get("handle")
        stem = path.stem
        if handle and stem != handle:
            errors.append(
                f"<filename>: handle '{handle}' does not match filename stem '{stem}'. "
                f"Rename file to '{handle}.yml' or fix identity.handle."
            )

    # Identity-grammar anti-regression gate (ADR 1043 Phase C). Only meaningful
    # on a schema-valid manifest (it reads identity.added / netbird.auto_groups),
    # and templates are documentation, not onboardable manifests.
    if not errors and not str(path).endswith(".yml.example"):
        errors += _grammar_regression_errors(data)

    # (The non-ASCII-name pre-flight guard was removed once the
    # UTF-8 path was fixed end to end: operator-onboard.yml Phase 2.5 now POSTs
    # the Kratos identity body as raw JSON with ensure_ascii=False, so a name
    # with diacritics crosses as literal UTF-8 and Kratos accepts it; the
    # \u-escaped form fails in Postgres with 22P05.
    # Foreign partners keep their real spelling; no transliteration needed.)

    return errors


def classify_shape(data: dict) -> tuple[str, str]:
    """Classify a validated manifest into one of the canonical shapes.

    Returns (shape, note). shape is one of:
        - 'vendor-primary'   — VENDOR/DEVELOPER group, vendor block, zenoh enabled
        - 'vendor-sibling'   — VENDOR/DEVELOPER group, vendor block, zenoh disabled
        - 'operator'         — ADMIN/OPERATOR/MGMT-HOST group, no vendor block
        - 'browse'           — OBSERVER-only, no publish (zenoh disabled OR a
                               read-only observer cert). Web-surface browse +
                               own read-only fabric identity. The
                               browse-only tier. Optional vendor.display_name
                               records org affiliation.
        - 'unclassifiable'   — none of the above

    Schema validation must succeed before this is called. The classifier
    does NOT re-validate; it only labels.
    """
    auto_groups = set(data.get("netbird", {}).get("auto_groups", []))
    has_vendor_block = "vendor" in data
    zenoh = data.get("zenoh", {}) or {}
    zenoh_enabled = zenoh.get("enabled", False)
    zenoh_observer = bool(zenoh.get("observer", False))
    # "publishes" = a trial/prod vendor slot. A read-only observer cert does
    # NOT publish, so it stays compatible with the browse tier.
    zenoh_publishes = zenoh_enabled and not zenoh_observer

    # DEVELOPER is the post-cutover role-sense of VENDOR (ADR 1043): the same
    # full/primary tier, just the un-deprecated group token. The grammar gate
    # above FORBIDS VENDOR on new manifests and REQUIRES DEVELOPER, so the
    # classifier must treat them as one shape or every post-cutover full-tier
    # manifest is 'unclassifiable'. (The vendor->org noun rename is ADR 1052.)
    is_vendor_shape = bool(auto_groups & {"VENDOR", "DEVELOPER"})
    is_operator_shape = bool(auto_groups & {"ADMIN", "OPERATOR", "MGMT-HOST"})
    is_browse_shape = "OBSERVER" in auto_groups and not is_vendor_shape and not is_operator_shape

    if is_vendor_shape and has_vendor_block and zenoh_publishes:
        vendor_name = data["vendor"]["display_name"]
        zenoh_vendor = zenoh.get("trial", {}).get("vendor", "?")
        return "vendor-primary", f"{vendor_name} (cert={zenoh_vendor})"
    if is_vendor_shape and has_vendor_block and not zenoh_enabled:
        vendor_name = data["vendor"]["display_name"]
        return "vendor-sibling", f"{vendor_name}"
    if is_operator_shape and not has_vendor_block:
        groups = ",".join(sorted(auto_groups))
        return "operator", f"groups={groups}"
    if is_browse_shape and not zenoh_publishes:
        org = data.get("vendor", {}).get("display_name", "—")
        cert = " +observer-cert" if zenoh_observer else ""
        return "browse", f"OBSERVER-only ({org}){cert}"
    return "unclassifiable", f"auto_groups={sorted(auto_groups)} vendor_block={has_vendor_block} zenoh={zenoh_enabled}"


# Subdirectories under ops/operators/ that are NOT per-env manifest folders.
_NON_ENV_SUBDIRS = {"out", "schema", "test-revoke"}


def _gather_manifests(manifests_dir: Path) -> list[Path]:
    """Top-level <handle>.yml plus one level of per-env subfolders
    (ops/operators/<env>/<handle>.yml). Skips out/ schema/ test-revoke/ and
    *.yml.example templates. One level deep only — envs don't nest.
    """
    files = [
        p for p in manifests_dir.glob("*.yml")
        if not p.name.endswith(".yml.example")
    ]
    for sub in manifests_dir.iterdir():
        if sub.is_dir() and sub.name not in _NON_ENV_SUBDIRS:
            files += [
                p for p in sub.glob("*.yml")
                if not p.name.endswith(".yml.example")
            ]
    return sorted(files)


def run_report(manifests_dir: Path | None = None) -> int:
    """Walk every <handle>.yml in `manifests_dir` (incl. per-env subfolders);
    validate + classify + print table.

    Defaults to the directory containing this script when not provided.
    """
    schema = load_schema()
    Draft7Validator.check_schema(schema)
    validator = Draft7Validator(schema)

    if manifests_dir is None:
        manifests_dir = Path(__file__).parent
    files = _gather_manifests(manifests_dir)

    if not files:
        print("(no manifests at {})".format(manifests_dir))
        return 0

    rows: list[tuple[str, str, str, list[str]]] = []
    for path in files:
        # Display relative to the manifests dir so per-env manifests read as
        # `<env>/<handle>.yml` (not just the basename, which could
        # collide across envs).
        display = str(path.relative_to(manifests_dir))
        errors = validate_manifest(path, validator)
        if errors:
            rows.append((display, "INVALID", "; ".join(errors[:1]), errors))
            continue
        try:
            with path.open() as f:
                data = yaml.safe_load(f)
        except yaml.YAMLError as e:
            rows.append((display, "INVALID", f"YAML parse error: {e}", []))
            continue
        shape, note = classify_shape(data or {})
        rows.append((display, shape, note, []))

    name_w = max(len(r[0]) for r in rows)
    shape_w = max(len(r[1]) for r in rows)
    print(f"{'manifest'.ljust(name_w)}  {'shape'.ljust(shape_w)}  detail")
    print(f"{'-' * name_w}  {'-' * shape_w}  {'-' * 40}")
    counts: dict[str, int] = {}
    for name, shape, note, _ in rows:
        counts[shape] = counts.get(shape, 0) + 1
        print(f"{name.ljust(name_w)}  {shape.ljust(shape_w)}  {note}")

    print()
    print("summary:")
    for shape in ("vendor-primary", "vendor-sibling", "operator", "browse", "unclassifiable", "INVALID"):
        if shape in counts:
            print(f"  {shape.ljust(16)}  {counts[shape]}")

    # Cross-manifest consistency: each zenoh.trial.vendor (URI-SAN
    # authority) is the unique-on-the-wire identity and MUST appear in
    # at most one vendor-primary manifest. Multiple primaries sharing a
    # prefix means the portal returns 409 on every mint after the first
    # — drift between manifest intent and portal state.
    #
    # vendor.display_name is intentionally NOT the uniqueness key:
    # multiple people at the same vendor company each get their own
    # primary manifest with a distinct prefix (e.g. `org59x`,
    # `org61x`), keeping the self-inclusive-onboarding model
    # (no person-to-person OOB credential handoff). They share the
    # human-readable display_name; the portal + id-admin-console
    # surface vendor affiliation off of that.
    primaries_by_prefix: dict[str, list[str]] = {}
    siblings_by_display: dict[str, list[str]] = {}
    primary_display_names: set[str] = set()
    for name, shape, note, _ in rows:
        if shape == "vendor-primary":
            # note format: "<display_name> (cert=<prefix>)"
            display_name, _, rest = note.partition(" (cert=")
            prefix = rest.rstrip(")")
            primaries_by_prefix.setdefault(prefix, []).append(name)
            primary_display_names.add(display_name)
        elif shape == "vendor-sibling":
            # vendor-sibling shape is deprecated in favor of one
            # primary per person; we still classify existing siblings
            # so the drift report can flag orphans.
            siblings_by_display.setdefault(note, []).append(name)

    duplicates = {p: m for p, m in primaries_by_prefix.items() if len(m) > 1}
    orphan_siblings = {v: m for v, m in siblings_by_display.items() if v not in primary_display_names}

    if duplicates or orphan_siblings:
        print()
        print("cross-manifest drift:")
        for prefix, manifests in sorted(duplicates.items()):
            print(f"  duplicate primary prefix  {prefix}: {', '.join(manifests)}")
            print(f"    → each vendor-primary manifest needs a unique zenoh.trial.vendor (URI-SAN authority)")
        for vendor, manifests in sorted(orphan_siblings.items()):
            print(f"  orphan sibling     {vendor}: {', '.join(manifests)}")
            print(f"    → no vendor-primary with display_name='{vendor}' exists; convert sibling to its own primary with a distinct prefix")

    any_failed = any(s in ("INVALID", "unclassifiable") for _, s, _, _ in rows)
    if any_failed:
        print()
        print("failures:")
        for name, shape, _, errors in rows:
            if shape in ("INVALID", "unclassifiable"):
                print(f"  {name}: {shape}")
                for e in errors:
                    print(f"    - {e}")
        return 1
    return 0


def _split_error(err: str) -> dict:
    """Split a `loc: message` error string into structured {loc, message}.

    validate_manifest emits errors as "<location>: <message>" (schema path,
    `<filename>`, or `identity-grammar`). Split on the FIRST `: ` only, since
    messages themselves contain colons ("is not one of [...]"). A string with
    no separator becomes a general finding with loc "".
    """
    loc, sep, message = err.partition(": ")
    if not sep:
        return {"loc": "", "message": err}
    return {"loc": loc, "message": message}


def run_json(paths: list[str]) -> int:
    """Validate each manifest and emit a JSON array of structured results.

    Shape per record: {file, ok, errors:[{loc, message}], shape, detail}.
    This is the machine-readable form of the default text mode — the linter-
    as-a-service (portal POST /api/onboard/lint) and batch upload consume it,
    so the webform runs IDENTICAL validation to the goat admin onboard Phase-0
    gate. Additive: the text + --report modes are unchanged.

    Exit code mirrors the text mode: 1 if any manifest is invalid, else 0.
    """
    schema = load_schema()
    Draft7Validator.check_schema(schema)
    validator = Draft7Validator(schema)

    results = []
    any_failed = False
    for raw in paths:
        path = Path(raw)
        errors = validate_manifest(path, validator)
        rec: dict = {
            "file": str(path),
            "ok": not errors,
            "errors": [_split_error(e) for e in errors],
            "shape": None,
            "detail": None,
        }
        if errors:
            any_failed = True
        else:
            try:
                with path.open() as f:
                    data = yaml.safe_load(f)
                shape, note = classify_shape(data or {})
                rec["shape"] = shape
                rec["detail"] = note
            except yaml.YAMLError as e:
                rec["ok"] = False
                rec["errors"] = [{"loc": "", "message": f"YAML parse error: {e}"}]
                any_failed = True
        results.append(rec)

    print(json.dumps(results, indent=2))
    return 1 if any_failed else 0


def main(argv: list[str]) -> int:
    if len(argv) >= 2 and argv[1] == "--json":
        if len(argv) < 3:
            print(json.dumps([]), file=sys.stderr)
            print("Usage: validate.py --json <manifest.yml> [<manifest.yml> ...]", file=sys.stderr)
            return 2
        return run_json(argv[2:])

    if len(argv) >= 2 and argv[1] == "--report":
        manifests_dir = Path(argv[2]).resolve() if len(argv) >= 3 else None
        if manifests_dir is not None and not manifests_dir.is_dir():
            print(f"ERROR: --report directory does not exist: {manifests_dir}", file=sys.stderr)
            return 2
        return run_report(manifests_dir)

    if len(argv) < 2:
        print("Usage: validate.py <manifest.yml> [<manifest.yml> ...]", file=sys.stderr)
        print("       validate.py --report", file=sys.stderr)
        return 2

    schema = load_schema()
    Draft7Validator.check_schema(schema)
    validator = Draft7Validator(schema)

    any_failed = False
    for raw in argv[1:]:
        path = Path(raw)
        errors = validate_manifest(path, validator)
        if errors:
            any_failed = True
            print(f"FAIL: {path}")
            for err in errors:
                print(f"  - {err}")
        else:
            print(f"OK:   {path}")

    return 1 if any_failed else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
