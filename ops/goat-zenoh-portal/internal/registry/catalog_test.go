package registry

import (
	"context"
	"testing"
)

// seedCatalog builds a registry with three orgs: one with two topics (one of
// which carries two schema versions), one with a retired topic only, and one
// that has declared nothing.
func seedCatalog(t *testing.T) *Registry {
	t.Helper()
	r := testRegistry(t)
	ctx := context.Background()

	for _, v := range []struct {
		prefix    string
		synthetic bool
	}{{"alpha", false}, {"beta", false}, {"gamma", true}} {
		if err := r.CreateVendor(ctx, &Vendor{Prefix: v.prefix, DisplayName: v.prefix, Synthetic: v.synthetic}); err != nil {
			t.Fatalf("create vendor %s: %v", v.prefix, err)
		}
	}

	liveID, err := r.CreateTopic(ctx, &Topic{VendorPrefix: "alpha", KeyPattern: "alpha/tracks/v1", Format: "protobuf:Track"})
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	if _, err := r.CreateTopic(ctx, &Topic{VendorPrefix: "alpha", KeyPattern: "alpha/status/v1", Format: "json"}); err != nil {
		t.Fatalf("create topic: %v", err)
	}
	retiredID, err := r.CreateTopic(ctx, &Topic{VendorPrefix: "beta", KeyPattern: "beta/gone/v1", Format: "json"})
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	if err := r.RetireTopic(ctx, retiredID); err != nil {
		t.Fatalf("retire topic: %v", err)
	}
	for _, v := range []string{"v1", "v2"} {
		if _, err := r.InsertSchema(ctx, &TopicSchema{TopicID: liveID, Version: v, Format: "protobuf-source", Compatibility: "backward", FilePath: "alpha/tracks/" + v + ".proto", SHA256: "x" + v}); err != nil {
			t.Fatalf("insert schema %s: %v", v, err)
		}
	}
	return r
}

// A retired topic is not part of the declared surface, and an org whose only
// topic is retired counts as having declared nothing.
func TestListCatalogOmitsRetiredTopicsAndEmptyOrgs(t *testing.T) {
	r := seedCatalog(t)
	cat, err := r.ListCatalog(context.Background(), false)
	if err != nil {
		t.Fatalf("ListCatalog: %v", err)
	}
	if got, want := len(cat.Orgs), 1; got != want {
		t.Fatalf("orgs rendered = %d, want %d (%+v)", got, want, cat.Orgs)
	}
	if cat.Orgs[0].Prefix != "alpha" {
		t.Errorf("rendered org = %q, want alpha", cat.Orgs[0].Prefix)
	}
	if got, want := len(cat.Orgs[0].Topics), 2; got != want {
		t.Errorf("alpha topics = %d, want %d", got, want)
	}
	for _, tp := range cat.Orgs[0].Topics {
		if tp.KeyPattern == "beta/gone/v1" {
			t.Error("retired topic appears in the catalog")
		}
		// `goat topic ls` reads the whole goatnet from the catalog and prints
		// each row's id in --json, as the per-org listing does.
		if tp.ID == 0 {
			t.Errorf("catalog topic %q carries no id", tp.KeyPattern)
		}
	}
}

// The omission has to be visible: a reader looking at a goatnet where most
// orgs have declared nothing must be able to tell that from the totals rather
// than conclude the fabric is empty.
func TestListCatalogTotalsReportOmittedOrgs(t *testing.T) {
	r := seedCatalog(t)
	cat, err := r.ListCatalog(context.Background(), false)
	if err != nil {
		t.Fatalf("ListCatalog: %v", err)
	}
	if got, want := cat.Totals.Orgs, 3; got != want {
		t.Errorf("Totals.Orgs = %d, want %d", got, want)
	}
	if got, want := cat.Totals.OrgsWithTopics, 1; got != want {
		t.Errorf("Totals.OrgsWithTopics = %d, want %d", got, want)
	}
	if got, want := cat.Totals.OrgsWithoutTopics, 2; got != want {
		t.Errorf("Totals.OrgsWithoutTopics = %d, want %d", got, want)
	}
	if got, want := cat.Totals.Topics, 2; got != want {
		t.Errorf("Totals.Topics = %d, want %d", got, want)
	}
	if got, want := cat.Totals.SchemaVersions, 2; got != want {
		t.Errorf("Totals.SchemaVersions = %d, want %d", got, want)
	}
	if got, want := cat.Totals.Formats, 2; got != want {
		t.Errorf("Totals.Formats = %d, want %d", got, want)
	}
}

func TestListCatalogIncludeEmptyKeepsSilentOrgs(t *testing.T) {
	r := seedCatalog(t)
	cat, err := r.ListCatalog(context.Background(), true)
	if err != nil {
		t.Fatalf("ListCatalog: %v", err)
	}
	if got, want := len(cat.Orgs), 3; got != want {
		t.Fatalf("orgs rendered = %d, want %d", got, want)
	}
	for _, o := range cat.Orgs {
		if o.Topics == nil {
			t.Errorf("org %s has nil Topics; want an empty slice so it marshals as [] not null", o.Prefix)
		}
	}
}

// Schema versions have to arrive attached to their own topic — the failure
// this guards is an off-by-one join that shifts every version onto the
// neighbouring row, which reads as plausible data and is wrong everywhere.
func TestListCatalogAttachesSchemaVersionsToTheRightTopic(t *testing.T) {
	r := seedCatalog(t)
	cat, err := r.ListCatalog(context.Background(), false)
	if err != nil {
		t.Fatalf("ListCatalog: %v", err)
	}
	byKey := map[string][]CatalogSchema{}
	for _, tp := range cat.Orgs[0].Topics {
		byKey[tp.KeyPattern] = tp.Schemas
	}
	if got, want := len(byKey["alpha/tracks/v1"]), 2; got != want {
		t.Errorf("alpha/tracks/v1 schema versions = %d, want %d", got, want)
	}
	if got := byKey["alpha/status/v1"]; len(got) != 0 {
		t.Errorf("alpha/status/v1 schema versions = %v, want none", got)
	}
	if byKey["alpha/status/v1"] == nil {
		t.Error("a topic with no schema has nil Schemas; want an empty slice so it marshals as [] not null")
	}
}

// The catalog carries, per org, the only publish-time fact the registry holds
// (the audit tailer's per-org roll-up) and the slot's display alias. Both are
// LEFT-join shaped: an org with no progress row reports no publish and a count
// of zero, and an org that is not a slot (or a revoked slot) has no alias. The
// alias is display-only (ADR 1039) — the test pins that it arrives as its own
// field and never replaces the prefix.
func TestListCatalogCarriesPublishRollupAndAlias(t *testing.T) {
	r := testRegistry(t)
	ctx := context.Background()

	if _, err := r.InitializeSlotPool(ctx, "cat-test", 2); err != nil {
		t.Fatalf("init slot pool: %v", err)
	}
	slot, err := r.AllocateSlot(ctx, "mantis-alpha")
	if err != nil {
		t.Fatalf("allocate slot: %v", err)
	}
	for _, prefix := range []string{slot.SlotID, "quiet"} {
		if err := r.CreateVendor(ctx, &Vendor{Prefix: prefix, DisplayName: prefix}); err != nil {
			t.Fatalf("create vendor %s: %v", prefix, err)
		}
		if _, err := r.CreateTopic(ctx, &Topic{VendorPrefix: prefix, KeyPattern: prefix + "/tracks/v1", Format: "json"}); err != nil {
			t.Fatalf("create topic under %s: %v", prefix, err)
		}
	}
	if err := r.ApplyAuditObject(ctx, "sub", "audit/obj-1", []AuditProgressUpdate{
		{Prefix: slot.SlotID, LastPublishAt: 1_700_000_000, PublishDelta: 41, LastTopic: slot.SlotID + "/tracks/v1"},
	}); err != nil {
		t.Fatalf("apply audit object: %v", err)
	}

	cat, err := r.ListCatalog(ctx, false)
	if err != nil {
		t.Fatalf("ListCatalog: %v", err)
	}
	byPrefix := map[string]CatalogOrg{}
	for _, o := range cat.Orgs {
		byPrefix[o.Prefix] = o
	}
	got, ok := byPrefix[slot.SlotID]
	if !ok {
		t.Fatalf("slot org %s missing from catalog: %+v", slot.SlotID, cat.Orgs)
	}
	if got.Alias != "mantis-alpha" {
		t.Errorf("alias = %q, want mantis-alpha", got.Alias)
	}
	if got.Prefix != slot.SlotID {
		t.Errorf("prefix = %q, want the slot anchor %q (alias must not replace it)", got.Prefix, slot.SlotID)
	}
	if got.LastPublishAt == nil || *got.LastPublishAt != 1_700_000_000 {
		t.Errorf("last_publish_at = %v, want 1700000000", got.LastPublishAt)
	}
	if got.PublishCount != 41 {
		t.Errorf("publish_count = %d, want 41", got.PublishCount)
	}

	quiet := byPrefix["quiet"]
	if quiet.Alias != "" {
		t.Errorf("non-slot org alias = %q, want empty", quiet.Alias)
	}
	if quiet.LastPublishAt != nil || quiet.PublishCount != 0 {
		t.Errorf("org with no progress row = (%v, %d), want (nil, 0)", quiet.LastPublishAt, quiet.PublishCount)
	}

	// A revoked slot drops its alias; the org row stays.
	if err := r.RevokeSlot(ctx, slot.SlotID); err != nil {
		t.Fatalf("revoke slot: %v", err)
	}
	cat, err = r.ListCatalog(ctx, false)
	if err != nil {
		t.Fatalf("ListCatalog after revoke: %v", err)
	}
	for _, o := range cat.Orgs {
		if o.Prefix == slot.SlotID && o.Alias != "" {
			t.Errorf("revoked slot still carries alias %q", o.Alias)
		}
	}
}
