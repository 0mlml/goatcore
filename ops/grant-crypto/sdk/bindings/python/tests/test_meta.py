"""Smoke checks for the ABI plumbing itself."""

from __future__ import annotations

import unittest

import grant_crypto as gc


class Meta(unittest.TestCase):
    def test_abi_version_is_positive(self):
        self.assertGreaterEqual(gc.abi_version(), 1)

    def test_error_message_is_advisory_text(self):
        self.assertEqual(gc.error_message(0), "ok")
        self.assertIn("no slot", gc.error_message(-5))

    def test_exception_carries_code(self):
        error = gc.NoSlotError("x", -5)
        self.assertEqual(error.code, -5)
        self.assertIsInstance(error, gc.GrantCryptoError)


if __name__ == "__main__":
    unittest.main()
