package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Fabric-credential collection — the browser half of mint-on-collect
// (docs/design/fabric-credential-collection.md).
//
//	GET  /fabric-credential   — any signed-in person, their OWN credential
//	POST /fabric-credential   — mints it and shows it once
//
// Self-service, like /ssh-key and /operator-key, and for the same reason: the
// people who need it are ordinary members with no admin rights. It is wrapped
// in selfServiceAuth, which identifies without authorizing (F-309 — a route
// registered bare reads the zero Actor and refuses everyone).
//
// The person never names their slot. The console sends the collector's KRATOS
// IDENTITY and nothing else, and the portal resolves what is theirs. Two
// consequences worth stating: nobody can collect against someone else's anchor
// by asking, and the identity is resolved from Kratos by the signed-in email
// rather than read off a header, so it does not depend on which claim an IdP
// happens to map into X-Forwarded-Preferred-Username.
//
// There is deliberately no "do you have one yet?" pre-check. The portal has no
// read side for collectability, and inventing one here would mean the page
// rendering a guess it cannot stand behind. The answer to "can I collect" is
// the collect attempt, whose refusals are specific.

// handleFabricCredentialSelf renders the explain-and-offer page.
func (s *Server) handleFabricCredentialSelf(w http.ResponseWriter, r *http.Request) {
	s.renderFabricCredSelf(w, r, nil)
}

// handleFabricCredentialCollect mints the credential and shows it once.
func (s *Server) handleFabricCredentialCollect(w http.ResponseWriter, r *http.Request) {
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

	// Resolve the Kratos identity from the SESSION's email — never from a form
	// field, the same rule the SSH-key and operator-key pages enforce. A
	// form-supplied identity would let anyone with a console session collect
	// somebody else's fabric credential, which is the whole ballgame.
	identityID := s.kratosIdentityIDForEmail(r.Context(), email)
	if identityID == "" {
		s.renderFabricCredSelf(w, r, &flash{Level: "error",
			Message: "could not resolve your identity in the directory, so there is nothing to collect against. " +
				"This is usually two accounts sharing your address — an admin can tell you which."})
		return
	}

	res, err := s.collectFabricCredential(r.Context(), identityID, email)
	if err != nil {
		s.renderFabricCredSelf(w, r, &flash{Level: "error", Message: err.Error()})
		return
	}

	s.emitAudit(r, actor, "fabric_credential_collect", nil, "", nil,
		map[string]interface{}{"anchor": res.SlotID, "alias": res.SystemHandle,
			"identity_id": identityID, "via": "console-self"}, false, "ok", "")

	s.render(w, r, "fabric_credential", &pageVars{
		Title:                "Your fabric credential",
		FabricCredSelf:       true,
		FabricCredAnchor:     res.SlotID,
		FabricCredHandle:     res.SystemHandle,
		FabricCredAPIKey:     res.APIKey,
		FabricCredBundle:     res.BundleURL,
		FabricCredBundleLink: strings.HasPrefix(res.BundleURL, "https://") || strings.HasPrefix(res.BundleURL, "http://"),
		FabricCredURISAN:     res.URISAN,
		FabricCredCollected:  true,
	})
}

func (s *Server) renderFabricCredSelf(w http.ResponseWriter, r *http.Request, f *flash) {
	actor := actorFromCtx(r.Context())
	if strings.TrimSpace(actor.Email) == "" {
		s.renderError(w, r, http.StatusUnauthorized, "no signed-in identity on this request")
		return
	}
	s.render(w, r, "fabric_credential", &pageVars{
		Title:          "Your fabric credential",
		FabricCredSelf: true,
		Flashes:        flashes(f),
	})
}

// --- portal call ------------------------------------------------------------

// fabricCollectResponse mirrors the portal's one-time hand-over
// (POST /api/orgs/collect).
type fabricCollectResponse struct {
	SlotID       string `json:"slot_id"`
	SystemHandle string `json:"system_handle"`
	APIKey       string `json:"api_key"`
	BundleURL    string `json:"bundle_url"`
	URISAN       string `json:"uri_san"`
}

// collectFabricCredential asks the portal to mint this identity's fabric
// credential. Errors are returned as reader-facing sentences, because every one
// of them is something the person on the page has to act on.
//
// Auth: the console's portal bearer, plus X-Goat-Operator-Actor naming the
// person — so the portal's audit row attributes the mint to whoever collected
// it rather than to the shared console credential.
func (s *Server) collectFabricCredential(ctx context.Context, identityID, actorEmail string) (*fabricCollectResponse, error) {
	base := strings.TrimRight(strings.TrimSpace(s.Cfg.PortalInternalURL), "/")
	token := strings.TrimSpace(s.Cfg.PortalLookupToken)
	if base == "" || token == "" {
		return nil, errors.New("this console is not wired to a portal, so it cannot mint a fabric credential; " +
			"an admin sets PORTAL_INTERNAL_URL and PORTAL_LOOKUP_TOKEN")
	}
	body, err := json.Marshal(map[string]string{"identity_id": identityID})
	_ = err // map[string]string is always JSON-marshalable; encode failure is not expected here.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/orgs/collect", bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("could not build the request to the portal")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Goat-Operator-Actor", actorEmail)

	resp, err := onboardHTTP.Do(req)
	if err != nil {
		return nil, errors.New("the portal did not answer, so nothing was minted — try again shortly")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		var out fabricCollectResponse
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			// The mint happened; we just cannot show it. Say so plainly rather
			// than offering a retry that will now 409 — that would read as
			// "collect again" when the credential is already spent.
			return nil, errors.New("the portal minted your credential but the response could not be read, " +
				"so it cannot be shown; it is spent, so ask an admin to re-issue it rather than trying again")
		}
		return &out, nil
	}

	var errEnv struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&errEnv) //nolint:errcheck // refusal details are optional; status still determines the error
	return nil, fabricCollectMessage(resp.StatusCode, errEnv.Error.Code, errEnv.Error.Message)
}

// fabricCollectMessage turns a portal refusal into something the reader can act
// on. Each case names what is true and who can change it; a bare "409" on a
// page whose entire job is handing over a credential would send them to an
// admin with nothing useful to say.
func fabricCollectMessage(status int, code, msg string) error {
	switch code {
	case "already_collected":
		return errors.New("you already collected this, and it was shown once — the key cannot be read again by anyone, " +
			"including an admin; if you lost it, ask an admin to re-issue it — that deliberately invalidates the old one")
	case "already_minted":
		return errors.New("your fabric credential was minted when you were onboarded and sent to you then — " +
			"check your welcome email and your bundle; collecting now would invalidate the copy you already have, " +
			"so if it never arrived, ask an admin to re-issue it")
	case "no_slot":
		return errors.New("no fabric slot is recorded for you on this goatnet, so there is nothing to collect — " +
			"that is normal if you were onboarded for mesh and login only; an admin can tell you which you have")
	case "ambiguous_identity":
		return errors.New("more than one fabric slot is recorded against your identity, so this will not guess which is yours; " +
			"an admin needs to fix the duplicate before you can collect")
	case "no_org_row":
		return errors.New("your slot exists but your onboarding did not finish, so there is nothing to mint against; " +
			"an admin needs to complete it")
	case "not_slot_mode":
		return errors.New("this goatnet does not issue per-person fabric slots, so there is nothing to collect here")
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return errors.New("this console was refused by the portal, which is a configuration problem rather than " +
			"anything about your account; an admin should check the console's portal credential")
	}
	if strings.TrimSpace(msg) != "" {
		return fmt.Errorf("the portal refused: %s", msg)
	}
	return fmt.Errorf("the portal refused with status %d and no reason", status)
}
