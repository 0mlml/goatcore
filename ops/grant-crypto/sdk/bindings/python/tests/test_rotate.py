"""Member-leaf rotation: Destroy loses access, KeepAccess re-wraps. Both emit a
structured, self-dating receipt. The rotation target is the vectors' "outsider"
scalar (a known-valid P-256 key), so no random-key generation is needed."""

from __future__ import annotations

import json
import unittest

import grant_crypto as gc

from _support import base_fixture, outsider_scalar


class Rotate(unittest.TestCase):
    def test_destroy_loses_access_and_reports_receipt(self):
        member_id, scalar, bundle_json, _keyexpr, rs = base_fixture()
        bundles_json = json.dumps([json.loads(bundle_json)]).encode()
        with gc.Identity(member_id, scalar) as ident:
            receipt = ident.rotate(outsider_scalar(), keep_access=False, bundles_json=bundles_json)
            self.assertEqual(receipt["policy"], "destroy")
            self.assertEqual(receipt["now_unreadable_generations"], 1)
            self.assertEqual(receipt["rewrapped_generations"], 0)
            self.assertNotIn("rewrapped", receipt)
            self.assertTrue(receipt["timestamp_rfc3339"].endswith("Z"))

            # The handle now holds the NEW key: the old slot (wrapped to the old
            # key) can no longer be unwrapped.
            with gc.KeyStore() as ks:
                with self.assertRaises(gc.UnwrapError):
                    ks.admit(ident, bundle_json)

    def test_keep_access_rewraps_so_the_new_key_opens(self):
        member_id, scalar, bundle_json, _keyexpr, rs = base_fixture()
        bundles_json = json.dumps([json.loads(bundle_json)]).encode()
        with gc.Identity(member_id, scalar) as ident:
            receipt = ident.rotate(outsider_scalar(), keep_access=True, bundles_json=bundles_json)
            self.assertEqual(receipt["policy"], "keep-access")
            self.assertEqual(receipt["now_unreadable_generations"], 1)
            self.assertEqual(receipt["rewrapped_generations"], 1)
            rewrapped = receipt["rewrapped"]
            self.assertEqual(len(rewrapped), 1)
            self.assertEqual(rewrapped[0]["rs"], rs)
            self.assertEqual(rewrapped[0]["gen"], json.loads(bundle_json)["gen"])

            # Reconstruct a bundle from the re-wrapped slot; the (now-new) key
            # unwraps it -> proves KeepAccess re-wrapped to the new key.
            rewrapped_bundle = {
                "suite": "v1-ecdh-p256",
                "rs": rs,
                "gen": rewrapped[0]["gen"],
                "members": {member_id: rewrapped[0]["wrapped"]},
                "signer": "",
                "sig": "",
            }
            with gc.KeyStore() as ks:
                self.assertTrue(ks.admit(ident, json.dumps(rewrapped_bundle).encode()))

    def test_bad_scalar_length_is_value_error(self):
        member_id, scalar, bundle_json, _keyexpr, _rs = base_fixture()
        with gc.Identity(member_id, scalar) as ident:
            with self.assertRaises(ValueError):
                ident.rotate(b"too short", keep_access=False, bundles_json=bundle_json)


if __name__ == "__main__":
    unittest.main()
