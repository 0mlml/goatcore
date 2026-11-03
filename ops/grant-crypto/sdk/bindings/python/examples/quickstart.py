#!/usr/bin/env python3
"""Runnable, self-verifying tour of the grant-crypto Python binding.

Walks the whole surface once — identity -> acquire -> seal -> verify -> admit ->
open -> rotate — asserts the roundtrip, prints the rotation receipt, and exits
non-zero if anything fails (so CI can run it as a smoke test).

Run it from anywhere once the C ABI library is built:

    cd ops/grant-crypto/sdk && cargo build -p grant-crypto-capi
    python3 bindings/python/examples/quickstart.py

Stdlib only. The signed KeyBundle and key material come from the committed
cross-language conformance vectors (the same material every binding validates
against) — this example does not re-implement the key service. The vectors'
`member` identity has a slot in the `base` bundle, so it acts as both the producer
(acquire/seal) and the consumer (admit/open) here.
"""

from __future__ import annotations

import base64
import json
import sys
from pathlib import Path

# examples/ -> python/ (the package root) -> bindings/ -> sdk/ -> grant-crypto/
_PKG_ROOT = Path(__file__).resolve().parents[1]
_GRANT_CRYPTO = Path(__file__).resolve().parents[4]
sys.path.insert(0, str(_PKG_ROOT))  # so `import grant_crypto` works when run directly

import grant_crypto as gc  # noqa: E402  (after the sys.path shim)

VECTORS = _GRANT_CRYPTO / "envelope" / "testdata" / "vectors" / "vectors.json"

RELEASE_SET = "coalition-air"
KEYEXPR = "data/coalition-air/cop"


def main() -> int:
    v = json.loads(VECTORS.read_text())
    member_scalar = bytes.fromhex(v["keys"]["member"]["priv_scalar_hex"])
    outsider_scalar = bytes.fromhex(v["keys"]["outsider"]["priv_scalar_hex"])
    key_service_spki = base64.b64decode(v["keys"]["keyservice"]["pub_spki_b64"])
    bundle_json = json.dumps(v["artifacts"]["base"]["bundle"]).encode()
    bundles_json = json.dumps([v["artifacts"]["base"]["bundle"]]).encode()

    message = b"brigade COP snapshot"

    with gc.Identity("member", member_scalar) as ident:
        # --- Producer: acquire a seal-capable key from our own slot, then seal. ---
        # seal takes no recipients: the key service already resolved the audience.
        with gc.StreamKey.acquire(bundles_json, RELEASE_SET, ident) as sk:
            sk.set_cap(2)  # tighten-only, for a legible `remaining` below
            envelope = sk.seal(KEYEXPR, message)
            remaining = sk.remaining
            assert remaining == 1, f"remaining after one seal = {remaining}, want 1"

        # --- Consumer: verify the bundle, admit our slot, open the message. ---
        # Consumers MUST verify before admitting; a bad signature raises here.
        gc.verify_bundle(bundle_json, key_service_spki)
        with gc.KeyStore() as ks:
            had_slot = ks.admit(ident, bundle_json)
            assert had_slot, "member should have a slot in the base bundle"
            plaintext = ks.open(KEYEXPR, envelope)
            assert plaintext == message, f"roundtrip mismatch: {plaintext!r} != {message!r}"

            # Opening under a different topic must fail closed (the key-expr is AEAD-bound).
            try:
                ks.open(KEYEXPR + "/OTHER", envelope)
            except gc.DecryptError:
                pass  # expected: wrong topic must fail closed
            else:  # pragma: no cover
                raise AssertionError("wrong-topic open should have raised DecryptError")

        # --- Rotate: destroy-by-default forward secrecy (re-wraps nothing). ---
        receipt = ident.rotate(outsider_scalar, keep_access=False, bundles_json=bundles_json)
        assert receipt["policy"] == "destroy"
        assert receipt["now_unreadable_generations"] == 1
        assert receipt["rewrapped_generations"] == 0
        assert receipt["timestamp_rfc3339"].endswith("Z")

    print(f"opened: {plaintext.decode()}")
    print(f"seals remaining after one seal (cap 2): {remaining}")
    print("rotation receipt:", json.dumps(receipt, sort_keys=True))
    print("OK")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
