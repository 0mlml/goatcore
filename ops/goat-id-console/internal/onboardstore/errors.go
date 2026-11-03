package onboardstore

import (
	"database/sql"
	"errors"
)

// Domain errors. Callers should errors.Is against these.
var (
	ErrNotFound       = errors.New("not found")
	ErrInviteExpired  = errors.New("onboarding invite has expired")
	ErrInviteConsumed = errors.New("onboarding invite is no longer open (already submitted, revoked, or expired)")
	// ErrProposalNotApproved is returned by ClaimProposal when the proposal
	// exists but is not in the approved state (already claimed/onboarded/etc).
	// Callers map it to HTTP 409.
	ErrProposalNotApproved = errors.New("proposal is not in approved state")
	// ErrProposalNotOffboardRequested is returned by ClaimOffboard when the
	// proposal exists but no cut is queued (never requested, already claimed,
	// or already terminal). Callers map it to HTTP 409.
	ErrReissueRunning               = errors.New("offboarding is waiting for a running key reissue or notification to finish")
	ErrProposalNotOffboardRequested = errors.New("proposal is not in offboard-requested state")
)

// sqlErrNoRows returns the sentinel for "no rows" so callers can compare
// portably even if we swap drivers later.
func sqlErrNoRows() error { return sql.ErrNoRows }
