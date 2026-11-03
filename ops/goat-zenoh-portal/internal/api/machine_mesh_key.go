package api

// Mesh-key re-mint for an already-enrolled machine (machine-mesh-alignment
// plan, decision D1).
//
// The mint's setup key is one-off with a TTL that is a JOIN WINDOW, and the
// documented field pattern is mint-then-hand-carry (the site-one cohort: keys to
// a USB stick, boxes joining days later). When the key expires before the box
// joins, the machine holds a fabric identity and consumed quota with no way
// onto the mesh — and without this endpoint the only recoveries were
// revoke-and-re-enroll (destroys the identity the mint just created, forces an
// OOB re-delivery to gear that is by hypothesis hard to reach) or a hand-minted
// key (the side door the provenance guard exists to close).
//
// POST /api/machines/{anchor}/mesh-key re-derives the groups from the
// machine's BORN-AS class and mints a fresh one-off key. Quota-neutral: the
// enrollment already consumed its unit; a join window is not a second machine.

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

type machineMeshKeyRequest struct {
	// Exercise scopes the key to an exercise's synthetic-site groups, same
	// contract as the enroll-time field. Empty = the goatnet site scope.
	Exercise string `json:"exercise,omitempty"`
}

type machineMeshKeyResponse struct {
	SlotID       string   `json:"slot_id"`
	SystemHandle string   `json:"system_handle"`
	NodeClass    string   `json:"node_class"`
	MeshGroups   []string `json:"mesh_groups"`
	MeshSetupKey string   `json:"mesh_setup_key,omitempty"`
	MeshNote     string   `json:"mesh_note"`
	MeshScope    string   `json:"mesh_scope,omitempty"`
}

// machineMeshKey re-mints the mesh setup key for one enrolled machine. Gate
// mirrors the custody read model: an operator for any machine, a vendor-scoped
// caller only for machines under its own custody edge.
func (s *Server) machineMeshKey(w http.ResponseWriter, r *http.Request) {
	anchor := r.PathValue("anchor")
	p := PrincipalFromContext(r.Context())
	if p == nil {
		writeError(w, http.StatusUnauthorized, "auth", "missing principal")
		return
	}

	// An empty (or whitespace-only) body means "defaults"; the decoder
	// reports that as io.EOF. Anything else goes through the shared reader
	// so an oversize body is a 413 problem document, not a silent truncation
	// reported as bad_json. The request is a handful of fields; 64 KiB is
	// generous and stays the cap it was.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	var req machineMeshKeyRequest
	if err := decodeJSON(w, r, &req); err != nil && !errors.Is(err, io.EOF) {
		writeBodyError(w, err, "bad_json", err.Error())
		return
	}
	exercisePrefix, mf := exercisePrefixFor(s.cfg.MeshExercisePrefixes, req.Exercise)
	if mf != nil {
		s.writeMintFailure(w, mf)
		return
	}

	m, err := s.reg.MachineByAnchor(r.Context(), anchor)
	if errors.Is(err, registry.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_a_machine",
			"no live custody-enrolled machine at this anchor — the re-mint is for machines the mint already enrolled; a person's slot or a free slot has no derived mesh reach")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_machine", err.Error())
		return
	}
	custodian := derefStr(m.EnrolledBy)
	if !p.Operator && (p.Vendor == "" || p.Vendor != custodian) {
		writeError(w, http.StatusForbidden, "custodian_mismatch",
			"a non-operator may re-mint a mesh key only for machines under its own custody edge")
		return
	}

	nodeClass := derefStr(m.NodeClass)
	meshGroups, derr := registry.MeshGroupsForMachine(nodeClass, splitWorkloads(m.Workloads))
	if derr != nil {
		// A legacy born-as class (whole-system SENSOR, PROCESSOR) has no
		// derivation — deliberate: re-stamping the machine with a gear class is
		// the fix, not guessing here.
		writeError(w, http.StatusConflict, "no_mesh_mapping",
			fmt.Sprintf("this machine's born-as node-class %q has no mesh derivation: %v. Re-stamp it with a gear class (PUT /api/slots/{anchor}/custody) and re-mint.", nodeClass, derr))
		return
	}

	handle := derefStr(m.AssignedTo)
	names, setupKey, note, mf := s.mintMeshKey(r.Context(), "machine-"+handle, nodeClass, exercisePrefix, meshGroups)
	if mf != nil {
		s.writeMintFailure(w, mf)
		return
	}

	_ = s.reg.LogAuditEvent(r.Context(), actorOf(r), "machine-mesh-key", handle,
		fmt.Sprintf(`{"anchor":%q,"custodian":%q,"node_class":%q,"mesh_groups":%q,"mesh_key_minted":%t,"mesh_scope":%q}`,
			anchor, custodian, nodeClass, strings.Join(names, ","), setupKey != "", req.Exercise))

	writeJSON(w, http.StatusOK, &machineMeshKeyResponse{
		SlotID:       anchor,
		SystemHandle: handle,
		NodeClass:    nodeClass,
		MeshGroups:   names,
		MeshSetupKey: setupKey,
		MeshNote:     note,
		MeshScope:    req.Exercise,
	})
}
