"""Locate and bind the grant-crypto C ABI (``grant_crypto.h``) via ctypes.

This module is the one place that touches the native library. It finds the
``grant-crypto-capi`` shared object, loads it, and declares the exact
``argtypes``/``restype`` of every ``gc_*`` entry point so the rest of the
package can call them like plain Python functions.

Loading order (first hit wins):

1. ``$GRANT_CRYPTO_CAPI_LIB`` — an explicit path, either the library file
   itself or a directory containing it.
2. the Cargo dev-tree output, ``../../target/{debug,release}/`` relative to the
   package (i.e. the workspace ``ops/grant-crypto/sdk/target/``).
3. the platform library name alongside this package (a wheel-bundled native).

Key material never crosses this boundary as a managed object — only opaque
``u64`` handles and wire bytes (bundle / envelope / receipt JSON, plaintext).
"""

from __future__ import annotations

import os
import sys
from ctypes import (
    CDLL,
    string_at,
    POINTER,
    byref,  # re-exported for callers' convenience
    c_bool,
    c_char_p,
    c_int32,
    c_size_t,
    c_uint32,
    c_uint64,
    c_void_p,
)
from pathlib import Path

__all__ = ["lib", "byref", "error_message"]


def _lib_filename() -> str:
    if sys.platform == "darwin":
        return "libgrant_crypto_capi.dylib"
    if sys.platform in ("win32", "cygwin"):
        return "grant_crypto_capi.dll"
    return "libgrant_crypto_capi.so"


def _candidates(name: str):
    """Yield candidate library paths in priority order."""
    env = os.environ.get("GRANT_CRYPTO_CAPI_LIB")
    if env:
        yield Path(env)
    here = Path(__file__).resolve().parent  # .../bindings/python/grant_crypto
    pkg_parent = here.parent  # .../bindings/python
    for profile in ("debug", "release"):
        # ../../target/<profile>/ == ops/grant-crypto/sdk/target/<profile>/
        yield pkg_parent / ".." / ".." / "target" / profile / name
    yield here / name  # wheel-bundled native, next to the package


def _load() -> CDLL:
    name = _lib_filename()
    tried = []
    for cand in _candidates(name):
        path = cand / name if cand.is_dir() else cand
        tried.append(str(path))
        if path.exists():
            return CDLL(str(path))
    raise RuntimeError(
        "could not locate the grant-crypto C ABI library ({name}).\n"
        "Set GRANT_CRYPTO_CAPI_LIB to the library (or its directory), or build "
        "it from the SDK workspace with:\n"
        "    cargo build -p grant-crypto-capi\n"
        "Searched:\n  {paths}".format(name=name, paths="\n  ".join(tried))
    )


lib = _load()


def _bind() -> None:
    """Declare the ABI signatures. Byte buffers are passed as ``c_char_p`` (a raw
    pointer to the Python ``bytes`` buffer) with an explicit length argument, so
    embedded NUL bytes in wire data / plaintext are carried faithfully."""
    u64p = POINTER(c_uint64)

    lib.gc_abi_version.argtypes = []
    lib.gc_abi_version.restype = c_uint32

    lib.gc_error_message.argtypes = [c_int32]
    lib.gc_error_message.restype = c_char_p

    lib.gc_identity_new.argtypes = [c_char_p, c_char_p, u64p]
    lib.gc_identity_new.restype = c_int32
    lib.gc_identity_dispose.argtypes = [c_uint64]
    lib.gc_identity_dispose.restype = c_int32

    lib.gc_keystore_new.argtypes = [u64p]
    lib.gc_keystore_new.restype = c_int32
    lib.gc_keystore_dispose.argtypes = [c_uint64]
    lib.gc_keystore_dispose.restype = c_int32
    lib.gc_keystore_admit.argtypes = [c_uint64, c_uint64, c_char_p, c_size_t, POINTER(c_bool)]
    lib.gc_keystore_admit.restype = c_int32
    lib.gc_keystore_open.argtypes = [c_uint64, c_char_p, c_char_p, c_size_t, u64p]
    lib.gc_keystore_open.restype = c_int32

    lib.gc_streamkey_acquire.argtypes = [c_char_p, c_size_t, c_char_p, c_uint64, u64p]
    lib.gc_streamkey_acquire.restype = c_int32
    lib.gc_streamkey_set_cap.argtypes = [c_uint64, c_uint64]
    lib.gc_streamkey_set_cap.restype = c_int32
    lib.gc_streamkey_remaining.argtypes = [c_uint64, u64p]
    lib.gc_streamkey_remaining.restype = c_int32
    lib.gc_streamkey_dispose.argtypes = [c_uint64]
    lib.gc_streamkey_dispose.restype = c_int32
    lib.gc_streamkey_seal.argtypes = [c_uint64, c_char_p, c_char_p, c_size_t, u64p]
    lib.gc_streamkey_seal.restype = c_int32

    lib.gc_rotate.argtypes = [c_uint64, c_char_p, c_bool, c_char_p, c_size_t, u64p]
    lib.gc_rotate.restype = c_int32

    lib.gc_verify_bundle.argtypes = [c_char_p, c_size_t, c_char_p, c_size_t]
    lib.gc_verify_bundle.restype = c_int32

    lib.gc_buf_ptr.argtypes = [c_uint64]
    lib.gc_buf_ptr.restype = c_void_p
    lib.gc_buf_len.argtypes = [c_uint64]
    lib.gc_buf_len.restype = c_size_t
    lib.gc_buf_dispose.argtypes = [c_uint64]
    lib.gc_buf_dispose.restype = c_int32


_bind()


def abi_version() -> int:
    """The native ABI version (``gc_abi_version``). Core and bindings ship in
    lockstep from one tag; there is no cross-version ABI promise."""
    return int(lib.gc_abi_version())


def error_message(code: int) -> str:
    """The static, human-readable diagnostic for a status code
    (``gc_error_message``). Advisory only — never branch on this text."""
    raw = lib.gc_error_message(code)
    return raw.decode("utf-8", "replace") if raw else "unknown status"


def take_buf(handle: int) -> bytes:
    """Copy a ``gc_buf`` handle's bytes into an owned ``bytes`` and dispose the
    handle (which zeroizes the native copy). The returned ``bytes`` is the
    caller's to manage from here on."""
    ptr = lib.gc_buf_ptr(handle)
    length = lib.gc_buf_len(handle)
    try:
        if not ptr or length == 0:
            return b""
        return string_at(ptr, length)
    finally:
        lib.gc_buf_dispose(handle)
