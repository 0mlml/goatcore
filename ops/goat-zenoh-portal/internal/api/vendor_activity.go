package api

import (
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/kratosadmin"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

// vendorActivityResponse is the JSON shape returned by
// GET /api/admin/vendor-activity. One Vendors entry per row of the
// portal's vendors table, joined with per-surface aggregate counts and
// (when KratosAdminURL is set) the list of Ory identities associated
// with the vendor via traits.vendor.short_tag.
type vendorActivityResponse struct {
	Vendors           []vendorActivityRow `json:"vendors"`
	IdentityRosterGap string              `json:"identity_roster_gap,omitempty"`
	PublishGap        string              `json:"publish_gap"`
	GeneratedAt       int64               `json:"generated_at"`
}

type vendorActivityRow struct {
	registry.VendorActivity
	Identities []vendorActivityIdentity `json:"identities"`
}

type vendorActivityIdentity struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	State string `json:"state"`
}

// handleAdminVendorActivity serves GET /api/admin/vendor-activity. Operator-
// only — gated by requireOperator at the route layer.
func (s *Server) handleAdminVendorActivity(w http.ResponseWriter, r *http.Request) {
	_ = s.reg.LogAuditEvent(r.Context(), actorOf(r), "admin.vendor-activity.read", "", "")
	rows, err := s.reg.ListVendorActivity(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "registry_query_failed", err.Error())
		return
	}

	// Best-effort identity lookup. When Kratos is unreachable we log + carry
	// on with empty identity lists; the page renders "Kratos unreachable"
	// inline so the operator can act.
	identitiesByVendor := map[string][]kratosadmin.Identity{}
	gap := ""
	if s.kratos != nil {
		ids, err := s.kratos.ListIdentities(r.Context())
		if err != nil {
			slog.WarnContext(r.Context(), "kratos admin lookup failed for vendor-activity",
				slog.String("err", err.Error()))
			gap = "Kratos admin lookup failed at render time — identity roster shown below may be empty or stale. " +
				"See portal logs for the underlying error."
		} else {
			identitiesByVendor = kratosadmin.IdentitiesByVendor(ids)
		}
	} else {
		gap = "--kratos-admin-url not configured for this portal instance. " +
			"Set PORTAL_KRATOS_ADMIN_URL (e.g. http://kratos:4434) to populate the identities column."
	}

	resp := vendorActivityResponse{
		Vendors:           make([]vendorActivityRow, 0, len(rows)),
		IdentityRosterGap: gap,
		PublishGap: "Wire-level publish/subscribe counts (audit-subscriber NDJSON in MinIO) are NOT shown here. " +
			"This page surfaces only what the portal mediates: topic/schema/code-example mutations + the operator audit log. " +
			"Per-individual action attribution requires operator OIDC against the portal admin API (queued follow-on).",
		GeneratedAt: time.Now().Unix(),
	}

	for _, vrow := range rows {
		ids := identitiesByVendor[vrow.Prefix]
		// Stable order — by email so the operator sees the same shape across
		// refreshes.
		sort.Slice(ids, func(i, j int) bool { return ids[i].Email() < ids[j].Email() })
		idsOut := make([]vendorActivityIdentity, 0, len(ids))
		for _, i := range ids {
			idsOut = append(idsOut, vendorActivityIdentity{
				ID: i.ID, Email: i.Email(), Name: i.Name(), State: i.State,
			})
		}
		resp.Vendors = append(resp.Vendors, vendorActivityRow{
			VendorActivity: vrow,
			Identities:     idsOut,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleAdminVendorAuditEvents serves GET /api/admin/vendor-activity/{vendor}/events.
// Returns up to 50 most-recent audit events involving the named vendor (as actor
// OR target). Operator-only — gated at the route layer.
func (s *Server) handleAdminVendorAuditEvents(w http.ResponseWriter, r *http.Request) {
	vendor := r.PathValue("vendor")
	_ = s.reg.LogAuditEvent(r.Context(), actorOf(r), "admin.vendor-activity.read", vendor, "")
	const limit = 50
	rows, err := s.reg.ListVendorAuditEvents(r.Context(), vendor, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "registry_query_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"vendor": vendor,
		"events": rows,
		"limit":  limit,
	})
}
