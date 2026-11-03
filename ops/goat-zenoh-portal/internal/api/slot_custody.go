package api

// Retro-stamp the custody edge on an EXISTING slot (operator-only).
//
// WHY THIS EXISTS. ADR 1096's custody edge — who is answerable
// for a machine — is written by RecordMachineEnrollment, which only runs
// inside POST /api/machines/enroll. That handler ALLOCATES a slot and mints
// its certs: it is the birth of a machine, not a record-keeping call. So a
// slot created any other way (the operator slot-onboard path, a pre-ADR-1096
// enrollment, an import) has no other way to acquire a custodian afterwards,
// leaving a machine with no answerable person.
//
// The alternative was an UPDATE straight into the registry. Refused: custody
// is an ownership record, revocation targets ownership, and a write that
// leaves no audit row is exactly the kind of ownership claim nobody should
// trust later. This endpoint reuses RecordMachineEnrollment verbatim, so the
// grammar validation (node_class, autonomy, workloads), the custodian
// normalization to a single spelling, the affiliation spelling guard, and the
// quota ceiling all apply identically to a retro-stamp and a fresh enrollment — a backfilled machine
// counts against its custodian's quota exactly like a newly enrolled one,
// because it is exactly as real.
//
// What it deliberately does NOT do: allocate, mint, or issue anything. It
// records a relationship that already exists in the world and was never
// written down.

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

type putSlotCustodyRequest struct {
	// Custodian person handle (or their anchor — normalizeCustodian folds both
	// to one spelling, which is what the quota join reads).
	Custodian string `json:"custodian"`
	NodeClass string `json:"node_class"`
	Autonomy  string `json:"autonomy"`
	// Affiliation is inherited from the custodian's grant when omitted, the
	// same rule POST /api/machines/enroll applies — a machine carries its
	// custodian's unit rather than declaring its own.
	Affiliation string   `json:"affiliation,omitempty"`
	Workloads   []string `json:"workloads,omitempty"`
}

// handlePutSlotCustody records the ADR 1096 custody edge on a slot that
// already exists. Operator-gated and audited: this writes who is answerable
// for a machine.
func (s *Server) handlePutSlotCustody(w http.ResponseWriter, r *http.Request) {
	anchor := strings.TrimSpace(r.PathValue("anchor"))
	if anchor == "" {
		writeError(w, http.StatusBadRequest, "anchor", "slot anchor required")
		return
	}
	var req putSlotCustodyRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeBodyError(w, err, "decode", err.Error())
		return
	}
	custodian := strings.TrimSpace(req.Custodian)
	if custodian == "" {
		writeError(w, http.StatusBadRequest, "custodian",
			"custodian handle required — a custody edge with no person is the state this endpoint exists to fix")
		return
	}

	// Affiliation: inherit from the custodian's standing grant unless the
	// caller states one. Same precedence as a fresh enrollment.
	affiliation := strings.TrimSpace(req.Affiliation)
	if affiliation == "" {
		grant, err := s.reg.MachineGrantFor(r.Context(), custodian)
		if err != nil {
			writeError(w, http.StatusForbidden, "no_enrollment_grant",
				fmt.Sprintf("custodian %q holds no machine enrollment grant, so there is no affiliation to inherit; seed the grant (POST /api/machines/grant) or pass affiliation explicitly", custodian))
			return
		}
		if grant.Affiliation == "" {
			writeError(w, http.StatusConflict, "grant_missing_affiliation",
				"the custodian's grant carries no affiliation; re-seed the grant before stamping custody")
			return
		}
		affiliation = grant.Affiliation
	}

	if err := s.reg.RecordMachineEnrollment(r.Context(), anchor, custodian,
		req.NodeClass, req.Autonomy, affiliation, req.Workloads); err != nil {
		switch {
		case errors.Is(err, registry.ErrSlotNotEnrollable):
			writeError(w, http.StatusNotFound, "slot_not_enrollable",
				fmt.Sprintf("slot %q is unknown, unassigned or revoked", anchor))
		case errors.Is(err, registry.ErrQuotaExhausted):
			// Deliberately the same refusal a fresh enrollment gets: a
			// backfilled machine is as real as a new one and consumes the
			// same allowance. Raising the grant is the honest fix, not
			// exempting the backfill.
			writeError(w, http.StatusConflict, "quota_exhausted",
				fmt.Sprintf("custodian %q is at their machine-enrollment quota; raise the grant before stamping this slot", custodian))
		case errors.Is(err, registry.ErrInvalidNodeClass), errors.Is(err, registry.ErrInvalidAutonomy):
			writeError(w, http.StatusBadRequest, "invalid_grammar", err.Error())
		case errors.Is(err, registry.ErrAffiliationVariant):
			// Same refusal the grant path gives: a near-miss spelling of a
			// unit on file is the caller's error, and the message names the
			// spelling to send instead.
			writeError(w, http.StatusBadRequest, "affiliation_variant", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "record_custody", err.Error())
		}
		return
	}

	_ = s.reg.LogAuditEvent(r.Context(), actorOf(r), "slot.custody.set", anchor,
		fmt.Sprintf(`{"custodian":%q,"node_class":%q,"autonomy":%q,"affiliation":%q}`,
			custodian, req.NodeClass, req.Autonomy, affiliation))

	writeJSON(w, http.StatusOK, map[string]any{
		"anchor":      anchor,
		"custodian":   custodian,
		"node_class":  req.NodeClass,
		"autonomy":    req.Autonomy,
		"affiliation": affiliation,
	})
}
