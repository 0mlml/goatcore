"""The enforced message-count seal cap: tighten-only, remaining counts down, and
seal at the cap raises RotationRequiredError."""

from __future__ import annotations

import json
import unittest

import grant_crypto as gc

from _support import base_fixture


class SealCap(unittest.TestCase):
    def _acquire(self):
        member_id, scalar, bundle_json, keyexpr, rs = base_fixture()
        bundles_json = json.dumps([json.loads(bundle_json)]).encode()
        ident = gc.Identity(member_id, scalar)
        sk = gc.StreamKey.acquire(bundles_json, rs, ident)
        self.addCleanup(ident.close)
        self.addCleanup(sk.close)
        return sk, keyexpr

    def test_cannot_loosen_above_default(self):
        sk, _ = self._acquire()
        with self.assertRaises(gc.MalformedError):
            sk.set_cap(2**60)

    def test_cap_enforced_and_remaining_counts_down(self):
        sk, keyexpr = self._acquire()
        # The seal budget is process-cumulative per (rs, gen) and the bindings share the
        # one committed generation, so tighten relative to what remains now (leave 2).
        default_cap = 1 << 24
        already = default_cap - sk.remaining
        sk.set_cap(already + 2)
        self.assertEqual(sk.remaining, 2)

        sk.seal(keyexpr, b"m1")
        self.assertEqual(sk.remaining, 1)
        sk.seal(keyexpr, b"m2")
        self.assertEqual(sk.remaining, 0)

        with self.assertRaises(gc.RotationRequiredError):
            sk.seal(keyexpr, b"one too many")

    def test_default_cap_is_large(self):
        sk, _ = self._acquire()
        # DEFAULT_SEAL_CAP is 2^24. The counter is process-cumulative, so a handful of
        # seals from other tests may already have run under this generation; assert the
        # budget is within a small band of 2^24 rather than exactly equal.
        default_cap = 1 << 24
        self.assertLessEqual(sk.remaining, default_cap)
        self.assertGreater(sk.remaining, default_cap - 1024)


if __name__ == "__main__":
    unittest.main()
