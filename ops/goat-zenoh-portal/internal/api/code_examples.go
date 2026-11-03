package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

// Vendor-provided code example endpoints (Block 85 PR-D follow-on).
//
//   PUT    /api/vendors/{vendor}/topics/{id}/examples/{lang}
//   GET    /api/vendors/{vendor}/topics/{id}/examples/{lang}
//   DELETE /api/vendors/{vendor}/topics/{id}/examples/{lang}
//
// The schema-render endpoint overlays these onto autogen output: when a
// row exists for (topic, language), it's returned with source="org"
// (ADR 1052 Phase 3; was "vendor") and the viewer renders the green
// org-provided badge. Without a row, the renderer falls back to
// portal-templated autogen and emits source="autogen" with the
// "notional, not validated" disclaimer.

type codeExampleRequest struct {
	Code string `json:"code"`
}

type codeExampleResponse struct {
	TopicID   int64  `json:"topic_id"`
	Language  string `json:"language"`
	Code      string `json:"code"`
	UpdatedAt int64  `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`
}

func toCodeExampleResponse(e *registry.TopicCodeExample) codeExampleResponse {
	return codeExampleResponse{
		TopicID:   e.TopicID,
		Language:  e.Language,
		Code:      e.Code,
		UpdatedAt: e.UpdatedAt,
		UpdatedBy: e.UpdatedBy,
	}
}

// putCodeExample handles PUT /api/vendors/{v}/topics/{id}/examples/{lang}.
// UPSERT semantics: replaces an existing row for the (topic, language)
// pair or creates a new one.
func (s *Server) putCodeExample(w http.ResponseWriter, r *http.Request) {
	vendor := r.PathValue("vendor")
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "decode", "id must be integer")
		return
	}
	lang := strings.ToLower(strings.TrimSpace(r.PathValue("lang")))
	if err := registry.ValidExampleLanguage(lang); err != nil {
		writeError(w, http.StatusBadRequest, "validate", err.Error())
		return
	}
	t, err := s.reg.GetTopic(r.Context(), id)
	if err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			writeError(w, http.StatusNotFound, "notfound", "topic not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	if t.VendorPrefix != vendor {
		writeError(w, http.StatusNotFound, "notfound", "topic not found under this vendor")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, registry.MaxCodeExampleBytes+(1<<12))
	var req codeExampleRequest
	if err := readJSON(w, r, &req); err != nil {
		writeDecodeError(w, err)
		return
	}
	if req.Code == "" {
		writeError(w, http.StatusBadRequest, "validate", "code body is empty")
		return
	}
	if len(req.Code) > registry.MaxCodeExampleBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large",
			"code exceeds "+strconv.Itoa(registry.MaxCodeExampleBytes)+" bytes")
		return
	}

	updatedBy := vendor
	if p := PrincipalFromContext(r.Context()); p != nil && p.Operator {
		updatedBy = "operator"
	}
	e := &registry.TopicCodeExample{
		TopicID:   id,
		Language:  lang,
		Code:      req.Code,
		UpdatedBy: updatedBy,
	}
	if _, err := s.reg.UpsertCodeExample(r.Context(), e); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	details, _ := json.Marshal(map[string]any{
		"topic_id": id,
		"language": lang,
		"size":     len(req.Code),
	})
	_ = s.reg.LogAuditEvent(r.Context(), updatedBy, "code-example-upsert", t.KeyPattern, string(details))

	writeJSON(w, http.StatusOK, toCodeExampleResponse(e))
}

// listCodeExamples handles GET /api/vendors/{v}/topics/{id}/examples. Returns
// every vendor-provided example for the topic — the reconciler reads this to
// compute its upsert/delete plan.
func (s *Server) listCodeExamples(w http.ResponseWriter, r *http.Request) {
	vendor := r.PathValue("vendor")
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "decode", "id must be integer")
		return
	}
	t, err := s.reg.GetTopic(r.Context(), id)
	if err != nil || t.VendorPrefix != vendor {
		writeError(w, http.StatusNotFound, "notfound", "topic not found under this vendor")
		return
	}
	rows, err := s.reg.ListCodeExamplesByTopic(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	out := make([]codeExampleResponse, 0, len(rows))
	for i := range rows {
		out = append(out, toCodeExampleResponse(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"examples": out})
}

// getCodeExample handles GET /api/vendors/{v}/topics/{id}/examples/{lang}.
func (s *Server) getCodeExample(w http.ResponseWriter, r *http.Request) {
	vendor := r.PathValue("vendor")
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "decode", "id must be integer")
		return
	}
	lang := strings.ToLower(strings.TrimSpace(r.PathValue("lang")))
	t, err := s.reg.GetTopic(r.Context(), id)
	if err != nil || t.VendorPrefix != vendor {
		writeError(w, http.StatusNotFound, "notfound", "topic not found under this vendor")
		return
	}
	e, err := s.reg.GetCodeExample(r.Context(), id, lang)
	if err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			writeError(w, http.StatusNotFound, "notfound", "no vendor-provided example for this language")
			return
		}
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toCodeExampleResponse(e))
}

// deleteCodeExample handles DELETE /api/vendors/{v}/topics/{id}/examples/{lang}.
// After deletion the autogen path takes over for that language again.
func (s *Server) deleteCodeExample(w http.ResponseWriter, r *http.Request) {
	vendor := r.PathValue("vendor")
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "decode", "id must be integer")
		return
	}
	lang := strings.ToLower(strings.TrimSpace(r.PathValue("lang")))
	t, err := s.reg.GetTopic(r.Context(), id)
	if err != nil || t.VendorPrefix != vendor {
		writeError(w, http.StatusNotFound, "notfound", "topic not found under this vendor")
		return
	}
	if err := s.reg.DeleteCodeExample(r.Context(), id, lang); err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			writeError(w, http.StatusNotFound, "notfound", "no vendor-provided example for this language")
			return
		}
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	actor := vendor
	if p := PrincipalFromContext(r.Context()); p != nil && p.Operator {
		actor = "operator"
	}
	details, _ := json.Marshal(map[string]any{"topic_id": id, "language": lang})
	_ = s.reg.LogAuditEvent(r.Context(), actor, "code-example-delete", t.KeyPattern, string(details))
	w.WriteHeader(http.StatusNoContent)
}

// drainAndClose is used by the writer functions to ensure request bodies
// are drained when we abort early; keeps connection reuse healthy.
func drainAndClose(r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	_ = r.Body.Close()
}

// drainAndClose lives here so the compiler doesn't complain when other
// PR-D files reference io without using it.
var _ = drainAndClose
