// Review-gate field edits (ADR 1137, docs/design/onboarding-review-field-edits.md).
//
// WHY THIS FILE EXISTS. A candidate typed his own NAME into the "Organization /
// unit" field. The intake slugified it into affiliation.id, the review page
// raised its org-match panel — and that panel offers ONLY fuzzy-suggest
// candidates, so an org with zero string similarity to the typed value could
// never surface. The reviewer's two available actions were "attach to a wrong
// org" and "create a duplicate org named after the person". Neither is a
// correction, and no machine-realm route wrote the manifest either, so the fix
// went in by hand against the SQLite store on the mgmt host and wrote no audit
// row.
//
// So: a SHORT closed set of fields a reviewer may correct before anything is
// minted, one validation path, one audit path, and two doors — a browser form
// and a machine-realm API. The API is the substantive half. A capability that
// exists only behind a browser session is a capability that gets exercised with
// sqlite3 instead, which is exactly how this gap was discovered.
package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/coalition"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// Editable field names. The set IS the policy: an unknown name is refused, not
// ignored, so a caller can never believe it set something it did not.
const (
	FieldAffiliationID  = "affiliation.id"
	FieldAffiliationNat = "affiliation.nation"
	FieldOrgDisplayName = "org.display_name"
	FieldIdentityGitHub = "identity.github"
)

// editableFields is the closed set (ADR 1137 D1). Adding one is an ADR
// amendment, not a judgement call at the keyboard.
var editableFields = map[string]bool{
	FieldAffiliationID:  true,
	FieldAffiliationNat: true,
	FieldOrgDisplayName: true,
	FieldIdentityGitHub: true,
}

// frozenFieldReasons names the fields most likely to be asked for, with the
// reason each is refused HERE rather than a generic "not allowed". None of
// these is caution; each has a dependent that would silently break (ADR 1137 D2).
var frozenFieldReasons = map[string]string{
	"identity.email": "email is the person-plane dedup spine (ADR 1048/F-239) and its uniqueness check ran at invite time — " +
		"reject and re-invite instead",
	"identity.handle": "handle is the manifest filename and a peer-label segment — reject and re-invite instead",
	"zenoh.trial.org": "the trial prefix is the per-person URI-SAN authority and MUST stay distinct per person " +
		"(a shared value 409s every mint after the first) — it deliberately does not follow the org",
	"zenoh.trial.vendor": "the trial prefix is the per-person URI-SAN authority and MUST stay distinct per person " +
		"(a shared value 409s every mint after the first) — it deliberately does not follow the org",
	"tier":                "tier is the sponsorship-capped authorization decision (ADR 1071), not a typo class — reject and re-invite",
	"netbird.auto_groups": "mesh groups are the authorization decision (ADR 1071), not a typo class — reject and re-invite",
}

// nationPattern + validNationCode live in onboard_support.go — the SAME
// validator the candidate submit path uses, deliberately shared so a value a
// reviewer can set is exactly a value intake would have accepted.

// githubPattern is GitHub's own login shape: alphanumeric with single hyphens,
// no leading or trailing hyphen, max 39. RE2 has no lookahead, so the
// no-consecutive-hyphens half is a separate check in setGitHub rather than a
// cleverer regex.
var githubPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)

// fieldEdit is one applied change, for the audit diff and the API response.
type fieldEdit struct {
	Field  string `json:"field"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// fieldEditError carries an HTTP status alongside the message so both doors
// render the same refusal with the same code.
type fieldEditError struct {
	Status  int
	Message string
}

func (e *fieldEditError) Error() string { return e.Message }

func badRequest(format string, a ...any) *fieldEditError {
	return &fieldEditError{Status: http.StatusBadRequest, Message: fmt.Sprintf(format, a...)}
}

// applyFieldEdits validates and applies a set of field edits to one proposal,
// returning the applied diff. It is the ONLY writer: both the browser form and
// the machine API call through here, so validation, the stage gate and the
// audit shape cannot drift between them.
//
// Empty-string values are meaningful: they CLEAR an optional field (a github
// handle entered in error). affiliation.id is the exception — clearing the org
// would leave the proposal unattributable, so it is refused.
func (s *Server) applyFieldEdits(r *http.Request, id string, fields map[string]string) ([]fieldEdit, *fieldEditError) {
	return s.applyFieldEditsSourced(r, id, fields, "")
}

// applyFieldEditsSourced is applyFieldEdits with the provenance of a seeded
// nation. nationSource is the registered org a posted affiliation.nation was
// read from (reconcileSeededNation), or empty when a person typed it. It changes
// nothing about what is written; it is recorded on the audit row so a
// registry-sourced value is never mistaken for an admin's correction.
func (s *Server) applyFieldEditsSourced(r *http.Request, id string, fields map[string]string, nationSource string) ([]fieldEdit, *fieldEditError) {
	if s.Onboard == nil {
		return nil, &fieldEditError{Status: http.StatusServiceUnavailable, Message: "onboarding is not configured"}
	}
	if len(fields) == 0 {
		return nil, badRequest("no fields given")
	}

	// Refuse the whole request when ANY field is outside the set — a partial
	// apply would leave the caller guessing which half landed.
	for name := range fields {
		if editableFields[name] {
			continue
		}
		if why, frozen := frozenFieldReasons[name]; frozen {
			return nil, &fieldEditError{Status: http.StatusUnprocessableEntity,
				Message: fmt.Sprintf("%s is not editable at review: %s", name, why)}
		}
		return nil, badRequest("%s is not an editable field — editable: %s", name, strings.Join(editableFieldNames(), ", "))
	}

	p, err := s.Onboard.GetProposal(r.Context(), id)
	if err != nil {
		return nil, &fieldEditError{Status: http.StatusNotFound, Message: "proposal not found"}
	}
	// The store enforces the same boundary; checking here lets the refusal name
	// the remedy instead of surfacing a bare row-count miss.
	if p.Status != onboardstore.ProposalProposed && p.Status != onboardstore.ProposalSponsorApproved {
		return nil, &fieldEditError{Status: http.StatusConflict,
			Message: fmt.Sprintf("proposal is %s — the manifest is frozen once anything is minted; "+
				"an org change after the mint goes through `goat admin org` plus a re-mint (ADR 1051)", p.Status)}
	}

	// A draft holds an intake record, not a manifest: an edit written into it
	// would be lost the moment the draft assembles. Complete it first when the
	// current rules can; refuse, naming the gap, when they cannot. A row that
	// merely lacks the flag but already holds a manifest is edited as-is.
	if !p.Complete {
		// Refuse a no-op or invalid edit before the completion persists and
		// writes its audit row, so a refused Apply leaves the draft as it was.
		if ferr := s.precheckDraftEdit(p, fields); ferr != nil {
			return nil, ferr
		}
		outcome, why, cerr := s.completeDraft(r.Context(), p, requestCompleter(r))
		if errors.Is(cerr, errDraftChanged) {
			return nil, &fieldEditError{Status: http.StatusConflict, Message: cerr.Error()}
		}
		if cerr != nil {
			return nil, &fieldEditError{Status: http.StatusInternalServerError, Message: "could not complete the draft: " + cerr.Error()}
		}
		if outcome == draftBlocked {
			return nil, &fieldEditError{Status: http.StatusConflict,
				Message: fmt.Sprintf("proposal is an incomplete draft — %s — so there is no manifest to edit yet; reject it and re-invite", why)}
		}
	}

	m, err := unmarshalManifest(*p)
	if err != nil {
		return nil, &fieldEditError{Status: http.StatusInternalServerError, Message: "proposal manifest is unreadable"}
	}
	edits, ferr := s.diffFieldEdits(m, fields)
	if ferr != nil {
		return nil, ferr
	}
	if len(edits) == 0 {
		return nil, nothingToChange()
	}

	out, err := json.Marshal(m)
	if err != nil {
		return nil, &fieldEditError{Status: http.StatusInternalServerError, Message: "manifest rewrite failed"}
	}
	// The approve gate re-lints the manifest and refuses one that fails. An
	// edit that would take the document from passing to failing is refused
	// here, before the write, with the field named (field_edits_lint.go).
	if ferr := lintRegression([]byte(p.ManifestJSON), out); ferr != nil {
		return nil, ferr
	}
	if err := s.Onboard.UpdateProposalManifest(r.Context(), id, string(out)); err != nil {
		// The store's own stage guard — a status flip between our read and this
		// write lands here rather than silently succeeding.
		return nil, &fieldEditError{Status: http.StatusConflict,
			Message: "proposal moved past the review stages while this edit was in flight — re-read it and retry"}
	}

	// An admin who types affiliation.id has decided the organisation: the value
	// is resolve-or-refuse (setAffiliationOrg), so it names a registered org and
	// a human put it there. That is the same decision the org panel's attach
	// records, and it clears the same claim (#1086).
	for _, e := range edits {
		if e.Field == FieldAffiliationID {
			s.recordOrgDecision(r.Context(), id)
			break
		}
	}

	before := map[string]any{}
	after := map[string]any{}
	for _, e := range edits {
		before[e.Field] = e.Before
		after[e.Field] = e.After
		if e.Field == FieldAffiliationNat && nationSource != "" {
			after[FieldAffiliationNat+".source"] = "registry:" + nationSource
		}
	}
	s.emitAuditWithInviteToken(r, auditActor(r), "onboard_field_edit", id, "",
		before, after, false, "ok", "")
	return edits, nil
}

func editableFieldNames() []string {
	out := make([]string, 0, len(editableFields))
	for k := range editableFields {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// setAffiliationOrg points the proposal at a REGISTERED org. It resolves the
// value through the registry (id or alias) and never creates one — creation
// stays the deliberate org-new act that records negative pairs. This resolve-or-
// refuse rule is what makes the editor safe to expose: it can only point a
// proposal at something a human already registered.
func (s *Server) setAffiliationOrg(m map[string]any, val string) (string, string, *fieldEditError) {
	aff := mapAt(m, "affiliation")
	before, _ := aff["id"].(string)
	if val == "" {
		return before, "", badRequest("affiliation.id cannot be cleared — a proposal with no org is unattributable")
	}
	if s.Orgs == nil {
		return before, "", &fieldEditError{Status: http.StatusServiceUnavailable,
			Message: "the org registry is not configured — affiliation.id cannot be resolved"}
	}
	canonical, err := s.Orgs.Resolve(val)
	if err != nil {
		// Name a remedy the operator can actually reach on THIS goatnet. The
		// create-new action (handleOrgNew) is gated on orgMatchActive(), so on a
		// goatnet with ORG_MATCH_ENABLED off this message used to send the operator
		// to a door that answers 404 -- a closed loop with no way out, and nothing
		// saying so. apiOrgCreate (`goat admin org register`) is NOT flag-gated: it
		// checks only the store and the operator key, so it works either way.
		if !s.orgMatchActive() {
			return before, "", &fieldEditError{Status: http.StatusUnprocessableEntity,
				Message: fmt.Sprintf("%q is not a registered org or alias. Org matching is disabled "+
					"on this goatnet (ORG_MATCH_ENABLED), so the review page's create-new action is "+
					"unavailable — register the org with `goat admin org register`, then attach", val)}
		}
		return before, "", &fieldEditError{Status: http.StatusUnprocessableEntity,
			Message: fmt.Sprintf("%q is not a registered org or alias — register it deliberately first "+
				"(the review page's create-new action), then attach", val)}
	}
	aff["id"] = canonical
	return before, canonical, nil
}

// setNation is deliberately STRICTER than the manifest schema's shared
// `^[A-Z]{2,8}$`. That pattern admits an 8-letter word, so upper-casing the
// input first would turn "Ukraine" — the exact mistake intake exists to catch
// — into an accepted value. Real values are 2–4 characters: an ISO
// alpha-3 code, or a multinational marker (NATO, EU). A longer marker would need
// this bound raised deliberately, which is the point.
func setNation(m map[string]any, val string) (string, string, *fieldEditError) {
	aff := mapAt(m, "affiliation")
	before, _ := aff["nation"].(string)
	if val == "" {
		delete(aff, "nation")
		return before, "", nil
	}
	val = strings.TrimSpace(val)
	if !validNationCode(val) {
		return before, "", badRequest(
			"nation must be an ISO 3166-1 alpha-3 code (e.g. UKR) or a marker (NATO, EU) — %q looks like a country name, not a code", val)
	}
	aff["nation"] = val
	return before, val, nil
}

// orgDisplayName / setOrgDisplayName honor BOTH block names: `org` is the
// going-forward key and `vendor` the legacy alias (ADR 1052 dual-read). Reads
// prefer whichever is populated; writes update every block that already exists
// so the two cannot disagree after an edit.
func orgDisplayName(m map[string]any) string {
	for _, key := range []string{"org", "vendor"} {
		if blk, ok := m[key].(map[string]any); ok {
			if dn, _ := blk["display_name"].(string); dn != "" {
				return dn
			}
		}
	}
	return ""
}

func setOrgDisplayName(m map[string]any, val string) {
	touched := false
	for _, key := range []string{"org", "vendor"} {
		if blk, ok := m[key].(map[string]any); ok {
			blk["display_name"] = val
			touched = true
		}
	}
	if !touched {
		// Neither block present (an operator-shape manifest): write the
		// going-forward key rather than reviving the legacy one.
		mapAt(m, "org")["display_name"] = val
	}
}

func setGitHub(m map[string]any, val string) (string, string, *fieldEditError) {
	ident := mapAt(m, "identity")
	before, _ := ident["github"].(string)
	if val == "" {
		delete(ident, "github")
		return before, "", nil
	}
	val = strings.TrimPrefix(val, "@") // a paste artifact, not an error
	if !githubPattern.MatchString(val) || strings.Contains(val, "--") {
		return before, "", badRequest("%q is not a GitHub login (alphanumeric with single hyphens, max 39)", val)
	}
	ident["github"] = val
	return before, val, nil
}

// mapAt returns m[key] as a map, creating it when absent so a manifest that
// never carried the block can still be corrected.
func mapAt(m map[string]any, key string) map[string]any {
	if blk, ok := m[key].(map[string]any); ok {
		return blk
	}
	blk := map[string]any{}
	m[key] = blk
	return blk
}

// --- door 1: the browser form -------------------------------------------
//
// POST /onboarding/{id}/fields — admin act. Sponsors are refused here for the
// same reason org-attach refuses them: correcting org structure is not vouching
// for a person (ADR 1137 D7). Whether a sponsor may fix their own invitee's
// nation or display name is a real question, deliberately left unanswered
// rather than quietly widened.
func (s *Server) handleFieldEdits(w http.ResponseWriter, r *http.Request) {
	if err := s.validateCSRF(r); err != nil {
		s.renderError(w, r, 400, err.Error())
		return
	}
	if sponsorFromCtx(r.Context()) != nil {
		s.renderError(w, r, 403, "correcting a proposal's org facts is an admin act; sponsors vouch for people")
		return
	}
	id := r.PathValue("id")
	fields := map[string]string{}
	for _, name := range editableFieldNames() {
		// Only fields actually present in the form are considered, so the form
		// can grow or shrink without clearing what it does not render.
		if vals, ok := r.Form[name]; ok && len(vals) > 0 {
			fields[name] = vals[0]
		}
	}
	// A seeded Nation box posts the org it came from; an untouched seed is
	// written as a registry-sourced value and follows an org change made in
	// the same Apply (onboard_review_nation.go).
	nationSource := s.reconcileSeededNation(fields, r.FormValue(nationSeedField))
	if _, ferr := s.applyFieldEditsSourced(r, id, fields, nationSource); ferr != nil {
		s.renderError(w, r, ferr.Status, ferr.Message)
		return
	}
	p, err := s.Onboard.GetProposal(r.Context(), id)
	status := onboardstore.ProposalProposed
	if err == nil {
		status = p.Status
	}
	http.Redirect(w, r, "/onboarding?status="+status+"&id="+id, http.StatusSeeOther)
}

// --- door 2: the machine realm ------------------------------------------
//
// POST /api/onboard/proposals/{id}/fields — the half that matters. Body:
//
//	{"fields": {"affiliation.id": "demoforge", "affiliation.nation": "UKR"}}
//
// Returns the APPLIED diff, so a caller sees what actually changed rather than
// assuming its request was total.
type fieldEditsRequest struct {
	Fields map[string]string `json:"fields"`
}

type fieldEditsResponse struct {
	ID      string      `json:"id"`
	Applied []fieldEdit `json:"applied"`
}

func (s *Server) apiFieldEdits(w http.ResponseWriter, r *http.Request) {
	var req fieldEditsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		onboardError(w, http.StatusBadRequest, "decode", "body must be {\"fields\": {\"<name>\": \"<value>\"}}")
		return
	}
	p := onboardPrincipalFromCtx(r.Context())
	if !p.IsOperator {
		onboardError(w, http.StatusForbidden, "not_operator",
			"correcting a proposal's org facts is an operator act")
		return
	}
	id := r.PathValue("id")
	applied, ferr := s.applyFieldEdits(r, id, req.Fields)
	if ferr != nil {
		onboardError(w, ferr.Status, "field_edit_refused", ferr.Message)
		return
	}
	onboardJSON(w, http.StatusOK, fieldEditsResponse{ID: id, Applied: applied})
}

// fillEditable turns on the review-card field editor for one row and prefills it
// with the CURRENT values (ADR 1137 D8). Called only from the admin, pre-mint
// branch of the queue assembly — the same guard the org panel sits behind.
//
// The org box shows what the manifest actually carries — including an alias,
// which setAffiliationOrg resolves to the canonical id on submit — rather than
// an empty box that means "unknown".
//
// EVERY rendered box is prefilled from the MANIFEST, and that is load-bearing
// rather than tidy. The browser door posts whatever the form renders, and an
// empty value CLEARS the field (setNation deletes affiliation.nation). So a box
// that renders empty while the manifest holds a value turns the form's own
// promise — "leave a box untouched to leave the field alone" — into a silent
// delete on the next Apply for any unrelated reason.
//
// Nation used to be the exception, and it was empty exactly when it mattered:
// it was sourced from the org-match panel, which SUPPRESSES ITSELF the moment
// the typed org resolves in the registry (orgMatchesForProposal returns zero
// facts for a known org — there is no match decision left to make). So creating
// the org at review blanked the nation box from that click onward, on a
// proposal whose manifest said USA. Nothing had overwritten anything yet; the
// next Apply correction would have. Read it here, from the document, so the
// prefill cannot depend on whether some other panel decided to render.
func (s *Server) fillEditable(v *proposalView, p onboardstore.Proposal, hints intakeHintsSource) {
	v.Editable = true
	m := s.editableManifest(p, hints)
	if m == nil {
		return
	}
	v.OrgDisplayName = orgDisplayName(m)
	if aff, ok := m["affiliation"].(map[string]any); ok {
		v.OrgID, _ = aff["id"].(string)
		v.OrgNation, _ = aff["nation"].(string)
		s.fillStandingReconcile(v, aff)
	}
	if ident, ok := m["identity"].(map[string]any); ok {
		v.GitHub, _ = ident["github"].(string)
		v.CandidateNationality, _ = ident["nationality"].(string)
	}
}

// fillStandingReconcile is ADR 1151 migration step 2's reconcile leg, on the one
// path that can actually see a seeded value: the proposal's own manifest.
//
// A candidate's manifest may carry affiliation.alliance_standing (or the legacy
// affiliation.coalition) because intake accepted an explicit one. ADR 1077 §5
// makes the DERIVED standing authoritative — it is nation-derived against the
// active framework and changes when the world does — so the seeded value is
// never used. But discarding it silently hides that somebody wrote down
// something that is no longer true, and the reviewer is exactly the person who
// should see that before minting anything. So: derive, compare, and if they
// disagree, say so on the page.
//
// Silent when there is nothing to say: no seeded value (the healthy state), no
// nation to derive from, or no framework registry loaded. In particular a
// missing registry must not render as "no conflict" — it renders as nothing,
// and the identity view's own Unresolved line is what names a missing registry.
func (s *Server) fillStandingReconcile(v *proposalView, aff map[string]any) {
	if s.Coalition == nil {
		return
	}
	seeded, _ := aff["alliance_standing"].(string)
	if strings.TrimSpace(seeded) == "" {
		seeded, _ = aff["coalition"].(string)
	}
	if strings.TrimSpace(seeded) == "" {
		return
	}
	nation, _ := aff["nation"].(string)
	if strings.TrimSpace(nation) == "" {
		return
	}
	res := s.Coalition.Resolve(nation, s.Cfg.CoalitionFramework, time.Now().UTC())
	if c := coalition.Reconcile(seeded, res); c != nil {
		v.StandingConflict = c.Error()
	}
}
