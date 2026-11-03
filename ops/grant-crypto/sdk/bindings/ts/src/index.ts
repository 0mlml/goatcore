/**
 * grant-crypto — TypeScript/Node binding (ADR 1106).
 *
 * Endpoint forward-secrecy for the goat data fabric: the platform holds no keys; only
 * members decrypt, in-process; the producer is not a key authority. This package is a
 * thin, hand-written wrapper over the audited C ABI — the same library as the Go,
 * Python, and Rust surfaces, gated on the same conformance vectors.
 */

export {
  Identity,
  KeyStore,
  StreamKey,
  verifyBundle,
  abiVersion,
  type RotationReceipt,
  type ReWrappedDek,
} from "./grant-crypto";

export {
  Code,
  GrantCryptoError,
  UnsupportedSuiteError,
  DecryptError,
  UnwrapError,
  BadSignatureError,
  NoSlotError,
  MalformedError,
  RotationRequiredError,
  NullArgError,
  DisposedHandleError,
  BadUtf8Error,
  InternalError,
} from "./errors";
