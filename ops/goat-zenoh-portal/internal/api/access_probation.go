package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

// access_probation.go is the operator-gated HTTP surface for the anomaly
// probation list (ADR 1068). It is the COP's "hold, don't delete" disposition
// layer: hold an entity that doesn't reconcile, list what's held (with the
// cure_by countdown), and record a repair/purge verdict. The actual repair /
// purge runs through the existing onboard-intake / revoke paths — decide only
// records the outcome + who + when (non-load-bearing, no enforcement here). All
// three routes are requireOperator; every mutation writes an audit_events row
// via actorOf so the decisions are attributed.

type probationHoldRequest struct {
	Ref        string `json:"ref"`
	Kind       string `json:"kind"`
	Label      string `json:"label"`
	Reason     string `json:"reason"`
	DetectedAt int64  `json:"detected_at"`
	CureBy     int64  `json:"cure_by"`
	Note       string `json:"note"`
}

type probationDecideRequest struct {
	Disposition string `json:"disposition"` // repaired | purged
	Note        string `json:"note"`
}

// handleProbationList serves GET /api/access/probation — operator-gated. Lists
// open entries by default; ?all=true returns the full history.
func (s *Server) handleProbationList(w http.ResponseWriter, r *http.Request) {
	openOnly := r.URL.Query().Get("all") != "true"
	rows, err := s.reg.ListProbation(r.Context(), openOnly)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_error", "could not list probation")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"probation": rows})
}

// handleProbationHold serves POST /api/access/probation — operator-gated. Holds
// an entity: records it with the operator (held_by) + a cure_by deadline. A ref
// already on probation → 409 (no double-hold).
func (s *Server) handleProbationHold(w http.ResponseWriter, r *http.Request) {
	var req probationHoldRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeBodyError(w, err, "decode", "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Ref) == "" {
		writeError(w, http.StatusBadRequest, "missing_ref", "a `ref` (the entity to hold) is required")
		return
	}
	p := &registry.Probation{
		Ref: strings.TrimSpace(req.Ref), Kind: req.Kind, Label: req.Label,
		Reason: req.Reason, DetectedAt: req.DetectedAt, CureBy: req.CureBy,
		Note: req.Note, HeldBy: actorOf(r),
	}
	if err := s.reg.HoldProbation(r.Context(), p); err != nil {
		switch err {
		case registry.ErrProbationOpen:
			writeError(w, http.StatusConflict, "already_held",
				fmt.Sprintf("%q is already on probation", req.Ref))
		default:
			writeError(w, http.StatusBadRequest, "hold_error", err.Error())
		}
		return
	}
	_ = s.reg.LogAuditEvent(r.Context(), actorOf(r), "access.probation.hold", p.Ref,
		fmt.Sprintf(`{"id":%q,"kind":%q,"reason":%q,"cure_by":%d}`, p.ID, p.Kind, p.Reason, p.CureBy))
	writeJSON(w, http.StatusOK, p)
}

// handleProbationDecide serves POST /api/access/probation/{id}/decide —
// operator-gated. Records the terminal disposition (repaired | purged) + the
// operator + note, and returns the suggested follow-up command to actually
// effect it (the repair/purge itself is out-of-band, like the onboard queue).
func (s *Server) handleProbationDecide(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := s.reg.GetProbation(r.Context(), id)
	if err == registry.ErrNotFound {
		writeError(w, http.StatusNotFound, "not_found", "probation entry not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get_error", "could not load probation entry")
		return
	}
	if existing.Disposition != registry.ProbationOpen {
		writeError(w, http.StatusConflict, "already_decided",
			fmt.Sprintf("entry is already %q — cannot re-decide", existing.Disposition))
		return
	}
	var req probationDecideRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeBodyError(w, err, "decode", "invalid JSON body")
		return
	}
	disp := strings.TrimSpace(req.Disposition)
	if disp != registry.ProbationRepaired && disp != registry.ProbationPurged {
		writeError(w, http.StatusBadRequest, "bad_disposition",
			"disposition must be `repaired` or `purged`")
		return
	}
	if err := s.reg.DecideProbation(r.Context(), id, disp, actorOf(r), req.Note); err != nil {
		if err == registry.ErrNotFound {
			writeError(w, http.StatusConflict, "already_decided", "entry was decided concurrently")
			return
		}
		writeError(w, http.StatusInternalServerError, "decide_error", "could not record decision")
		return
	}
	_ = s.reg.LogAuditEvent(r.Context(), actorOf(r), "access.probation."+disp, existing.Ref,
		fmt.Sprintf(`{"id":%q,"note":%q}`, id, req.Note))
	writeJSON(w, http.StatusOK, map[string]any{
		"id":          id,
		"ref":         existing.Ref,
		"disposition": disp,
		// Guidance only — the actual repair/purge runs out-of-band through the
		// existing paths (the portal holds no repo/creds to run them itself).
		"next": probationNextCommand(disp, existing.Ref),
	})
}

// probationNextCommand returns the operator command that actually effects the
// recorded disposition. Best-effort — a stray has no manifest handle, so the
// ref is what the operator resolves against.
func probationNextCommand(disposition, ref string) string {
	switch disposition {
	case registry.ProbationRepaired:
		return "goat admin onboard <handle> [--env <goatnet>]   # regularize " + ref
	case registry.ProbationPurged:
		return "goat admin onboard --revoke " + ref + " --purge   # mesh-first cut + cleanse"
	default:
		return ""
	}
}
