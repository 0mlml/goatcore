/**
 * Error taxonomy for the grant-crypto TS binding — a 1:1 rendering of the C ABI
 * status codes (see `capi/include/grant_crypto.h` and the frozen taxonomy in
 * `docs/design/grant-crypto-sdk.md` §"Error taxonomy"). Every non-zero status the
 * ABI can return maps to exactly one subclass of {@link GrantCryptoError}; nothing
 * is collapsed. The `.code` field carries the numeric C code verbatim.
 *
 * The **capability-vs-reject distinction** is preserved structurally:
 * {@link UnsupportedSuiteError} is a capability signal raised *before* any key
 * operation (an implementation that does not know a suite), whereas the reject
 * variants ({@link DecryptError}, {@link UnwrapError}, {@link BadSignatureError},
 * {@link NoSlotError}, {@link MalformedError}) are authenticated-failure outcomes and
 * are never a decrypt oracle. {@link RotationRequiredError} is the write-side local
 * refusal at the seal cap — distinct from a decrypt reject, and never observed on the
 * wire.
 */

/** The C ABI status codes (frozen; mirror `grant_crypto.h`). `0` is success. */
export const Code = {
  Ok: 0,
  UnsupportedSuite: -1,
  Decrypt: -2,
  Unwrap: -3,
  BadSignature: -4,
  NoSlot: -5,
  Malformed: -6,
  RotationRequired: -7,
  NullArg: -100,
  DisposedHandle: -101,
  BadUtf8: -102,
  Internal: -103,
} as const;

export type Code = (typeof Code)[keyof typeof Code];

/**
 * Base class for every error the binding raises from a C ABI status. `code` is the
 * numeric C code (always negative for a failure). Subclasses add nothing but a stable
 * name/identity so callers can branch with `instanceof`.
 */
export class GrantCryptoError extends Error {
  /** The numeric C ABI status code (e.g. `-5` for `GC_ERR_NO_SLOT`). */
  readonly code: number;

  constructor(code: number, message: string) {
    super(message);
    this.name = new.target.name;
    this.code = code;
    // Restore the prototype chain when compiled to older targets (belt-and-braces;
    // ES2022 target keeps it, but this makes `instanceof` robust regardless).
    Object.setPrototypeOf(this, new.target.prototype);
  }
}

/** `GC_ERR_UNSUPPORTED_SUITE` (-1): the suite is not implemented — a capability signal, pre-key. */
export class UnsupportedSuiteError extends GrantCryptoError {}
/** `GC_ERR_DECRYPT` (-2): AEAD authentication failed (wrong key/gen/rs/topic, tamper). Never an oracle. */
export class DecryptError extends GrantCryptoError {}
/** `GC_ERR_UNWRAP` (-3): the wrapped DEK could not be recovered. */
export class UnwrapError extends GrantCryptoError {}
/** `GC_ERR_BAD_SIGNATURE` (-4): the bundle signature did not verify. */
export class BadSignatureError extends GrantCryptoError {}
/** `GC_ERR_NO_SLOT` (-5): the member has no slot / no held key for this `(rs, gen)`. */
export class NoSlotError extends GrantCryptoError {}
/** `GC_ERR_MALFORMED` (-6): a field was malformed (bad base64/length/key encoding, bad JSON). */
export class MalformedError extends GrantCryptoError {}
/** `GC_ERR_ROTATION_REQUIRED` (-7): the stream key hit its message-count cap; rotate to continue. */
export class RotationRequiredError extends GrantCryptoError {}
/** `GC_ERR_NULL_ARG` (-100): a required pointer argument was NULL (a binding bug if it ever surfaces). */
export class NullArgError extends GrantCryptoError {}
/** `GC_ERR_DISPOSED_HANDLE` (-101): a handle was already disposed, never issued, or of the wrong type. */
export class DisposedHandleError extends GrantCryptoError {}
/** `GC_ERR_BAD_UTF8` (-102): a string argument was not valid UTF-8. */
export class BadUtf8Error extends GrantCryptoError {}
/** `GC_ERR_INTERNAL` (-103): an internal invariant failed inside the core. */
export class InternalError extends GrantCryptoError {}

/** `code` → subclass constructor. Any code not listed becomes a plain {@link GrantCryptoError}. */
const BY_CODE: ReadonlyMap<number, new (code: number, message: string) => GrantCryptoError> =
  new Map([
    [Code.UnsupportedSuite, UnsupportedSuiteError],
    [Code.Decrypt, DecryptError],
    [Code.Unwrap, UnwrapError],
    [Code.BadSignature, BadSignatureError],
    [Code.NoSlot, NoSlotError],
    [Code.Malformed, MalformedError],
    [Code.RotationRequired, RotationRequiredError],
    [Code.NullArg, NullArgError],
    [Code.DisposedHandle, DisposedHandleError],
    [Code.BadUtf8, BadUtf8Error],
    [Code.Internal, InternalError],
  ]);

/**
 * Build (but do not throw) the typed error for a non-zero status `code`. `message` is
 * the ABI's diagnostic string (`gc_error_message`), used verbatim — advisory only, the
 * caller must branch on `instanceof`/`.code`, never on the text.
 */
export function errorForCode(code: number, message: string): GrantCryptoError {
  const ctor = BY_CODE.get(code) ?? GrantCryptoError;
  return new ctor(code, message);
}
