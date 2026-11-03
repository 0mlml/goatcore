package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

type inviteRecipientRefusal struct {
	status  int
	code    string
	message string
}

// checkInviteRecipient runs before minting an addressed invite and before
// later delivery/export, since either the recipient or their state can change.
// An address-free invite remains available for an out-of-band handover.
func (s *Server) checkInviteRecipient(r *http.Request, email string) *inviteRecipientRefusal {
	if strings.TrimSpace(email) == "" {
		return nil
	}
	if s.Onboard == nil {
		return &inviteRecipientRefusal{http.StatusServiceUnavailable, "recipient_check_failed",
			"onboarding is not configured; an invite cannot be sent"}
	}
	p, found, err := s.Onboard.ActiveProposalForEmail(r.Context(), email)
	if err != nil {
		return &inviteRecipientRefusal{http.StatusServiceUnavailable, "recipient_check_failed",
			"could not check existing onboarding records; retry before sending an invite"}
	}
	if !found {
		return nil
	}
	recovery := "use the existing record for access recovery"
	if p.Status == onboardstore.ProposalArchived {
		recovery = "ask an admin to restore the existing archived record"
	}
	return &inviteRecipientRefusal{http.StatusConflict, "duplicate_email",
		fmt.Sprintf("an onboarding already exists (proposal %s, stage %s); %s", p.ID, p.Status, recovery)}
}
