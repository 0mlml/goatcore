package api

// GET /api/machines/grant/{custodian} — read a custodian's standing machine
// enrollment grant, including the ADR 1113 delegated network-admin entitlement.
//
// WHY THIS EXISTS. `POST /api/machines/grant` could already SET the entitlement
// and `registry.MachineGrantFor` could already READ it, but nothing exposed the
// read over HTTP — so the only way to answer "may this person administer bearer
// gear?" was to be the portal. ADR 1131 needs exactly that answer from outside:
// the Herd gear console gates every interactive session on Ory identity AND this
// entitlement, so that a person's console access and their entitlement are the
// same record and one revoke cuts both.
//
// FAIL-CLOSED CONTRACT FOR CALLERS. Two distinct negative answers, both meaning
// "denied", kept distinct because they are different situations to debug:
//   - 404 `no_grant`      — no grant row at all for this custodian.
//   - 200 with `network_admin: false` — a grant exists, the entitlement does not.
// A caller MUST treat any non-200, any transport error and any timeout as denied.
// The entitlement is an authorization fact; an unreachable portal is not a
// licence to proceed.
//
// `site` travels with the answer on purpose. The entitlement is bounded to a
// site (ADR 1113 §3 — a network admin administers THEIR site's gear, never
// another's), so an answer that omitted the scope would be an over-broad yes.

import (
	"errors"
	"net/http"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

func (s *Server) getMachineGrant(w http.ResponseWriter, r *http.Request) {
	custodian := r.PathValue("custodian")
	if custodian == "" {
		writeError(w, http.StatusBadRequest, "custodian_required",
			"custodian handle required in the path")
		return
	}

	if p := PrincipalFromContext(r.Context()); p == nil {
		writeError(w, http.StatusUnauthorized, "auth", "missing principal")
		return
	}

	g, err := s.reg.MachineGrantFor(r.Context(), custodian)
	if errors.Is(err, registry.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no_grant",
			"no machine grant for this custodian — no quota, no affiliation, and no network-admin entitlement")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_grant", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, g)
}
