package registry_test

// machine_grants.custodian was a polymorphic free-text key — an anchor, a slot
// alias, or operator free text — and the quota join keyed on it, so a grant
// written under one spelling and an enrollment stamped under another read quota
// 0 or a doubled quota, silently (identity fragility audit §1.2).
// Every read and write now funnels through the anchor form.

import (
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/pgtest"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

const custSalt = "custodian-normalization-test"

// poolRegistry is a registry with a small slot pool baked, so handles can be
// assigned and therefore normalized to anchors.
func poolRegistry(t *testing.T) (*registry.Registry, string) {
	t.Helper()
	path := pgtest.DSN(t)
	r, err := registry.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if _, err := r.InitializeSlotPool(t.Context(), custSalt, 4); err != nil {
		t.Fatalf("InitializeSlotPool: %v", err)
	}
	return r, path
}

// A grant written under the person's HANDLE and a quota read under their ANCHOR
// (or vice versa) must be the same row. Before normalization these were two
// independent keys and the quota read as 0.
func TestGrantUnderHandleReadsBackUnderAnchor(t *testing.T) {
	r, _ := poolRegistry(t)
	ctx := t.Context()

	slot, err := r.AllocateSlot(ctx, "dana-ops")
	if err != nil {
		t.Fatalf("AllocateSlot: %v", err)
	}
	anchor := slot.SlotID

	if err := r.SetMachineGrant(ctx, "dana-ops", 3, "unit-alpha", "fra", "operator"); err != nil {
		t.Fatalf("SetMachineGrant(handle): %v", err)
	}
	g, err := r.MachineGrantFor(ctx, anchor)
	if err != nil {
		t.Fatalf("MachineGrantFor(anchor): %v", err)
	}
	if g.Custodian != anchor {
		t.Errorf("grant custodian = %q, want the anchor %q", g.Custodian, anchor)
	}
	if g.Quota != 3 {
		t.Errorf("quota = %d, want 3", g.Quota)
	}
	// And the reverse direction: written under the anchor, read under the handle.
	if err := r.SetMachineQuota(ctx, anchor, 5, "operator"); err != nil {
		t.Fatalf("SetMachineQuota(anchor): %v", err)
	}
	q, err := r.MachineQuotaFor(ctx, "dana-ops")
	if err != nil {
		t.Fatalf("MachineQuotaFor(handle): %v", err)
	}
	if q.Quota != 5 {
		t.Errorf("quota via handle = %d, want 5 — the two spellings are still separate rows", q.Quota)
	}
}

// The quota join spans two tables: the grant row and the slots.enrolled_by
// stamps counted against it. Normalizing only one side is what splits a quota,
// so an enrollment stamped under the handle must consume the anchor's quota.
func TestEnrollmentUnderHandleConsumesTheAnchorsQuota(t *testing.T) {
	r, _ := poolRegistry(t)
	ctx := t.Context()

	custodianSlot, err := r.AllocateSlot(ctx, "dana-ops")
	if err != nil {
		t.Fatalf("AllocateSlot(custodian): %v", err)
	}
	anchor := custodianSlot.SlotID
	if err := r.SetMachineGrant(ctx, anchor, 2, "unit-alpha", "", "operator"); err != nil {
		t.Fatalf("SetMachineGrant: %v", err)
	}

	machine, err := r.AllocateSlot(ctx, "dana-sensor-1")
	if err != nil {
		t.Fatalf("AllocateSlot(machine): %v", err)
	}
	// Enrolled naming the custodian by HANDLE.
	if err := r.RecordMachineEnrollment(ctx, machine.SlotID, "dana-ops", "SENSOR", "manual", "unit-alpha", nil); err != nil {
		t.Fatalf("RecordMachineEnrollment: %v", err)
	}

	q, err := r.MachineQuotaFor(ctx, anchor)
	if err != nil {
		t.Fatalf("MachineQuotaFor: %v", err)
	}
	if q.Used != 1 || q.Remaining != 1 {
		t.Errorf("quota = %+v, want used 1 / remaining 1 — the handle-stamped enrollment did not count against the anchor's grant", q)
	}
	machines, err := r.MachinesForCustodian(ctx, "dana-ops")
	if err != nil {
		t.Fatalf("MachinesForCustodian: %v", err)
	}
	if len(machines) != 1 {
		t.Errorf("MachinesForCustodian(handle) returned %d machines, want 1", len(machines))
	}
}

// A custodian value that names no live slot is left alone: an operator may
// pre-grant against a handle that has no slot yet, and refusing would break that.
func TestUnresolvableCustodianIsLeftAsWritten(t *testing.T) {
	r, _ := poolRegistry(t)
	ctx := t.Context()

	if err := r.SetMachineGrant(ctx, "not-yet-onboarded", 1, "unit-alpha", "", "operator"); err != nil {
		t.Fatalf("SetMachineGrant: %v", err)
	}
	g, err := r.MachineGrantFor(ctx, "not-yet-onboarded")
	if err != nil {
		t.Fatalf("MachineGrantFor: %v", err)
	}
	if g.Custodian != "not-yet-onboarded" {
		t.Errorf("custodian = %q, want it left as written", g.Custodian)
	}
}

// Legacy rows written before normalization are rewritten at Open() — both the
// grant row and the custody stamps that the quota counts, or the quota splits.
func TestOpenBackfillsLegacyHandleKeyedRows(t *testing.T) {
	r, path := poolRegistry(t)
	ctx := t.Context()

	custodianSlot, err := r.AllocateSlot(ctx, "dana-ops")
	if err != nil {
		t.Fatalf("AllocateSlot(custodian): %v", err)
	}
	anchor := custodianSlot.SlotID
	machine, err := r.AllocateSlot(ctx, "dana-sensor-1")
	if err != nil {
		t.Fatalf("AllocateSlot(machine): %v", err)
	}

	// Write the pre-fix shape directly: both keyed by handle.
	db := r.DB()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO machine_grants (custodian, quota, affiliation, site, granted_by, granted_at, updated_at)
		 VALUES ('dana-ops', 4, 'unit-alpha', '', 'operator', 1, 1)`); err != nil {
		t.Fatalf("seed legacy grant: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE slots SET enrolled_by = 'dana-ops', node_class = 'SENSOR', autonomy = 'manual',
		    affiliation = 'unit-alpha', enrolled_at = 1 WHERE slot_id = $1`, machine.SlotID); err != nil {
		t.Fatalf("seed legacy stamp: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	r2, err := registry.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = r2.Close() })

	var custodian string
	if err := r2.DB().GetContext(ctx, &custodian, `SELECT custodian FROM machine_grants`); err != nil {
		t.Fatalf("read grant: %v", err)
	}
	if custodian != anchor {
		t.Errorf("grant custodian after backfill = %q, want the anchor %q", custodian, anchor)
	}
	var stamped string
	if err := r2.DB().GetContext(ctx, &stamped,
		`SELECT enrolled_by FROM slots WHERE slot_id = $1`, machine.SlotID); err != nil {
		t.Fatalf("read stamp: %v", err)
	}
	if stamped != anchor {
		t.Errorf("custody stamp after backfill = %q, want the anchor %q", stamped, anchor)
	}
	q, err := r2.MachineQuotaFor(ctx, anchor)
	if err != nil {
		t.Fatalf("MachineQuotaFor: %v", err)
	}
	if q.Quota != 4 || q.Used != 1 {
		t.Errorf("quota after backfill = %+v, want quota 4 / used 1", q)
	}
}
