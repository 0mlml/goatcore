package registry

import (
	"database/sql"
	"errors"
)

// Domain errors. Callers should errors.Is against these.
var (
	ErrNotFound          = errors.New("not found")
	ErrVendorExists      = errors.New("vendor already exists")
	ErrTopicExists       = errors.New("topic with this key_pattern already exists for this vendor")
	ErrSchemaExists      = errors.New("schema with this version already exists for this topic")
	ErrSpecExists        = errors.New("trashgoat spec with this slug already exists")
	ErrInvalidTransition = errors.New("invalid trashgoat spec lifecycle transition")
	ErrSpecImmutable     = errors.New("trashgoat spec is not in draft state; mutations refused")
	ErrSpecNotRetired    = errors.New("trashgoat spec must be retired before delete")
	ErrInviteExpired     = errors.New("onboarding invite has expired")
	ErrInviteConsumed    = errors.New("onboarding invite is no longer open (already submitted, revoked, or expired)")
	// ErrAmbiguous flags a lookup that must resolve to exactly one identity
	// but matched several (e.g. two sponsorship annotations bound to the
	// same login email). Fail closed — an operator must fix the annotations.
	ErrAmbiguous = errors.New("lookup matched more than one identity")
	// ErrAffiliationVariant flags a grant affiliation that differs from a
	// unit already on file only by case, spacing, or separator style —
	// accepting it would fork the registry into two spellings of the same
	// unit. The wrapped message names the existing spelling.
	ErrAffiliationVariant = errors.New("affiliation is a spelling variant of an existing unit")
)

// ErrInvalid marks an error the caller's input caused, as opposed to a
// store fault: errors.Is(err, ErrInvalid) is how a handler decides between a
// 400 that quotes the message and a 5xx that does not. The message itself is
// unchanged.
var ErrInvalid = errors.New("invalid input")

type invalidError struct{ err error }

func (e invalidError) Error() string        { return e.err.Error() }
func (e invalidError) Unwrap() error        { return e.err }
func (e invalidError) Is(target error) bool { return target == ErrInvalid }

// invalid wraps a validation failure so it answers to ErrInvalid.
func invalid(err error) error {
	if err == nil {
		return nil
	}
	return invalidError{err: err}
}

// sqlErrNoRows returns the sentinel for "no rows" so callers can compare
// portably even if we swap drivers later.
func sqlErrNoRows() error { return sql.ErrNoRows }
