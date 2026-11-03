"""
affiliation.kind from `sector` for the onboard play, from the one table (ADR 1151).

operator-onboard.yml puts a `kind` on the Ory `aff` trait. A manifest that
declares only the ADR 1151 target block carries `sector` and no kind, so the
play derives one. The sector-to-kind table lives in ops/operators/gen-roster.py
(derive_kind), paired with the console's Go coalition.DeriveKind and held to it
by ops/operators/test_gen_roster_derive_kind.py. This filter calls that function
by path instead of carrying a third copy, so a sector added there reaches the
play with no edit here.

    {{ manifest.affiliation.sector | default('') | affiliation_derive_kind }}

Returns '' wherever derive_kind cannot decide, never a guess: an unknown
sector, or a `state` sector with no operating sovereign, since organic vs
partner is relative to the goatnet's operator. The play passes no sovereign,
so a state sector mints no kind.
"""

import functools
import importlib.util
from pathlib import Path

GEN_ROSTER = Path(__file__).resolve().parents[2] / "operators" / "gen-roster.py"


@functools.cache
def _derive_kind():
    """gen-roster.py's derive_kind, loaded on first use (the controller has PyYAML)."""
    spec = importlib.util.spec_from_file_location("gen_roster_derive_kind", GEN_ROSTER)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module.derive_kind


def affiliation_derive_kind(sector, nation="", operating_sovereign=""):
    """derive_kind for a Jinja caller: '' where derive_kind returns None."""
    text = "" if sector is None else str(sector)
    return _derive_kind()(text, nation, operating_sovereign) or ""


class FilterModule:
    def filters(self):
        return {"affiliation_derive_kind": affiliation_derive_kind}
