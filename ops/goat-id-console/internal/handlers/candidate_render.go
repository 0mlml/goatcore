package handlers

import (
	"bytes"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/usareur-af/goatcore/ops/goat-id-console/web"
)

// Candidate responses deliberately omit admin context and step-up lookups.
func (s *Server) renderCandidate(w http.ResponseWriter, r *http.Request, page string, status int, vars *pageVars) {
	s.writeCandidate(w, r, page, status, struct {
		*pageVars
		Message string
	}{pageVars: vars})
}

func (s *Server) candidateError(w http.ResponseWriter, r *http.Request, status int, message string) {
	s.writeCandidate(w, r, "candidate_error", status, struct {
		Title   string
		Message string
	}{Title: "Invitation", Message: message})
}

func (s *Server) writeCandidate(w http.ResponseWriter, r *http.Request, page string, status int, vars any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	body, err := s.candidateHTML(page, vars)
	if err != nil {
		slog.ErrorContext(r.Context(), "candidate page render failed", "page", page, "err", err)
		http.Error(w, "This page could not load. Please contact your sponsor.", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	//nolint:errcheck // HTTP response delivery is best-effort after the handler has selected its result.
	_, _ = w.Write(body)
}

func (s *Server) candidateHTML(page string, vars any) ([]byte, error) {
	t, err := s.Templates.Clone()
	if err != nil {
		return nil, err
	}
	data, err := fs.ReadFile(web.FS, "templates/"+page+".html")
	if err != nil {
		return nil, err
	}
	if _, err = t.Parse(string(data)); err != nil {
		return nil, err
	}
	var body bytes.Buffer
	if err = t.ExecuteTemplate(&body, "candidate_layout", vars); err != nil {
		return nil, err
	}
	return body.Bytes(), nil
}
