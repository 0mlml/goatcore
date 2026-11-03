package api

// Single-machine registry read with the mesh derivation attached
// (machine-mesh-alignment plan, decision D6a).
//
// GET /api/machines/{anchor} is the read `bundle-create --from-machine`
// consumes: a gear-class offline-enrollment bundle must be issued FROM the
// registry, so the custody edge + born-as node-class exist at issuance and the
// bundle's acl_groups are DERIVED from the class — never hand-typed, which is
// a shape that has put unrelated gear in one group. It returns the machineListItem
// fields plus the FULL derived group names, without minting anything.
//
// Unlike the re-mint (machine_mesh_key.go), a born-as class with no derivation
// is NOT a refusal here — this is a read, and the honest answer is
// mesh_derivable:false with the fix named, at 200.

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

type machineGetResponse struct {
	machineListItem

	// MeshGroups are the FULL NetBird group names derived from the machine's
	// born-as node-class (ADR 1116 §9), under the goatnet site prefix or the
	// requested ?exercise= scope. Present only when MeshDerivable.
	MeshGroups []string `json:"mesh_groups,omitempty"`
	// MeshDerivable reports whether the born-as class has a mesh derivation.
	// False for legacy whole-system classes (SENSOR, PROCESSOR); MeshNote then
	// names the re-stamp fix. A false here is information, not an error — the
	// consumers that must refuse (bundle-create, the mint) do so themselves.
	MeshDerivable bool   `json:"mesh_derivable"`
	MeshNote      string `json:"mesh_note,omitempty"`
	// MeshScope echoes the ?exercise= the derivation was scoped to; empty for
	// the goatnet site scope.
	MeshScope string `json:"mesh_scope,omitempty"`
}

// getMachine serves `GET /api/machines/{anchor}[?exercise=<name>]`. Gate
// mirrors the custody read model (machineMeshKey): an operator reads any
// machine, a vendor-scoped caller only machines under its own custody edge.
func (s *Server) getMachine(w http.ResponseWriter, r *http.Request) {
	anchor := r.PathValue("anchor")
	p := PrincipalFromContext(r.Context())
	if p == nil {
		writeError(w, http.StatusUnauthorized, "auth", "missing principal")
		return
	}

	exercise := r.URL.Query().Get("exercise")
	exercisePrefix, mf := exercisePrefixFor(s.cfg.MeshExercisePrefixes, exercise)
	if mf != nil {
		s.writeMintFailure(w, mf)
		return
	}

	m, err := s.reg.MachineByAnchor(r.Context(), anchor)
	if errors.Is(err, registry.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_a_machine",
			"no live custody-enrolled machine at this anchor — a person's slot or a free slot has no derived mesh reach")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_machine", err.Error())
		return
	}
	custodian := derefStr(m.EnrolledBy)
	if !p.Operator && (p.Vendor == "" || p.Vendor != custodian) {
		writeError(w, http.StatusForbidden, "custodian_mismatch",
			"a non-operator may read only machines under its own custody edge")
		return
	}

	nodeClass := derefStr(m.NodeClass)
	workloads := splitWorkloads(m.Workloads)
	resp := &machineGetResponse{
		machineListItem: machineListItem{
			SlotID:       m.SlotID,
			SystemHandle: derefStr(m.AssignedTo),
			Custodian:    custodian,
			NodeClass:    nodeClass,
			Autonomy:     derefStr(m.Autonomy),
			Affiliation:  derefStr(m.Affiliation),
			Workloads:    workloads,
			EnrolledAt:   derefInt64(m.EnrolledAt),
		},
		MeshScope: exercise,
	}

	meshGroups, derr := registry.MeshGroupsForMachine(nodeClass, workloads)
	if derr != nil {
		// Same fix-naming text as the re-mint's 409, but at 200 — a read
		// reports, it does not refuse.
		resp.MeshDerivable = false
		resp.MeshNote = fmt.Sprintf("this machine's born-as node-class %q has no mesh derivation: %v. Re-stamp it with a gear class (PUT /api/slots/{anchor}/custody) and re-mint.", nodeClass, derr)
	} else {
		names := make([]string, len(meshGroups))
		for i, g := range meshGroups {
			names[i] = s.nbKeys.GroupNameScoped(exercisePrefix, g)
		}
		resp.MeshGroups = names
		resp.MeshDerivable = true
	}

	writeJSON(w, http.StatusOK, resp)
}
