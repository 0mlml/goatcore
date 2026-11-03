package api

import (
	"errors"
	"net/http"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

func (s *Server) repairMachineAffiliation(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFromContext(r.Context())
	if p == nil {
		writeError(w, http.StatusUnauthorized, "auth", "missing principal")
		return
	}
	if !p.Operator {
		writeError(w, http.StatusForbidden, "operator_only", "affiliation repair requires an operator token")
		return
	}
	var req registry.AffiliationRepairRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeBodyError(w, err, "bad_json", err.Error())
		return
	}
	result, err := s.reg.RepairAffiliations(r.Context(), req, p.Actor, actorOf(r))
	switch {
	case errors.Is(err, registry.ErrAffiliationRepairInvalid):
		writeError(w, http.StatusBadRequest, "invalid_affiliation_repair", err.Error())
	case errors.Is(err, registry.ErrAffiliationRepairStale):
		writeError(w, http.StatusConflict, "affiliation_repair_stale", err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, "affiliation_repair_failed", err.Error())
	default:
		writeJSON(w, http.StatusOK, result)
	}
}
