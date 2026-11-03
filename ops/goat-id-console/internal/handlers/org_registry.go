// Org-registry API (ADR 1112, docs/design/org-hierarchy-and-dedup.md §3):
// the machine-realm surface over the first-class org store — create, parent,
// alias, list, export. Rides the onboard machine realm (per-handle operator
// keys + participant delegation) because that is where the increment-2
// match-or-create surfaces live; MUTATIONS are operator-only (an org is a
// governance object, not a sponsor convenience), reads are open to any
// authenticated machine-realm principal.
//
// Every mutation is audited (F-207 attribution via the resolved per-handle
// principal) with a justification carried onto the effective-dated edge.
package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/audit"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/coalition"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/orgstore"
)

// orgRegistryEnabled reports whether the store is wired (main.go always wires
// it; tests opt in). Fail-closed with an explicit verdict, never a nil panic.
func (s *Server) orgRegistryEnabled(w http.ResponseWriter) bool {
	if s.Orgs == nil {
		onboardError(w, http.StatusServiceUnavailable, "org_registry_unavailable", "org registry store not configured")
		return false
	}
	return true
}

// requireOrgOperator enforces operator-only mutation.
func requireOrgOperator(w http.ResponseWriter, r *http.Request) bool {
	if !onboardPrincipalFromCtx(r.Context()).IsOperator {
		onboardError(w, http.StatusForbidden, "operator_only", "org-registry mutations require an operator key")
		return false
	}
	return true
}

func orgStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, orgstore.ErrNotFound):
		onboardError(w, http.StatusNotFound, "org_not_found", err.Error())
	case errors.Is(err, orgstore.ErrConflict):
		onboardError(w, http.StatusConflict, "org_conflict", err.Error())
	case errors.Is(err, orgstore.ErrCycle):
		onboardError(w, http.StatusConflict, "org_cycle", err.Error())
	case errors.Is(err, orgstore.ErrValidation):
		onboardError(w, http.StatusBadRequest, "org_invalid", err.Error())
	default:
		onboardError(w, http.StatusInternalServerError, "org_store_error", "org store operation failed")
	}
}

// GET /api/org-registry/orgs
// GET /api/org-registry/orgs/{id}/children — the DIRECT children of an org.
//
// Reads the same rows as the parent direction; org_parent_edges_parent_idx has
// existed since the table was created and nothing queried it. There is
// deliberately no reciprocal `children` field on the org row: an edge is one
// row and must stay one row, or the two copies can disagree with nothing to
// arbitrate. Current open edges only, matching Ancestors.
func (s *Server) apiOrgChildren(w http.ResponseWriter, r *http.Request) {
	if !s.orgRegistryEnabled(w) {
		return
	}
	kids, err := s.Orgs.Children(r.PathValue("id"))
	if err != nil {
		orgStoreError(w, err)
		return
	}
	onboardJSON(w, http.StatusOK, map[string]any{"orgs": kids})
}

// GET /api/org-registry/orgs/{id}/descendants — the whole subtree, breadth-first,
// excluding the org itself. This is what makes "everything under USAREUR-AF"
// answerable in one call instead of a client-side walk.
func (s *Server) apiOrgDescendants(w http.ResponseWriter, r *http.Request) {
	if !s.orgRegistryEnabled(w) {
		return
	}
	desc, err := s.Orgs.Descendants(r.PathValue("id"))
	if err != nil {
		orgStoreError(w, err)
		return
	}
	onboardJSON(w, http.StatusOK, map[string]any{"orgs": desc})
}

func (s *Server) apiOrgList(w http.ResponseWriter, r *http.Request) {
	if !s.orgRegistryEnabled(w) {
		return
	}
	orgs, err := s.Orgs.ListOrgs()
	if err != nil {
		orgStoreError(w, err)
		return
	}
	onboardJSON(w, http.StatusOK, map[string]any{"orgs": orgs})
}

// GET /api/org-registry/export — the catalog the sync script materializes
// into ops/identity/orgs/<goatnet>.yaml.
func (s *Server) apiOrgExport(w http.ResponseWriter, r *http.Request) {
	if !s.orgRegistryEnabled(w) {
		return
	}
	cat, err := s.Orgs.Export()
	if err != nil {
		orgStoreError(w, err)
		return
	}
	onboardJSON(w, http.StatusOK, cat)
}

type orgCreateReq struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	// Kind is DEPRECATED (ADR 1151) and derived from Sector where absent. It is
	// still accepted so callers minted before the change keep working.
	Kind string `json:"kind"`
	// Sector / GovBranch are the ADR 1151 target block. The review form has
	// written them since the form was rebuilt; this registration path had not
	// caught up, so an org created through the API or CLI was born without the
	// facts the UI now insists on.
	Sector        string `json:"sector"`
	GovBranch     string `json:"gov_branch"`
	Nation        string `json:"nation"`
	EchelonHint   string `json:"echelon_hint"`
	Provenance    string `json:"provenance"`
	Parent        string `json:"parent"`
	Justification string `json:"justification"`
	// Despite lists candidate org ids the caller was shown and deliberately
	// rejected by creating this org — recorded as negative match pairs so a
	// human-judged-distinct pairing is never re-suggested as a top candidate
	// (design §7: similar spelling is not the same org).
	Despite []string `json:"despite,omitempty"`
}

// POST /api/org-registry/orgs — create an org; optionally attach its parent
// in the same act (the create-chain affordance builds on this).
func (s *Server) apiOrgCreate(w http.ResponseWriter, r *http.Request) {
	if !s.orgRegistryEnabled(w) || !requireOrgOperator(w, r) {
		return
	}
	var req orgCreateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		onboardError(w, http.StatusBadRequest, "bad_json", "could not parse body")
		return
	}
	actor := onboardAPIActor(r)
	// Derive kind when the caller gave only the target block, so the two paths
	// agree with the form (ADR 1151 step 3). An explicit kind still wins.
	kind := strings.TrimSpace(req.Kind)
	sector := strings.ToLower(strings.TrimSpace(req.Sector))
	govBranch := strings.ToLower(strings.TrimSpace(req.GovBranch))
	if sector != "state" {
		govBranch = ""
	}
	if kind == "" && sector != "" {
		sovereign := ""
		if s.Cfg != nil {
			sovereign = s.Cfg.OperatingSovereign
		}
		organic, known := coalition.Organic(req.Nation, sovereign)
		if derived, ok := coalition.DeriveKind(sector, organic, known); ok {
			kind = derived
		}
	}
	//nolint:contextcheck // org registry write intentionally decoupled from HTTP context; store owns its own background context
	o, err := s.Orgs.CreateOrg(orgstore.Org{
		ID: req.ID, DisplayName: req.DisplayName, Kind: kind,
		Sector: sector, GovBranch: govBranch,
		Nation: req.Nation, EchelonHint: req.EchelonHint,
		Provenance: req.Provenance, CreatedBy: actor,
	})
	if err != nil {
		orgStoreError(w, err)
		return
	}
	if p := strings.TrimSpace(req.Parent); p != "" {
		//nolint:contextcheck // parent edge changes are owned by the org store rather than HTTP context
		if err := s.Orgs.SetParent(o.ID, p, actor, req.Justification); err != nil {
			// The org row exists; the parent attach failed. Report the
			// failure honestly rather than pretending the whole act failed —
			// the caller retries the parent attach alone.
			s.auditOrg(r, "org-registry.create", o.ID, "error", err.Error(), map[string]any{"parent_requested": p})
			orgStoreError(w, err)
			return
		}
	}
	for _, despite := range req.Despite {
		if strings.TrimSpace(despite) != "" {
			//nolint:contextcheck // negative-pair recording is best-effort and not gated on HTTP context
			_ = s.Orgs.RecordNegativePair(o.ID, despite, actor) //nolint:errcheck // best-effort matcher hint; org creation already succeeded
		}
	}
	s.auditOrg(r, "org-registry.create", o.ID, "ok", "", map[string]any{
		"kind": o.Kind, "sector": o.Sector, "gov_branch": o.GovBranch,
		"nation": o.Nation, "parent": req.Parent, "provenance": o.Provenance, "despite": req.Despite,
	})
	v, err := s.Orgs.GetOrg(o.ID)
	if err != nil {
		orgStoreError(w, err)
		return
	}
	onboardJSON(w, http.StatusCreated, v)
}

type orgParentReq struct {
	Parent        string `json:"parent"` // empty = clear (org becomes a root)
	Justification string `json:"justification"`
}

// POST /api/org-registry/orgs/{id}/parent
func (s *Server) apiOrgSetParent(w http.ResponseWriter, r *http.Request) {
	if !s.orgRegistryEnabled(w) || !requireOrgOperator(w, r) {
		return
	}
	id := r.PathValue("id")
	var req orgParentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		onboardError(w, http.StatusBadRequest, "bad_json", "could not parse body")
		return
	}
	actor := onboardAPIActor(r)
	var err error
	if strings.TrimSpace(req.Parent) == "" {
		//nolint:contextcheck // parent edge changes are owned by the org store rather than HTTP context
		err = s.Orgs.ClearParent(id, actor, req.Justification)
	} else {
		//nolint:contextcheck // parent edge changes are owned by the org store rather than HTTP context
		err = s.Orgs.SetParent(id, req.Parent, actor, req.Justification)
	}
	if err != nil {
		s.auditOrg(r, "org-registry.set-parent", id, "refused", err.Error(), map[string]any{"parent": req.Parent})
		orgStoreError(w, err)
		return
	}
	s.auditOrg(r, "org-registry.set-parent", id, "ok", "", map[string]any{
		"parent": req.Parent, "justification": req.Justification,
	})
	v, err := s.Orgs.GetOrg(id)
	if err != nil {
		orgStoreError(w, err)
		return
	}
	onboardJSON(w, http.StatusOK, v)
}

type orgAliasReq struct {
	Alias string `json:"alias"`
	Kind  string `json:"kind"` // name-variant | abbreviation | absorbed-id
}

// POST /api/org-registry/orgs/{id}/aliases
func (s *Server) apiOrgAddAlias(w http.ResponseWriter, r *http.Request) {
	if !s.orgRegistryEnabled(w) || !requireOrgOperator(w, r) {
		return
	}
	id := r.PathValue("id")
	var req orgAliasReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		onboardError(w, http.StatusBadRequest, "bad_json", "could not parse body")
		return
	}
	//nolint:contextcheck // alias accretion is best-effort and not gated on HTTP context
	if err := s.Orgs.AddAlias(req.Alias, id, req.Kind, onboardAPIActor(r)); err != nil {
		orgStoreError(w, err)
		return
	}
	s.auditOrg(r, "org-registry.add-alias", id, "ok", "", map[string]any{"alias": req.Alias, "kind": req.Kind})
	v, err := s.Orgs.GetOrg(id)
	if err != nil {
		orgStoreError(w, err)
		return
	}
	onboardJSON(w, http.StatusOK, v)
}

func (s *Server) auditOrg(r *http.Request, action, orgID, result, errMsg string, after map[string]any) {
	if after == nil {
		after = map[string]any{}
	}
	// The audit Target type is identity-shaped; the org id rides the After
	// map under a stable key instead of overloading an identity field.
	after["org"] = orgID
	s.Audit.Emit(r.Context(), audit.Event{
		Actor:        audit.Actor{Email: onboardAPIActor(r)},
		Action:       action,
		After:        after,
		RequestID:    requestID(r),
		Result:       result,
		ErrorMessage: errMsg,
	})
}

type orgUpdateReq struct {
	// Pointer semantics: an absent field is left unchanged; a present field
	// replaces. Kind/nation refuse empty replacements store-side.
	DisplayName   *string `json:"display_name"`
	Kind          *string `json:"kind"`
	Nation        *string `json:"nation"`
	Justification string  `json:"justification"`
	// ADR 1171. Correctable like every other org fact — and unlike Kind and
	// Nation it may be set back to "": "not yet declared" is a real state for
	// this axis, and refusing to return to it would make a wrong guess
	// permanent. Every org registered before this axis existed carries "".
	Composition *string `json:"composition,omitempty"`
}

// POST /api/org-registry/orgs/{id}/update — correct an existing org's facts
// (kind / nation / composition / display name) in place. The verb the `origin`
// kind mislabel lacked: a wrong fact captured at onboarding previously
// required DB surgery. Audited with before → after; never touches identity,
// grants, or keys (ADR 1112 §3 — org ids are immutable and off every wire
// anchor, so a fact edit is never a re-key event).
func (s *Server) apiOrgUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.orgRegistryEnabled(w) || !requireOrgOperator(w, r) {
		return
	}
	id := r.PathValue("id")
	var req orgUpdateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		onboardError(w, http.StatusBadRequest, "bad_json", "could not parse body")
		return
	}
	before, err := s.Orgs.GetOrg(id)
	if err != nil {
		orgStoreError(w, err)
		return
	}
	//nolint:contextcheck // org registry writes are intentionally decoupled from HTTP context; store owns its background context
	v, err := s.Orgs.UpdateOrg(id, orgstore.OrgUpdate{
		DisplayName: req.DisplayName, Kind: req.Kind, Nation: req.Nation,
		Composition: req.Composition,
	})
	if err != nil {
		s.auditOrg(r, "org-registry.update", before.ID, "error", err.Error(), nil)
		orgStoreError(w, err)
		return
	}
	s.auditOrg(r, "org-registry.update", v.ID, "ok", "", map[string]any{
		"before":        map[string]any{"kind": before.Kind, "nation": before.Nation, "display_name": before.DisplayName},
		"after":         map[string]any{"kind": v.Kind, "nation": v.Nation, "display_name": v.DisplayName},
		"justification": req.Justification,
	})
	onboardJSON(w, http.StatusOK, v)
}

type orgMergeReq struct {
	Into          string `json:"into"`
	Justification string `json:"justification"`
	// AcknowledgeGovernanceUnion asserts the caller fetched and reviewed
	// BOTH orgs' authority sets (govern nominations, grants-as-grantee,
	// shared pools — portal registry) before merging. Fail-closed: absent
	// or false refuses (design §8 — a merge is also a governance union).
	AcknowledgeGovernanceUnion bool `json:"acknowledge_governance_union"`
}

// POST /api/org-registry/orgs/{id}/merge — absorb {id} into `into`.
func (s *Server) apiOrgMerge(w http.ResponseWriter, r *http.Request) {
	if !s.orgRegistryEnabled(w) || !requireOrgOperator(w, r) {
		return
	}
	loser := r.PathValue("id")
	var req orgMergeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		onboardError(w, http.StatusBadRequest, "bad_json", "could not parse body")
		return
	}
	if strings.TrimSpace(req.Into) == "" {
		onboardError(w, http.StatusBadRequest, "into_required", "into is required")
		return
	}
	actor := onboardAPIActor(r)
	//nolint:contextcheck // merge semantics deliberately owned by org store; HTTP cancellation should not abort registry merge
	if err := s.Orgs.MergeOrg(loser, req.Into, actor, req.Justification, req.AcknowledgeGovernanceUnion); err != nil {
		s.auditOrg(r, "org-registry.merge", loser, "refused", err.Error(), map[string]any{"into": req.Into})
		orgStoreError(w, err)
		return
	}
	s.auditOrg(r, "org-registry.merge", loser, "ok", "", map[string]any{
		"into": req.Into, "justification": req.Justification,
	})
	v, err := s.Orgs.GetOrg(req.Into)
	if err != nil {
		orgStoreError(w, err)
		return
	}
	onboardJSON(w, http.StatusOK, v)
}

// POST /api/org-registry/orgs/{id}/unmerge — reactivate an absorbed org.
// Parent edges are NOT auto-reverted (effective-dated history is never
// rewritten); the operator re-attaches deliberately.
func (s *Server) apiOrgUnmerge(w http.ResponseWriter, r *http.Request) {
	if !s.orgRegistryEnabled(w) || !requireOrgOperator(w, r) {
		return
	}
	id := r.PathValue("id")
	actor := onboardAPIActor(r)
	//nolint:contextcheck // un-merge semantics deliberately owned by org store; HTTP cancellation should not abort registry un-merge
	if err := s.Orgs.UnmergeOrg(id, actor); err != nil {
		s.auditOrg(r, "org-registry.unmerge", id, "refused", err.Error(), nil)
		orgStoreError(w, err)
		return
	}
	s.auditOrg(r, "org-registry.unmerge", id, "ok", "", nil)
	v, err := s.Orgs.GetOrg(id)
	if err != nil {
		orgStoreError(w, err)
		return
	}
	onboardJSON(w, http.StatusOK, v)
}
