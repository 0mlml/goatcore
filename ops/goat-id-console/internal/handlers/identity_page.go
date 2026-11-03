package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// identitySelfView contains only the canonical handle resolved for the signed-in
// session. An empty handle deliberately renders guidance instead of a guess.
type identitySelfView struct {
	Handle string
}

func (s *Server) handleIdentitySelf(w http.ResponseWriter, r *http.Request) {
	actor := actorFromCtx(r.Context())
	email := strings.TrimSpace(actor.Email)
	if email == "" {
		s.renderError(w, r, http.StatusUnauthorized, "no signed-in identity on this request")
		return
	}

	handle, err := s.handleForLoginEmail(r.Context(), email)
	if errors.Is(err, onboardstore.ErrNotFound) {
		handle = ""
	} else if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "could not look up your identity")
		return
	}
	s.render(w, r, "identity", &pageVars{
		Title:    "Your identity",
		Identity: &identitySelfView{Handle: handle},
	})
}
