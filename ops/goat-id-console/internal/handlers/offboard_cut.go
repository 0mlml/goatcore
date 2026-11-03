package handlers

import (
	"net/http"
	"net/url"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// handleOffboardCut is the Offboard button that does the REAL cut (goat#372):
// it queues the row as offboard-requested for the onboard-runner, which
// executes the reversible native revoke and writes the result back. No access
// changes in this handler — its whole job is an attributed, step-up-gated,
// double-click-safe queue entry. Contrast the "offboard" decide action, which
// asserts the cut already happened elsewhere (and prechecks that assertion).
func (s *Server) handleOffboardCut(w http.ResponseWriter, r *http.Request, id string, actor Actor) {
	var auditBefore map[string]interface{}
	if p0, err := s.Onboard.GetProposal(r.Context(), id); err == nil {
		auditBefore = map[string]interface{}{"status": p0.Status}
	}

	stepUpVerified := false
	if s.stepUpRequired("offboard_identity") {
		out := s.requireStepUp(w, r, actor, "offboard_identity")
		if out.Handled {
			s.emitAuditWithInviteToken(r, actor, "onboard_offboard_cut", id, "",
				auditBefore, nil, false, "refused", stepUpChallengeAudit)
			return
		}
		stepUpVerified = out.Verified
		actor.AAL = aalLabel(stepUpVerified)
	}

	redirect := func(code string) {
		q := url.Values{}
		q.Set("flash", code)
		http.Redirect(w, r, "/onboarding?"+q.Encode(), http.StatusSeeOther)
	}

	ok, err := s.Onboard.RequestOffboard(r.Context(), id, actor.Email)
	if err != nil {
		s.emitAuditWithInviteToken(r, actor, "onboard_offboard_cut", id, "",
			auditBefore, nil, stepUpVerified, "error", err.Error())
		redirect(flashOffboardCutFailed)
		return
	}
	if !ok {
		// Unknown id, a pre-provisioning row, or a cut already queued/running
		// (the two-admins double-click) — refused loudly, nothing moved.
		s.emitAuditWithInviteToken(r, actor, "onboard_offboard_cut", id, "",
			auditBefore, nil, stepUpVerified, "refused", flashOffboardCutState)
		redirect(flashOffboardCutState)
		return
	}
	s.emitAuditWithInviteToken(r, actor, "onboard_offboard_cut", id, "",
		auditBefore, map[string]interface{}{"status": onboardstore.ProposalOffboardRequested},
		stepUpVerified, "ok", "")
	redirect(flashOffboardCutQueued)
}
