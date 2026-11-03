package api

// Topic-budget grant surface + slot lifecycle classes (operator-only).
//
// The registry home of the per-goatnet allowance record
// (zenoh-cardinality-budget-and-lazy-crawler.md §2 layer 2b + §3 layer 3;
// ADR 1075 grant-store fold-in) and the ADR 1104 lifecycle-class stamp.
// /api/budget-projection serves the fabric-crawler's budget-source contract
// — the same JSON shape the ansible-rendered projection file carried — so
// the crawler's source flips from a file path to this resolver endpoint
// with its live/stale/unavailable honesty semantics unchanged.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

// handleBudgetProjection serves the crawler budget-source contract:
//
//	{
//	  "self_service_block": 100,
//	  "auto_self_service_anchors": ["<anchor>", ...],   // assigned slots
//	  "grants": {"<owner>": {"granted": N, "prefixes": [...], "note": "..."}}
//	}
//
// 503 when the self-block is unconfigured — the crawler then degrades to its
// honest stale/unavailable states rather than misreading every auto-block
// owner as unmeasured.
//
// It also 503s on an implausibly EMPTY projection (the sanity floor below): a
// successful 200 carrying zero anchors and zero grants is indistinguishable, to
// the crawler, from a fabric where nobody is onboarded — so a restored or emptied
// registry silently reclassified every owner as unmeasured while the
// source-health metric stayed green (identity fragility audit §1.3
// violation 1). The honesty ladder only works if this endpoint refuses to serve
// an answer it cannot stand behind.
func (s *Server) handleBudgetProjection(w http.ResponseWriter, r *http.Request) {
	if s.cfg.TopicBudgetSelfBlock <= 0 {
		writeError(w, http.StatusServiceUnavailable, "config",
			"PORTAL_TOPIC_BUDGET_SELF_BLOCK unset; budget projection disabled")
		return
	}
	anchors, err := s.reg.AssignedSlotAnchors(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	if anchors == nil {
		anchors = []string{} // marshal as [], never null
	}
	rows, err := s.reg.ListTopicBudgets(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	grants := map[string]any{}
	for _, b := range rows {
		entry := map[string]any{"granted": b.Granted}
		if ps := b.PrefixList(); len(ps) > 0 {
			entry["prefixes"] = ps
		}
		if b.Note != "" {
			entry["note"] = b.Note
		}
		grants[b.Owner] = entry
	}
	// Sanity floor. A pool with slots baked but NOTHING assigned and NO grant on
	// record is not a quiet fabric — a goatnet reaches that state only by having
	// its registry reset or restored from an empty snapshot. Refuse rather than
	// hand the crawler a valid-looking empty projection it will believe.
	if len(anchors) == 0 && len(grants) == 0 {
		total, _, cerr := s.reg.SlotPoolCounts(r.Context())
		if cerr != nil {
			writeError(w, http.StatusInternalServerError, "db", cerr.Error())
			return
		}
		if total > 0 {
			writeError(w, http.StatusServiceUnavailable, "registry_reset",
				fmt.Sprintf("registry looks reset: %d slots are baked into the pool but 0 are assigned and 0 budget grants exist. "+
					"Refusing to serve an empty projection — consuming it would reclassify every owner as unmeasured. "+
					"Verify the portal is pointed at the live registry (restore it if it was reset), then re-poll.", total))
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"self_service_block":        s.cfg.TopicBudgetSelfBlock,
		"auto_self_service_anchors": anchors,
		"grants":                    grants,
	})
}

// handleListBudgetGrants returns every explicit allowance row.
func (s *Server) handleListBudgetGrants(w http.ResponseWriter, r *http.Request) {
	rows, err := s.reg.ListTopicBudgets(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	type outRow struct {
		registry.TopicBudget
		Prefixes []string `json:"prefixes,omitempty"`
	}
	out := make([]outRow, 0, len(rows))
	for _, b := range rows {
		out = append(out, outRow{TopicBudget: b, Prefixes: b.PrefixList()})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"self_service_block": s.cfg.TopicBudgetSelfBlock,
		"grants":             out,
	})
}

// handlePutBudgetGrant upserts one owner's allowance.
// Body: {"granted": N, "prefixes": ["chan", ...], "note": "approver + date"}.
// A prefixes change actuates in the router ACL only at the next render +
// restart window — recorded here, enforced there.
func (s *Server) handlePutBudgetGrant(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	var body struct {
		Granted  int64    `json:"granted"`
		Prefixes []string `json:"prefixes"`
		Note     string   `json:"note"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeBodyError(w, err, "decode", err.Error())
		return
	}
	if err := s.reg.UpsertTopicBudget(r.Context(), owner, body.Granted, body.Prefixes, body.Note, actorOf(r)); err != nil {
		writeError(w, http.StatusBadRequest, "validate", err.Error())
		return
	}
	details, _ := json.Marshal(body)
	_ = s.reg.LogAuditEvent(r.Context(), actorOf(r), "budget.grant.put", owner, string(details))
	writeJSON(w, http.StatusOK, map[string]any{"owner": owner, "granted": body.Granted, "prefixes": body.Prefixes})
}

// handleDeleteBudgetGrant removes an owner's explicit allowance (falls back
// to the automatic block for onboarded slots, unmeasured otherwise).
func (s *Server) handleDeleteBudgetGrant(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	if err := s.reg.DeleteTopicBudget(r.Context(), owner); err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			writeError(w, http.StatusNotFound, "notfound", "no grant on record for "+owner)
			return
		}
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	_ = s.reg.LogAuditEvent(r.Context(), actorOf(r), "budget.grant.delete", owner, "")
	writeJSON(w, http.StatusOK, map[string]any{"owner": owner, "deleted": true})
}

// handlePutSlotLifecycle stamps a slot's ADR 1104 lifecycle class.
// Body: {"class": "test" | "notional" | "real" | ""} ('' clears; absent =
// real for every consumer). Per-class storage TTL actuates at the next
// router render + restart window.
func (s *Server) handlePutSlotLifecycle(w http.ResponseWriter, r *http.Request) {
	anchor := r.PathValue("anchor")
	var body struct {
		Class string `json:"class"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeBodyError(w, err, "decode", err.Error())
		return
	}
	if err := s.reg.SetSlotLifecycleClass(r.Context(), anchor, body.Class); err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			writeError(w, http.StatusNotFound, "notfound", "no such slot")
			return
		}
		writeError(w, http.StatusBadRequest, "validate", err.Error())
		return
	}
	_ = s.reg.LogAuditEvent(r.Context(), actorOf(r), "slot.lifecycle.set", anchor, `{"class":"`+body.Class+`"}`)
	writeJSON(w, http.StatusOK, map[string]any{"anchor": anchor, "lifecycle_class": body.Class})
}

// handlePutSlotIdentity records the Kratos identity UUID of the person a slot
// belongs to, for the two cases the inline onboard stamp cannot cover: a caller
// whose identity leg runs after the slot mint (the native `goat admin onboard`
// order is NetBird → slot → Ory), and the backfill of slots that predate the
// column. Body: {"identity_id": "<uuid>"}; an empty value CLEARS the linkage,
// which is how a wrong stamp is withdrawn — leaving an honest "not recorded"
// rather than a confident wrong owner.
//
// Operator-gated and audited: this writes who owns a namespace, and ownership is
// what revocation targets.
func (s *Server) handlePutSlotIdentity(w http.ResponseWriter, r *http.Request) {
	anchor := r.PathValue("anchor")
	var body struct {
		IdentityID string `json:"identity_id"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeBodyError(w, err, "decode", err.Error())
		return
	}
	id := strings.TrimSpace(body.IdentityID)
	if id != "" {
		if err := registry.ValidIdentityID(id); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_identity_id", err.Error())
			return
		}
	}
	if err := s.reg.StampSlotIdentity(r.Context(), anchor, id); err != nil {
		writeError(w, http.StatusNotFound, "notfound", err.Error())
		return
	}
	_ = s.reg.LogAuditEvent(r.Context(), actorOf(r), "slot.identity.set", anchor,
		fmt.Sprintf(`{"identity_id":%q}`, id))
	writeJSON(w, http.StatusOK, map[string]any{"anchor": anchor, "identity_id": id})
}

// handleListSlotLifecycles returns anchor -> class for every classified slot
// (the ADR 1104 render input; absent = real).
func (s *Server) handleListSlotLifecycles(w http.ResponseWriter, r *http.Request) {
	classes, err := s.reg.SlotLifecycleClasses(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"classes": classes})
}
