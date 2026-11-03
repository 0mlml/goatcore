"""The exception hierarchy, mapping the C ABI status codes 1:1.

Every negative status code in ``grant_crypto.h`` becomes exactly one exception
class; ``GC_OK`` (0) is the absence of one. The taxonomy is frozen with the wire
format (see ``docs/design/grant-crypto-sdk.md`` §"Error taxonomy") — no class is
collapsed into another, and the **capability-vs-reject** distinction is
preserved: ``UnsupportedSuiteError`` is a capability signal raised *before* any
key operation, while every other reject is advisory and never a decrypt oracle.
"""

from __future__ import annotations

from ._ffi import error_message


class GrantCryptoError(Exception):
    """Base class for every grant-crypto failure. The ``code`` attribute carries
    the raw C status code (a negative ``int32`` from ``grant_crypto.h``)."""

    code: int = 0

    def __init__(self, message: str, code: int = 0):
        super().__init__(message)
        self.code = code


# ---- the frozen reject / capability taxonomy (GC_ERR_* -1 .. -7) ----


class UnsupportedSuiteError(GrantCryptoError):
    """``GC_ERR_UNSUPPORTED_SUITE`` (-1). The named suite is not implemented — a
    capability signal raised before any key operation, not a reject of data."""


class DecryptError(GrantCryptoError):
    """``GC_ERR_DECRYPT`` (-2). AEAD authentication failed (wrong key / gen / rs /
    topic, or tampered ciphertext). Never a decrypt oracle — advisory only."""


class UnwrapError(GrantCryptoError):
    """``GC_ERR_UNWRAP`` (-3). The wrapped DEK could not be recovered (e.g. a slot
    wrapped to a key this identity no longer holds)."""


class BadSignatureError(GrantCryptoError):
    """``GC_ERR_BAD_SIGNATURE`` (-4). The bundle signature did not verify against
    the supplied key-service verifying key."""


class NoSlotError(GrantCryptoError):
    """``GC_ERR_NO_SLOT`` (-5). The member has no slot / no held key for this
    ``(rs, gen)`` — it is not in the audience. A normal fail-closed outcome."""


class MalformedError(GrantCryptoError, ValueError):
    """``GC_ERR_MALFORMED`` (-6). A field was malformed: bad base64/length/key
    encoding, bad JSON, or a seal-cap request above the default cap. Also a
    ``ValueError`` so pythonic ``except ValueError`` callers catch bad inputs
    (and cross-binding taxonomy holds: Go maps this to ErrMalformed, TS to
    MalformedError)."""


class RotationRequiredError(GrantCryptoError):
    """``GC_ERR_ROTATION_REQUIRED`` (-7). The stream key hit its message-count cap
    and refuses to seal until rotation (the enforced nonce-safety cap). A
    write-side local refusal — nothing on the wire, never a decrypt oracle."""


class DisposedHandleError(GrantCryptoError):
    """``GC_ERR_DISPOSED_HANDLE`` (-101). A handle was already disposed, never
    issued, or of the wrong type. Raised on any use-after-close."""


# ---- programmer-error codes: also ValueError-ish so ``except ValueError`` /
#      ``except RuntimeError`` catches the "shouldn't happen" cases too. ----


class NullArgError(GrantCryptoError, ValueError):
    """``GC_ERR_NULL_ARG`` (-100). A required pointer argument was NULL. The
    binding fills in every out-param, so this indicates a binding bug."""


class BadUtf8Error(GrantCryptoError, ValueError):
    """``GC_ERR_BAD_UTF8`` (-102). A string argument was not valid UTF-8."""


class InternalError(GrantCryptoError, RuntimeError):
    """``GC_ERR_INTERNAL`` (-103). An internal invariant failed (e.g.
    serialization of a value the core itself built)."""


_CODE_TO_EXC = {
    -1: UnsupportedSuiteError,
    -2: DecryptError,
    -3: UnwrapError,
    -4: BadSignatureError,
    -5: NoSlotError,
    -6: MalformedError,
    -7: RotationRequiredError,
    -100: NullArgError,
    -101: DisposedHandleError,
    -102: BadUtf8Error,
    -103: InternalError,
}


def check(code: int, context: str = "") -> None:
    """Raise the mapped exception if ``code`` is not ``GC_OK``."""
    if code == 0:
        return
    exc_cls = _CODE_TO_EXC.get(code, GrantCryptoError)
    detail = error_message(code)
    message = f"{detail} (code {code})"
    if context:
        message = f"{context}: {message}"
    raise exc_cls(message, code)
