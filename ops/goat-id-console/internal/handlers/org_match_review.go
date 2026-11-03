// Org match-or-create at the review gates (ADR 1112 increment 2, design doc
// §6/§7). The enrollee's free-typed organization becomes affiliation.id by
// slugification — the silent-duplicate mint point. When ORG_MATCH_ENABLED,
// the review page shows ranked candidates from the org registry beside each
// pending proposal, and the ADMIN either ATTACHES the proposal to an
// existing org (manifest affiliation.id rewritten to the canonical id, the
// typed form accreted as an alias) or CREATES the org deliberately (negative
// pairs recorded against the offered candidates so the matcher never drifts
// toward re-suggesting a pairing a human already rejected).
//
// Load-bearing safety posture (operator-directed): similar
// spelling is NOT the same org. Nothing here ever auto-attaches — an
// exact-band candidate is still one deliberate click; a high-band candidate
// is offered, never preselected; every candidate renders its distinguishing
// facts (nation/kind/parent/display name); create-new is always a
// first-class action, never buried behind a "no match" state.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/coalition"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/orgstore"
)

// orgMatchActive reports whether the affordance is on and wired.
//
// Cfg is nil-guarded: it is a *config.Config and tests construct a Server
// without one. Before #503 every caller sat behind a route handler, where Cfg
// is always populated, so the deref was safe by position rather than by
// construction. #503 added a call from applyFieldEdits -- reached directly by
// unit tests -- and TestApplyFieldEdits_unregisteredOrgRefused panicked. A nil
// Cfg means "not configured", which is correctly not-active.
func (s *Server) orgMatchActive() bool {
	return s.Cfg != nil && s.Cfg.OrgMatchEnabled && s.Orgs != nil
}

// manifestOrgFacts pulls the org-relevant fields out of a proposal manifest.
type manifestOrgFacts struct {
	AffID  string
	Kind   string
	Nation string
	// ADR 1151 target block, carried from the manifest so the create-new form
	// can prefill it. Empty = the candidate never declared it; the form shows
	// "not declared" rather than pre-selecting a plausible value.
	Sector    string
	GovBranch string
	// Typed is the organization exactly as the candidate wrote it, known only
	// for drafts (the intake record keeps the submission verbatim). The review
	// panel heads with it so the admin reads a name, not a slug.
	Typed string
	// Registered marks the #1086 case: the id DOES resolve in the registry and
	// the panel is open only because the candidate typed it. The heading says
	// so, and the create-new action is withheld — the org exists.
	Registered bool
}

// orgClaimedByCandidate reports whether affiliation.id is the candidate's own
// claim rather than a binding somebody accountable stands behind.
//
// True for a typed organisation, and for rows written before org_source
// existed. That default is the asymmetry #1086 turns on: an admin confirming a
// membership that was already true costs one click, while the reverse
// provisions a person into an organisation they never belonged to, with its
// roster, its revoke cascade and its data-sharing decisions.
func orgClaimedByCandidate(p onboardstore.Proposal) bool {
	switch strings.TrimSpace(p.OrgSource) {
	case onboardstore.OrgSourceSponsor, onboardstore.OrgSourceOperator:
		return false
	default:
		return true
	}
}

func orgFactsFromManifest(manifestJSON string) manifestOrgFacts {
	var m map[string]any
	if err := json.Unmarshal([]byte(manifestJSON), &m); err != nil {
		return manifestOrgFacts{}
	}
	var f manifestOrgFacts
	if aff, _ := m["affiliation"].(map[string]any); aff != nil {
		get := func(k string) string {
			v, _ := aff[k].(string)
			return strings.TrimSpace(v)
		}
		f = manifestOrgFacts{
			AffID:     get("id"),
			Kind:      get("kind"),
			Nation:    get("nation"),
			Sector:    get("sector"),
			GovBranch: get("gov_branch"),
		}
	}

	// Drafts store the intake record (candidate-intake/v-draft) instead of an
	// assembled manifest. For those rows, derive the typed org from the SAME
	// inputs the assembler would: the sponsor's prebound org first, then the
	// id affiliationIDFromOrg derives from the candidate's typed organization
	// (so a non-Latin name resolves here too). That makes the org-match
	// panel and the org-new handler reachable on real drafts, not just on
	// fabricated rows with a top-level affiliation block.
	if schema, _ := m["schema_version"].(string); strings.TrimSpace(schema) == intakeRecordSchema {
		var rec intakeRecord
		if err := json.Unmarshal([]byte(manifestJSON), &rec); err == nil {
			var pb onboardstore.Prebound
			if len(rec.Prebound) > 0 {
				_ = json.Unmarshal(rec.Prebound, &pb) //nolint:errcheck // malformed optional legacy prebinding falls back to the typed org
			}
			sub, _ := decodeCandidateSubmission(rec.Submission) //nolint:errcheck // display-only matching degrades to empty candidate fields
			if strings.TrimSpace(f.AffID) == "" {
				f.AffID = firstNonEmpty(strings.TrimSpace(pb.Org), affiliationIDFromOrg(sub.Organization))
			}
			f.Typed = strings.TrimSpace(sub.Organization)
			if strings.TrimSpace(f.Sector) == "" {
				_, f.Sector = draftOrgFacts(manifestJSON)
			}
		}
	}

	return f
}

// orgMatchesForProposal computes the candidate panel for one pending
// proposal. Returns the zero facts when the affordance is off, the manifest
// names no org, or the org is one nobody has to decide about — a sponsor
// pre-binding, or an id an admin already attached or created. The manifest's
// kind/nation ride along so the create-new form can render them as
// admin-confirmable prefills rather than inheriting them invisibly (the
// `origin` kind mislabel).
//
// A RESOLVING org that the candidate typed still raises the panel (#1086).
// Until then a hit was the one case that got no panel at all, so "I belong to
// Demoforge" and "I typed Demoforge Ltd. on an invite meant for someone
// else's organisation" produced the same enabled Approve, and affiliation.id —
// the roster key, the revoke-cascade key, the key data-sharing decisions run
// on — became a membership assertion nobody made.
func (s *Server) orgMatchesForProposal(p onboardstore.Proposal) (manifestOrgFacts, []orgstore.Candidate) {
	if !s.orgMatchActive() {
		return manifestOrgFacts{}, nil
	}
	f := orgFactsFromManifest(p.ManifestJSON)
	if f.AffID == "" {
		return manifestOrgFacts{}, nil
	}
	canonical, rerr := s.Orgs.Resolve(f.AffID)
	if rerr == nil && !orgClaimedByCandidate(p) {
		return manifestOrgFacts{}, nil // sponsor-bound or already decided — nothing to ask
	}
	cs, err := s.Orgs.Suggest(f.AffID, f.Nation, 3)
	if err != nil {
		return manifestOrgFacts{}, nil // matcher trouble degrades to no panel, never a 500 on the queue
	}
	if rerr == nil {
		f.Registered = true
		cs = s.withCanonicalCandidate(cs, canonical)
	}
	// The panel renders — with candidates when any exist, and, for an org the
	// registry does not know, with the deliberate create-new action.
	return f, cs
}

// withCanonicalCandidate guarantees the org the proposal actually names is
// attachable from the panel. Suggest ranks by name and drops an org whose
// declared nation contradicts the query's, so the claimed org itself can be
// missing from its own panel — and with the org already registered, create-new
// is refused, which would leave the admin a panel with no way out. Prepended
// rather than appended: an id that resolves is the exact match by definition.
func (s *Server) withCanonicalCandidate(cs []orgstore.Candidate, canonical string) []orgstore.Candidate {
	for _, c := range cs {
		if c.ID == canonical {
			return cs
		}
	}
	o, err := s.Orgs.GetOrg(canonical)
	if err != nil {
		return cs // display-only: a registry hiccup loses a row, never the page
	}
	self := orgstore.Candidate{
		ID: o.ID, DisplayName: o.DisplayName, Nation: o.Nation, Parent: o.Parent,
		Score: 1, Band: orgstore.BandExact, Via: "id",
	}
	return append([]orgstore.Candidate{self}, cs...)
}

// orgUnregistered reports whether the proposal's org id is one the registry
// does not know — the marker behind the queue pill, the sponsor's note and the
// admin's approve guard. Derived from Resolve at render time, never from
// stored draft state, so it clears the moment the org is registered or
// attached. False whenever the affordance is off (no fix path to point at),
// the manifest names no org, or the store fails for any reason other than
// not-found (fail-quiet on a marker, never a 500 on the queue).
//
// Fail-open here is deliberate and asymmetric with requireRegisteredOrg,
// which treats any store error as unregistered: a hint may go quiet, a gate
// may not. A proposal minted before #257 can still carry a handle-derived
// affiliation id; the registry does not know it, so the marker fires, which
// is the honest reading: that person was never attached to a registered
// organisation.
func (s *Server) orgUnregistered(p onboardstore.Proposal) bool {
	if !s.orgMatchActive() {
		return false
	}
	id := orgFactsFromManifest(p.ManifestJSON).AffID
	if id == "" {
		return false
	}
	_, err := s.Orgs.Resolve(id)
	return errors.Is(err, orgstore.ErrNotFound)
}

// orgResolves reports whether the registry knows this org id, by id or alias.
// A miss is an answer (false, nil). Any other store error is returned rather
// than read as a miss: the caller is the approve gate (requireAttachedOrg), and
// a registry that cannot be read must hold an unverified membership, not wave
// it through as "not registered, not this gate's business".
func (s *Server) orgResolves(id string) (bool, error) {
	_, err := s.Orgs.Resolve(id)
	if errors.Is(err, orgstore.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// orgChainForProposal returns the registered org + its ancestor chain
// (nearest-first) when the proposal's typed org resolves — the complement of
// orgMatchesForProposal: that one renders the decision panel for UNREGISTERED
// orgs; this one renders proof that a registered org (and its hierarchy) was
// actually captured. Nil when the affordance is off, no org is named, the org
// does not resolve, or the store hiccups (display-only — never a 500).
func (s *Server) orgChainForProposal(p onboardstore.Proposal) []orgstore.OrgView {
	if !s.orgMatchActive() {
		return nil
	}
	f := orgFactsFromManifest(p.ManifestJSON)
	if f.AffID == "" {
		return nil
	}
	self, err := s.Orgs.GetOrg(f.AffID)
	if err != nil {
		return nil
	}
	ancestors, err := s.Orgs.Ancestors(self.ID)
	if err != nil {
		return nil
	}
	return append([]orgstore.OrgView{self}, ancestors...)
}

// recordOrgDecision marks the proposal's organisation as an admin's decision
// rather than the candidate's claim, which is what closes the org panel and the
// approve gate behind it.
//
// Logged rather than returned: the attach or create it follows has already been
// applied and audited, and a panel that keeps asking is the safe way for this
// to fail.
func (s *Server) recordOrgDecision(ctx context.Context, id string) {
	if err := s.Onboard.SetProposalOrgSource(ctx, id, onboardstore.OrgSourceOperator); err != nil {
		slog.Error("onboarding org decision not recorded",
			slog.String("proposal", id), slog.Any("err", err))
	}
}

// rewriteManifestAffiliation sets affiliation.id in the manifest JSON,
// returning the updated document. When the manifest holds an intake record
// (candidate-intake/v-draft), it also rewrites the prebound org id so draft
// completion re-assembles against the CANONICAL org the admin attached,
// not the candidate's original free-typed organization string.
func rewriteManifestAffiliation(manifestJSON, orgID string) (string, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(manifestJSON), &m); err != nil {
		return "", err
	}
	aff, _ := m["affiliation"].(map[string]any)
	if aff == nil {
		aff = map[string]any{}
	}
	aff["id"] = orgID
	m["affiliation"] = aff
	// Drafts store the intake record under schema_version=candidate-intake/v-draft
	// — the sponsor's prebound binding and the candidate's submission. The
	// assembler that completeDraft calls reads ONLY those nested blocks, not the
	// top-level affiliation we just set. Keep the intake record in step by
	// pinning its prebound org to the same canonical id so completion cannot
	// re-slug the original free-typed name back into affiliation.id.
	if schema, _ := m["schema_version"].(string); strings.TrimSpace(schema) == intakeRecordSchema {
		pre, _ := m["prebound"].(map[string]any)
		if pre == nil {
			pre = map[string]any{}
		}
		pre["org"] = orgID
		m["prebound"] = pre
	}
	out, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// POST /onboarding/{id}/org-attach — ADMIN action: attach the proposal to an
// existing org. The typed value is recorded as an alias of the canonical org
// (recall accretes), the manifest affiliation.id is rewritten to the
// canonical id, and the act is audited. Always a deliberate human click —
// there is no auto-attach path anywhere.
func (s *Server) handleOrgAttach(w http.ResponseWriter, r *http.Request) {
	if !s.orgMatchActive() {
		s.renderError(w, r, 404, "org matching is not enabled on this goatnet")
		return
	}
	if err := s.validateCSRF(r); err != nil {
		s.renderError(w, r, 400, err.Error())
		return
	}
	if sponsorFromCtx(r.Context()) != nil {
		s.renderError(w, r, 403, "org registry changes are an admin act; sponsors vouch for people, not org structure")
		return
	}
	id := r.PathValue("id")
	target := strings.TrimSpace(r.FormValue("org"))
	if target == "" {
		s.renderError(w, r, 400, "org is required")
		return
	}
	p, err := s.Onboard.GetProposal(r.Context(), id)
	if err != nil {
		s.renderError(w, r, 404, "proposal not found")
		return
	}
	canonical, err := s.Orgs.Resolve(target)
	if err != nil {
		s.renderError(w, r, 404, "org not found: "+target)
		return
	}
	f := orgFactsFromManifest(p.ManifestJSON)
	actor := actorFromCtx(r.Context())
	updated, err := rewriteManifestAffiliation(p.ManifestJSON, canonical)
	if err != nil {
		s.renderError(w, r, 500, "manifest rewrite failed")
		return
	}
	if err := s.Onboard.UpdateProposalManifest(r.Context(), id, updated); err != nil {
		s.renderError(w, r, 409, "proposal is past the review stages — manifest is frozen")
		return
	}
	s.recordOrgDecision(r.Context(), id)
	// Accrete the typed form as an alias (ignore a conflict: the alias may
	// already exist from a previous confirm — that is the accretion working).
	if f.AffID != "" && f.AffID != canonical {
		//nolint:contextcheck // alias accretion is best-effort and not gated on HTTP context
		_ = s.Orgs.AddAlias(f.AffID, canonical, "name-variant", actor.Email) //nolint:errcheck // best-effort alias accretion; canonical attachment already succeeded
	}
	s.emitAuditWithInviteToken(r, actor, "onboard_org_attach", id, "", nil,
		map[string]any{"typed": f.AffID, "attached_to": canonical}, false, "ok", "")
	if derr := s.completeDraftAfterOrgAction(r, p, updated, "org attach"); derr != nil {
		s.renderError(w, r, derr.status, derr.msg)
		return
	}
	http.Redirect(w, r, "/onboarding?status="+p.Status, http.StatusSeeOther)
}

// POST /onboarding/{id}/org-new — ADMIN action: the typed org is genuinely
// new; create it in the registry (optionally under a parent, which must
// already exist — chain ancestors are created top-down) and record negative
// pairs against every candidate that was offered, so a pairing a human
// rejected is never re-suggested as a top candidate.
func (s *Server) handleOrgNew(w http.ResponseWriter, r *http.Request) {
	if !s.orgMatchActive() {
		s.renderError(w, r, 404, "org matching is not enabled on this goatnet")
		return
	}
	if err := s.validateCSRF(r); err != nil {
		s.renderError(w, r, 400, err.Error())
		return
	}
	if sponsorFromCtx(r.Context()) != nil {
		s.renderError(w, r, 403, "org registry changes are an admin act; sponsors vouch for people, not org structure")
		return
	}
	id := r.PathValue("id")
	p, err := s.Onboard.GetProposal(r.Context(), id)
	if err != nil {
		s.renderError(w, r, 404, "proposal not found")
		return
	}
	// Past the review stages the manifest is frozen: a late org create must
	// NOT write back into a proposal the admin has already decided on. Mirror
	// the org-attach path and refuse with a 409 instead of creating the org.
	if p.Status != onboardstore.ProposalProposed && p.Status != onboardstore.ProposalSponsorApproved {
		s.renderError(w, r, http.StatusConflict, "proposal is past the review stages — manifest is frozen")
		return
	}
	f := orgFactsFromManifest(p.ManifestJSON)
	if f.AffID == "" {
		s.renderError(w, r, 400, "proposal manifest names no organization")
		return
	}
	actor := actorFromCtx(r.Context())
	display := strings.TrimSpace(r.FormValue("display_name"))
	parent := strings.TrimSpace(r.FormValue("parent"))
	// Org facts are ADMIN-CONFIRMED, not blind-inherited: the form renders
	// kind/nation prefilled from the manifest and the admin's values win.
	// Empty falls back to the manifest (an older cached form without the
	// fields degrades to the previous behavior, never to a kindless org).
	kind := strings.TrimSpace(r.FormValue("kind"))
	// ADR 1171: NO fallback to the manifest. An organization's facts are typed
	// by the admin from what they know about the organization; a candidate's
	// self-description is not a source for them.
	//
	// This reverses the decision that made these fields prefilled and
	// editable. That fixed the INVISIBILITY of an inherited value — the `origin`
	// row was minted `kind: partner` from one of three same-company candidates
	// with no admin visibility — and it did not fix the INHERITANCE, which is
	// the defect. a Canadian candidate's `nation: CAN` was on
	// offer as the controlling nation of a United States command. The admin's
	// values always won; the problem was that a wrong one was pre-entered for
	// them to accept.
	nation := strings.ToUpper(strings.TrimSpace(r.FormValue("nation")))
	// ADR 1151 migration steps 3-4. The form now asks the two questions
	// separately; `kind` is DERIVED from them and the admin only supplies it to
	// override. Precedence, most deliberate first:
	//
	//   1. an explicit kind on this form (an override, or an older cached form),
	//   2. derived from sector + the organic test,
	//   3. whatever the manifest carried.
	//
	// Step 3's dual-emit is exactly this: the row keeps a `kind` for everything
	// that still reads one, while the sector/gov_branch pair becomes the thing
	// that was actually decided.
	sector := strings.ToLower(strings.TrimSpace(r.FormValue("sector")))
	govBranch := strings.ToLower(strings.TrimSpace(r.FormValue("gov_branch")))
	// A branch under a non-state sector is a contradiction, not a preference.
	// CreateOrg refuses it; drop it here so a stale form field cannot turn an
	// otherwise-valid registration into an error the admin cannot act on.
	if sector != "state" {
		govBranch = ""
	}
	if kind == "" && sector != "" {
		organic, known := coalition.Organic(nation, s.Cfg.OperatingSovereign)
		if derived, ok := coalition.DeriveKind(sector, organic, known); ok {
			kind = derived
		}
	}
	// `kind` keeps its derivation from sector + the organic test above, which is
	// computed rather than inherited. What it no longer does is fall back to the
	// manifest's kind — that was the same inheritance by another name, and it is
	// what minted the `origin` row wrong.
	// ADR 1171's org axis, typed here like every other org fact.
	composition := strings.ToLower(strings.TrimSpace(r.FormValue("composition")))
	// Pin the proposal to the org id BEFORE the registry is touched. Two
	// reasons. The intake record's prebound org becomes the id about to be
	// created, so the assembler cannot re-slug the candidate's typed
	// organization back into affiliation.id at completion. And the write is
	// the claim on the proposal: a row decided between the status check above
	// and this point answers 409 here, before any org row, parent edge,
	// negative pair or audit row exists, instead of leaving a created org
	// with no onboard_org_new row behind a refused response. The pinned id is
	// the one the assembler would derive from the record anyway, so a create
	// the registry then refuses leaves the draft's meaning unchanged.
	updated, err := rewriteManifestAffiliation(p.ManifestJSON, f.AffID)
	if err != nil {
		s.renderError(w, r, 500, "manifest rewrite failed")
		return
	}
	if err := s.Onboard.UpdateProposalManifest(r.Context(), id, updated); err != nil {
		s.renderError(w, r, 409, "proposal is past the review stages — manifest is frozen")
		return
	}
	//nolint:contextcheck // org registry write intentionally decoupled from HTTP context; store owns its own background context
	if _, err := s.Orgs.CreateOrg(orgstore.Org{
		ID: f.AffID, DisplayName: display, Kind: kind, Nation: nation,
		Composition: composition, Sector: sector, GovBranch: govBranch,
		Provenance: "operator-verified", CreatedBy: actor.Email,
	}); err != nil {
		s.renderError(w, r, 409, "org create refused: "+err.Error())
		return
	}
	s.recordOrgDecision(r.Context(), id)
	if parent != "" {
		//nolint:contextcheck // parent edge changes are owned by the org store rather than HTTP context
		if err := s.Orgs.SetParent(f.AffID, parent, actor.Email, "created at onboarding review (proposal "+id+")"); err != nil {
			// The org exists; the parent attach failed (unknown parent /
			// cycle). Honest partial: report it, the admin retries the
			// attach via the registry API.
			s.emitAuditWithInviteToken(r, actor, "onboard_org_new", id, "", nil,
				map[string]any{"org": f.AffID, "parent_requested": parent, "parent_error": err.Error()}, false, "error", err.Error())
			s.renderError(w, r, 409, "org created but parent attach refused: "+err.Error())
			return
		}
	}
	// The human looked at the candidates and chose NEW — record the negative
	// pairs so the matcher never drifts toward forcing these together.
	for _, despite := range r.Form["despite"] {
		if strings.TrimSpace(despite) != "" {
			//nolint:contextcheck // negative-pair recording is best-effort and not gated on HTTP context
			_ = s.Orgs.RecordNegativePair(f.AffID, despite, actor.Email) //nolint:errcheck // best-effort matcher hint; org creation already succeeded
		}
	}
	s.emitAuditWithInviteToken(r, actor, "onboard_org_new", id, "", nil,
		map[string]any{"org": f.AffID, "kind": kind, "nation": nation, "parent": parent, "despite": r.Form["despite"]}, false, "ok", "")
	if derr := s.completeDraftAfterOrgAction(r, p, updated, "org create"); derr != nil {
		s.renderError(w, r, derr.status, derr.msg)
		return
	}
	http.Redirect(w, r, "/onboarding?status="+p.Status, http.StatusSeeOther)
}

// completeDraftAfterOrgAction retries completion once an org attach or create
// has removed what was blocking a draft, so the queue reflects the current
// state instead of waiting for a later decision or sweep. updated is the
// manifest the action just persisted; action names the door for the log.
// Only a completion raced by another actor stops the request, with the same
// 409 the other completion call sites answer; by then the org action itself
// is already recorded and audited. A draft that still cannot assemble gets
// its INCOMPLETE note rewritten to name the current blocker, and the action
// still succeeds.
func (s *Server) completeDraftAfterOrgAction(r *http.Request, p *onboardstore.Proposal, updated, action string) *decideError {
	if p.Complete {
		return nil
	}
	p.ManifestJSON = updated
	outcome, why, cerr := s.completeDraft(r.Context(), p, requestCompleter(r))
	switch {
	case errors.Is(cerr, errDraftChanged):
		return &decideError{status: http.StatusConflict, msg: cerr.Error()}
	case cerr != nil:
		slog.Error("onboarding draft completion after "+action+" failed",
			slog.String("proposal", p.ID), slog.Any("err", cerr))
	case outcome == draftBlocked:
		s.refreshDraftBlocker(r, p, action, why)
	}
	return nil
}

// refreshDraftBlocker rewrites a still-blocked draft's INCOMPLETE marker so
// the note names the current blocker rather than the one from submit. A row
// that is no longer an incomplete review-stage draft, because another actor
// completed or decided it in the meantime, is the expected race and is logged
// as one; only a store failure is an error.
func (s *Server) refreshDraftBlocker(r *http.Request, p *onboardstore.Proposal, action, why string) {
	note := draftNoteRe.ReplaceAllString(p.Note, "") + " — INCOMPLETE (" + why + "); admin to complete"
	err := s.Onboard.UpdateDraftNote(r.Context(), p.ID, note)
	switch {
	case errors.Is(err, onboardstore.ErrNotFound):
		slog.Warn("onboarding draft note not rewritten after "+action+": the row is no longer an incomplete draft",
			slog.String("proposal", p.ID))
	case err != nil:
		slog.Error("onboarding draft note update after "+action+" failed",
			slog.String("proposal", p.ID), slog.Any("err", err))
	default:
		p.Note = note
	}
	slog.Warn("onboarding draft still incomplete after "+action,
		slog.String("proposal", p.ID), slog.String("why", why))
}

// GET /onboarding/org-suggest?q=&nation= — browser-realm live feedback for
// the parent-org input on the review panel. Answers two questions at once:
// does the typed value resolve to a registered org (directly or via alias),
// and what registered orgs look like it. Read-only and advisory — the server
// still refuses an unknown parent at create time; this only lets the admin
// see the refusal coming instead of discovering it on submit.
func (s *Server) handleOnboardOrgSuggest(w http.ResponseWriter, r *http.Request) {
	if !s.orgMatchActive() {
		onboardError(w, http.StatusNotFound, "org_match_disabled", "org matching is not enabled on this goatnet")
		return
	}
	if sponsorFromCtx(r.Context()) != nil {
		onboardError(w, http.StatusForbidden, "operator_only", "org registry lookups at review are an admin act")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	resp := map[string]any{"query": q, "resolves": false}
	if q == "" {
		onboardJSON(w, http.StatusOK, resp)
		return
	}
	if canonical, err := s.Orgs.Resolve(q); err == nil {
		resp["resolves"] = true
		resp["canonical"] = canonical
		// The parent chain, nearest-first — the indicator shows where in
		// the hierarchy the typed parent sits, not just that it exists.
		if chain, err := s.Orgs.Ancestors(canonical); err == nil && len(chain) > 0 {
			ids := make([]string, len(chain))
			for i, a := range chain {
				ids[i] = a.ID
			}
			resp["ancestors"] = ids
		}
	}
	cs, err := s.Orgs.Suggest(q, strings.TrimSpace(r.URL.Query().Get("nation")), 5)
	if err != nil {
		orgStoreError(w, err)
		return
	}
	resp["candidates"] = cs
	onboardJSON(w, http.StatusOK, resp)
}

// GET /api/org-registry/suggest?q=&nation=&limit= — the machine-realm mirror
// of the review panel (goat-cli / future surfaces). 404 when the affordance
// is off (the §13.2 rollback lever governs the whole feature, not just the
// web panel).
func (s *Server) apiOrgSuggest(w http.ResponseWriter, r *http.Request) {
	if !s.orgMatchActive() {
		onboardError(w, http.StatusNotFound, "org_match_disabled", "org matching is not enabled on this goatnet")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		onboardError(w, http.StatusBadRequest, "q_required", "q is required")
		return
	}
	cs, err := s.Orgs.Suggest(q, strings.TrimSpace(r.URL.Query().Get("nation")), 5)
	if err != nil {
		orgStoreError(w, err)
		return
	}
	onboardJSON(w, http.StatusOK, map[string]any{
		"query":      q,
		"candidates": cs,
		"policy":     "never auto-merge; a human confirms — similar spelling is not the same org",
	})
}

// apiAffiliationDerive answers, for a nation and sector the operator has typed
// but not yet submitted, the two things this console DERIVES rather than stores:
// alliance standing, and whether the organization is ours or foreign. It exists
// so the review form can show those live instead of revealing them only after a
// registration is committed.
//
// Read-only and side-effect free. It writes nothing, decides nothing, and
// authorizes nothing — ADR 1077 §5 keeps standing out of the ACL, out of the
// cert and out of any NetBird group, and `organic` is a comparison of two nation
// codes. The values it returns are the same ones the create path computes, from
// the same functions, so the form cannot promise one answer and record another.
func (s *Server) apiAffiliationDerive(w http.ResponseWriter, r *http.Request) {
	nation := strings.TrimSpace(r.URL.Query().Get("nation"))
	sector := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sector")))

	// Echo the NORMALISED nation with its full name where the registry knows one,
	// so an operator who typed `US` sees "United States (USA)" and can tell the
	// match was right rather than reading their own input back.
	out := map[string]any{
		"nation":       coalition.NormalizeNation(nation),
		"nation_label": coalition.NationDisplay(s.Coalition, nation),
		"sector":       sector,
	}

	sovereign := ""
	if s.Cfg != nil {
		sovereign = s.Cfg.OperatingSovereign
	}
	organic, organicKnown := coalition.Organic(nation, sovereign)
	out["organic_known"] = organicKnown
	if organicKnown {
		out["organic"] = organic
		if organic {
			out["organic_label"] = "ours"
			out["organic_basis"] = "runs this environment"
		} else {
			out["organic_label"] = "foreign"
			out["organic_basis"] = "this environment is run by " + coalition.NationDisplay(s.Coalition, sovereign)
		}
	} else if strings.TrimSpace(sovereign) == "" {
		out["organic_basis"] = "this environment does not say which nation runs it"
	} else {
		out["organic_basis"] = ""
	}

	// Alliance standing. A nation we cannot resolve reads `partner`, which is the
	// non-privileged answer — the unknown case must not read as membership.
	if s.Coalition != nil && strings.TrimSpace(nation) != "" {
		res := s.Coalition.Resolve(nation, s.Cfg.CoalitionFramework, time.Now().UTC())
		out["alliance_standing"] = res.Standing
		out["alliance_basis"] = res.Basis
		out["framework"] = res.Framework
		if res.FrameworkName != "" {
			out["framework_name"] = res.FrameworkName
		}
	} else if s.Coalition == nil {
		out["alliance_basis"] = "the alliance registry did not load"
	} else {
		out["alliance_basis"] = "enter a nation"
	}
	onboardJSON(w, http.StatusOK, out)
}
