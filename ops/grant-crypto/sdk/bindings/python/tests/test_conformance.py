"""The conformance gate: every committed decrypt-direction vector run through the
binding's verify -> admit -> open path, with byte-for-byte accept/reject parity.

The reference consumer order (conformance.go / envelope/vectors_test.go) is:
verify (if a trust identity is given) -> the member's slot must exist -> unwrap ->
open. The binding renders that as ``verify_bundle`` -> ``KeyStore.admit`` (a
``False`` return is the "no slot" reject) -> ``KeyStore.open``.

Per conformance.go the *reason* strings are advisory — the binding contract is the
accept/reject decision and, on accept, the plaintext. So the hard gate here is
accept/reject parity plus plaintext. On a reject we additionally assert the raised
exception is a sensible reject-class :class:`GrantCryptoError` for that reason (a
soft class check). Note one honest divergence: the "wrong generation" case has an
advisory reason of ``open-failed`` but the binding rejects earlier, at the
gen-indexed keystore lookup, with :class:`NoSlotError` — both are correct rejects,
which is why ``open-failed`` admits either class below.
"""

from __future__ import annotations

import base64
import json
import unittest

import grant_crypto as gc

from _support import load_vectors

# Advisory reason -> the reject class(es) acceptable for it through this binding.
_REASON_CLASSES = {
    "no-slot": (gc.NoSlotError,),
    "bad-signature": (gc.BadSignatureError,),
    "unsupported-suite": (gc.UnsupportedSuiteError,),
    # AEAD / gen-lookup rejects: DecryptError (wrong topic, tampered ct) or
    # NoSlotError (wrong generation — no held DEK for that gen).
    "open-failed": (gc.DecryptError, gc.NoSlotError),
}


class ConformanceVectors(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.v = load_vectors()
        assert cls.v["suite"] == "v1-ecdh-p256", cls.v["suite"]
        assert cls.v["cases"], "no cases in vectors"

    def _run_case(self, case) -> bytes:
        """Run one vector through the binding; return plaintext or raise the mapped
        reject exception."""
        v = self.v
        art = v["artifacts"][case["artifact"]]
        bundle_json = json.dumps(art["bundle"]).encode()
        envelope_json = json.dumps(art["envelope"]).encode()
        keyexpr = case.get("keyexpr") or art["keyexpr"]

        member = v["keys"][case["member"]]
        scalar = bytes.fromhex(member["priv_scalar_hex"])

        # 1. verify the signature if the case supplies a trust identity.
        if case.get("verify"):
            vk = base64.b64decode(v["keys"][case["verify"]]["pub_spki_b64"])
            gc.verify_bundle(bundle_json, vk)

        # 2-4. slot must exist -> unwrap -> open.
        with gc.Identity(case["member"], scalar) as ident, gc.KeyStore() as ks:
            if not ks.admit(ident, bundle_json):
                raise gc.NoSlotError("member has no slot in the bundle")
            return ks.open(keyexpr, envelope_json)


def _make(case):
    def test(self):
        expect = case["expect"]
        if expect["result"] == "accept":
            got = self._run_case(case)
            want = base64.b64decode(expect["plaintext_b64"])
            self.assertEqual(got, want, "plaintext parity")
        else:
            with self.assertRaises(gc.GrantCryptoError) as cm:
                self._run_case(case)
            reason = expect.get("reason")
            if reason in _REASON_CLASSES:
                self.assertIsInstance(cm.exception, _REASON_CLASSES[reason])

    return test


# Attach one test method per vector case so `-v` reports each by name.
for _i, _case in enumerate(load_vectors()["cases"]):
    _slug = "".join(c if c.isalnum() else "_" for c in _case["name"]).strip("_")
    setattr(ConformanceVectors, f"test_{_i:02d}_{_slug}", _make(_case))


if __name__ == "__main__":
    unittest.main()
