package registry

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// VendorProgress matches the vendor_progress table. Level timestamps
// are Unix seconds; nil means "not reached yet." L1 is NOT here — it
// is derived from vendors.bundle_minted_at on read (see ListProgress).
type VendorProgress struct {
	Prefix        string `db:"prefix"           json:"prefix"`
	L2At          *int64 `db:"l2_at"            json:"l2_at,omitempty"`
	L3At          *int64 `db:"l3_at"            json:"l3_at,omitempty"`
	L4At          *int64 `db:"l4_at"            json:"l4_at,omitempty"`
	LastPublishAt *int64 `db:"last_publish_at"  json:"last_publish_at,omitempty"`
	PeerCount     int64  `db:"peer_count"       json:"peer_count"`
	UpdatedAt     int64  `db:"updated_at"       json:"updated_at"`
}

// MarkL2 sets l2_at (sticky) and updates peer_count. Inserts a row if
// none exists yet so a vendor that registered but never minted a
// bundle still appears in the swimlane when their first peer shows up.
func (r *Registry) MarkL2(ctx context.Context, prefix string, peerCount int64) error {
	now := time.Now().Unix()
	if peerCount > 0 {
		_, err := r.db.ExecContext(ctx, `
			INSERT INTO vendor_progress (prefix, l2_at, peer_count, updated_at)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT(prefix) DO UPDATE SET
				l2_at = COALESCE(vendor_progress.l2_at, excluded.l2_at),
				peer_count = excluded.peer_count,
				updated_at = excluded.updated_at
		`, prefix, now, peerCount, now)
		return err
	}
	// No peers — just update peer_count (don't touch l2_at).
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO vendor_progress (prefix, peer_count, updated_at)
		VALUES ($1, $2, $3)
		ON CONFLICT(prefix) DO UPDATE SET
			peer_count = excluded.peer_count,
			updated_at = excluded.updated_at
	`, prefix, peerCount, now)
	return err
}

// MarkPublish updates last_publish_at and either l3_at (helloWorld=true)
// or l4_at (helloWorld=false). Both timestamps are sticky via COALESCE.
// ts is the event timestamp (Unix seconds); when 0, uses now.
func (r *Registry) MarkPublish(ctx context.Context, prefix string, ts int64, helloWorld bool) error {
	now := time.Now().Unix()
	if ts == 0 {
		ts = now
	}
	if helloWorld {
		_, err := r.db.ExecContext(ctx, `
			INSERT INTO vendor_progress (prefix, l3_at, last_publish_at, updated_at)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT(prefix) DO UPDATE SET
				l3_at = COALESCE(vendor_progress.l3_at, excluded.l3_at),
				last_publish_at = GREATEST(COALESCE(vendor_progress.last_publish_at, 0), excluded.last_publish_at),
				updated_at = excluded.updated_at
		`, prefix, ts, ts, now)
		return err
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO vendor_progress (prefix, l4_at, last_publish_at, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT(prefix) DO UPDATE SET
			l4_at = COALESCE(vendor_progress.l4_at, excluded.l4_at),
			last_publish_at = GREATEST(COALESCE(vendor_progress.last_publish_at, 0), excluded.last_publish_at),
			updated_at = excluded.updated_at
	`, prefix, ts, ts, now)
	return err
}

// VendorProgressRow is the joined row consumed by the swimlane API. L1
// is derived from vendors.bundle_minted_at; L2/L3/L4 come from the
// progress table; rows for vendors that have never had progress
// activity still appear (LEFT JOIN) so a freshly-registered vendor
// shows up immediately with just L1.
type VendorProgressRow struct {
	Prefix        string `db:"prefix"`
	DisplayName   string `db:"display_name"`
	Synthetic     bool   `db:"synthetic"`
	L1At          *int64 `db:"l1_at"`
	L2At          *int64 `db:"l2_at"`
	L3At          *int64 `db:"l3_at"`
	L4At          *int64 `db:"l4_at"`
	LastPublishAt *int64 `db:"last_publish_at"`
	PeerCount     int64  `db:"peer_count"`
	// PublishCount and LastTopic are per-ORG rollups maintained by the audit
	// tailer, one row per org. They used to be derived by aggregating one row
	// per (org, topic) keyed on the concrete key expression, which is
	// unbounded by publisher behaviour and reached 86,810 rows.
	PublishCount int64   `db:"publish_count"`
	LastTopic    *string `db:"last_topic"`
	// DistinctTopics is NOT read from the registry — it comes from the
	// activity source (internal/activity), which counts DECLARED key patterns
	// rather than raw key strings. The registry cannot answer it without
	// storing the per-topic rows this change exists to stop storing, so the
	// API layer fills these two in after the query. DistinctTopicsKnown is
	// false when the source could not answer, and the surface must then render
	// the field as unknown rather than as zero.
	DistinctTopics      int64 `db:"-"`
	DistinctTopicsKnown bool  `db:"-"`
	// Bundle collection, which sits BETWEEN L1 (minted) and L2 (peer seen) and
	// was invisible previously . Minted-but-never-downloaded and
	// downloaded-but-never-published are different stalls with different
	// remedies, and the swimlane could not tell them apart.
	// BundleDownloadedAt nil on a row minted before the column existed means
	// unknown, not never.
	BundleDownloadedAt  *int64 `db:"bundle_downloaded_at"`
	BundleDownloadCount int64  `db:"bundle_download_count"`
	// RegisteredTopics is how many topics the org has DECLARED in the portal
	// (the topics table), as against DistinctTopics above, which counts topics
	// observed on the wire. The two diverge in both directions and the gap is
	// diagnostic: declared-but-never-published is an integration that never
	// started, published-but-never-declared is a publisher whose schemas the
	// decode surfaces cannot render.
	RegisteredTopics int64 `db:"registered_topics"`
}

func (r *Registry) ListProgress(ctx context.Context, includeSynthetic bool) ([]VendorProgressRow, error) {
	q := `
		SELECT v.prefix, v.display_name, v.synthetic,
		       v.bundle_minted_at AS l1_at,
		       p.l2_at, p.l3_at, p.l4_at,
		       p.last_publish_at, COALESCE(p.peer_count, 0) AS peer_count,
		       COALESCE(p.publish_count, 0) AS publish_count, p.last_topic,
		       v.bundle_downloaded_at, v.bundle_download_count,
		       COALESCE((SELECT COUNT(*) FROM topics t WHERE t.vendor_prefix = v.prefix AND t.retired_at IS NULL), 0) AS registered_topics
		FROM vendors v
		LEFT JOIN vendor_progress p ON p.prefix = v.prefix
	`
	if !includeSynthetic {
		q += ` WHERE v.synthetic = 0`
	}
	q += ` ORDER BY
		(CASE WHEN p.l4_at IS NOT NULL THEN 4
		      WHEN p.l3_at IS NOT NULL THEN 3
		      WHEN p.l2_at IS NOT NULL THEN 2
		      WHEN v.bundle_minted_at IS NOT NULL THEN 1
		      ELSE 0 END) DESC,
		COALESCE(p.last_publish_at, 0) DESC,
		v.prefix ASC`
	var rows []VendorProgressRow
	if err := r.reader().SelectContext(ctx, &rows, q); err != nil {
		return nil, err
	}
	return rows, nil
}

// AuditReplayCursor holds the per-audit-subscriber tailer cursor.
type AuditReplayCursor struct {
	SubscriberID string `db:"subscriber_id"`
	LastKey      string `db:"last_key"`
	LastSeenAt   int64  `db:"last_seen_at"`
}

// GetAuditCursor returns the cursor row for a subscriber. Empty cursor
// (zero value LastKey) when no row exists — the caller starts from the
// beginning of the bucket. Not an error.
func (r *Registry) GetAuditCursor(ctx context.Context, subscriberID string) (*AuditReplayCursor, error) {
	var c AuditReplayCursor
	err := r.reader().GetContext(ctx, &c,
		`SELECT subscriber_id, last_key, last_seen_at FROM audit_replay_cursor WHERE subscriber_id = $1`,
		subscriberID)
	if errors.Is(err, sqlErrNoRows()) {
		return &AuditReplayCursor{SubscriberID: subscriberID}, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// SetAuditCursor upserts the cursor row.
func (r *Registry) SetAuditCursor(ctx context.Context, subscriberID, lastKey string) error {
	now := time.Now().Unix()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO audit_replay_cursor (subscriber_id, last_key, last_seen_at)
		VALUES ($1, $2, $3)
		ON CONFLICT(subscriber_id) DO UPDATE SET
			last_key = excluded.last_key,
			last_seen_at = excluded.last_seen_at
	`, subscriberID, lastKey, now)
	return err
}

// AuditProgressUpdate is one vendor's publish evidence aggregated from a
// single audit object. HelloFirstAt/OtherFirstAt carry the EARLIEST
// hello (L3) and non-hello (L4) event timestamps so the sticky level
// stamps land on the same value per-event processing would have picked;
// LastPublishAt carries the latest publish of either kind. All Unix
// seconds; 0 means "no such event in this object".
type AuditProgressUpdate struct {
	Prefix        string
	HelloFirstAt  int64
	OtherFirstAt  int64
	LastPublishAt int64
	// PublishDelta is how many publishes this object carried for the org. It
	// accumulates into a per-ORG counter, which is the whole point: the
	// previous shape kept the same total as one row per (org, topic) keyed on
	// the concrete key expression, so a publisher using a fresh key per
	// message earned a fresh row per message — 86,810 rows against 353
	// declared topics. One row per org cannot grow.
	PublishDelta int64
	// LastTopic is the last key expression seen for the org in this object,
	// carried so the swimlane keeps its "most recently touched" field without
	// the registry storing one row per topic to derive it from.
	LastTopic string
}

// ApplyAuditObject applies one audit object's aggregated writes — the
// per-org vendor_progress upserts AND the cursor advance to lastKey — in ONE
// transaction with one commit.
//
// Two properties, both load-bearing, and they fix different halves of F-537.
//
// ONE COMMIT PER OBJECT bounds the transaction RATE. The original shape was
// two autocommit transactions per EVENT, roughly 118 a second at ZAGROS's
// message rate. The audit-subscriber batches on a 60-second interval, so an
// object is a minute of traffic and this is about one transaction a minute.
// It also makes replay exactly-once at object grain: any failure rolls back
// the whole object INCLUDING the cursor, so a retried object never
// double-counts.
//
// ONE ROW PER ORG bounds the transaction SIZE, which the rate alone does not.
// This used to also upsert vendor_topic_activity, one row per (org, topic)
// keyed on the concrete key expression — so a publisher using a fresh key per
// message earned a fresh row per message, and one org held 61,540 rows for
// 74,427 publishes. Each commit then dirtied pages all over an 86,810-row
// b-tree that only ever grew. The counters are per-org now, against a table
// with one row per org, which cannot grow with traffic.
//
// What was lost by doing that, and where it went: the count of DISTINCT topics
// an org has published under is no longer derivable here. It comes from
// internal/activity instead, which reads the audit-subscriber's Prometheus
// counters — already labelled by DECLARED key pattern, 224 series against
// these 86,810 rows, bounded by the component that actually sees the traffic.
func (r *Registry) ApplyAuditObject(ctx context.Context, subscriberID, lastKey string, updates []AuditProgressUpdate) error {
	now := time.Now().Unix()
	tx, err := r.beginLocked(ctx)
	if err != nil {
		return fmt.Errorf("begin audit-object tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, u := range updates {
		// Same sticky/monotonic semantics as MarkPublish, one row per
		// vendor instead of one write per event: COALESCE keeps an
		// already-set level stamp, MAX keeps last_publish_at monotonic.
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO vendor_progress (prefix, l3_at, l4_at, last_publish_at, publish_count, last_topic, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT(prefix) DO UPDATE SET
				l3_at = COALESCE(vendor_progress.l3_at, excluded.l3_at),
				l4_at = COALESCE(vendor_progress.l4_at, excluded.l4_at),
				last_publish_at = GREATEST(COALESCE(vendor_progress.last_publish_at, 0), COALESCE(excluded.last_publish_at, 0)),
				publish_count = vendor_progress.publish_count + excluded.publish_count,
				-- last_topic means "the topic of the most recent publish", so
				-- it may only move when last_publish_at moves. Two cases this
				-- guards, both real: an object replayed out of order carries an
				-- OLDER publish and must not rewind the name, and an object with
				-- no publishes for this org carries no name at all and must not
				-- blank the one the surface is showing.
				last_topic = CASE
					WHEN excluded.last_topic IS NOT NULL
					 AND COALESCE(excluded.last_publish_at, 0) >= COALESCE(vendor_progress.last_publish_at, 0)
					THEN excluded.last_topic
					ELSE vendor_progress.last_topic
				END,
				updated_at = excluded.updated_at
		`, u.Prefix, nullIfZero(u.HelloFirstAt), nullIfZero(u.OtherFirstAt), nullIfZero(u.LastPublishAt),
			u.PublishDelta, nullIfEmpty(u.LastTopic), now); err != nil {
			return fmt.Errorf("upsert vendor_progress %s: %w", u.Prefix, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO audit_replay_cursor (subscriber_id, last_key, last_seen_at)
		VALUES ($1, $2, $3)
		ON CONFLICT(subscriber_id) DO UPDATE SET
			last_key = excluded.last_key,
			last_seen_at = excluded.last_seen_at
	`, subscriberID, lastKey, now); err != nil {
		return fmt.Errorf("advance audit cursor: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit audit object: %w", err)
	}
	return nil
}

// nullIfEmpty maps "" to SQL NULL, so an object that carried no topic for an
// org leaves last_topic alone instead of overwriting it with a blank.
func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// nullIfZero maps 0 to SQL NULL for the optional Unix-seconds columns.
func nullIfZero(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}
