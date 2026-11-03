package registry_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

// seedTopicForExamples creates a vendor + topic and returns the topic id.
// Mirrors the pattern in registry_test.go's seedVendor/seedTopic helpers.
func seedTopicForExamples(t *testing.T, r *registry.Registry, ctx context.Context) int64 {
	t.Helper()
	if err := r.CreateVendor(ctx, &registry.Vendor{
		Prefix: "caracal", APIKeyHash: "stub", DisplayName: "Caracal",
	}); err != nil {
		t.Fatalf("create vendor: %v", err)
	}
	id, err := r.CreateTopic(ctx, &registry.Topic{
		VendorPrefix: "caracal",
		KeyPattern:   "caracal/tracks-demo/entity/v1",
		Format:       "protobuf:Entity",
		Description:  "test",
	})
	if err != nil {
		t.Fatalf("insert topic: %v", err)
	}
	return id
}

func TestCodeExamples_UpsertGetListDelete(t *testing.T) {
	r := open(t)
	ctx := context.Background()
	tid := seedTopicForExamples(t, r, ctx)

	// Upsert (create).
	e := &registry.TopicCodeExample{
		TopicID:   tid,
		Language:  "rust",
		Code:      "// hello rust\nfn main() {}\n",
		UpdatedBy: "caracal",
	}
	if _, err := r.UpsertCodeExample(ctx, e); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if e.UpdatedAt == 0 {
		t.Errorf("UpdatedAt not stamped")
	}

	// Upsert (replace) — same topic + language, different body.
	e.Code = "// updated rust\nfn main() { println!(\"x\"); }\n"
	if _, err := r.UpsertCodeExample(ctx, e); err != nil {
		t.Fatalf("upsert replace: %v", err)
	}

	// Get returns the latest body.
	got, err := r.GetCodeExample(ctx, tid, "rust")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.Contains(got.Code, "updated rust") {
		t.Errorf("get returned stale body: %q", got.Code)
	}

	// Upsert another language; list returns both, alphabetical.
	if _, err := r.UpsertCodeExample(ctx, &registry.TopicCodeExample{
		TopicID: tid, Language: "go", Code: "package main\n", UpdatedBy: "caracal",
	}); err != nil {
		t.Fatalf("upsert go: %v", err)
	}
	list, err := r.ListCodeExamplesByTopic(ctx, tid)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 || list[0].Language != "go" || list[1].Language != "rust" {
		t.Errorf("list shape: %+v", list)
	}

	// Delete the rust override.
	if err := r.DeleteCodeExample(ctx, tid, "rust"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := r.GetCodeExample(ctx, tid, "rust"); !errors.Is(err, registry.ErrNotFound) {
		t.Errorf("get after delete: want ErrNotFound, got %v", err)
	}

	// Second delete is a clean 404 — gives the API caller a stable shape.
	if err := r.DeleteCodeExample(ctx, tid, "rust"); !errors.Is(err, registry.ErrNotFound) {
		t.Errorf("double delete: want ErrNotFound, got %v", err)
	}
}

func TestCodeExamples_Validation(t *testing.T) {
	r := open(t)
	ctx := context.Background()
	tid := seedTopicForExamples(t, r, ctx)

	// Empty code.
	if _, err := r.UpsertCodeExample(ctx, &registry.TopicCodeExample{
		TopicID: tid, Language: "rust", Code: "", UpdatedBy: "caracal",
	}); err == nil {
		t.Errorf("empty code: want error, got nil")
	}

	// Bad language.
	if _, err := r.UpsertCodeExample(ctx, &registry.TopicCodeExample{
		TopicID: tid, Language: "brainfuck", Code: "+++", UpdatedBy: "caracal",
	}); err == nil {
		t.Errorf("bad language: want error, got nil")
	}

	// Oversized.
	big := strings.Repeat("x", registry.MaxCodeExampleBytes+1)
	if _, err := r.UpsertCodeExample(ctx, &registry.TopicCodeExample{
		TopicID: tid, Language: "rust", Code: big, UpdatedBy: "caracal",
	}); err == nil {
		t.Errorf("oversized: want error, got nil")
	}

	// Language case is normalized: "Rust" matches "rust" on lookup.
	if _, err := r.UpsertCodeExample(ctx, &registry.TopicCodeExample{
		TopicID: tid, Language: "Rust", Code: "fn main() {}\n", UpdatedBy: "caracal",
	}); err != nil {
		t.Fatalf("mixed case upsert: %v", err)
	}
	if _, err := r.GetCodeExample(ctx, tid, "rust"); err != nil {
		t.Errorf("lookup after mixed-case insert: %v", err)
	}
}
