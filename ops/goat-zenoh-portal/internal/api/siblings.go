package api

import (
	"net/http"
	"strings"
)

// siblingsResponse is the JSON shape served at GET /api/siblings.
//
// Block 94 cross-surface deep-link contract — sibling-surface base
// URLs the static catalog JS reads on page load to render `diagnose ↗`
// + `tail ↗` buttons on vendor / topic / progress rows. Each field is
// the empty string when no sibling exists at this deployment; the JS
// hides the corresponding button rather than rendering a dead href
// (the contract's "no runtime coupling" rule — a portal deployed
// without a stethoscope sibling still works, it just doesn't offer
// the diagnose action).
//
// Field names match the param canon in docs/design/zenoh-surface-deep-
// links.md §"URL parameter canon": panoscope / stethoscope / flashlight.
type siblingsResponse struct {
	Panoscope   string `json:"panoscope"`
	Stethoscope string `json:"stethoscope"`
	Flashlight  string `json:"flashlight"`
}

// handleSiblings serves the public Block 94 sibling-URL map. No auth
// — the URLs are mesh-DNS hostnames already visible in DNS, and the
// catalog pages reading this endpoint are themselves public-mesh
// surfaces. Trailing slashes are stripped so the client can append
// `/?key=...` without worrying about double slashes.
func (s *Server) handleSiblings(w http.ResponseWriter, r *http.Request) {
	resp := siblingsResponse{
		Panoscope:   strings.TrimRight(s.cfg.SiblingPanoscopeURL, "/"),
		Stethoscope: strings.TrimRight(s.cfg.SiblingStethoscopeURL, "/"),
		Flashlight:  strings.TrimRight(s.cfg.SiblingFlashlightURL, "/"),
	}
	writeJSON(w, http.StatusOK, resp)
}
