package api

import "net/http"

// brandingResponse is the JSON shape served at GET /api/branding. It is the
// frontend JS counterpart to the serve-time HTML token substitution: static
// HTML gets __LEX_*__ tokens replaced before it leaves the server, but labels
// the JS generates at runtime (table headings, button text, generated prose)
// read these values on page load instead. Field names are the lowercase
// lexicon slot names. See docs/design/portal-lexicon.md.
//
// Public, no auth — same rationale as /api/siblings: it is branding text, not
// data, and every page that reads it is itself a public-mesh surface.
type brandingResponse struct {
	ProductName  string `json:"product_name"`
	ProductTitle string `json:"product_title"`
	Org          string `json:"org"`
	Orgs         string `json:"orgs"`
	OrgTitle     string `json:"org_title"`
	OrgsTitle    string `json:"orgs_title"`
	PanoscopeURL string `json:"panoscope_url"`
}

// handleBranding serves the resolved display lexicon for frontend JS.
func (s *Server) handleBranding(w http.ResponseWriter, r *http.Request) {
	l := s.cfg.Lexicon
	writeJSON(w, http.StatusOK, brandingResponse{
		ProductName:  l.ProductName,
		ProductTitle: l.ProductTitle,
		Org:          l.Org,
		Orgs:         l.Orgs,
		OrgTitle:     l.OrgTitle,
		OrgsTitle:    l.OrgsTitle,
		PanoscopeURL: l.PanoscopeURL,
	})
}
