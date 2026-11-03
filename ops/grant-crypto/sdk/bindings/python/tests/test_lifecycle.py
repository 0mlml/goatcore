"""Handle lifecycle: fail-closed use-after-close, harmless double-close, and the
finalizer that warns (ResourceWarning) but never disposes."""

from __future__ import annotations

import gc as garbage_collector
import json
import unittest
import warnings
import weakref

import grant_crypto as gc

from _support import base_fixture


class Lifecycle(unittest.TestCase):
    def test_use_after_close_raises_disposed_handle(self):
        member_id, scalar, bundle_json, keyexpr, _rs = base_fixture()

        ks = gc.KeyStore()
        ident = gc.Identity(member_id, scalar)
        ks.close()
        # A closed keystore fails closed.
        with self.assertRaises(gc.DisposedHandleError):
            ks.admit(ident, bundle_json)
        with self.assertRaises(gc.DisposedHandleError):
            ks.open(keyexpr, b"{}")

        # A closed identity is likewise unusable, even via an open keystore.
        ident.close()
        with gc.KeyStore() as ks2:
            with self.assertRaises(gc.DisposedHandleError):
                ks2.admit(ident, bundle_json)

    def test_closed_streamkey_fails_closed(self):
        member_id, scalar, bundle_json, keyexpr, rs = base_fixture()
        bundles_json = json.dumps([json.loads(bundle_json)]).encode()
        with gc.Identity(member_id, scalar) as ident:
            sk = gc.StreamKey.acquire(bundles_json, rs, ident)
            sk.close()
            with self.assertRaises(gc.DisposedHandleError):
                sk.seal(keyexpr, b"x")
            with self.assertRaises(gc.DisposedHandleError):
                _ = sk.remaining

    def test_double_close_is_harmless(self):
        ks = gc.KeyStore()
        ks.close()
        ks.close()  # no raise
        # And via the context manager after an explicit close.
        with gc.KeyStore() as ks2:
            ks2.close()
        # exiting the with-block calls close() again -> still no raise.

    def test_finalizer_warns_but_does_not_dispose(self):
        member_id, scalar, bundle_json, _keyexpr, _rs = base_fixture()
        ident = gc.Identity(member_id, scalar)
        reference = weakref.ref(ident)
        # Retain ownership of the native handle for this test only. The original
        # Python wrapper must really be collected; the surviving owner lets us
        # prove that collection did not dispose, and guarantees explicit cleanup.
        survivor = gc.Identity.__new__(gc.Identity)
        survivor._h = ident._h
        with survivor:
            with warnings.catch_warnings(record=True) as caught:
                warnings.simplefilter("always")
                del ident
                garbage_collector.collect()
            self.assertIsNone(reference())
            self.assertTrue(
                any(issubclass(w.category, ResourceWarning) for w in caught),
                "finalizer must emit a ResourceWarning about the leak",
            )
            with gc.KeyStore() as ks:
                self.assertTrue(ks.admit(survivor, bundle_json), "identity still usable")

    def test_bad_scalar_length_rejected_before_ffi(self):
        with self.assertRaises(ValueError):
            gc.Identity("x", b"\x01\x02\x03")

    def test_embedded_nul_in_string_args_rejected(self):
        # The C ABI reads string args NUL-terminated, so an interior NUL would silently
        # truncate. The binding rejects it client-side as MalformedError before the call.
        member_id, scalar, bundle_json, keyexpr, rs = base_fixture()
        bundles_json = json.dumps([json.loads(bundle_json)]).encode()
        with self.assertRaises(gc.MalformedError):
            gc.Identity("member\x00evil", scalar)
        with gc.Identity(member_id, scalar) as ident:
            with self.assertRaises(gc.MalformedError):
                gc.StreamKey.acquire(bundles_json, "coalition\x00air", ident)
            with gc.StreamKey.acquire(bundles_json, rs, ident) as sk:
                with self.assertRaises(gc.MalformedError):
                    sk.seal("data/x\x00y", b"m")
            with gc.KeyStore() as ks:
                with self.assertRaises(gc.MalformedError):
                    ks.open("data/x\x00y", b"{}")

    def test_str_wire_argument_rejected(self):
        # Wire data must be bytes, not str.
        with gc.KeyStore() as ks, gc.Identity(*base_fixture()[:2]) as ident:
            with self.assertRaises(TypeError):
                ks.admit(ident, "not-bytes")


if __name__ == "__main__":
    unittest.main()
