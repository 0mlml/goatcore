package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// SSH-key machine realm — the CLI half of "self-service submit, admin-gated
// install" (docs/design/ssh-key-self-service.md).
//
// Every route here is operator-only. A participant key must not be able to
// submit on someone else's behalf, and certainly not approve: approval is the
// gate that turns a public key into a shell on a production host.

// sshKeySubmitRequest is the wire shape for a submission. `pubkey` is the whole
// authorized_keys line — the same string a person copies out of their .pub —
// so nobody has to split it by hand and get the fields wrong.
type sshKeySubmitRequest struct {
	Handle  string `json:"handle"`
	Email   string `json:"email,omitempty"`
	Env     string `json:"env,omitempty"`
	Pubkey  string `json:"pubkey"`
	Note    string `json:"note,omitempty"`
	TTLDays int    `json:"ttl_days,omitempty"`
}

// sshKeySkipReportRequest is the installer skip-report payload. Closed enum
// for reason; env + groups are structured context only and never rendered
// as free text on the approval page.
type sshKeySkipReportRequest struct {
	Reason string   `json:"reason"`
	Env    string   `json:"env,omitempty"`
	Groups []string `json:"groups,omitempty"`
}

// skipReportGroupPattern bounds the structured group tokens the installer may
// attach to a skip report. They land only in the audit trail, but the audit
// log is a rendered surface too — refuse anything that isn't token-shaped
// rather than storing it.
var skipReportGroupPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:+-]{0,63}$`)

// maxSkipReportGroups caps the group list: a manifest carries a handful of
// roles, not hundreds.
const maxSkipReportGroups = 32

// apiSubmitSSHKey (POST /api/onboard/ssh-keys) records a public key for a
// handle in the `submitted` state. It GRANTS NOTHING — an admin still has to
// approve, and the installer only ever acts on approved rows.
//
// This is the "a fellow admin pushes it over for me" path: an operator can
// submit on someone's behalf when that person would rather send a key over
// chat than log into the console.
func (s *Server) apiSubmitSSHKey(w http.ResponseWriter, r *http.Request) {
	if !requireOperatorPrincipal(w, r) {
		return
	}
	var req sshKeySubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		onboardError(w, http.StatusBadRequest, "bad_request", `body must be {"handle":"…","pubkey":"ssh-… …"}`)
		return
	}
	if strings.TrimSpace(req.Handle) == "" || strings.TrimSpace(req.Pubkey) == "" {
		onboardError(w, http.StatusBadRequest, "bad_request", "handle and pubkey are both required")
		return
	}
	keyType, body, comment, err := onboardstore.ParseSSHPublicKey(req.Pubkey)
	if err != nil {
		// The parser's message names the specific problem (private key, wrong
		// type, wrapped paste) — pass it through rather than flattening it.
		onboardError(w, http.StatusBadRequest, "bad_pubkey", err.Error())
		return
	}
	// Trim + shape-check env at the boundary, like apiReportSSHKeySkip: the
	// installer's per-row env gate compares byte-for-byte, so an untrimmed
	// env would mint a row skipped env_mismatch on every run forever — while
	// the queue page's TrimSpace-based gate makes it LOOK consistent. Email
	// is the identity join key for the install panel gate and the legacy
	// backfill; trim it for the same reason.
	env := strings.TrimSpace(req.Env)
	if env != "" && !envPattern.MatchString(env) {
		onboardError(w, http.StatusBadRequest, "bad_request", `"env" must be a goatnet slug (lowercase letters, digits, hyphens)`)
		return
	}
	ttl := req.TTLDays
	if ttl == 0 {
		ttl = s.Cfg.SSHKeyTTLDays
	}
	k, err := s.Onboard.SubmitSSHKey(r.Context(), &onboardstore.SSHKey{
		Handle: strings.TrimSpace(req.Handle), Email: strings.TrimSpace(req.Email), Env: env,
		KeyType: keyType, KeyBody: body, Comment: comment, Note: req.Note,
	}, ttl)
	if err != nil {
		if errors.Is(err, onboardstore.ErrSSHKeyBadFormat) {
			onboardError(w, http.StatusBadRequest, "bad_pubkey", err.Error())
			return
		}
		onboardError(w, http.StatusInternalServerError, "submit_error", "could not record the key")
		return
	}
	s.emitAudit(r, Actor{Email: onboardAPIActor(r)}, "ssh_key_submit", nil, "", nil,
		map[string]interface{}{"handle": k.Handle, "fingerprint": k.Fingerprint, "id": k.ID},
		false, "ok", "")
	onboardJSON(w, http.StatusCreated, k)
}

// apiListSSHKeys (GET /api/onboard/ssh-keys?status=…) is what the installer and
// `goat admin ssh-key ls` read. Default is the admin's queue: what is waiting
// on a decision.
func (s *Server) apiListSSHKeys(w http.ResponseWriter, r *http.Request) {
	if !requireOperatorPrincipal(w, r) {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = onboardstore.SSHKeySubmitted
	}
	if status == "all" {
		status = ""
	}
	ks, err := s.Onboard.ListSSHKeys(r.Context(), status)
	if err != nil {
		onboardError(w, http.StatusInternalServerError, "list_error", "could not list ssh keys")
		return
	}
	if ks == nil {
		ks = []onboardstore.SSHKey{}
	}
	onboardJSON(w, http.StatusOK, ks)
}

// sshKeyDecision routes the four admin transitions through one guarded path so
// they cannot drift apart in how they audit or how they report a conflict.
func (s *Server) sshKeyDecision(w http.ResponseWriter, r *http.Request, action string) {
	if !requireOperatorPrincipal(w, r) {
		return
	}
	id := r.PathValue("id")
	actor := onboardAPIActor(r)
	switch action {
	case "approve", "reject", "revoke", "installed":
	default:
		onboardError(w, http.StatusBadRequest, "bad_request", "unknown action")
		return
	}
	// Handle and fingerprint never change with a decision, so read them
	// before the state change. The audit record then never depends on a
	// read-back that can fail after the change has already committed.
	before, err := s.Onboard.SSHKeyByID(r.Context(), id)
	switch {
	case errors.Is(err, onboardstore.ErrNotFound):
		onboardError(w, http.StatusNotFound, "not_found", "ssh key not found")
		return
	case err != nil:
		onboardError(w, http.StatusInternalServerError, "read_error", "could not load the key")
		return
	}
	switch action {
	case "approve":
		err = s.Onboard.ApproveSSHKey(r.Context(), id, actor)
	case "reject":
		err = s.Onboard.RejectSSHKey(r.Context(), id, actor)
	case "revoke":
		err = s.Onboard.RevokeSSHKey(r.Context(), id, actor)
	case "installed":
		err = s.Onboard.MarkSSHKeyInstalled(r.Context(), id, actor)
	}
	switch {
	case errors.Is(err, onboardstore.ErrNotFound):
		onboardError(w, http.StatusNotFound, "not_found", "ssh key not found")
		return
	case errors.Is(err, onboardstore.ErrSSHKeyWrongState):
		// 409: the request is well-formed, the STATE refuses it. Naming the
		// current state saves the caller a second round-trip to find out why.
		cur := "unknown"
		if k, gerr := s.Onboard.SSHKeyByID(r.Context(), id); gerr == nil {
			cur = k.Status
		}
		onboardError(w, http.StatusConflict, "wrong_state",
			"cannot "+action+" a key that is currently "+cur)
		return
	case err != nil:
		onboardError(w, http.StatusInternalServerError, action+"_error", "could not "+action+" the key")
		return
	}
	s.emitAudit(r, Actor{Email: actor}, "ssh_key_"+action, nil, "", nil,
		map[string]interface{}{"id": id, "handle": before.Handle, "fingerprint": before.Fingerprint},
		false, "ok", "")
	k, err := s.Onboard.SSHKeyByID(r.Context(), id)
	if err != nil {
		onboardError(w, http.StatusInternalServerError, "read_after_write_error",
			"the decision was saved, but the key's updated record could not be loaded")
		return
	}
	onboardJSON(w, http.StatusOK, k)
}

func (s *Server) apiApproveSSHKey(w http.ResponseWriter, r *http.Request) {
	s.sshKeyDecision(w, r, "approve")
}
func (s *Server) apiRejectSSHKey(w http.ResponseWriter, r *http.Request) {
	s.sshKeyDecision(w, r, "reject")
}
func (s *Server) apiRevokeSSHKey(w http.ResponseWriter, r *http.Request) {
	s.sshKeyDecision(w, r, "revoke")
}
func (s *Server) apiMarkSSHKeyInstalled(w http.ResponseWriter, r *http.Request) {
	s.sshKeyDecision(w, r, "installed")
}

// apiReportSSHKeySkip (POST /api/onboard/ssh-keys/{id}/skip-report) records a
// display-only skip annotation when the installer skips an approved key
// (no manifest, or role resolves to no host). Auth matches the installed
// endpoint: onboardAPIGate + operator-only principal.
func (s *Server) apiReportSSHKeySkip(w http.ResponseWriter, r *http.Request) {
	if !requireOperatorPrincipal(w, r) {
		return
	}
	var req sshKeySkipReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		onboardError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason != "no_manifest" && reason != "no_scope" && reason != "env_mismatch" {
		onboardError(w, http.StatusBadRequest, "bad_request", `"reason" must be "no_manifest", "no_scope", or "env_mismatch"`)
		return
	}
	// The context fields are audit-only, but hold them to the same
	// structured-fields-only bar as the reason: shapes, not prose.
	if req.Env != "" && !envPattern.MatchString(req.Env) {
		onboardError(w, http.StatusBadRequest, "bad_request", `"env" must be a goatnet slug (lowercase letters, digits, hyphens)`)
		return
	}
	if len(req.Groups) > maxSkipReportGroups {
		onboardError(w, http.StatusBadRequest, "bad_request", `"groups" lists more entries than any manifest carries`)
		return
	}
	for _, g := range req.Groups {
		if !skipReportGroupPattern.MatchString(g) {
			onboardError(w, http.StatusBadRequest, "bad_request", `"groups" entries must be role tokens`)
			return
		}
	}
	id := r.PathValue("id")
	actor := onboardAPIActor(r)
	err := s.Onboard.RecordSSHKeySkip(r.Context(), id, actor, reason)
	switch {
	case errors.Is(err, onboardstore.ErrNotFound):
		onboardError(w, http.StatusNotFound, "not_found", "ssh key not found")
		return
	case errors.Is(err, onboardstore.ErrSSHKeyWrongState):
		// Same 409 semantics as the installed endpoint: the request is
		// well-formed, the STATE refuses it. Name the current state.
		cur := "unknown"
		if k, gerr := s.Onboard.SSHKeyByID(r.Context(), id); gerr == nil {
			cur = k.Status
		}
		onboardError(w, http.StatusConflict, "wrong_state",
			"cannot report skip for a key that is currently "+cur)
		return
	case err != nil:
		onboardError(w, http.StatusInternalServerError, "skip_error", "could not record skip annotation")
		return
	}
	k, gerr := s.Onboard.SSHKeyByID(r.Context(), id)
	detail := map[string]interface{}{"id": id, "reason": reason}
	if gerr == nil && k != nil {
		detail["handle"] = k.Handle
		detail["fingerprint"] = k.Fingerprint
	}
	if req.Env != "" {
		detail["env"] = req.Env
	}
	if len(req.Groups) > 0 {
		detail["groups"] = req.Groups
	}
	s.emitAudit(r, Actor{Email: actor}, "ssh_key_skip_reported", nil, "", nil,
		detail, false, "ok", "")
	if gerr == nil && k != nil {
		onboardJSON(w, http.StatusOK, k)
		return
	}
	onboardJSON(w, http.StatusOK, map[string]interface{}{"id": id})
}
