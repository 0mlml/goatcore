package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

// envelopeInput is the editable body of PUT /api/vendors/{vendor}/envelope and
// the optional `envelope` block on a registration payload (ADR 1045). It is the
// eight throughput numbers + an optional class label; the anchor comes from the
// path / the row being created, never the body.
type envelopeInput struct {
	MinMsgsPerS  float64 `json:"min_msgs_per_s"`
	MeanMsgsPerS float64 `json:"mean_msgs_per_s"`
	HighMsgsPerS float64 `json:"high_msgs_per_s"`
	MaxMsgsPerS  float64 `json:"max_msgs_per_s"`

	MinBytesPerS  float64 `json:"min_bytes_per_s"`
	MeanBytesPerS float64 `json:"mean_bytes_per_s"`
	HighBytesPerS float64 `json:"high_bytes_per_s"`
	MaxBytesPerS  float64 `json:"max_bytes_per_s"`

	// Optional provenance label (e.g. "C2"); free text, display only.
	ClassKey string `json:"class_key"`
}

func (in *envelopeInput) toEnvelope(anchor string) *registry.Envelope {
	return &registry.Envelope{
		Anchor:        anchor,
		MinMsgsPerS:   in.MinMsgsPerS,
		MeanMsgsPerS:  in.MeanMsgsPerS,
		HighMsgsPerS:  in.HighMsgsPerS,
		MaxMsgsPerS:   in.MaxMsgsPerS,
		MinBytesPerS:  in.MinBytesPerS,
		MeanBytesPerS: in.MeanBytesPerS,
		HighBytesPerS: in.HighBytesPerS,
		MaxBytesPerS:  in.MaxBytesPerS,
		ClassKey:      in.ClassKey,
	}
}

// handleListEnvelopes serves GET /api/envelopes — the anchor→envelope map the
// reveal layer (`goat top`) caches. Read like the alias registry; the consumer
// degrades to "no envelope" (an honest `—`) when this can't be fetched.
func (s *Server) handleListEnvelopes(w http.ResponseWriter, r *http.Request) {
	envs, err := s.reg.ListEnvelopes(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"envelopes": envs})
}

// putEnvelope handles PUT /api/vendors/{vendor}/envelope (scoped to the owning
// vendor or the operator). It records the registering party's declared
// expectation — validated (ascending, non-negative) and stored as
// source="edited". It does NOT touch the wire/ACL/cert/namespace, and never
// gates anything: this is declared intent, not authorization (ADR 1045).
func (s *Server) putEnvelope(w http.ResponseWriter, r *http.Request) {
	anchor := r.PathValue("vendor")
	if anchor == "" {
		writeError(w, http.StatusBadRequest, "decode", "missing vendor/anchor")
		return
	}
	// The anchor must be a known actor — fail closed on an unknown one rather
	// than declaring an envelope for a phantom (a typo'd anchor here is
	// harmless governance data, but a 404 is the honest answer).
	if _, err := s.reg.GetVendor(r.Context(), anchor); err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "no such actor/anchor")
			return
		}
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}

	var in envelopeInput
	if err := readJSON(w, r, &in); err != nil {
		writeDecodeError(w, err)
		return
	}
	env := in.toEnvelope(anchor)
	if err := s.reg.UpsertEnvelope(r.Context(), env); err != nil {
		// Validate() failure (non-ascending / negative) is a client error.
		if env.Validate() != nil {
			writeError(w, http.StatusBadRequest, "validate", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "upsert", err.Error())
		return
	}
	_ = s.reg.LogAuditEvent(r.Context(), actorOf(r), "envelope-edit", anchor, "")
	writeJSON(w, http.StatusOK, env)
}

// assignInitialEnvelope gives a freshly-created actor its envelope at
// registration (ADR 1045 §3). If the registration payload supplied a valid
// envelope block, that is stored as the party's edited declaration; otherwise
// the rough class-baseline default is assigned. BEST-EFFORT BY CONTRACT — any
// failure (or an invalid supplied block) is swallowed and falls back to the
// default; registration never gates on the envelope.
func (s *Server) assignInitialEnvelope(ctx context.Context, anchor string, supplied *envelopeInput) {
	if supplied != nil {
		env := supplied.toEnvelope(anchor)
		if env.Validate() == nil {
			if err := s.reg.UpsertEnvelope(ctx, env); err == nil {
				return
			}
		}
		// Invalid or failed supplied envelope: fall through to the default so
		// the actor still gets one (never gate registration).
	}
	_ = s.reg.EnsureDefaultEnvelope(ctx, anchor, s.envDefaultClass, s.envBaselines)
}
