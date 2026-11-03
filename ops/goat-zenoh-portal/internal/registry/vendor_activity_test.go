package registry_test

import (
	"testing"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

// TestVendorActivity_Aggregates seeds a vendor, creates a topic, retires
// another, uploads a schema, logs audit events, and verifies the
// per-vendor aggregate query returns the correct counts.
func TestVendorActivity_Aggregates(t *testing.T) {
	r := open(t)
	ctx := t.Context()

	// Two vendors so we can verify the SELECT scopes per-vendor.
	if err := r.CreateVendor(ctx, &registry.Vendor{
		Prefix: "acme-corp", APIKeyHash: "hash1", DisplayName: "Acme Corporation",
	}); err != nil {
		t.Fatalf("create vendor acme-corp: %v", err)
	}
	if err := r.CreateVendor(ctx, &registry.Vendor{
		Prefix: "globex", APIKeyHash: "hash2", DisplayName: "Globex Corp",
	}); err != nil {
		t.Fatalf("create vendor globex: %v", err)
	}

	// Two active topics + one retired for acme-corp; one active for globex.
	t1, err := r.CreateTopic(ctx, &registry.Topic{
		VendorPrefix: "acme-corp", KeyPattern: "acme-corp/system/track/v1", Format: "json",
	})
	if err != nil {
		t.Fatalf("create topic t1: %v", err)
	}
	if _, err := r.CreateTopic(ctx, &registry.Topic{
		VendorPrefix: "acme-corp", KeyPattern: "acme-corp/system/health/v1", Format: "json",
	}); err != nil {
		t.Fatalf("create topic t2: %v", err)
	}
	t3, err := r.CreateTopic(ctx, &registry.Topic{
		VendorPrefix: "acme-corp", KeyPattern: "acme-corp/system/dead/v1", Format: "json",
	})
	if err != nil {
		t.Fatalf("create topic t3: %v", err)
	}
	if err := r.RetireTopic(ctx, t3); err != nil {
		t.Fatalf("retire t3: %v", err)
	}
	if _, err := r.CreateTopic(ctx, &registry.Topic{
		VendorPrefix: "globex", KeyPattern: "globex/system/pulse/v1", Format: "json",
	}); err != nil {
		t.Fatalf("create topic globex: %v", err)
	}

	// One schema upload on acme-corp's t1.
	if _, err := r.InsertSchema(ctx, &registry.TopicSchema{
		TopicID: t1, Version: "v1", Compatibility: "none", Format: "protobuf-fds",
		FilePath: "1/v1.fds", SizeBytes: 16, SHA256: "deadbeef", UploadedBy: "acme-corp",
	}); err != nil {
		t.Fatalf("insert schema: %v", err)
	}

	// One code-example upload on acme-corp's t1.
	if _, err := r.UpsertCodeExample(ctx, &registry.TopicCodeExample{
		TopicID: t1, Language: "python", Code: "print('hi')", UpdatedBy: "acme-corp",
	}); err != nil {
		t.Fatalf("upsert code example: %v", err)
	}

	// Audit events: one recent (last 24h) where acme-corp is actor, one
	// older (>24h) where acme-corp is target, none for globex.
	if err := r.LogAuditEvent(ctx, "acme-corp", "topic-create", "acme-corp/system/track/v1", "{}"); err != nil {
		t.Fatalf("log audit 1: %v", err)
	}
	// Force-insert an older audit row to exercise the 24h cutoff.
	twoDaysAgo := time.Now().Add(-48 * time.Hour).Unix()
	if _, err := r.DB().ExecContext(ctx,
		`INSERT INTO audit_events (occurred_at, actor, action, target, details)
		 VALUES ($1, $2, $3, $4, $5)`,
		twoDaysAgo, "operator", "vendor-register", "acme-corp", "{}"); err != nil {
		t.Fatalf("seed old audit row: %v", err)
	}

	rows, err := r.ListVendorActivity(ctx)
	if err != nil {
		t.Fatalf("ListVendorActivity: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 vendor rows, got %d", len(rows))
	}

	byPrefix := map[string]registry.VendorActivity{}
	for _, r := range rows {
		byPrefix[r.Prefix] = r
	}

	acme := byPrefix["acme-corp"]
	if acme.TopicsActive != 2 {
		t.Errorf("acme topics_active: want 2, got %d", acme.TopicsActive)
	}
	if acme.TopicsRetired != 1 {
		t.Errorf("acme topics_retired: want 1, got %d", acme.TopicsRetired)
	}
	if acme.SchemasUploaded != 1 {
		t.Errorf("acme schemas_uploaded: want 1, got %d", acme.SchemasUploaded)
	}
	if acme.CodeExamples != 1 {
		t.Errorf("acme code_examples: want 1, got %d", acme.CodeExamples)
	}
	if acme.AuditEventsTotal != 2 {
		t.Errorf("acme audit_events_total: want 2, got %d", acme.AuditEventsTotal)
	}
	if acme.AuditEventsLast24h != 1 {
		t.Errorf("acme audit_events_last24h: want 1, got %d", acme.AuditEventsLast24h)
	}
	if acme.LastAuditAt == nil {
		t.Errorf("acme last_audit_at: expected non-nil")
	}

	globex := byPrefix["globex"]
	if globex.TopicsActive != 1 {
		t.Errorf("globex topics_active: want 1, got %d", globex.TopicsActive)
	}
	if globex.AuditEventsTotal != 0 {
		t.Errorf("globex audit_events_total: want 0, got %d", globex.AuditEventsTotal)
	}
}

// TestVendorAuditEvents_Limits checks LIMIT + ordering on the per-vendor
// recent-events query.
func TestVendorAuditEvents_Limits(t *testing.T) {
	r := open(t)
	ctx := t.Context()

	if err := r.CreateVendor(ctx, &registry.Vendor{
		Prefix: "acme-corp", APIKeyHash: "hash", DisplayName: "Acme Corp",
	}); err != nil {
		t.Fatalf("create vendor: %v", err)
	}

	// 5 events; expect newest-first ordering.
	for i := 0; i < 5; i++ {
		if err := r.LogAuditEvent(ctx, "acme-corp", "topic-create", "acme-corp/x", "{}"); err != nil {
			t.Fatalf("log audit %d: %v", i, err)
		}
		time.Sleep(10 * time.Millisecond) // distinct occurred_at
	}

	rows, err := r.ListVendorAuditEvents(ctx, "acme-corp", 3)
	if err != nil {
		t.Fatalf("ListVendorAuditEvents: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows (limit), got %d", len(rows))
	}
	if rows[0].OccurredAt < rows[1].OccurredAt || rows[1].OccurredAt < rows[2].OccurredAt {
		t.Errorf("rows not newest-first: %v %v %v", rows[0].OccurredAt, rows[1].OccurredAt, rows[2].OccurredAt)
	}
}
