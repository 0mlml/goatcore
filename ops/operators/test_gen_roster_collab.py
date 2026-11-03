#!/usr/bin/env python3
"""Unit test for the collaboration-environment projection in gen-roster.py
Standalone (no pytest dependency — same style as
test_validate_grammar.py); exits non-zero on any failed assertion so a CI step
can gate on it. Run: python3 ops/operators/test_gen_roster_collab.py

What it locks — the honesty property first, because it is the one that matters:

  - **absent ≠ none.** A manifest with no collab block projects `unrecorded`,
    never `none`. Conflating them would assert that a person has no
    collaboration-environment account when nobody ever checked — which is
    exactly how 65 live accounts went unnoticed.
  - a partial block (present, but stating neither a decision nor an account) is
    also `unrecorded`, not `none`.
  - `enabled: false` → `none`; `enabled: true` with no account → `declared`;
    an `account` → `account` (even if `enabled` was never written).
  - the projection reaches BOTH roster views (nested org members + the flat
    member list the portal consumes).
  - no account id, and no email, leaves the generator — the projection is
    presence/status/service only.
  - the markdown cell for `unrecorded` is NOT a dash (a dash reads as "none").
"""
from __future__ import annotations

import importlib.util
import json
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
# gen-roster.py is hyphenated (a script, not an importable module name), so load
# it by path rather than renaming a file the whole repo references.
_spec = importlib.util.spec_from_file_location("gen_roster", HERE / "gen-roster.py")
gen_roster = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(gen_roster)

FAILS: list[str] = []


def check(name: str, cond: bool, detail: str = "") -> None:
    if cond:
        print(f"ok   - {name}")
    else:
        print(f"FAIL - {name}  {detail}")
        FAILS.append(name)


def manifest(handle: str, collab=None, **ident) -> dict:
    """A minimal manifest fixture. `collab=None` means the block is ABSENT —
    the case the honesty rule turns on."""
    m = {
        "identity": {
            "handle": handle,
            "email": f"{handle}@example.gov",
            "status": "active",
            "added": "2026-07-01",
            **ident,
        },
        "affiliation": {"id": "acme", "kind": "vendor"},
    }
    if collab is not None:
        m["collab"] = collab
    return m


# --- _collab_projection unit cases -----------------------------------------

proj = gen_roster._collab_projection

check(
    "absent block projects 'unrecorded' — NOT 'none' (the F-274 honesty rule)",
    proj(manifest("a")) == {"state": "unrecorded", "provider": None},
    detail=f"got {proj(manifest('a'))}",
)
check(
    "explicit enabled:false projects 'none' (a recorded decision)",
    proj(manifest("b", {"enabled": False}))["state"] == "none",
)
check(
    "enabled:true with no account projects 'declared'",
    proj(manifest("c", {"enabled": True}))["state"] == "declared",
)
check(
    "a recorded account projects 'account'",
    proj(manifest("d", {"enabled": True, "account": "d"}))["state"] == "account",
)
check(
    "an account recorded without 'enabled' still projects 'account'",
    proj(manifest("e", {"account": "e"}))["state"] == "account",
)
check(
    "a partial block stating neither decision nor account is 'unrecorded'",
    proj(manifest("f", {"provider": "goat-collab"}))["state"] == "unrecorded",
    detail=f"got {proj(manifest('f', {'provider': 'goat-collab'}))}",
)
check(
    "a non-object collab block degrades to 'unrecorded', never crashes",
    proj({"identity": {}, "collab": "yes"})["state"] == "unrecorded",
)
check(
    "provider slug is carried",
    proj(manifest("g", {"enabled": True, "account": "g", "provider": "goat-collab"}))["provider"]
    == "goat-collab",
)
check(
    "blank/whitespace provider normalizes to null",
    proj(manifest("h", {"enabled": True, "provider": "   "}))["provider"] is None,
)
check(
    "whitespace-only account does not count as a recorded account",
    proj(manifest("i", {"enabled": True, "account": "  "}))["state"] == "declared",
)
check(
    "every projected state is inside the closed vocabulary",
    all(
        proj(m)["state"] in gen_roster.COLLAB_STATES
        for m in (
            manifest("j"),
            manifest("k", {"enabled": False}),
            manifest("l", {"enabled": True}),
            manifest("m", {"account": "m"}),
        )
    ),
)

# --- the projection reaches both roster views ------------------------------

reg = gen_roster.build_registry(
    [
        manifest("has-account", {"enabled": True, "account": "has-account", "provider": "goat-collab"}),
        manifest("declared-only", {"enabled": True}),
        manifest("explicit-none", {"enabled": False}),
        manifest("no-block"),
    ]
)

flat = {r["handle"]: r for r in reg["members"]}
nested = {r["handle"]: r for org in reg["orgs"] for r in org["members"]}

check("flat member list carries the projection", all("collab" in r for r in flat.values()))
check("nested org members carry the projection", all("collab" in r for r in nested.values()))
check(
    "both views agree per handle",
    all(flat[h]["collab"] == nested[h]["collab"] for h in flat),
)
check(
    "states land as expected across the four fixtures",
    [flat[h]["collab"]["state"] for h in ("declared-only", "explicit-none", "has-account", "no-block")]
    == ["declared", "none", "account", "unrecorded"],
    detail=f"got {[flat[h]['collab']['state'] for h in ('declared-only', 'explicit-none', 'has-account', 'no-block')]}",
)

# --- sanitization: no account id, no email, ever ---------------------------

digest_input = json.dumps({"orgs": reg["orgs"], "members": reg["members"]})
check(
    "the account id never leaves the generator",
    "has-account" not in json.dumps([r["collab"] for r in flat.values()]),
    detail=json.dumps([r["collab"] for r in flat.values()]),
)
check(
    "no email address anywhere in the registry payload",
    "@" not in digest_input,
)
check(
    "the projection carries exactly {state, provider}",
    all(set(r["collab"]) == {"state", "provider"} for r in flat.values()),
)

# --- markdown view ---------------------------------------------------------

md = gen_roster.render_markdown(reg)
check("markdown table has a collab column", "| collab |" in md)
check(
    "unrecorded renders as '?', not a dash (a dash reads as 'none')",
    gen_roster._collab_cell({"state": "unrecorded", "provider": None}) == "?",
)
check(
    "an account cell names the service",
    gen_roster._collab_cell({"state": "account", "provider": "goat-collab"}) == "account (goat-collab)",
)
check(
    "the legend states that unrecorded is not evidence of no account",
    "NOT evidence of" in md and "no account" in md,
)

# --- the JSON artifact is self-describing ----------------------------------

js = json.loads(gen_roster.render_json(reg, "2026-08-03T00:00:00Z", gen_roster.content_digest(reg)))
check(
    "the _comment explains the collab projection + the unrecorded caveat",
    "collab" in js["_comment"] and "unrecorded" in js["_comment"],
)
check(
    "adding the projection keeps the digest content-derived (changes with content)",
    gen_roster.content_digest(reg)
    != gen_roster.content_digest(gen_roster.build_registry([manifest("no-block")])),
)

# --- the CI guard accepts this shape and rejects an off-vocabulary one -----

sys.path.insert(0, str(HERE.parent / "ci"))
_guard_spec = importlib.util.spec_from_file_location(
    "check_roster_registry", HERE.parent / "ci" / "check-roster-registry.py"
)
guard = importlib.util.module_from_spec(_guard_spec)
_guard_spec.loader.exec_module(guard)

check(
    "CI guard accepts a generated projection",
    guard._collab_errors("m", flat["has-account"]) == [],
    detail=str(guard._collab_errors("m", flat["has-account"])),
)
check(
    "CI guard accepts a member with no collab key (pre-projection rosters)",
    guard._collab_errors("m", {"handle": "old"}) == [],
)
check(
    "CI guard rejects a state outside the closed vocabulary",
    len(guard._collab_errors("m", {"collab": {"state": "maybe", "provider": None}})) == 1,
)
check(
    "CI guard rejects an account id smuggled into the projection",
    any(
        "account" in e
        for e in guard._collab_errors("m", {"collab": {"state": "account", "account": "jane-doe"}})
    ),
)

if FAILS:
    print(f"\n{len(FAILS)} assertion(s) failed.")
    sys.exit(1)
print("\nall collab-projection assertions passed.")
