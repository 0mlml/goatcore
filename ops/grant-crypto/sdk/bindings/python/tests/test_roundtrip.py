"""Write-side roundtrip through the binding: acquire -> seal -> admit -> open,
using the committed vector key material (the "member" is both producer and
consumer of the base bundle)."""

from __future__ import annotations

import json
import unittest

import grant_crypto as gc

from _support import base_fixture


class Roundtrip(unittest.TestCase):
    def _roundtrip(self, plaintext: bytes):
        member_id, scalar, bundle_json, keyexpr, rs = base_fixture()
        bundles_json = json.dumps([json.loads(bundle_json)]).encode()
        with gc.Identity(member_id, scalar) as ident:
            with gc.StreamKey.acquire(bundles_json, rs, ident) as sk:
                envelope = sk.seal(keyexpr, plaintext)
            self.assertTrue(envelope, "seal produced a non-empty envelope")
            with gc.KeyStore() as ks:
                self.assertTrue(ks.admit(ident, bundle_json), "member has a slot")
                self.assertEqual(ks.open(keyexpr, envelope), plaintext)

    def test_text_plaintext(self):
        self._roundtrip(b"brigade COP snapshot via the python binding")

    def test_empty_plaintext(self):
        self._roundtrip(b"")

    def test_binary_plaintext_with_nul_bytes(self):
        self._roundtrip(bytes(range(256)) + b"\x00\x00trailing")

    def test_wrong_topic_fails_the_aead(self):
        member_id, scalar, bundle_json, keyexpr, rs = base_fixture()
        bundles_json = json.dumps([json.loads(bundle_json)]).encode()
        with gc.Identity(member_id, scalar) as ident:
            with gc.StreamKey.acquire(bundles_json, rs, ident) as sk:
                envelope = sk.seal(keyexpr, b"top secret")
            with gc.KeyStore() as ks:
                ks.admit(ident, bundle_json)
                with self.assertRaises(gc.DecryptError):
                    ks.open(keyexpr + "/OTHER", envelope)

    def test_nonce_is_fresh_each_seal(self):
        # Eight seals of the SAME plaintext under one key must each carry a distinct
        # nonce (a fresh random 96-bit nonce per seal — the property the seal cap protects).
        member_id, scalar, bundle_json, keyexpr, rs = base_fixture()
        bundles_json = json.dumps([json.loads(bundle_json)]).encode()
        with gc.Identity(member_id, scalar) as ident:
            with gc.StreamKey.acquire(bundles_json, rs, ident) as sk:
                nonces = set()
                for _ in range(8):
                    env = json.loads(sk.seal(keyexpr, b"same plaintext each time"))
                    nonces.add(env["nonce"])
        self.assertEqual(len(nonces), 8, "each seal must use a fresh, distinct nonce")

    def test_bytearray_and_memoryview_accepted(self):
        member_id, scalar, bundle_json, keyexpr, rs = base_fixture()
        bundles_json = json.dumps([json.loads(bundle_json)]).encode()
        with gc.Identity(member_id, bytearray(scalar)) as ident:
            with gc.StreamKey.acquire(bundles_json, rs, ident) as sk:
                envelope = sk.seal(keyexpr, memoryview(b"via memoryview"))
            with gc.KeyStore() as ks:
                ks.admit(ident, bytearray(bundle_json))
                self.assertEqual(ks.open(keyexpr, envelope), b"via memoryview")


if __name__ == "__main__":
    unittest.main()
