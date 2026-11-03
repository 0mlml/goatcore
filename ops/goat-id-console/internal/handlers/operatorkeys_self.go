package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// The self-service binding for the operator-key machine door.
//
// A participant's portal key resolves to an org anchor, not a person. The
// person behind it is known only through the login the portal recorded on that
// identity (ADR 1072) and the onboarding record filed under it — the same two
// facts the browser page reads from the session. Binding through them keeps
// one rule for both doors: the handle a key is issued to is never the caller's
// word, and a token that cannot be tied to an onboarded person is refused
// rather than guessed at.

// errUnboundPrincipal: the caller's token does not resolve to an onboarded
// person, so the console cannot say whose key a request or collection is for.
var errUnboundPrincipal = errors.New("this token does not resolve to an onboarded person")

// operatorKeyHolderTier is the one level that holds an operator key
// (docs/design/platform-admin-lifecycle.md § Operator key, ADR 1182). A
// participant principal's sponsorship cap follows their level, so it is the
// tier the machine door reads. The browser door does not enforce this yet;
// the new door starts under the rule rather than widening the gap.
const operatorKeyHolderTier = "platform-admin"

// operatorKeyNotPlatformAdminMessage is the refusal for a level that holds no
// operator key. %s is the recorded level. It names the credential that already
// covers what most such requests were for, and the path to a real promotion.
const operatorKeyNotPlatformAdminMessage = "operator keys are held by platform admins only (ADR 1182); " +
	"this identity's recorded level is %s. Your slot key already covers your own slot. If you were " +
	"promoted, an admin runs `goat admin platform-admin grant`, which records the level and requests " +
	"and approves the key for you."

// operatorKeyMissingReasonMessage: a request says what admin task needs it.
const operatorKeyMissingReasonMessage = "say what admin task needs it (--reason): a change to another " +
	"organisation, another person's slot, or someone else's machines. Reading the portal or your own " +
	"slot is covered by your slot key, and is not a reason."

// operatorKeyUnboundMessage is the refusal a person reads when the token binds
// to nobody. It names the door that binds from the sign-in instead.
const operatorKeyUnboundMessage = "this token does not resolve to an onboarded person, so the console " +
	"cannot record whose key this would be. Ask an admin to finish your onboarding, or request the key " +
	"from the console at /operator-key, which binds from your sign-in."

// operatorKeySelfHandle is the handle an operator-key request or collection is
// bound to for the CALLING principal.
//
// An operator principal binds to its own attributed handle. A
// participant principal binds through handleForLoginEmail on the login the
// portal bound to the identity, so the machine door and the browser page
// resolve a person by one resolver. Neither path reads a handle the caller
// supplied.
func (s *Server) operatorKeySelfHandle(ctx context.Context, p onboardPrincipal) (string, error) {
	if p.IsOperator {
		return strings.TrimSpace(p.Handle), nil
	}
	email := strings.TrimSpace(p.SponsorEmail)
	if email == "" {
		return "", fmt.Errorf("%w: no login is bound to this identity", errUnboundPrincipal)
	}
	handle, err := s.handleForLoginEmail(ctx, email)
	if err != nil {
		return "", fmt.Errorf("%w: %w", errUnboundPrincipal, err)
	}
	return handle, nil
}
