package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/kratos"
)

// Admin credential rotation (goat#320) — the console door onto ADR 1044's
// in-place re-issue, from the user detail page.
//
//	POST /users/{id}/rotate-credential — admin-gated, CSRF'd
//
// The console sends the TARGET's Kratos identity UUID (the page's own id,
// never a form field) and nothing else; the portal resolves which slot is
// theirs (POST /api/orgs/rotate, the collect trust boundary applied to
// rotation). Auth is the console's portal operator bearer plus
// X-Goat-Operator-Actor naming the signed-in admin, so the portal's
// vendor-rotate audit row attributes the re-key to the human.
//
// Deliberately NOT step-up-gated: rotate_credential is not in
// group-permissions.yml step_up_required (the documented "highest-blast
// five"), matching the fabric-credential mint precedent. Adding it is a
// one-line policy change plus a requireStepUp call — sequenced behind the
// goat#247 fix (PR #252), or enforced goatnets lose rotation to the
// unwinnable enroll-2FA interstitial. TestRotateCredential_UngatedByStepUpPolicyDecision
// pins this decision.
//
// The fresh api_key renders exactly once and is never logged or audited; the
// audit row carries the anchor, not the secret.

// rotateNonceTTL bounds the nonce store: a detail page left open longer than
// this must be reloaded before it can rotate. Expired entries are pruned on
// mint, so the map never outgrows the pages rendered in the last window.
const rotateNonceTTL = time.Hour

// mintRotateNonce issues a fresh one-time nonce for a rendered rotate form.
func (s *Server) mintRotateNonce() string {
	n := mintNonce()
	now := time.Now()
	s.rotateNonceMu.Lock()
	defer s.rotateNonceMu.Unlock()
	for k, exp := range s.rotateNonces {
		if now.After(exp) {
			delete(s.rotateNonces, k)
		}
	}
	s.rotateNonces[n] = now.Add(rotateNonceTTL)
	return n
}

// consumeRotateNonce spends a minted nonce exactly once. Unknown, expired,
// already-spent, or empty all answer false — fail closed: the cost of a
// wrong refusal is a page reload, the cost of a wrong acceptance is an
// unrecoverable re-key (goat#580).
func (s *Server) consumeRotateNonce(n string) bool {
	if n == "" {
		return false
	}
	s.rotateNonceMu.Lock()
	defer s.rotateNonceMu.Unlock()
	exp, ok := s.rotateNonces[n]
	if !ok {
		return false
	}
	delete(s.rotateNonces, n)
	return time.Now().Before(exp)
}

// handleUserRotateCredential re-keys the person's fabric credential in place.
func (s *Server) handleUserRotateCredential(w http.ResponseWriter, r *http.Request) {
	if err := s.validateCSRF(r); err != nil {
		s.renderError(w, r, 400, err.Error())
		return
	}

	// Fail closed on NotifyPortalURL BEFORE the portal mints anything: the
	// result page's bundle_url is only ever a path relative to the portal
	// (POST /api/orgs/rotate), and the fixed page copy ("the link works
	// once and expires") is only true once that's resolved to a fetchable
	// URL. Discovering the misconfiguration AFTER rotating would strand the
	// admin looking at a broken, unrecoverable one-time secret — same
	// fail-loud shape as the PortalInternalURL/PortalLookupToken check
	// below, just earlier, because this one can't be recovered from after
	// the fact. Checked before the nonce is spent so a misconfigured console
	// never burns a nonce on a request that could not have rotated anything.
	if strings.TrimSpace(s.Cfg.NotifyPortalURL) == "" {
		s.renderOperatorError(w, r, 503, "this console has no NOTIFY_PORTAL_URL configured, so it cannot show a "+
			"fetchable bundle link after rotating a credential. An admin sets NOTIFY_PORTAL_URL before this action is used.")
		return
	}

	// The nonce is spent BEFORE the portal is called, deliberately: spending it
	// after a successful rotation would leave the whole call open to the second
	// click. That means a rotation which FAILED also burns its nonce, so this
	// refusal must not claim the earlier submission rotated anything — on a
	// failed first attempt the person's existing credential is still the live
	// one, and telling an admin otherwise is wrong at the moment it matters.
	if !s.consumeRotateNonce(r.FormValue("rotate_nonce")) {
		s.renderError(w, r, http.StatusConflict,
			"this looks like a re-submitted rotate — this request rotated nothing. Whether the earlier one "+
				"did is answered by this person's audit trail: if it succeeded, its credential is now the live "+
				"one; if it failed, their existing credential still is. Either way, reload the person's page to "+
				"rotate afresh.")
		return
	}
	idp := idpFromCtx(r.Context())
	id := r.PathValue("id")
	cli := s.Kratos[idp.Key]

	// Fail closed on the target fetch, like reset-2FA: the audit row's whole
	// justification is attribution, so the portal is never asked to rotate a
	// credential for an identity the console cannot name.
	target, err := cli.GetIdentity(r.Context(), id)
	if err != nil {
		var httpErr *kratos.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
			s.renderError(w, r, 404, "identity not found")
			return
		}
		s.renderError(w, r, 502, fmt.Sprintf("Kratos get failed: %v", err))
		return
	}
	actor := actorFromCtx(r.Context())

	res, status, err := s.rotatePortalCredential(r.Context(), id, actor.Email)
	if err != nil {
		s.emitAudit(r, actor, "rotate_credential", target, idp.Key, nil, nil, false, "error", err.Error())
		if status == 0 {
			status = 502
		}
		s.renderOperatorError(w, r, status, err.Error())
		return
	}

	// Anchor + URI-SAN in the audit trail; the api_key deliberately absent —
	// secrets never land in the audit stream.
	s.emitAudit(r, actor, "rotate_credential", target, idp.Key, nil,
		map[string]interface{}{"anchor": res.SlotID, "alias": res.SystemHandle, "uri_san": res.URISAN},
		false, "ok", "")

	// bundleLink resolves the portal-relative bundle_url (e.g.
	// "/api/org-bundle/<anchor>/<token>.tar.gz") against NotifyPortalURL, the
	// goatnet's PUBLIC mesh portal host — the same base the onboarding welcome
	// email uses for the same shape of path. PortalInternalURL, which
	// rotatePortalCredential calls against above, is only the console's own
	// backend target and is not guaranteed to be what an admin's browser can
	// reach, so it must not be what gets shown on the page. The precondition
	// check at the top of this handler guarantees NotifyPortalURL is set here,
	// so this can't silently degrade to the bare unresolved path.
	s.render(w, r, "rotate_credential", &pageVars{
		Title:           "Credential rotated",
		RotatedForEmail: target.Email(),
		RotateAnchor:    res.SlotID,
		RotateHandle:    res.SystemHandle,
		RotateAPIKey:    res.APIKey,
		RotateBundleURL: s.bundleLink(res.BundleURL),
	})
}

// rotateResponse mirrors the portal's one-time hand-over (POST /api/orgs/rotate).
type rotateResponse struct {
	SlotID       string `json:"slot_id"`
	SystemHandle string `json:"system_handle"`
	APIKey       string `json:"api_key"`
	BundleURL    string `json:"bundle_url"`
	URISAN       string `json:"uri_san"`
}

// rotatePortalCredential asks the portal to re-key the person's credential in
// place. Errors come back as reader-facing sentences with the HTTP status the
// console should surface; the portal's own refusals are specific (no slot,
// nothing minted yet, duplicate linkage) and are passed through rather than
// flattened into a generic failure (the goat#214 defect class).
func (s *Server) rotatePortalCredential(ctx context.Context, identityID, actorEmail string) (*rotateResponse, int, error) {
	base := strings.TrimRight(strings.TrimSpace(s.Cfg.PortalInternalURL), "/")
	token := strings.TrimSpace(s.Cfg.PortalLookupToken)
	if base == "" || token == "" {
		return nil, 503, errors.New("this console is not wired to a portal, so it cannot rotate a fabric credential. " +
			"An admin sets PORTAL_INTERNAL_URL and PORTAL_LOOKUP_TOKEN")
	}
	body, err := json.Marshal(map[string]string{"identity_id": identityID})
	if err != nil {
		return nil, 500, errors.New("could not encode the request to the portal")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/orgs/rotate", bytes.NewReader(body))
	if err != nil {
		return nil, 500, errors.New("could not build the request to the portal")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Goat-Operator-Actor", actorEmail)

	resp, err := onboardHTTP.Do(req)
	if err != nil {
		return nil, 502, errors.New("the portal did not answer, so nothing was rotated — try again shortly")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var apiErr struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&apiErr)
		portalSaid := ""
		if decodeErr == nil && apiErr.Error.Message != "" && !strings.Contains(apiErr.Error.Message, "://") {
			portalSaid = apiErr.Error.Message
		}
		// A 401/403 on this route is the portal refusing the CONSOLE's bearer
		// (requireOperator); rotateByIdentity itself never answers with either.
		// Mirrored to a signed-in admin it reads as "you are not allowed to do
		// this", so it lands in the unwired-portal bucket above instead (#581).
		// The portal's own words are kept so "invalid token" and "operator-only
		// endpoint" (a vendor key where an operator key belongs) stay distinct.
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			detail := fmt.Sprintf("HTTP %d", resp.StatusCode)
			if portalSaid != "" {
				detail = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, portalSaid)
			}
			return nil, 503, fmt.Errorf("the portal refused this console's own credential (%s), so nothing was rotated. "+
				"An admin checks PORTAL_LOOKUP_TOKEN", detail)
		}
		if portalSaid != "" {
			return nil, resp.StatusCode, errors.New(portalSaid)
		}
		return nil, resp.StatusCode, fmt.Errorf("the portal refused the rotation (HTTP %d)", resp.StatusCode)
	}

	var res rotateResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, 502, errors.New("the portal answered unreadably; whether the credential rotated is unknown — check the portal audit trail before retrying")
	}
	return &res, 0, nil
}
