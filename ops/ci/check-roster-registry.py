#!/usr/bin/env python3
"""CI guard for the committed principal-graph registry artifacts.

`ops/operators/gen-roster.py` generates one artifact per goatnet from the
gitignored operator manifests (the SoT):

  * `docs/operators/<goatnet>/roster.json` — what that goatnet's portal is
    served, built from `ops/operators/<goatnet>/` alone (ADR 1172).
  * `docs/operators/roster.json` — the controller-side union across every
    environment, read by repo-side tooling and deployed nowhere.

Every one of them is checked, by the same rules. Because the manifests are
gitignored, CI **cannot** re-derive a roster to diff it (a literal
`gen-roster.py --check` only works locally, where the manifests exist). This
guard does what CI *can* do without the manifests: assert each committed
artifact is well-formed, internally consistent, and free of identity material.

Catches: hand-edits, corruption, a regen of roster.md but not roster.json,
and any name/email leak into the public-safe artifacts.

Exit 0 = clean; non-zero = a problem (printed to stderr).
"""
from __future__ import annotations

import json
import re
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
OPERATORS_DIR = REPO / "docs" / "operators"
ROSTER_JSON = OPERATORS_DIR / "roster.json"


def roster_paths() -> list[Path]:
    """Every committed roster artifact: the union plus one per goatnet."""
    paths = [ROSTER_JSON] if ROSTER_JSON.exists() else []
    paths += sorted(
        d / "roster.json" for d in OPERATORS_DIR.iterdir()
        if d.is_dir() and (d / "roster.json").exists()
    ) if OPERATORS_DIR.is_dir() else []
    return paths

EMAIL_RE = re.compile(r"[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}")
# Capitalized "First Last" — a cheap human-name sniff. Slugs are kebab/lower.
NAME_RE = re.compile(r"\b[A-Z][a-z]+ [A-Z][a-z]+\b")
VALID_KIND = {"vendor", "organic", "partner", None}
# Closed collab-projection vocabulary, mirroring gen-roster.py's
# COLLAB_STATES. Checked only WHEN PRESENT: rosters generated before the
# projection landed carry no collab key and stay valid.
VALID_COLLAB_STATE = {"account", "declared", "none", "unrecorded"}
# ADR 1171 member nationality: alpha-3 shape. gen-roster.py folds alpha-2 to
# alpha-3 before emitting, so an alpha-2 value reaching the artifact means the
# fold was bypassed. NATO / EU are ORG-level multinational markers, not a
# person's citizenship, so they are deliberately not admitted here.
NATIONALITY_RE = re.compile(r"^[A-Z]{3}$")


def _kebab(s: str) -> str:
    """Lowercase, non-alphanumerics to single dashes — the handle/id shape."""
    return re.sub(r"-+", "-", re.sub(r"[^a-z0-9]+", "-", s.lower())).strip("-")


def _raw_without_org_display_names(raw: str, orgs: list) -> str:
    """The artifact text with every org display_name value removed."""
    out = raw
    for org in orgs:
        dn = org.get("display_name")
        if isinstance(dn, str) and dn:
            out = out.replace(json.dumps(dn), '""')
    return out


def fail(msg: str) -> None:
    print(f"FAIL: {msg}", file=sys.stderr)


def _nationality_errors(where: str, entry: dict) -> list[str]:
    """Validate one member's own nationality (ADR 1171), when it carries one.

    Shape only — CI cannot check whether the value is TRUE. What it can hold is
    that the field never silently becomes something other than an alpha-3 code,
    and that null survives as null. Absent and null both mean UNDECLARED, and a
    reader must not turn either into the organisation's nation, which is a
    separate field on the org row.
    """
    if "nationality" not in entry:
        return []  # pre-ADR-1171 rows carry no key at all and stay valid
    v = entry["nationality"]
    if v is None:
        return []
    if not isinstance(v, str):
        return [f"{where}: 'nationality' is not a string or null"]
    if not NATIONALITY_RE.match(v):
        return [
            f"{where}: nationality '{v}' is not an upper-case alpha-3 code — the "
            "generator folds alpha-2 before emitting, so this means either the "
            "fold was bypassed or the manifest carries something that is not a "
            "country"
        ]
    return []


def _collab_errors(where: str, entry: dict) -> list[str]:
    """Validate one member's collab projection, when it carries one.

    Locks two things CI can check without the manifests: the state stays inside
    the closed vocabulary (so a consumer's exhaustive switch cannot silently
    meet a new value), and no collab ACCOUNT ID rides along — the projection is
    presence/status/service only, and an account id is a login identifier on a
    partner-facing plane.
    """
    collab = entry.get("collab")
    if collab is None:
        return []
    if not isinstance(collab, dict):
        return [f"{where}: 'collab' is not an object"]
    errs = []
    state = collab.get("state")
    if state not in VALID_COLLAB_STATE:
        errs.append(f"{where}: invalid collab state '{state}'")
    provider = collab.get("provider")
    if provider is not None and not isinstance(provider, str):
        errs.append(f"{where}: collab provider is not a string or null")
    for leaked in ("account", "email", "activated_at"):
        if leaked in collab:
            errs.append(f"{where}: collab carries '{leaked}' — the projection is state+provider only")
    return errs


def main() -> int:
    paths = roster_paths()
    if not paths:
        # No roster yet (e.g. a fresh tree with no manifests ever generated) is
        # not a failure — there is simply nothing to guard.
        print("OK: no roster.json present (nothing to check)")
        return 0
    rc = 0
    for path in paths:
        rc |= check_one(path)
    return rc


def check_one(path: Path) -> int:
    rel = path.relative_to(REPO)
    per_goatnet = path.parent != OPERATORS_DIR
    raw = path.read_text()
    try:
        data = json.loads(raw)
    except json.JSONDecodeError as exc:
        fail(f"{rel} is not valid JSON: {exc}")
        return 1

    errors = []
    if not isinstance(data, dict):
        fail(f"{rel} top level is not an object")
        return 1

    orgs = data.get("orgs")
    members = data.get("members")
    if not isinstance(orgs, list):
        errors.append("missing/invalid 'orgs' list")
        orgs = []
    if not isinstance(members, list):
        errors.append("missing/invalid 'members' list")
        members = []

    org_ids = set()
    member_count_in_orgs = 0
    for i, org in enumerate(orgs):
        for key in ("id", "kind", "nation", "parent", "members"):
            if key not in org:
                errors.append(f"orgs[{i}] missing key '{key}'")
        oid = org.get("id")
        if oid in org_ids:
            errors.append(f"duplicate org id '{oid}'")
        org_ids.add(oid)
        if org.get("kind") not in VALID_KIND:
            errors.append(f"org '{oid}': invalid kind '{org.get('kind')}'")
        if not isinstance(org.get("members"), list):
            errors.append(f"org '{oid}': members is not a list")
        else:
            member_count_in_orgs += len(org["members"])

    # The flat 'members' list must account for exactly the nested members.
    if len(members) != member_count_in_orgs:
        errors.append(
            f"flat members ({len(members)}) != sum of org members ({member_count_in_orgs})"
        )
    for i, m in enumerate(members):
        if m.get("org") not in org_ids:
            errors.append(f"members[{i}] org '{m.get('org')}' not in orgs")
        errors += _collab_errors(f"members[{i}]", m)
        errors += _nationality_errors(f"members[{i}]", m)
    for org in orgs:
        for r in org.get("members", []) if isinstance(org.get("members"), list) else []:
            errors += _collab_errors(f"org '{org.get('id')}' member '{r.get('handle')}'", r)

    # Public-safe: no emails, no obvious human names anywhere in the artifact.
    if EMAIL_RE.search(raw):
        errors.append("roster.json contains an email address (identity leak)")
    # The capitalized-pair heuristic cannot be applied to org display names: a
    # two-word company name matches it as well as a person's name does. Scan everything the
    # artifact carries EXCEPT those labels, so the check keeps its teeth on the
    # member half where a real name would leak.
    #
    # The one case where a display_name really would be a person's name — an org
    # whose id is also somebody's handle — is removed by the generator instead,
    # which is the precise rule rather than this heuristic one.
    member_handles = {
        m.get("handle") for m in members if isinstance(m, dict) and m.get("handle")
    }
    scanned = _raw_without_org_display_names(raw, orgs)
    if NAME_RE.search(scanned):
        m = NAME_RE.search(scanned)
        errors.append(f"roster.json contains a capitalized name-like token ('{m.group()}') — possible identity leak")
    for org in orgs:
        dn = org.get("display_name")
        if isinstance(dn, str) and _kebab(dn) == org.get("id") and org.get("id") in member_handles:
            errors.append(
                f"org '{org.get('id')}': display_name '{dn}' is a person's own handle rendered as a name "
                f"— an org named after a person must not carry the name as a label"
            )

    # A goatnet artifact that declares nobody is refused. Deploying one does not
    # fail — the portal loads it, finds no declared half, and every live
    # identity on that goatnet reconciles to `out_of_band`, which reads as a
    # fleet of strays rather than as a missing file. The union may legitimately
    # be empty on a fresh tree with no manifests; a committed per-goatnet
    # artifact never can be, because the goatnet has people or it has no portal.
    if per_goatnet and isinstance(members, list) and not members:
        errors.append(
            f"{rel} declares zero members — a goatnet's roster is never empty. "
            f"Fill {rel.parent.name}'s manifest folder from its own console "
            f"(ops/scripts/sync-onboarded-manifests.py --env {rel.parent.name}), "
            f"regenerate (gen-roster.py --env {rel.parent.name}), or set "
            f"portal_roster_path: \"\" for that goatnet so the COP reports the "
            f"gap instead of reporting strays"
        )

    if errors:
        for e in errors:
            fail(e)
        return 1

    print(f"OK: {rel} valid — {len(orgs)} orgs, {len(members)} members, no identity leak")
    return 0


if __name__ == "__main__":
    sys.exit(main())
