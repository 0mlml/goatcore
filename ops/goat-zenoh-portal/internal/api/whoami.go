package api

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

// whoamiResponse is the resolved identity + ADR 1071 sponsorship of the
// presented key. It is the read-only auth-delegation surface goat-id-console
// calls to resolve a participant's portal key when the onboarding engine
// (which moved to the console) needs to mint an invite on that participant's
// behalf: the key store + vendors registry stay the SoT here, the console reads
// them. See docs/design/onboarding-extraction-plan.md.
type whoamiResponse struct {
	// Operator is true for a backend super-user (operator-tier) key. Operators
	// are sponsorship-authorized by definition with no tier cap.
	Operator bool `json:"operator"`
	// Actor is the resolved person handle for an operator principal;
	// empty for a vendor/org principal.
	Actor string `json:"actor,omitempty"`
	// Vendor is the org prefix for a vendor/org principal; empty for operators.
	Vendor string `json:"vendor,omitempty"`
	// DisplayName is the org row's human-readable name (in slot-pool mode the
	// onboarded system handle, e.g. "maintainer-operator") — display-only attribution
	// so callers never have to render the raw UUID anchor; empty for operators.
	DisplayName string `json:"display_name,omitempty"`
	// SponsorAuthorized + SponsorMaxTier are the ADR 1071 sponsorship annotation
	// of the org row (always true / "" for operators). The console caps a minted
	// invite's tier by SponsorMaxTier exactly as the portal's mint did.
	SponsorAuthorized bool   `json:"sponsor_authorized"`
	SponsorMaxTier    string `json:"sponsor_max_tier,omitempty"`
	// Alias + SponsorEmail are the identity's other recorded sponsor forms
	// (live slot alias; bound Ory login — ADR 1072). The console matches an
	// invite/proposal `sponsor` value against {prefix, alias, email} when it
	// scopes a non-escalated sponsor to their own invitees.
	Alias        string `json:"alias,omitempty"`
	SponsorEmail string `json:"sponsor_email,omitempty"`
	// VisibleOrgs is the number of org rows the caller can see — the count the
	// frontend header renders as "N orgs visible". It is set only by
	// handleWhoami, so the sponsorship-lookup surface that shares this struct
	// does not pay for the query. The frontend used to derive this by fetching
	// the whole /api/orgs listing (~63 KB) on every page load.
	VisibleOrgs int `json:"visible_orgs,omitempty"`
}

// handleWhoami returns the caller's resolved principal + sponsorship. The caller
// authenticates with the key it wants resolved (AuthMiddleware(true) has already
// resolved the principal into context). Read-only; mutates nothing.
func (s *Server) handleWhoami(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFromContext(r.Context())
	if p == nil {
		writeError(w, http.StatusUnauthorized, "auth", "missing principal")
		return
	}
	// Best-effort: the count decorates the header, so a failed count must not
	// fail the identity answer the console and CLI depend on.
	visibleOrgs, err := s.reg.CountVendors(r.Context())
	if err != nil {
		visibleOrgs = 0
	}
	if p.Operator {
		writeJSON(w, http.StatusOK, whoamiResponse{
			Operator:          true,
			Actor:             p.Actor,
			SponsorAuthorized: true,
			VisibleOrgs:       visibleOrgs,
		})
		return
	}
	// Vendor/org principal: pull its ADR 1071 sponsorship from the registry.
	v, err := s.reg.GetVendor(r.Context(), p.Vendor)
	if err == registry.ErrNotFound {
		// A resolved vendor principal whose row vanished — fail closed.
		writeJSON(w, http.StatusOK, whoamiResponse{Vendor: p.Vendor, VisibleOrgs: visibleOrgs})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup_error", "could not resolve sponsorship")
		return
	}
	resp := s.sponsorResponseFor(r, v)
	resp.VisibleOrgs = visibleOrgs
	writeJSON(w, http.StatusOK, resp)
}

// sponsorResponseFor assembles the vendor-principal whoami/lookup payload,
// including the identity's other sponsor-match forms (alias, bound email).
func (s *Server) sponsorResponseFor(r *http.Request, v *registry.Vendor) whoamiResponse {
	alias := ""
	if a, err := s.reg.HandleForAnchor(r.Context(), v.Prefix); err == nil {
		alias = a
	}
	return whoamiResponse{
		Vendor:            v.Prefix,
		DisplayName:       v.DisplayName,
		SponsorAuthorized: v.SponsorAuthorized,
		SponsorMaxTier:    v.SponsorMaxTier,
		Alias:             alias,
		SponsorEmail:      v.SponsorEmail,
	}
}

// handleSponsorshipLookup resolves an Ory LOGIN EMAIL to its bound sponsorship
// annotation (ADR 1072) — the id-admin-console asks this when a non-ADMINS
// human session opens /onboarding, to decide whether that person is a sponsor
// and at what cap. Auth: an operator bearer, or the console↔portal shared
// service secret (the same pairing the candidate stage-notify channel uses;
// override with PORTAL_CONSOLE_LOOKUP_TOKEN). Fail-closed: with neither
// configured nor presented, the endpoint refuses — it never opens silently.
//
// GET /api/orgs/sponsorship?email=<ory-login-email>
func (s *Server) handleSponsorshipLookup(w http.ResponseWriter, r *http.Request) {
	if !s.lookupAuthorized(r) {
		writeError(w, http.StatusUnauthorized, "auth", "operator bearer or console service token required")
		return
	}
	email := strings.TrimSpace(r.URL.Query().Get("email"))
	if email == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "email query parameter is required")
		return
	}
	v, err := s.reg.VendorBySponsorEmail(r.Context(), email)
	switch {
	case errors.Is(err, registry.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_sponsor", "no sponsorship annotation is bound to this login")
		return
	case errors.Is(err, registry.ErrAmbiguous):
		writeError(w, http.StatusConflict, "ambiguous_sponsor",
			"more than one sponsorship annotation is bound to this login — fix the annotations")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.sponsorResponseFor(r, v))
}

// lookupAuthorized admits an operator principal (normal bearer auth) or the
// console↔portal shared service secret for the sponsorship lookup.
func (s *Server) lookupAuthorized(r *http.Request) bool {
	token := extractToken(r)
	if token == "" {
		return false
	}
	// Console service secret: PORTAL_CONSOLE_LOOKUP_TOKEN, defaulting to the
	// stage-notify shared secret (same trust pair, same blast radius).
	svc := strings.TrimSpace(s.cfg.ConsoleLookupToken)
	if svc == "" {
		svc = strings.TrimSpace(s.cfg.NotifyToken)
	}
	if svc != "" && subtle.ConstantTimeCompare([]byte(token), []byte(svc)) == 1 {
		return true
	}
	p, err := s.resolvePrincipal(r.Context(), token)
	return err == nil && p != nil && p.Operator
}
