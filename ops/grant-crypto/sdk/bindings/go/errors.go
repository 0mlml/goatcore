package grantcrypto

/*
#include <grant_crypto.h>
*/
import "C"

import (
	"errors"
	"fmt"
)

// The frozen error taxonomy, one exported sentinel per C ABI status code, mapped
// 1:1 — no variant is collapsed or reinterpreted (grant-crypto-sdk.md §"Error
// taxonomy"). Compare with errors.Is.
//
// The seven core variants carry the capability-vs-reject distinction the design
// mandates: ErrUnsupportedSuite is a pre-key capability signal, ErrRotationRequired
// is the write-side seal-cap refusal, and the rest are read-side rejects that are
// advisory only (never a decrypt oracle). The ABI-layer variants below them report
// caller/lifecycle misuse, not a cryptographic outcome.
var (
	// ErrUnsupportedSuite — the named suite is not implemented (capability signal,
	// raised before any key operation). Distinct from a reject: it says "this build
	// cannot speak this suite", not "this message failed".
	ErrUnsupportedSuite = errors.New("grantcrypto: unsupported suite")
	// ErrDecrypt — AEAD authentication failed (wrong key/gen/rs/topic, or tamper). A
	// reject, never an oracle.
	ErrDecrypt = errors.New("grantcrypto: decrypt failed")
	// ErrUnwrap — the wrapped DEK could not be recovered (e.g. a rotated key can no
	// longer open a slot wrapped to the retired one).
	ErrUnwrap = errors.New("grantcrypto: unwrap failed")
	// ErrBadSignature — the bundle signature did not verify against the supplied
	// key-service verifying key.
	ErrBadSignature = errors.New("grantcrypto: bad signature")
	// ErrNoSlot — the member has no slot / no held key for this (rs, gen): not in the
	// audience. A normal outcome, not an anomaly.
	ErrNoSlot = errors.New("grantcrypto: no slot")
	// ErrMalformed — a field was malformed (bad base64/length/key encoding, bad JSON),
	// or a cap request tried to loosen past the default (tighten-only).
	ErrMalformed = errors.New("grantcrypto: malformed input")
	// ErrRotationRequired — the stream key hit its message-count cap and refuses to
	// seal until rotation (the enforced nonce-safety cap). A write-side reject,
	// distinct from ErrDecrypt; it changes nothing on the wire.
	ErrRotationRequired = errors.New("grantcrypto: rotation required (seal cap reached)")

	// ErrNullArg — a required argument was nil (an out-param, a string, or a byte
	// buffer the ABI requires).
	ErrNullArg = errors.New("grantcrypto: null argument")
	// ErrDisposed — a handle was already closed, never issued, or of the wrong type.
	// Returned by any use-after-Close and by a double Close (both harmless).
	ErrDisposed = errors.New("grantcrypto: disposed or invalid handle")
	// ErrBadUTF8 — a string argument (member id, key-expression, release set) was not
	// valid UTF-8.
	ErrBadUTF8 = errors.New("grantcrypto: invalid utf-8")
	// ErrInternal — an internal invariant failed inside the core (should not occur;
	// report it).
	ErrInternal = errors.New("grantcrypto: internal error")
	// ErrBadScalar — a private scalar was not exactly 32 bytes (checked in Go before it
	// reaches the ABI). Wraps ErrMalformed so callers matching on the crypto taxonomy
	// still catch it.
	ErrBadScalar = fmt.Errorf("grantcrypto: scalar must be 32 bytes: %w", ErrMalformed)
	// ErrEmbeddedNUL — a string argument (member id, key-expression, or release set)
	// contained a NUL byte. The C ABI reads these as NUL-terminated, so an interior NUL
	// would silently truncate the argument; the binding rejects it client-side instead.
	// Wraps ErrMalformed (the same class the other bindings raise for this).
	ErrEmbeddedNUL = fmt.Errorf("grantcrypto: string argument contains an embedded NUL: %w", ErrMalformed)
	// ErrBufferTooLarge — a payload buffer returned by the core exceeded the addressable
	// range of a C int (2 GiB) and cannot be copied out without truncation. Not expected
	// in practice; wraps ErrInternal.
	ErrBufferTooLarge = fmt.Errorf("grantcrypto: output buffer exceeds 2 GiB: %w", ErrInternal)
)

// errFromCode maps a C ABI status code to its sentinel error (nil for GC_OK). The
// mapping is total and 1:1; an unrecognized code is reported as ErrInternal rather
// than silently swallowed.
func errFromCode(code C.int32_t) error {
	switch int32(code) {
	case C.GC_OK:
		return nil
	case C.GC_ERR_UNSUPPORTED_SUITE:
		return ErrUnsupportedSuite
	case C.GC_ERR_DECRYPT:
		return ErrDecrypt
	case C.GC_ERR_UNWRAP:
		return ErrUnwrap
	case C.GC_ERR_BAD_SIGNATURE:
		return ErrBadSignature
	case C.GC_ERR_NO_SLOT:
		return ErrNoSlot
	case C.GC_ERR_MALFORMED:
		return ErrMalformed
	case C.GC_ERR_ROTATION_REQUIRED:
		return ErrRotationRequired
	case C.GC_ERR_NULL_ARG:
		return ErrNullArg
	case C.GC_ERR_DISPOSED_HANDLE:
		return ErrDisposed
	case C.GC_ERR_BAD_UTF8:
		return ErrBadUTF8
	case C.GC_ERR_INTERNAL:
		return ErrInternal
	default:
		return ErrInternal
	}
}
