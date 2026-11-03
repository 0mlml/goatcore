package api

// ADR 1096 stage 5 — the custody surface (read). `GET /api/machines` renders a
// custodian's fleet (or, for an operator, every enrolled machine): the
// person→system custody edge plus each machine's node-class/autonomy/affiliation
// born-as stamp. Read-only; the enrollment write path is machine_enroll.go.

import (
	"net/http"
	"strings"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

type machineListItem struct {
	SlotID       string   `json:"slot_id"`
	SystemHandle string   `json:"system_handle"`
	Custodian    string   `json:"custodian"`
	NodeClass    string   `json:"node_class"`
	Autonomy     string   `json:"autonomy"`
	Affiliation  string   `json:"affiliation"`
	Workloads    []string `json:"workloads,omitempty"` // ADR 1098: PLATFORM composition
	EnrolledAt   int64    `json:"enrolled_at"`
}

type machineListResponse struct {
	Machines []machineListItem `json:"machines"`
	// Quota is included only when the view is scoped to a single custodian
	// (a `?custodian=` filter or a vendor-scoped caller); omitted for the
	// operator fleet view where there is no single custodian.
	Quota *registry.MachineQuota `json:"quota,omitempty"`
}

// listMachines serves `GET /api/machines[?custodian=<handle>]`. Operator: the
// whole fleet, or a `?custodian=` slice. Vendor-scoped caller: only its OWN
// machines (any other `?custodian=` is refused), mirroring the enroll gate.
func (s *Server) listMachines(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFromContext(r.Context())
	if p == nil {
		writeError(w, http.StatusUnauthorized, "auth", "missing principal")
		return
	}
	custodian := r.URL.Query().Get("custodian")

	var (
		slots []registry.Slot
		err   error
	)
	if p.Operator {
		if custodian != "" {
			slots, err = s.reg.MachinesForCustodian(r.Context(), custodian)
		} else {
			slots, err = s.reg.AllEnrolledMachines(r.Context())
		}
	} else {
		if p.Vendor == "" {
			writeError(w, http.StatusForbidden, "no_custodian", "caller has no custodian identity")
			return
		}
		if custodian != "" && custodian != p.Vendor {
			writeError(w, http.StatusForbidden, "custodian_mismatch",
				"a non-operator may only list machines under its own custodian identity")
			return
		}
		custodian = p.Vendor
		slots, err = s.reg.MachinesForCustodian(r.Context(), custodian)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_machines", err.Error())
		return
	}

	resp := machineListResponse{Machines: make([]machineListItem, 0, len(slots))}
	for _, sl := range slots {
		resp.Machines = append(resp.Machines, machineListItem{
			SlotID:       sl.SlotID,
			SystemHandle: derefStr(sl.AssignedTo),
			Custodian:    derefStr(sl.EnrolledBy),
			NodeClass:    derefStr(sl.NodeClass),
			Autonomy:     derefStr(sl.Autonomy),
			Affiliation:  derefStr(sl.Affiliation),
			Workloads:    splitWorkloads(sl.Workloads),
			EnrolledAt:   derefInt64(sl.EnrolledAt),
		})
	}
	// A single-custodian view carries that custodian's quota so the caller sees
	// remaining headroom alongside the fleet.
	if custodian != "" {
		if q, qerr := s.reg.MachineQuotaFor(r.Context(), custodian); qerr == nil {
			resp.Quota = q
		}
	}
	writeJSON(w, http.StatusOK, &resp)
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// splitWorkloads turns the comma-joined stored composition into a slice; nil when
// empty so `workloads` is omitted for a simple system.
func splitWorkloads(p *string) []string {
	if p == nil || *p == "" {
		return nil
	}
	return strings.Split(*p, ",")
}
