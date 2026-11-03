package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/kanidmdoor"
)

// Self-service sign-on setup codes for the two Kanidm doors — the collaboration
// space and the public dashboards (docs/design/self-service-door-codes.md).
//
//	GET  /setup-code   — which doors this person has an account on
//	POST /setup-code   — mints a fresh code for one of them and shows it once
//
// THE PROBLEM THIS EXISTS FOR. A Kanidm credential-setup code is single-use and
// lives 24 hours, and that ceiling cannot be raised: it is a server constant,
// with no account-policy knob (measured a 7-day request came back
// as 24.00 h exactly). Kanidm also has no user-initiated recovery: /ui/forgot
// is a 404 and `send-reset-token` answers 404 on this server version. So the
// only route back in for somebody whose code lapsed was to email an operator
// and wait — which is exactly how 100 people across two doors ended up unable
// to sign in without anyone noticing.
//
// Self-service does not lengthen the window. It makes the window stop
// mattering: a fresh code is five seconds away from a page you are already
// signed in to.
//
// THE AUTHORIZATION MODEL, stated plainly because it is the whole thing. The
// binding between "the person on this page" and "the account at the door" is
// the EMAIL ADDRESS, and it is taken from the mesh session — never from a form
// field. The requester has already authenticated to the mesh Ory plane; the
// door account carries a `mail` attribute; when those match, the requester owns
// that account. A form-supplied address would let anyone with a console session
// mint a credential-reset code for somebody else's collab account, which is an
// account-takeover path with a nice UI on it. Same rule as /fabric-credential
// and /ssh-key, for the same reason.
//
// This deliberately DOES allow a reset for an account that already has a
// credential. That is not the account-takeover case the provisioner refuses
// (docs/design/collab-onboarding-provisioner.md) — there, the console mints an
// emailed link for someone who never asked. Here the account owner asked, in
// person, through their own authenticated session. Refusing them would leave
// "I forgot my collab password" with no answer at all, which is the gap.
//
// The code is rendered ON THE PAGE and never emailed. That sidesteps both mail
// failures at once: a scanner cannot spend what was never mailed, and
// there is no tokenised URL to jam the reset form.

// setupCodeDoor is one door as the page presents it.
type setupCodeDoor struct {
	Plane      string // "collab" | "public"
	Label      string // what a reader calls it
	Handle     string // their sign-in name at that door
	HasCred    bool   // they have completed setup before (so this is a reset)
	Configured bool
}

// handleSetupCodeSelf renders which doors this person can mint a code for.
func (s *Server) handleSetupCodeSelf(w http.ResponseWriter, r *http.Request) {
	s.renderSetupCodeSelf(w, r, nil)
}

// handleSetupCodeMint issues a code for one door and shows it once.
func (s *Server) handleSetupCodeMint(w http.ResponseWriter, r *http.Request) {
	if err := s.validateCSRF(r); err != nil {
		s.renderError(w, r, http.StatusForbidden, err.Error())
		return
	}
	actor := actorFromCtx(r.Context())
	email := strings.TrimSpace(actor.Email)
	if email == "" {
		s.renderError(w, r, http.StatusUnauthorized, "no signed-in identity on this request")
		return
	}
	plane := strings.TrimSpace(r.FormValue("plane"))
	cl, label := s.doorClient(plane)
	if cl == nil {
		s.renderSetupCodeSelf(w, r, &flash{Level: "error",
			Message: "that sign-on door is not configured on this goatnet."})
		return
	}

	// Resolve the account from the SESSION email. The form carries which door,
	// never which account — see the package comment.
	acct, err := cl.FindByEmail(r.Context(), email)
	if err != nil {
		s.renderSetupCodeSelf(w, r, &flash{Level: "error", Message: err.Error()})
		return
	}
	if acct == nil {
		s.renderSetupCodeSelf(w, r, &flash{Level: "error",
			Message: "there is no " + label + " account for your address, so there is nothing to reset. " +
				"Ask an admin to create one — this page can only reset an account you already have."})
		return
	}

	code, err := cl.MintCode(r.Context(), acct.Handle, 24*time.Hour)
	if err != nil {
		s.renderSetupCodeSelf(w, r, &flash{Level: "error", Message: err.Error()})
		return
	}

	// Audit the mint, never the code. `reset` distinguishes a genuine
	// forgot-password from a first-time setup, which is the line an auditor
	// cares about.
	s.emitAudit(r, actor, "door_setup_code_mint", nil, "", nil,
		map[string]interface{}{"plane": plane, "handle": acct.Handle,
			"reset": acct.HasCredential, "via": "console-self"}, false, "ok", "")

	doors := s.setupCodeDoors(r, email)
	s.render(w, r, "setup_code", &pageVars{
		Title:              "Your sign-on setup code",
		SetupCodeDoors:     doors,
		SetupCodeIssued:    true,
		SetupCodeIsReset:   acct.HasCredential,
		SetupCodePlane:     plane,
		SetupCodeLabel:     label,
		SetupCodeHandle:    code.Handle,
		SetupCodeValue:     code.Code,
		SetupCodeResetPage: code.ResetPage,
		SetupCodeExpires:   code.ExpiresAt.Format("2006-01-02 15:04 UTC"),
	})
}

func (s *Server) renderSetupCodeSelf(w http.ResponseWriter, r *http.Request, f *flash) {
	actor := actorFromCtx(r.Context())
	email := strings.TrimSpace(actor.Email)
	if email == "" {
		s.renderError(w, r, http.StatusUnauthorized, "no signed-in identity on this request")
		return
	}
	s.render(w, r, "setup_code", &pageVars{
		Title:          "Your sign-on setup code",
		SetupCodeDoors: s.setupCodeDoors(r, email),
		Flashes:        flashes(f),
	})
}

// doorClient maps a plane name to its client and reader-facing label. Returns
// nil when that door is not configured on this goatnet — most goatnets have no
// collab plane, and the page must say so rather than offering a dead button.
func (s *Server) doorClient(plane string) (*kanidmdoor.Client, string) {
	switch plane {
	case string(kanidmdoor.PlaneCollab):
		if s.CollabDoor.Configured() {
			return s.CollabDoor, "collaboration space"
		}
	case string(kanidmdoor.PlanePublic):
		if s.PublicDoor.Configured() {
			return s.PublicDoor, "team dashboards"
		}
	}
	return nil, ""
}

// setupCodeDoors reports, per door, whether this person has an account there.
//
// A door that is configured but that this person has no account on is listed
// with an empty Handle rather than hidden — "you have no collab account" is a
// different and more useful answer than the row silently not existing, and it
// is the one real case behind the collab-but-not-mesh population.
func (s *Server) setupCodeDoors(r *http.Request, email string) []setupCodeDoor {
	out := []setupCodeDoor{}
	for _, d := range []struct {
		plane string
		label string
		cl    *kanidmdoor.Client
	}{
		{string(kanidmdoor.PlaneCollab), "collaboration space (chat, docs, files)", s.CollabDoor},
		{string(kanidmdoor.PlanePublic), "team dashboards (relay health, consistency)", s.PublicDoor},
	} {
		if !d.cl.Configured() {
			continue
		}
		row := setupCodeDoor{Plane: d.plane, Label: d.label, Configured: true}
		// A door that is briefly unreachable must not render as "you have no
		// account" — that reads as a permissions answer and sends the person to
		// an admin for nothing. Leave the handle empty and let the mint attempt
		// produce the specific error.
		if acct, err := d.cl.FindByEmail(r.Context(), email); err == nil && acct != nil {
			row.Handle = acct.Handle
			row.HasCred = acct.HasCredential
		}
		out = append(out, row)
	}
	return out
}
