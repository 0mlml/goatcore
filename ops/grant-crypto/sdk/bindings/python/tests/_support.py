"""Shared test helpers: locate and load the committed conformance vectors."""

from __future__ import annotations

import json
from pathlib import Path

# _support.py -> tests/ -> python/ -> bindings/ -> sdk/ -> grant-crypto/
_GRANT_CRYPTO = Path(__file__).resolve().parents[4]
VECTORS_PATH = _GRANT_CRYPTO / "envelope" / "testdata" / "vectors" / "vectors.json"


def load_vectors() -> dict:
    return json.loads(VECTORS_PATH.read_text())


# Convenience fixtures drawn from the committed vectors, reused by the write-side
# and rotate tests. "member" and "outsider" both carry valid P-256 scalars, so
# "outsider" doubles as a known-valid target scalar for rotation (no random-key
# generation needed).
def base_fixture():
    """(member_id, member_scalar, base_bundle_json_bytes, keyexpr, rs)."""
    v = load_vectors()
    art = v["artifacts"]["base"]
    bundle_json = json.dumps(art["bundle"]).encode()
    member_scalar = bytes.fromhex(v["keys"]["member"]["priv_scalar_hex"])
    return "member", member_scalar, bundle_json, art["keyexpr"], art["bundle"]["rs"]


def outsider_scalar() -> bytes:
    return bytes.fromhex(load_vectors()["keys"]["outsider"]["priv_scalar_hex"])
