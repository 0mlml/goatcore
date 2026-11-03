package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/pgtest"
)

func TestTopicBudgetCRUD(t *testing.T) {
	ctx := context.Background()
	reg, err := Open(pgtest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	// Upsert with prefixes + read back.
	if err := reg.UpsertTopicBudget(ctx, "aabb", 100, []string{"video", "presence"}, "platform-admin test", "op"); err != nil {
		t.Fatal(err)
	}
	// Update in place (no prefixes).
	if err := reg.UpsertTopicBudget(ctx, "ops", 2000, nil, "infra", "op"); err != nil {
		t.Fatal(err)
	}
	rows, err := reg.ListTopicBudgets(ctx)
	if err != nil || len(rows) != 2 {
		t.Fatalf("list: %v rows %d, want 2", err, len(rows))
	}
	if rows[0].Owner != "aabb" || rows[0].Granted != 100 {
		t.Fatalf("row0 = %+v", rows[0])
	}
	if ps := rows[0].PrefixList(); len(ps) != 2 || ps[0] != "video" {
		t.Fatalf("prefixes = %v", ps)
	}
	if ps := rows[1].PrefixList(); ps != nil {
		t.Fatalf("ops prefixes = %v, want nil", ps)
	}
	// Replace: granted bump + prefix change lands on the same row.
	if err := reg.UpsertTopicBudget(ctx, "aabb", 400, []string{"video"}, "raised", "op2"); err != nil {
		t.Fatal(err)
	}
	rows, _ = reg.ListTopicBudgets(ctx)
	if len(rows) != 2 || rows[0].Granted != 400 || len(rows[0].PrefixList()) != 1 {
		t.Fatalf("after replace: %+v", rows[0])
	}
	// Rejections: zero/negative allowance, empty owner, wildcard prefix.
	if err := reg.UpsertTopicBudget(ctx, "x", 0, nil, "", "op"); err == nil {
		t.Fatal("granted=0 accepted, want error")
	}
	if err := reg.UpsertTopicBudget(ctx, "", 10, nil, "", "op"); err == nil {
		t.Fatal("empty owner accepted, want error")
	}
	if err := reg.UpsertTopicBudget(ctx, "y", 10, []string{"*"}, "", "op"); err == nil {
		t.Fatal("wildcard prefix accepted, want error")
	}
	// Delete + not-found.
	if err := reg.DeleteTopicBudget(ctx, "aabb"); err != nil {
		t.Fatal(err)
	}
	if err := reg.DeleteTopicBudget(ctx, "aabb"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double delete: %v, want ErrNotFound", err)
	}
}

func TestSlotLifecycleClass(t *testing.T) {
	ctx := context.Background()
	reg, err := Open(pgtest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.InitializeSlotPool(ctx, "test-salt", 3); err != nil {
		t.Fatal(err)
	}
	anchor := SlotAnchor("test-salt", 0)
	if err := reg.SetSlotLifecycleClass(ctx, anchor, "notional"); err != nil {
		t.Fatal(err)
	}
	classes, err := reg.SlotLifecycleClasses(ctx)
	if err != nil || len(classes) != 1 || classes[anchor] != "notional" {
		t.Fatalf("classes = %v err %v", classes, err)
	}
	// Clear back to unclassified drops it from the render input.
	if err := reg.SetSlotLifecycleClass(ctx, anchor, ""); err != nil {
		t.Fatal(err)
	}
	classes, _ = reg.SlotLifecycleClasses(ctx)
	if len(classes) != 0 {
		t.Fatalf("after clear: %v", classes)
	}
	// Closed enum + unknown slot.
	if err := reg.SetSlotLifecycleClass(ctx, anchor, "demo"); err == nil {
		t.Fatal("bad class accepted")
	}
	if err := reg.SetSlotLifecycleClass(ctx, "ffff", "test"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown slot: %v, want ErrNotFound", err)
	}
}

func TestAssignedSlotAnchors(t *testing.T) {
	ctx := context.Background()
	reg, err := Open(pgtest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.InitializeSlotPool(ctx, "s", 3); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.AllocateSlot(ctx, "tenant-a"); err != nil {
		t.Fatal(err)
	}
	anchors, err := reg.AssignedSlotAnchors(ctx)
	if err != nil || len(anchors) != 1 {
		t.Fatalf("anchors = %v err %v, want exactly the one assigned slot", anchors, err)
	}
}
