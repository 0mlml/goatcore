package api

// labels.go serves the ADR 1075 producer data-label surface: a producer (or an
// operator) stamps its namespace with the three closed lanes, and anyone can
// read the goatnet-wide restrictive-share drift counter. Labels attach at the
// producer-namespace grain (never per message); the authz-relevant lane is
// `access`, which aliases the ADR 1074 modes (community | org | controlled).

import (
	"fmt"
	"net/http"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

// labelResponse is a producer's stored labels plus the resolved effective
// values a surface should display: an unlabeled access lane resolves to the
// per-goatnet default, and an unlabeled fidelity lane bridges from the legacy
// synthetic flag to notional. The raw lanes are also returned so a surface can
// distinguish "explicitly community" from "unlabeled, defaulting to community".
type labelResponse struct {
	Producer          string `json:"producer"`
	Fidelity          string `json:"fidelity,omitempty"`
	Context           string `json:"context,omitempty"`
	Access            string `json:"access,omitempty"`
	Justification     string `json:"justification,omitempty"`
	EffectiveFidelity string `json:"effective_fidelity,omitempty"`
	EffectiveAccess   string `json:"effective_access"`
	AccessDefault     string `json:"access_default"`
}

func (s *Server) labelResponseFor(v *registry.Vendor) labelResponse {
	return labelResponse{
		Producer:          v.Prefix,
		Fidelity:          v.LabelFidelity,
		Context:           v.LabelContext,
		Access:            v.LabelAccess,
		Justification:     v.LabelJustification,
		EffectiveFidelity: registry.EffectiveFidelity(v.LabelFidelity, v.Synthetic),
		EffectiveAccess:   registry.EffectiveAccess(v.LabelAccess, s.cfg.DataLabelAccessDefault),
		AccessDefault:     registry.EffectiveAccess("", s.cfg.DataLabelAccessDefault),
	}
}

// handleGetLabels returns one producer's labels (any authenticated viewer —
// labels are catalog metadata, visible to everyone per ADR 1028).
func (s *Server) handleGetLabels(w http.ResponseWriter, r *http.Request) {
	producer := r.PathValue("vendor")
	v, err := s.reg.GetVendor(r.Context(), producer)
	if err == registry.ErrNotFound {
		writeError(w, http.StatusNotFound, "not_found", "no producer registered under this key")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.labelResponseFor(v))
}

type setLabelsRequest struct {
	Fidelity      string `json:"fidelity"`
	Context       string `json:"context"`
	Access        string `json:"access"`
	Justification string `json:"justification"`
}

// handleSetLabels stamps a producer's data labels (ADR 1075). The producer
// itself (vendor-scoped) or an operator may set them — the data-authority
// principle (data-access-roles.md §8): the producer owns its own labels. The
// justification-on-restriction gate is enforced in the store; an invalid lane
// or a restriction with no justification is a 400. F-207-audited.
func (s *Server) handleSetLabels(w http.ResponseWriter, r *http.Request) {
	producer := r.PathValue("vendor")
	var req setLabelsRequest
	if err := readJSON(w, r, &req); err != nil {
		writeDecodeError(w, err)
		return
	}
	labels := registry.Labels{
		Fidelity:      req.Fidelity,
		Context:       req.Context,
		Access:        req.Access,
		Justification: req.Justification,
	}
	if err := s.reg.SetVendorLabels(r.Context(), producer, labels); err != nil {
		if err == registry.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "no producer registered under this key")
			return
		}
		// Validation failure (bad enum / missing justification) — a 400 with the
		// store's explanatory message, so the restriction gate is legible.
		writeError(w, http.StatusBadRequest, "validate", err.Error())
		return
	}
	_ = s.reg.LogAuditEvent(r.Context(), actorOf(r), "data.label.set", producer,
		labelAuditDetails(labels))
	v, err := s.reg.GetVendor(r.Context(), producer)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.labelResponseFor(v))
}

// handleLabelDrift serves the ADR 1075 restrictive-share telemetry: the
// non-community label share per goatnet, the ratchet-up-bias counter. A plain
// honest ratio over the live producer population, resolved against the goatnet
// default — never a fabricated grade. Operator-gated (it is a governance
// health surface, like the other /admin counters).
func (s *Server) handleLabelDrift(w http.ResponseWriter, r *http.Request) {
	vendors, err := s.reg.ListVendors(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, registry.ComputeLabelDrift(vendors, s.cfg.DataLabelAccessDefault))
}

// labelAuditDetails renders the label set as a compact JSON detail blob for the
// audit row (the justification is the load-bearing why on restriction). Uses %q
// for JSON-string escaping, the same shape the other audit-detail blobs use.
func labelAuditDetails(l registry.Labels) string {
	return fmt.Sprintf(`{"fidelity":%q,"context":%q,"access":%q,"justification":%q}`,
		l.Fidelity, l.Context, l.Access, l.Justification)
}
