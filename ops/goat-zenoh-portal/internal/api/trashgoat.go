package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

// trashgoat — operator-controlled chaos publisher.
//
// HTTP surface (all operator-only, gated at the route layer):
//
//   GET    /api/admin/trashgoat/specs            list every spec
//   POST   /api/admin/trashgoat/specs            create new spec in draft
//   GET    /api/admin/trashgoat/specs/{id}       fetch one spec
//   PATCH  /api/admin/trashgoat/specs/{id}       mutate description / topic_pattern /
//                                                payload_spec / cadence_spec / notes
//                                                (draft only, except notes)
//   DELETE /api/admin/trashgoat/specs/{id}       delete (retired only)
//   POST   /api/admin/trashgoat/specs/{id}/activate
//   POST   /api/admin/trashgoat/specs/{id}/pause
//   POST   /api/admin/trashgoat/specs/{id}/retire
//   POST   /api/admin/trashgoat/specs/{id}/cleanse
//
// Every lifecycle-changing handler emits a JSON message onto the
// control topic ops/trashgoat/control/<spec-id>/v1 via the Zenoh
// REST plugin (best-effort; HTTP-layer success doesn't depend on the
// publish landing — the runner will pick up the next state-change
// event or poll on startup).
//
// Design: docs/design/trashgoat.md.

// trashgoatControlTopic builds the control-topic key for a spec id.
func trashgoatControlTopic(id int64) string {
	return fmt.Sprintf("ops/trashgoat/control/%d/v1", id)
}

// trashgoatControlMessage is the JSON shape published on every state
// change. Mirrors the design doc §"Control wire contract".
type trashgoatControlMessage struct {
	SpecID   int64                  `json:"spec_id"`
	Spec     *registry.TrashgoatSpec `json:"spec"`
	Command  string                 `json:"command"`
	IssuedBy string                 `json:"issued_by"`
	IssuedAt int64                  `json:"issued_at"`
}

// publishTrashgoatControl is a best-effort publish onto the spec's
// control topic. The HTTP response carries the persisted spec
// regardless — runner cold-start does a `session.get` against
// ops/trashgoat/control/** for a complete view.
func (s *Server) publishTrashgoatControl(ctx context.Context, spec *registry.TrashgoatSpec, command, issuedBy string) {
	if s.zenoh == nil {
		return
	}
	msg := trashgoatControlMessage{
		SpecID:   spec.ID,
		Spec:     spec,
		Command:  command,
		IssuedBy: issuedBy,
		IssuedAt: time.Now().Unix(),
	}
	body, err := json.Marshal(msg)
	if err != nil {
		slog.WarnContext(ctx, "trashgoat: marshal control message",
			slog.String("err", err.Error()),
			slog.Int64("spec_id", spec.ID))
		return
	}
	// Short timeout — the persistence has already happened; we don't
	// want the HTTP request hanging on a flaky router.
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.zenoh.Put(pctx, trashgoatControlTopic(spec.ID), body, "application/json"); err != nil {
		slog.WarnContext(ctx, "trashgoat: publish control message",
			slog.String("err", err.Error()),
			slog.Int64("spec_id", spec.ID),
			slog.String("command", command))
	}
}

// trashgoatPrincipalLabel returns a human-readable label for the audit
// trail. For operator we use "operator"; for vendor scope (which
// should never reach these handlers per requireOperator) we'd return
// the vendor prefix, but that path is structurally unreachable.
func trashgoatPrincipalLabel(p *Principal) string {
	if p == nil {
		return "anonymous"
	}
	if p.Operator {
		return "operator"
	}
	return p.Vendor
}

// --- request bodies --------------------------------------------------

type createTrashgoatSpecRequest struct {
	Slug         string          `json:"slug"`
	Description  string          `json:"description"`
	TopicPattern string          `json:"topic_pattern"`
	PayloadSpec  json.RawMessage `json:"payload_spec"`
	CadenceSpec  json.RawMessage `json:"cadence_spec"`
	Notes        string          `json:"notes"`
}

type patchTrashgoatSpecRequest struct {
	Description  *string         `json:"description"`
	TopicPattern *string         `json:"topic_pattern"`
	PayloadSpec  json.RawMessage `json:"payload_spec"`
	CadenceSpec  json.RawMessage `json:"cadence_spec"`
	Notes        *string         `json:"notes"`
}

// --- handlers --------------------------------------------------------

// listTrashgoatSpecs serves GET /api/admin/trashgoat/specs.
func (s *Server) listTrashgoatSpecs(w http.ResponseWriter, r *http.Request) {
	_ = s.reg.LogAuditEvent(r.Context(), actorOf(r), "trashgoat.specs.read", "", "")
	specs, err := s.reg.ListTrashgoatSpecs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	if specs == nil {
		specs = []registry.TrashgoatSpec{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"specs": specs})
}

// getTrashgoatSpec serves GET /api/admin/trashgoat/specs/{id}.
func (s *Server) getTrashgoatSpec(w http.ResponseWriter, r *http.Request) {
	id, ok := parseTrashgoatID(w, r)
	if !ok {
		return
	}
	_ = s.reg.LogAuditEvent(r.Context(), actorOf(r), "trashgoat.specs.read", r.PathValue("id"), "")
	spec, err := s.reg.GetTrashgoatSpec(r.Context(), id)
	if err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			writeError(w, http.StatusNotFound, "notfound", "trashgoat spec not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, spec)
}

// createTrashgoatSpec serves POST /api/admin/trashgoat/specs.
func (s *Server) createTrashgoatSpec(w http.ResponseWriter, r *http.Request) {
	var req createTrashgoatSpecRequest
	if err := readJSON(w, r, &req); err != nil {
		writeDecodeError(w, err)
		return
	}
	payloadJSON, err := validateTrashgoatPayloadSpec(req.PayloadSpec)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validate", err.Error())
		return
	}
	cadenceJSON, err := validateTrashgoatCadenceSpec(req.CadenceSpec)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validate", err.Error())
		return
	}
	spec := &registry.TrashgoatSpec{
		Slug:            req.Slug,
		Description:     req.Description,
		TopicPattern:    req.TopicPattern,
		PayloadSpecJSON: payloadJSON,
		CadenceSpecJSON: cadenceJSON,
		Notes:           req.Notes,
		CreatedBy:       trashgoatPrincipalLabel(PrincipalFromContext(r.Context())),
	}
	id, err := s.reg.CreateTrashgoatSpec(r.Context(), spec)
	if err != nil {
		if errors.Is(err, registry.ErrSpecExists) {
			writeError(w, http.StatusConflict, "exists", "trashgoat spec with this slug already exists")
			return
		}
		writeError(w, http.StatusBadRequest, "validate", err.Error())
		return
	}
	_ = s.reg.LogAuditEvent(r.Context(), spec.CreatedBy, "trashgoat-spec-create", spec.Slug, "")
	spec.ID = id
	writeJSON(w, http.StatusCreated, spec)
}

// patchTrashgoatSpec serves PATCH /api/admin/trashgoat/specs/{id}.
//
// Mutates description / topic_pattern / payload_spec / cadence_spec /
// notes. The registry refuses spec-definition mutations on non-draft
// specs (ErrSpecImmutable); notes-only PATCH on a non-draft spec lands
// here, gets refused, and we surface a clear 409 explaining why.
func (s *Server) patchTrashgoatSpec(w http.ResponseWriter, r *http.Request) {
	id, ok := parseTrashgoatID(w, r)
	if !ok {
		return
	}
	cur, err := s.reg.GetTrashgoatSpec(r.Context(), id)
	if err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			writeError(w, http.StatusNotFound, "notfound", "trashgoat spec not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	var req patchTrashgoatSpecRequest
	if err := readJSON(w, r, &req); err != nil {
		writeDecodeError(w, err)
		return
	}
	// Start from the current spec and overlay every supplied field.
	upd := *cur
	if req.Description != nil {
		upd.Description = *req.Description
	}
	if req.TopicPattern != nil {
		upd.TopicPattern = *req.TopicPattern
	}
	if len(req.PayloadSpec) > 0 {
		payloadJSON, err := validateTrashgoatPayloadSpec(req.PayloadSpec)
		if err != nil {
			writeError(w, http.StatusBadRequest, "validate", err.Error())
			return
		}
		upd.PayloadSpecJSON = payloadJSON
	}
	if len(req.CadenceSpec) > 0 {
		cadenceJSON, err := validateTrashgoatCadenceSpec(req.CadenceSpec)
		if err != nil {
			writeError(w, http.StatusBadRequest, "validate", err.Error())
			return
		}
		upd.CadenceSpecJSON = cadenceJSON
	}
	if req.Notes != nil {
		upd.Notes = *req.Notes
	}
	if err := s.reg.UpdateTrashgoatSpec(r.Context(), &upd); err != nil {
		if errors.Is(err, registry.ErrSpecImmutable) {
			writeError(w, http.StatusConflict, "immutable",
				fmt.Sprintf("spec in %q state — only draft specs are editable. retire + re-author for a new shape", cur.State))
			return
		}
		writeError(w, http.StatusBadRequest, "validate", err.Error())
		return
	}
	got, _ := s.reg.GetTrashgoatSpec(r.Context(), id)
	_ = s.reg.LogAuditEvent(r.Context(), trashgoatPrincipalLabel(PrincipalFromContext(r.Context())),
		"trashgoat-spec-update", cur.Slug, "")
	// Update on draft never publishes a control event (runner doesn't
	// know about the spec yet). Once the operator hits Activate, the
	// activate handler emits the first control event carrying the
	// then-current spec.
	writeJSON(w, http.StatusOK, got)
}

// deleteTrashgoatSpec serves DELETE /api/admin/trashgoat/specs/{id}.
func (s *Server) deleteTrashgoatSpec(w http.ResponseWriter, r *http.Request) {
	id, ok := parseTrashgoatID(w, r)
	if !ok {
		return
	}
	cur, err := s.reg.GetTrashgoatSpec(r.Context(), id)
	if err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			writeError(w, http.StatusNotFound, "notfound", "trashgoat spec not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	if err := s.reg.DeleteTrashgoatSpec(r.Context(), id); err != nil {
		if errors.Is(err, registry.ErrSpecNotRetired) {
			writeError(w, http.StatusConflict, "not_retired",
				"trashgoat spec must be in retired state before delete")
			return
		}
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	_ = s.reg.LogAuditEvent(r.Context(), trashgoatPrincipalLabel(PrincipalFromContext(r.Context())),
		"trashgoat-spec-delete", cur.Slug, "")
	w.WriteHeader(http.StatusNoContent)
}

// activateTrashgoatSpec serves POST /api/admin/trashgoat/specs/{id}/activate.
func (s *Server) activateTrashgoatSpec(w http.ResponseWriter, r *http.Request) {
	s.lifecycleTrashgoatSpec(w, r, "activate", s.reg.ActivateTrashgoatSpec)
}

// pauseTrashgoatSpec serves POST /api/admin/trashgoat/specs/{id}/pause.
func (s *Server) pauseTrashgoatSpec(w http.ResponseWriter, r *http.Request) {
	s.lifecycleTrashgoatSpec(w, r, "pause", s.reg.PauseTrashgoatSpec)
}

// retireTrashgoatSpec serves POST /api/admin/trashgoat/specs/{id}/retire.
func (s *Server) retireTrashgoatSpec(w http.ResponseWriter, r *http.Request) {
	s.lifecycleTrashgoatSpec(w, r, "retire", s.reg.RetireTrashgoatSpec)
}

// cleanseTrashgoatSpec serves POST /api/admin/trashgoat/specs/{id}/cleanse.
// Cleanse is orthogonal to lifecycle — it stamps last_cleansed_at and
// emits a "cleanse" control event the runner consumes to issue
// z_delete against every key the spec has published. State stays where
// it was.
func (s *Server) cleanseTrashgoatSpec(w http.ResponseWriter, r *http.Request) {
	s.lifecycleTrashgoatSpec(w, r, "cleanse", s.reg.RecordTrashgoatCleanse)
}

// lifecycleTrashgoatSpec is the common shape behind activate / pause /
// retire / cleanse: parse id, run the registry mutation, fetch fresh
// spec, emit control event, log audit, return the spec.
func (s *Server) lifecycleTrashgoatSpec(w http.ResponseWriter, r *http.Request, command string, op func(context.Context, int64) error) {
	id, ok := parseTrashgoatID(w, r)
	if !ok {
		return
	}
	if _, err := s.reg.GetTrashgoatSpec(r.Context(), id); err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			writeError(w, http.StatusNotFound, "notfound", "trashgoat spec not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	if err := op(r.Context(), id); err != nil {
		if errors.Is(err, registry.ErrInvalidTransition) {
			writeError(w, http.StatusConflict, "invalid_transition", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	spec, _ := s.reg.GetTrashgoatSpec(r.Context(), id)
	issuedBy := trashgoatPrincipalLabel(PrincipalFromContext(r.Context()))
	_ = s.reg.LogAuditEvent(r.Context(), issuedBy, "trashgoat-spec-"+command, spec.Slug, "")
	s.publishTrashgoatControl(r.Context(), spec, command, issuedBy)
	writeJSON(w, http.StatusOK, spec)
}

// parseTrashgoatID extracts {id} from the URL and writes a 400 + false
// when malformed.
func parseTrashgoatID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "decode", "id must be a positive integer")
		return 0, false
	}
	return id, true
}
