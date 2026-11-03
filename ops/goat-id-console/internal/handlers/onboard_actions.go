package handlers

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// The follow-up action queue (ADR 1175). The proposals queue carries a person
// to `onboarded` and stops; the runner never looks at them again. A lost phone
// needs one more mint for a person the runner has finished with, so the console
// queues an ACTION and the runner claims it exactly once — the same claim /
// result shape as a proposal, on a separate table (onboard_actions).

// actionFlashes are the four outcomes of a queue click, per action kind.
type actionFlashes struct{ queued, pending, state, failed string }

// handleHandsetKeyReissue is the browser door: the operator's button on the
// onboarded panel. It queues a reissue_handset action; the runner does the
// revoke + mint and the console re-sends the welcome package on the result.
// Admin-gated by the route; CSRF-checked here.
func (s *Server) handleHandsetKeyReissue(w http.ResponseWriter, r *http.Request) {
	s.queueFollowUp(w, r, onboardstore.ActionReissueHandset, "",
		actionFlashes{flashHandsetQueued, flashHandsetPending, flashHandsetState, flashHandsetFailed})
}

// defaultDeviceKeyUses is the reuse count a device-key reissue gets when the
// operator does not choose one — the same default the first mint uses
// (`netbird.usage_limit` in onboard-intake-form.json, F-345).
const defaultDeviceKeyUses = 5

// maxDeviceKeyUses matches the manifest schema's `netbird.usage_limit` maximum.
const maxDeviceKeyUses = 100

// parseDeviceKeyUses reads the operator's reuse count; blank means the default.
func parseDeviceKeyUses(v string) (int, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return defaultDeviceKeyUses, true
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > maxDeviceKeyUses {
		return 0, false
	}
	return n, true
}

// handleDeviceKeyReissue queues a reissue_device action: a fresh device setup
// key with the chosen reuse count, for a person whose key expired or was used
// up. The runner revokes any of their device keys still live, mints the new
// one, and the console stores it and re-sends the welcome package. The console
// audit event names the operator who clicked; the goat audit event the runner's
// mint emits carries the same operator as requested_by. Admin-gated by the route.
func (s *Server) handleDeviceKeyReissue(w http.ResponseWriter, r *http.Request) {
	uses, ok := parseDeviceKeyUses(r.FormValue("uses"))
	if !ok {
		q := url.Values{}
		q.Set("flash", flashDeviceKeyUses)
		http.Redirect(w, r, "/onboarding?"+q.Encode(), http.StatusSeeOther)
		return
	}
	params, err := json.Marshal(map[string]int{"uses": uses})
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "could not encode the device-key request")
		return
	}
	s.queueFollowUp(w, r, onboardstore.ActionReissueDevice, string(params),
		actionFlashes{flashDeviceKeyQueued, flashDeviceKeyPending, flashDeviceKeyState, flashDeviceKeyFailed})
}

// queueFollowUp enqueues one follow-up action for the proposal in the path,
// records the console audit event naming the operator, and redirects with the
// outcome's flash.
func (s *Server) queueFollowUp(w http.ResponseWriter, r *http.Request, kind, params string, f actionFlashes) {
	if s.Onboard == nil {
		s.renderError(w, r, 503, "onboarding is not configured")
		return
	}
	if err := s.validateCSRF(r); err != nil {
		s.renderError(w, r, http.StatusForbidden, err.Error())
		return
	}
	id := r.PathValue("id")
	actor := actorFromCtx(r.Context())
	a, err := s.Onboard.EnqueueActionWithParams(r.Context(), id, kind, actor.Email, params)
	code := f.queued
	outcome, errMsg := "ok", ""
	switch {
	case err == nil:
	case errors.Is(err, onboardstore.ErrActionPending):
		code, outcome = f.pending, "refused"
		errMsg = err.Error()
	case errors.Is(err, onboardstore.ErrActionNotOnboarded), errors.Is(err, onboardstore.ErrNotFound):
		code, outcome = f.state, "refused"
		errMsg = err.Error()
	default:
		code, outcome = f.failed, "error"
		errMsg = err.Error()
	}
	details := map[string]interface{}{"kind": kind}
	if params != "" {
		details["params"] = params
	}
	if a != nil {
		details["action_id"] = a.ID
		details["handle"] = a.Handle
		details["env"] = a.Env
	}
	s.emitAuditWithInviteToken(r, actor, "onboard_action_enqueue", id, "", nil, details, false, outcome, errMsg)
	q := url.Values{}
	q.Set("flash", code)
	http.Redirect(w, r, "/onboarding?"+q.Encode(), http.StatusSeeOther)
}

// apiListActions returns actions, filtered by ?status= (the runner asks for
// queued). Machine-gated like the proposals list.
func (s *Server) apiListActions(w http.ResponseWriter, r *http.Request) {
	if !requireOperatorPrincipal(w, r) {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	as, err := s.Onboard.ListActions(r.Context(), status)
	if err != nil {
		onboardError(w, http.StatusInternalServerError, "list_error", "could not list actions")
		return
	}
	if as == nil {
		as = []onboardstore.Action{}
	}
	onboardJSON(w, http.StatusOK, as)
}

// apiClaimAction atomically flips queued → claimed for the calling runner.
// 409 when it is no longer queued (another runner won, or it already ran).
func (s *Server) apiClaimAction(w http.ResponseWriter, r *http.Request) {
	if !requireOperatorPrincipal(w, r) {
		return
	}
	id := r.PathValue("id")
	runnerID := onboardPrincipalFromCtx(r.Context()).Handle
	if err := s.Onboard.ClaimAction(r.Context(), id, runnerID); err != nil {
		switch {
		case errors.Is(err, onboardstore.ErrNotFound):
			onboardError(w, http.StatusNotFound, "not_found", "action not found")
		case errors.Is(err, onboardstore.ErrActionNotQueued):
			onboardError(w, http.StatusConflict, "not_queued", "action is not queued — already claimed or finished")
		default:
			onboardError(w, http.StatusInternalServerError, "claim_error", "could not claim action")
		}
		return
	}
	a, err := s.Onboard.GetAction(r.Context(), id)
	if err != nil {
		onboardError(w, http.StatusInternalServerError, "get_error", "could not load action")
		return
	}
	onboardJSON(w, http.StatusOK, a)
}

type actionResultRequest struct {
	Status     string `json:"status"` // done | failed
	Error      string `json:"error,omitempty"`
	ClaimToken string `json:"claim_token"`
	// HandsetKey is the fresh key a reissue_handset produced. Persisted on the
	// proposal (never on the action row) and re-sent in the welcome package.
	HandsetKey string `json:"handset_key,omitempty"`
	// SetupKey is the fresh device key a reissue_device produced. Same
	// handling: stored on the proposal, re-sent, never on the action row.
	SetupKey string `json:"setup_key,omitempty"`
	Release  *struct {
		Outcome      string `json:"outcome"`
		Detail       string `json:"detail,omitempty"`
		AuditOutcome string `json:"audit_outcome"`
		AuditError   string `json:"audit_error,omitempty"`
	} `json:"release,omitempty"`
}

// apiActionResult records a claimed action's outcome. For reissue_handset with
// status done and a key, the key is stored on the proposal and the welcome
// package is re-sent; the delivery outcome is recorded in the action's result
// so an operator can see "minted, email skipped" rather than guess.
func (s *Server) apiActionResult(w http.ResponseWriter, r *http.Request) {
	if !requireOperatorPrincipal(w, r) {
		return
	}
	id := r.PathValue("id")
	var req actionResultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		onboardError(w, http.StatusBadRequest, "bad_json", "invalid request body")
		return
	}
	if req.Status != onboardstore.ActionDone && req.Status != onboardstore.ActionFailed {
		onboardError(w, http.StatusBadRequest, "bad_status", `status must be "done" or "failed"`)
		return
	}
	a, err := s.Onboard.GetAction(r.Context(), id)
	if errors.Is(err, onboardstore.ErrNotFound) {
		onboardError(w, http.StatusNotFound, "not_found", "action not found")
		return
	}
	if err != nil {
		onboardError(w, http.StatusInternalServerError, "get_error", "could not load action")
		return
	}
	if a.Status != onboardstore.ActionClaimed {
		onboardError(w, http.StatusConflict, "not_claimed", "action is "+a.Status+", not claimed — cannot record a result")
		return
	}
	legacyReissue := req.ClaimToken == "" &&
		(a.Kind == onboardstore.ActionReissueHandset || a.Kind == onboardstore.ActionReissueDevice)
	if !legacyReissue && (req.ClaimToken == "" || subtle.ConstantTimeCompare([]byte(req.ClaimToken), []byte(a.ClaimToken)) != 1) {
		onboardError(w, http.StatusConflict, "not_claimed", "action claim has expired or belongs to another runner")
		return
	}
	result := map[string]any{}
	status := req.Status
	var setupKey, handsetKey string
	var releaseParams releaseHoldParams
	if a.Kind == onboardstore.ActionReleaseHold {
		if req.Release == nil || json.Unmarshal([]byte(a.Params), &releaseParams) != nil {
			onboardError(w, http.StatusBadRequest, "bad_release_result", "release result or action parameters are invalid")
			return
		}
		validOutcome := req.Release.Outcome == "released" || req.Release.Outcome == "already_released" || req.Release.Outcome == "refused" || req.Release.Outcome == "retryable_partial"
		validAudit := req.Release.AuditOutcome == "recorded" || (req.Release.AuditOutcome == "failed" && strings.TrimSpace(req.Release.AuditError) != "")
		success := req.Release.Outcome == "released" || req.Release.Outcome == "already_released"
		if !validOutcome || !validAudit || (success != (status == onboardstore.ActionDone)) {
			onboardError(w, http.StatusBadRequest, "bad_release_result", "release outcome does not match action status")
			return
		}
		result["release"] = req.Release
	}
	if status == onboardstore.ActionDone {
		switch a.Kind {
		case onboardstore.ActionReissueHandset:
			handsetKey = strings.TrimSpace(req.HandsetKey)
			if handsetKey == "" {
				status = onboardstore.ActionFailed
				req.Error = "runner reported done without a handset key"
			}
		case onboardstore.ActionReissueDevice:
			setupKey = strings.TrimSpace(req.SetupKey)
			if setupKey == "" {
				status = onboardstore.ActionFailed
				req.Error = "runner reported done without a setup key"
			}
		}
	}
	if setupKey != "" {
		result["setup_key_set"] = true
	}
	if handsetKey != "" {
		result["handset_key_set"] = true
	}
	rj, err := json.Marshal(result)
	if err != nil {
		onboardError(w, http.StatusInternalServerError, "result_error", "could not encode the action result")
		return
	}
	if a.Kind == onboardstore.ActionReleaseHold {
		enteredAt, parseErr := time.Parse(time.RFC3339, releaseParams.HoldEnteredAt)
		if parseErr != nil {
			onboardError(w, http.StatusBadRequest, "bad_release_result", "release hold identity is invalid")
			return
		}
		closeRequest := req.Release.Outcome == "released" || req.Release.Outcome == "already_released"
		if err := s.Onboard.SetReleaseActionResult(r.Context(), id, req.ClaimToken, status, string(rj), req.Error, releaseParams.PeerID, enteredAt, a.RequestedBy, closeRequest); err != nil {
			if errors.Is(err, onboardstore.ErrActionNotClaimed) {
				onboardError(w, http.StatusConflict, "not_claimed", "action is no longer claimed")
			} else {
				onboardError(w, http.StatusInternalServerError, "status_error", "could not record the result")
			}
			return
		}
		outcome := "ok"
		if status == onboardstore.ActionFailed {
			outcome = "refused"
			if req.Release.Outcome == "retryable_partial" {
				outcome = "error"
			}
		}
		s.emitAudit(r, Actor{Email: a.RequestedBy, AAL: aalLabel(releaseParams.StepUpVerified)}, "release_hold_result", nil, releaseParams.Site, nil,
			map[string]interface{}{"peer_id": releaseParams.PeerID, "hold_entered_at": releaseParams.HoldEnteredAt, "prior_groups": releaseParams.PriorGroups, "runner_outcome": req.Release.Outcome, "operator_audit": req.Release.AuditOutcome, "operator_audit_error": req.Release.AuditError},
			releaseParams.StepUpVerified, outcome, req.Error)
	} else {
		p, err := s.Onboard.BeginReissueDelivery(r.Context(), id, onboardPrincipalFromCtx(r.Context()).Handle, status, string(rj), req.Error, setupKey, handsetKey)
		if err != nil {
			switch {
			case errors.Is(err, onboardstore.ErrActionNotClaimed):
				onboardError(w, http.StatusConflict, "not_claimed", "action is no longer claimed")
			case errors.Is(err, onboardstore.ErrActionNotOnboarded):
				onboardError(w, http.StatusConflict, "not_onboarded", "replacement refused: offboarding has closed eligibility")
			default:
				onboardError(w, http.StatusInternalServerError, "status_error", "could not record the result")
			}
			return
		}
		if status == onboardstore.ActionDone {
			outcome, msg := s.resendWelcomePackage(r, p, p.SetupKey, p.HandsetKey, Actor{Email: a.RequestedBy})
			result["welcome_resend"] = outcome
			if outcome != "ok" {
				result["welcome_resend_error"] = msg
			}
			rj, err = json.Marshal(result)
			if err != nil {
				onboardError(w, http.StatusInternalServerError, "result_error", "could not encode the delivery result")
				return
			}
			if err := s.Onboard.RecordActionDelivery(r.Context(), id, string(rj)); err != nil {
				onboardError(w, http.StatusInternalServerError, "status_error", "key stored but delivery outcome could not be recorded")
				return
			}
		}
	}
	onboardJSON(w, http.StatusOK, map[string]any{"id": id, "status": status, "result": result})
}
