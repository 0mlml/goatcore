package registry

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// ADR 1138 portal half. A portal that can only hand out the lowest free slot
// puts a partner outside its pre-cut block, where it matches no partner-<anchor>
// subject in the router and reads the whole fabric silently until somebody
// hand-edits the ACL and restarts. These tests encode the property that prevents
// it.

func poolReg(t *testing.T, size int) (*Registry, context.Context) {
	t.Helper()
	reg := testRegistry(t)
	ctx := context.Background()
	if _, err := reg.InitializeSlotPool(ctx, "test-salt-distinct-per-goatnet", size); err != nil {
		t.Fatalf("init pool: %v", err)
	}
	return reg, ctx
}

// The ordinary path is unchanged: lowest free slot.
func TestAllocateSlot_StillTakesLowestFree(t *testing.T) {
	reg, ctx := poolReg(t, 50)
	s, err := reg.AllocateSlot(ctx, "ordinary-peer")
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	if s.Idx != 0 {
		t.Fatalf("ordinary allocation must take idx 0, got %d", s.Idx)
	}
}

// The property that matters: a partner lands INSIDE its block even when lower
// slots are free. Without this the block is inert.
func TestAllocateSlotIn_LandsInsideTheBlockNotTheLowestFreeSlot(t *testing.T) {
	reg, ctx := poolReg(t, 1000)

	// Burn a few ordinary slots first, exactly as a real goatnet has.
	for i := 0; i < 14; i++ {
		if _, err := reg.AllocateSlot(ctx, fmt.Sprintf("peer-%d", i)); err != nil {
			t.Fatalf("seed allocate: %v", err)
		}
	}

	got, err := reg.AllocateSlotIn(ctx, "partner-org", 930, 949)
	if err != nil {
		t.Fatalf("allocate in block: %v", err)
	}
	if got.Idx < 930 || got.Idx > 949 {
		t.Fatalf("partner landed on idx %d, OUTSIDE its block [930,949] — this is the F-364 defect", got.Idx)
	}
	if got.Idx != 930 {
		t.Fatalf("want the lowest free slot WITHIN the block (930), got %d", got.Idx)
	}

	// The next partner takes the next slot in the block, not elsewhere.
	next, err := reg.AllocateSlotIn(ctx, "partner-org-2", 930, 949)
	if err != nil {
		t.Fatalf("second allocate in block: %v", err)
	}
	if next.Idx != 931 {
		t.Fatalf("want 931, got %d", next.Idx)
	}
}

// A full block must REFUSE, never silently fall back to an ordinary slot. The
// fallback is the over-sharing bug.
func TestAllocateSlotIn_FullBlockRefusesRatherThanFallingBack(t *testing.T) {
	reg, ctx := poolReg(t, 1000)

	// Fill a deliberately tiny block.
	for i := 0; i < 3; i++ {
		if _, err := reg.AllocateSlotIn(ctx, fmt.Sprintf("partner-%d", i), 900, 902); err != nil {
			t.Fatalf("fill block: %v", err)
		}
	}

	_, err := reg.AllocateSlotIn(ctx, "partner-overflow", 900, 902)
	if !errors.Is(err, ErrPartnerBlockExhausted) {
		t.Fatalf("a full partner block must return ErrPartnerBlockExhausted, got %v", err)
	}
	if errors.Is(err, ErrSlotPoolExhausted) {
		t.Fatal("must NOT report pool exhaustion — the pool has ~997 free slots; the BLOCK is full, and handing over one of those free slots is exactly the bug")
	}
}

func TestAllocateSlotIn_RejectsInvertedRange(t *testing.T) {
	reg, ctx := poolReg(t, 100)
	if _, err := reg.AllocateSlotIn(ctx, "x", 90, 10); err == nil {
		t.Fatal("inverted range must be rejected")
	}
}

// Alias uniqueness (ADR 1039) still holds on the block path.
func TestAllocateSlotIn_HonorsAliasUniqueness(t *testing.T) {
	reg, ctx := poolReg(t, 1000)
	if _, err := reg.AllocateSlotIn(ctx, "dup-partner", 930, 949); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := reg.AllocateSlotIn(ctx, "dup-partner", 930, 949); !errors.Is(err, ErrAliasTaken) {
		t.Fatalf("want ErrAliasTaken on a duplicate handle, got %v", err)
	}
}
