package handlers

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/config"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/kratos"
)

// Invite accept — UNAUTHENTICATED admin-wise; the token IS the auth.
func (s *Server) handleInviteAcceptForm(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	inv, err := s.Invites.GetForAccept(token)
	if err != nil {
		s.candidateError(w, r, 404, "This invitation is unavailable or has expired. Please ask your sponsor for a new invite.")
		return
	}
	// We render a stripped-down page; no IdP selector / no admin chrome.
	idp, _ := s.Cfg.IdP(inv.IdPKey)
	s.renderCandidate(w, r, "invite_accept", http.StatusOK, &pageVars{
		Title:        "Accept invitation",
		IdP:          idp,
		AllIdPs:      []config.IdP{idp}, // hide the selector
		Actor:        Actor{},           // no actor — token-bound
		Token:        token,
		PresetGroups: inv.PresetGroups,
		Email:        inv.Email, // bound at mint; displayed read-only
		TTLHuman:     kratos.FormatExpiresIn(inv.TTLAt),
	})
}

func (s *Server) handleInviteAcceptSubmit(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	name := strings.TrimSpace(r.FormValue("name"))
	inv, err := s.Invites.GetForAccept(token)
	if err != nil {
		s.candidateError(w, r, 400, "This invitation is unavailable or has expired. Please ask your sponsor for a new invite.")
		return
	}
	// Only an address bound by the admin at mint is imported verified.
	// A token holder's typed address does not prove mailbox ownership.
	bound := inv.Email != ""
	email := inv.Email
	if !bound {
		email = strings.TrimSpace(r.FormValue("email"))
	}
	if email == "" {
		s.candidateError(w, r, 400, "Enter your email address on the invitation form and submit again.")
		return
	}
	idp, ok := s.Cfg.IdP(inv.IdPKey)
	if !ok {
		slog.ErrorContext(r.Context(), "invite identity provider unavailable", "idp", inv.IdPKey)
		s.candidateError(w, r, 500, "We could not finish setting up your access. Please contact your sponsor.")
		return
	}
	cli := s.Kratos[inv.IdPKey]
	traits := map[string]interface{}{"email": email, "groups": inv.PresetGroups}
	if name != "" {
		traits["name"] = name
	}
	createReq := kratos.CreateIdentityRequest{Traits: traits}
	if bound {
		createReq.VerifiableAddresses = kratos.VerifiedEmail(email)
	}
	identity, err := cli.CreateIdentity(r.Context(), createReq)
	if err != nil {
		slog.ErrorContext(r.Context(), "invite identity creation failed", "err", err)
		s.candidateError(w, r, 502, "We could not confirm that your account was created. Please contact your sponsor before submitting again.")
		return
	}
	rl, err := cli.MintRecoveryLink(r.Context(), identity.ID)
	if err != nil {
		slog.ErrorContext(r.Context(), "invite recovery link failed", "err", err)
		s.candidateError(w, r, 502, "Your account was created, but we could not provide its setup link. Please ask your sponsor for a recovery link.")
		return
	}
	if err := s.Invites.MarkUsed(token, email); err != nil {
		// The Kratos identity was created; the invite couldn't be marked used.
		// Log but don't fail the user — they have their identity. Operator
		// will see the audit trail.
		slog.Error("mark used after accept", slog.String("token_prefix", token[:8]), slog.String("err", err.Error()))
	}

	tokPrefix := token
	if len(tokPrefix) > 8 {
		tokPrefix = tokPrefix[:8]
	}
	// We emit using a synthesized actor distinguished by a literal value
	// so the audit shows "invite redemption" without lying that an admin did it.
	systemActor := Actor{Email: "<invite-redemption>", AAL: "n/a", SourceIP: firstSet(r.Header.Get("X-Real-IP"), r.Header.Get("X-Forwarded-For"), r.RemoteAddr)}
	s.emitAuditWithInviteToken(r, systemActor, "accept_invite", tokPrefix, idp.Key, nil, map[string]interface{}{"email": email, "groups": inv.PresetGroups}, false, "ok", "")
	s.emitAudit(r, systemActor, "create_identity", identity, idp.Key, nil, map[string]interface{}{"email": email, "groups": inv.PresetGroups, "via": "invite", "email_verified": bound}, false, "ok", "")
	s.emitAudit(r, systemActor, "mint_recovery_link", identity, idp.Key, nil, nil, false, "ok", "")

	// Render the recovery-link page so the recipient can copy + open it.
	// This page is also where they'd hand it to the operator if delivered out-of-band.
	s.renderCandidate(w, r, "candidate_complete", http.StatusOK, &pageVars{
		Title:        "Welcome — your invite is accepted",
		IdP:          idp,
		AllIdPs:      []config.IdP{idp},
		Email:        identity.Email(),
		IdentityID:   identity.ID,
		RecoveryLink: rl.RecoveryLink,
		RecoveryCode: rl.RecoveryCode,
		ExpiresAt:    rl.ExpiresAt.UTC().Format(time.RFC3339),
	})
}
