package registry

import (
	"context"
	"time"
)

// VendorActivity is the per-vendor aggregate row consumed by the portal's
// /admin/vendor-activity.html surface. All counts are pulled from the
// portal's own SQLite tables; the "associated identities" join lives in
// the API layer (Kratos admin lookup).
type VendorActivity struct {
	Prefix             string  `db:"prefix"               json:"prefix"`
	DisplayName        string  `db:"display_name"         json:"display_name"`
	Synthetic          bool    `db:"synthetic"            json:"synthetic"`
	CreatedAt          int64   `db:"created_at"           json:"created_at"`
	ApprovedAt         *int64  `db:"approved_at"          json:"approved_at,omitempty"`
	TopicsActive       int64   `db:"topics_active"        json:"topics_active"`
	TopicsRetired      int64   `db:"topics_retired"       json:"topics_retired"`
	SchemasUploaded    int64   `db:"schemas_uploaded"     json:"schemas_uploaded"`
	CodeExamples       int64   `db:"code_examples"        json:"code_examples"`
	AuditEventsTotal   int64   `db:"audit_events_total"   json:"audit_events_total"`
	AuditEventsLast24h int64   `db:"audit_events_last24h" json:"audit_events_last24h"`
	LastAuditAt        *int64  `db:"last_audit_at"        json:"last_audit_at,omitempty"`
}

// ListVendorActivity returns one row per vendor with per-surface aggregate
// counters joined in. Synthetic vendors are included with the Synthetic flag
// set so the UI can badge or hide them; the caller decides.
//
// Counts come from the portal's own tables and reflect what the portal
// has been asked to mediate — topic mutations, schema uploads, code-example
// uploads, operator audit log. Wire-level traffic (Zenoh publishes /
// subscribes) is NOT counted here; that data lives in the audit-subscriber
// NDJSON in MinIO and would require a separate index. The admin page notes
// the gap inline.
func (r *Registry) ListVendorActivity(ctx context.Context) ([]VendorActivity, error) {
	const q = `
SELECT
    v.prefix                                                         AS prefix,
    v.display_name                                                   AS display_name,
    v.synthetic                                                      AS synthetic,
    v.created_at                                                     AS created_at,
    v.approved_at                                                    AS approved_at,
    (SELECT COUNT(*) FROM topics t
       WHERE t.vendor_prefix = v.prefix AND t.retired_at IS NULL)    AS topics_active,
    (SELECT COUNT(*) FROM topics t
       WHERE t.vendor_prefix = v.prefix AND t.retired_at IS NOT NULL) AS topics_retired,
    (SELECT COUNT(*) FROM topic_schemas s
       JOIN topics t ON s.topic_id = t.id
       WHERE t.vendor_prefix = v.prefix)                              AS schemas_uploaded,
    (SELECT COUNT(*) FROM topic_code_examples e
       JOIN topics t ON e.topic_id = t.id
       WHERE t.vendor_prefix = v.prefix)                              AS code_examples,
    (SELECT COUNT(*) FROM audit_events a
       WHERE a.actor = v.prefix OR a.target = v.prefix)               AS audit_events_total,
    (SELECT COUNT(*) FROM audit_events a
       WHERE (a.actor = v.prefix OR a.target = v.prefix)
         AND a.occurred_at >= $1)                                      AS audit_events_last24h,
    (SELECT MAX(a.occurred_at) FROM audit_events a
       WHERE a.actor = v.prefix OR a.target = v.prefix)               AS last_audit_at
FROM vendors v
ORDER BY v.prefix ASC
`
	since24h := time.Now().Add(-24 * time.Hour).Unix()
	var rows []VendorActivity
	if err := r.reader().SelectContext(ctx, &rows, q, since24h); err != nil {
		return nil, err
	}
	return rows, nil
}

// VendorAuditEvent is a single audit row scoped to a vendor (either as
// actor or as target). Used to render the "recent activity" sub-table
// on the admin page when an operator drills into a specific vendor.
type VendorAuditEvent struct {
	ID         int64  `db:"id"          json:"id"`
	OccurredAt int64  `db:"occurred_at" json:"occurred_at"`
	Actor      string `db:"actor"       json:"actor"`
	Action     string `db:"action"      json:"action"`
	Target     string `db:"target"      json:"target"`
	Details    string `db:"details"     json:"details"`
}

// ListVendorAuditEvents returns the most recent audit events for which the
// vendor appears as actor or target, limited to `limit` rows ordered
// newest-first.
func (r *Registry) ListVendorAuditEvents(ctx context.Context, vendor string, limit int) ([]VendorAuditEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	const q = `
SELECT id, occurred_at, actor, action, target, details
FROM audit_events
WHERE actor = $1 OR target = $2
ORDER BY occurred_at DESC
LIMIT $3
`
	var rows []VendorAuditEvent
	if err := r.reader().SelectContext(ctx, &rows, q, vendor, vendor, limit); err != nil {
		return nil, err
	}
	return rows, nil
}
