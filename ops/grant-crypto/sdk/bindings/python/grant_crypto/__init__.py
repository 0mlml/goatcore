"""grant-crypto — endpoint forward-secrecy for the goat data fabric (ADR 1106).

A thin, idiomatic Python binding over the audited C ABI (``grant_crypto.h``).
The five nouns / six verbs of the frozen API (``docs/design/grant-crypto-sdk.md``)
rendered as Python classes; nothing added, removed, or reinterpreted.

Load-bearing invariants this binding preserves:

* **The platform holds no keys, and neither does Python.** Key material never
  enters Python as a managed object — identities, keystores, and stream keys are
  opaque native handles. Only wire bytes (bundle / envelope / receipt JSON) and
  plaintext cross the boundary.
* **Explicit disposal.** Every handle-holding object must be closed — use it as a
  context manager (``with``) or call ``.close()``. A finalizer (``__del__``) only
  *warns* about a leak; it never disposes (per the ABI's fail-closed lifecycle).
* **Fail closed.** Any use after ``.close()`` raises :class:`DisposedHandleError`;
  the capability-vs-reject error distinction is kept intact.

Quickstart::

    import grant_crypto as gc

    with gc.Identity("coalition-air/con", member_scalar) as ident, gc.KeyStore() as ks:
        gc.verify_bundle(bundle_json, key_service_spki_der)  # authenticate first
        if ks.admit(ident, bundle_json):                     # unwrap our slot
            plaintext = ks.open("data/coalition-air/cop", envelope_json)
"""

from __future__ import annotations

import json
import warnings

from ._errors import (
    BadSignatureError,
    BadUtf8Error,
    DecryptError,
    DisposedHandleError,
    GrantCryptoError,
    InternalError,
    MalformedError,
    NoSlotError,
    NullArgError,
    RotationRequiredError,
    UnsupportedSuiteError,
    UnwrapError,
    check,
)
from ctypes import c_bool, c_uint64

from ._ffi import abi_version, byref, error_message, lib, take_buf

__all__ = [
    "Identity",
    "KeyStore",
    "StreamKey",
    "verify_bundle",
    "abi_version",
    "error_message",
    # exception hierarchy
    "GrantCryptoError",
    "UnsupportedSuiteError",
    "DecryptError",
    "UnwrapError",
    "BadSignatureError",
    "NoSlotError",
    "MalformedError",
    "RotationRequiredError",
    "DisposedHandleError",
    "NullArgError",
    "BadUtf8Error",
    "InternalError",
]

_SCALAR_LEN = 32


def _as_bytes(value, name: str) -> bytes:
    """Coerce a bytes-like argument to ``bytes`` (the FFI needs a contiguous
    buffer). ``str`` is rejected — wire data and key material are bytes."""
    if isinstance(value, bytes):
        return value
    if isinstance(value, (bytearray, memoryview)):
        return bytes(value)
    raise TypeError(f"{name} must be bytes-like, not {type(value).__name__}")


def _cstr(value: str, name: str) -> bytes:
    """Encode a string argument to UTF-8 for the FFI, rejecting an embedded NUL. The C
    ABI reads these as NUL-terminated (``c_char_p``), so an interior NUL would silently
    truncate the argument — reject it client-side as :class:`MalformedError` instead."""
    encoded = value.encode("utf-8")
    if b"\x00" in encoded:
        raise MalformedError(f"{name} must not contain an embedded NUL byte", -6)
    return encoded


def _scalar(value) -> bytes:
    """Validate a raw 32-byte scalar. The FFI reads exactly 32 bytes from the
    pointer, so a wrong length would be an out-of-bounds read — guard it here.
    A 32-byte value that is not a valid P-256 key is reported by the core as
    :class:`MalformedError` at construction."""
    b = _as_bytes(value, "scalar")
    if len(b) != _SCALAR_LEN:
        raise MalformedError(
            f"scalar must be exactly {_SCALAR_LEN} bytes, got {len(b)}", -6
        )
    return b


class _Managed:
    """Base for the opaque-handle objects: context-manager close, fail-closed
    use-after-close, and a finalizer that warns (never disposes)."""

    # Each subclass sets the FFI dispose function for its handle kind.
    _dispose_fn = None

    def __init__(self, handle: int):
        self._h = handle

    def _live(self) -> int:
        if self._h is None:
            raise DisposedHandleError(f"{type(self).__name__} is closed")
        return self._h

    def close(self) -> None:
        """Dispose the native handle (zeroizing its secret material). Idempotent:
        a second close is a harmless no-op."""
        if self._h is not None:
            type(self)._dispose_fn(self._h)
            self._h = None

    def __enter__(self):
        return self

    def __exit__(self, *_exc):
        self.close()
        return False

    def __del__(self):
        self._warn_if_unclosed()

    def _warn_if_unclosed(self):
        # Per the ABI contract a binding finalizer may report a leak but must
        # NEVER dispose — explicit close is the only sanctioned release path.
        if getattr(self, "_h", None) is not None:
            try:
                warnings.warn(
                    f"{type(self).__name__} was garbage-collected without close(); "
                    "its native handle leaked (the finalizer does not dispose). "
                    "Use `with` or call .close().",
                    ResourceWarning,
                    stacklevel=2,
                )
            except Exception:
                pass  # interpreter shutdown: warnings machinery may be gone


class Identity(_Managed):
    """A member's id + private key material (suite-typed), unwrap-capable.

    Constructed from the member's enrollment credential: its id and raw 32-byte
    P-256 scalar. The scalar is handed straight to the native layer and never
    retained in Python.
    """

    _dispose_fn = staticmethod(lib.gc_identity_dispose)

    def __init__(self, member_id: str, scalar32):
        scalar = _scalar(scalar32)
        out = c_uint64()
        check(
            lib.gc_identity_new(_cstr(member_id, "member_id"), scalar, byref(out)),
            "identity",
        )
        super().__init__(out.value)

    def rotate(self, new_scalar32, keep_access: bool = False, bundles_json=b"[]") -> dict:
        """Rotate this identity's leaf key to ``new_scalar32`` (raw 32-byte P-256
        scalar), returning the parsed rotation receipt.

        On success the identity **now holds the new key** (in-place swap; the old
        scalar is zeroized). ``keep_access=False`` (the ``Destroy`` default) drops
        the old key and re-wraps nothing — full forward secrecy. ``keep_access=True``
        first re-wraps this member's still-live DEKs from ``bundles_json`` (a JSON
        array of bundles) to the new key locally, **reducing forward secrecy** for
        those generations; the re-wrapped entries ride under the receipt's
        ``"rewrapped"`` key.

        The receipt is a structured, self-dating fact (retired-key generations now
        unreadable, count re-wrapped, RFC3339 UTC timestamp) — never time-relative
        prose.
        """
        h = self._live()
        new_scalar = _scalar(new_scalar32)
        bundles = _as_bytes(bundles_json, "bundles_json")
        out = c_uint64()
        check(
            lib.gc_rotate(h, new_scalar, c_bool(keep_access), bundles, len(bundles), byref(out)),
            "rotate",
        )
        return json.loads(take_buf(out.value))


class KeyStore(_Managed):
    """A consumer's held keys: the DEKs unwrapped from admitted bundles, indexed
    by ``(release-set, generation)``. Open data messages with the held DEK."""

    _dispose_fn = staticmethod(lib.gc_keystore_dispose)

    def __init__(self):
        out = c_uint64()
        check(lib.gc_keystore_new(byref(out)), "keystore")
        super().__init__(out.value)

    def admit(self, identity: "Identity", bundle_json) -> bool:
        """Admit a (already-verified) bundle for ``identity``: unwrap this member's
        slot and retain the DEK for the bundle's ``(rs, gen)``.

        Returns ``True`` if the member had a slot (key retained) or ``False`` if it
        had none (an outsider for this generation — nothing retained, a normal
        fail-closed outcome, not an error). Raises on a malformed slot / bad unwrap.
        Verify the bundle first with :func:`verify_bundle`.
        """
        ks = self._live()
        idh = identity._live()
        data = _as_bytes(bundle_json, "bundle_json")
        had_slot = c_bool(False)
        check(
            lib.gc_keystore_admit(ks, idh, data, len(data), byref(had_slot)),
            "admit",
        )
        return had_slot.value

    def open(self, keyexpr: str, envelope_json) -> bytes:
        """Open a data message received on ``keyexpr`` (its in-band JSON body) with
        the held DEK for the message's ``(rs, gen)``. Raises :class:`NoSlotError`
        if none is held; the AEAD binds ``keyexpr`` so a cross-topic replay fails.
        """
        ks = self._live()
        data = _as_bytes(envelope_json, "envelope_json")
        out = c_uint64()
        check(
            lib.gc_keystore_open(ks, _cstr(keyexpr, "keyexpr"), data, len(data), byref(out)),
            "open",
        )
        return take_buf(out.value)


class StreamKey(_Managed):
    """A producer's seal-capable stream key: the DEK unwrapped from the producer's
    own bundle slot for the highest visible generation of a release-set.

    Construct with :meth:`acquire`. Enforces the message-count cap: at the cap,
    :meth:`seal` refuses with :class:`RotationRequiredError`.
    """

    _dispose_fn = staticmethod(lib.gc_streamkey_dispose)

    @classmethod
    def acquire(cls, bundles_json, rs: str, identity: "Identity") -> "StreamKey":
        """Acquire the producer's stream key from a JSON array of bundles: pick the
        highest generation of ``rs`` in which ``identity`` has a slot and unwrap it.
        Raises :class:`NoSlotError` if the producer isn't in any visible audience.
        """
        idh = identity._live()
        data = _as_bytes(bundles_json, "bundles_json")
        out = c_uint64()
        check(
            lib.gc_streamkey_acquire(data, len(data), _cstr(rs, "rs"), idh, byref(out)),
            "acquire",
        )
        return cls(out.value)

    def seal(self, keyexpr: str, plaintext) -> bytes:
        """Seal ``plaintext`` for publication on ``keyexpr`` under this key,
        returning the serialized ``Envelope`` (the in-band wire body). ``keyexpr``
        is bound as AEAD additional data. Raises :class:`RotationRequiredError`
        once :attr:`remaining` reaches zero."""
        sk = self._live()
        data = _as_bytes(plaintext, "plaintext")
        out = c_uint64()
        check(
            lib.gc_streamkey_seal(sk, _cstr(keyexpr, "keyexpr"), data, len(data), byref(out)),
            "seal",
        )
        return take_buf(out.value)

    @property
    def remaining(self) -> int:
        """Messages that may still be sealed under this key before rotation is
        required — the structured approach-to-cap fact, surfaced well before
        refusal."""
        sk = self._live()
        out = c_uint64()
        check(lib.gc_streamkey_remaining(sk, byref(out)), "remaining")
        return out.value

    def set_cap(self, n: int) -> None:
        """Tighten the seal cap to ``n`` (a caller may tighten, never loosen). A
        request above the default cap raises :class:`MalformedError`."""
        sk = self._live()
        check(lib.gc_streamkey_set_cap(sk, n), "set_cap")


def verify_bundle(bundle_json, vk_spki_der) -> None:
    """Authenticate a bundle against the key-service verifying key (SPKI DER).
    Returns ``None`` on success; raises :class:`BadSignatureError` or
    :class:`MalformedError` otherwise. Consumers MUST verify before admit."""
    bundle = _as_bytes(bundle_json, "bundle_json")
    vk = _as_bytes(vk_spki_der, "vk_spki_der")
    check(
        lib.gc_verify_bundle(bundle, len(bundle), vk, len(vk)),
        "verify_bundle",
    )
