package registry_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

func repairFixture(t *testing.T) *registry.Registry {
	t.Helper()
	r := affRegistry(t)
	for _, row := range []struct{ id, affiliation string }{{"legacy", "Unit Alpha"}, {"current", "unit-alpha"}, {"other", "unit-bravo"}} {
		_, err := r.DB().ExecContext(t.Context(), `INSERT INTO machine_grants (custodian, quota, affiliation, site, granted_by, granted_at, updated_at, network_admin) VALUES ($1, 6, $2, 'fixture-site', 'fixture-op', 11, 12, 1)`, row.id, row.affiliation)
		if err != nil {
			t.Fatal(err)
		}
	}
	for i, id := range []string{"old-slot", "revoked-slot", "target-slot", "free-slot"} {
		_, err := r.DB().ExecContext(t.Context(), `INSERT INTO slots (slot_id, idx) VALUES ($1, $2)`, id, i)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := r.DB().ExecContext(t.Context(), `UPDATE slots SET assigned_to = 'fixture-machine', affiliation = 'Unit Alpha', enrolled_by = 'legacy', node_class = 'SERVICES', autonomy = 'supervised', enrolled_at = 13, cert_minted_at = 14, bundle_token = 'synthetic', bundle_path = 'fixture', identity_id = 'fixture-id' WHERE slot_id IN ('old-slot', 'revoked-slot')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.DB().ExecContext(t.Context(), `UPDATE slots SET revoked_at = 15 WHERE slot_id = 'revoked-slot'`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.DB().ExecContext(t.Context(), `UPDATE slots SET assigned_to = 'current-machine', affiliation = 'unit-alpha', enrolled_by = 'current' WHERE slot_id = 'target-slot'`)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func repairRequest() registry.AffiliationRepairRequest {
	return registry.AffiliationRepairRequest{From: []string{"Unit Alpha"}, To: "unit-alpha", Reason: "Owner verified both spellings describe one organization"}
}

func repairSnapshot(t *testing.T, r *registry.Registry) map[string][]map[string]any {
	t.Helper()
	snapshot := make(map[string][]map[string]any)
	for _, table := range []string{"machine_grants", "slots", "audit_events"} {
		rows, err := r.DB().QueryxContext(t.Context(), "SELECT * FROM "+table+" ORDER BY 1")
		if err != nil {
			t.Fatal(err)
		}
		var values []map[string]any
		for rows.Next() {
			value := make(map[string]any)
			if err := rows.MapScan(value); err != nil {
				t.Fatal(err)
			}
			values = append(values, value)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		snapshot[table] = values
	}
	return snapshot
}

func applyRepair(t *testing.T, r *registry.Registry) *registry.AffiliationRepairResult {
	t.Helper()
	req := repairRequest()
	preview, err := r.RepairAffiliations(t.Context(), req, "fixture-key-owner", "fixture-human")
	if err != nil {
		t.Fatal(err)
	}
	req.Apply, req.ExpectedRevision = true, preview.Revision
	result, err := r.RepairAffiliations(t.Context(), req, "fixture-key-owner", "fixture-human")
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestAffiliationRepairPreviewAndAtomicRecovery(t *testing.T) {
	r := repairFixture(t)
	ctx := t.Context()
	for _, spelling := range []string{"Unit Alpha", "unit-alpha"} {
		if err := r.SetMachineGrant(ctx, "new", 1, spelling, "", "op"); !errors.Is(err, registry.ErrAffiliationVariant) || !strings.Contains(err.Error(), "affiliation repair") {
			t.Fatalf("legacy conflict: %v", err)
		}
	}
	before := repairSnapshot(t, r)
	preview, err := r.RepairAffiliations(ctx, repairRequest(), "fixture-key-owner", "fixture-human")
	if err != nil {
		t.Fatal(err)
	}
	if preview.Applied || preview.AuditID != 0 || preview.Grants != 1 || preview.Slots != 2 {
		t.Fatalf("preview: %+v", preview)
	}
	if !reflect.DeepEqual(before, repairSnapshot(t, r)) {
		t.Fatal("preview wrote data")
	}
	result := applyRepair(t, r)
	if !result.Applied || result.AuditID == 0 {
		t.Fatalf("apply: %+v", result)
	}
	after := repairSnapshot(t, r)
	for _, table := range []string{"machine_grants", "slots"} {
		for _, row := range before[table] {
			if row["affiliation"] == "Unit Alpha" {
				row["affiliation"] = "unit-alpha"
			}
		}
		if !reflect.DeepEqual(before[table], after[table]) {
			t.Fatalf("unrelated %s fields changed", table)
		}
	}
	if len(after["audit_events"]) != len(before["audit_events"])+1 {
		t.Fatal("expected one audit")
	}
	event := after["audit_events"][0]
	if event["actor"] != "fixture-human" || event["action"] != "machine-affiliation-repair" {
		t.Fatalf("audit attribution: %v", event)
	}
	var details map[string]any
	if err := json.Unmarshal([]byte(event["details"].(string)), &details); err != nil {
		t.Fatal(err)
	}
	if details["principal"] != "fixture-key-owner" || details["reason"] != repairRequest().Reason || details["applied"] != true {
		t.Fatalf("audit details: %v", details)
	}
	slot, err := r.AllocateSlot(ctx, "new-machine")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.RecordMachineEnrollment(ctx, slot.SlotID, "current", "SERVICES", "supervised", "unit-alpha", nil); err != nil {
		t.Fatal(err)
	}
	if err := r.SetMachineGrant(ctx, "new", 1, "UNIT ALPHA", "", "op"); !errors.Is(err, registry.ErrAffiliationVariant) {
		t.Fatalf("guard weakened: %v", err)
	}
	if err := r.SetMachineGrant(ctx, "new", 1, "unit-charlie", "", "op"); err != nil {
		t.Fatal(err)
	}
}

func TestAffiliationRepairRejectsInvalidInputs(t *testing.T) {
	r := repairFixture(t)
	cases := []registry.AffiliationRepairRequest{
		{From: []string{"Unit Alpha"}, To: "unit-alpha"},
		{From: []string{"Unit Alpha"}, To: "unit-bravo", Reason: "verified"},
		{From: []string{"unit-alpha"}, To: "unit-alpha", Reason: "verified"},
		{From: []string{"Unit Alpha", "Unit Alpha"}, To: "unit-alpha", Reason: "verified"},
		{From: []string{"Unit Alpha"}, To: "unit-alpha", Reason: "verified", Apply: true},
		{From: []string{"Unit Alpha"}, To: "unit-alpha", Reason: "verified", ExpectedRevision: strings.Repeat("a", 64)},
		{From: []string{"---"}, To: "_", Reason: "verified"},
		{From: []string{"UNIT ALPHA"}, To: "unit-alpha", Reason: "verified"},
	}
	before := repairSnapshot(t, r)
	for _, req := range cases {
		if _, err := r.RepairAffiliations(t.Context(), req, "op", "op"); !errors.Is(err, registry.ErrAffiliationRepairInvalid) {
			t.Fatalf("%+v: %v", req, err)
		}
	}
	if !reflect.DeepEqual(before, repairSnapshot(t, r)) {
		t.Fatal("invalid request wrote data")
	}
}

func TestAffiliationRepairOmittedVariantAndStalePreview(t *testing.T) {
	r := repairFixture(t)
	req := repairRequest()
	preview, err := r.RepairAffiliations(t.Context(), req, "op", "op")
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.DB().ExecContext(t.Context(), `UPDATE slots SET affiliation = 'UNIT_ALPHA' WHERE slot_id = 'target-slot'`)
	if err != nil {
		t.Fatal(err)
	}
	before := repairSnapshot(t, r)
	req.Apply, req.ExpectedRevision = true, preview.Revision
	if _, err := r.RepairAffiliations(t.Context(), req, "op", "op"); !errors.Is(err, registry.ErrAffiliationRepairStale) {
		t.Fatal(err)
	}
	req.Apply, req.ExpectedRevision = false, ""
	if _, err := r.RepairAffiliations(t.Context(), req, "op", "op"); !errors.Is(err, registry.ErrAffiliationRepairInvalid) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, repairSnapshot(t, r)) {
		t.Fatal("stale or omitted variant wrote data")
	}
	req.From = append(req.From, "UNIT_ALPHA")
	preview, err = r.RepairAffiliations(t.Context(), req, "op", "op")
	if err != nil {
		t.Fatal(err)
	}
	req.Apply, req.ExpectedRevision = true, preview.Revision
	if _, err := r.RepairAffiliations(t.Context(), req, "op", "op"); err != nil {
		t.Fatal(err)
	}
	after := repairSnapshot(t, r)
	if _, err := r.RepairAffiliations(t.Context(), req, "op", "op"); !errors.Is(err, registry.ErrAffiliationRepairStale) {
		t.Fatalf("retry: %v", err)
	}
	if !reflect.DeepEqual(after, repairSnapshot(t, r)) {
		t.Fatal("retry wrote data")
	}
}

func TestAffiliationRepairRollsBackUpdatesAndAudit(t *testing.T) {
	for _, table := range []string{"machine_grants", "slots", "audit_events"} {
		t.Run(table, func(t *testing.T) {
			r := repairFixture(t)
			req := repairRequest()
			preview, err := r.RepairAffiliations(t.Context(), req, "op", "op")
			if err != nil {
				t.Fatal(err)
			}
			operation := "UPDATE"
			if table == "audit_events" {
				operation = "INSERT"
			}
			_, err = r.DB().ExecContext(t.Context(), `CREATE FUNCTION reject_repair() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture failure'; END $$;
				CREATE TRIGGER reject_repair BEFORE `+operation+` ON `+table+` FOR EACH ROW EXECUTE FUNCTION reject_repair()`)
			if err != nil {
				t.Fatal(err)
			}
			before := repairSnapshot(t, r)
			req.Apply, req.ExpectedRevision = true, preview.Revision
			if _, err := r.RepairAffiliations(t.Context(), req, "op", "op"); err == nil {
				t.Fatal("injected failure succeeded")
			}
			if !reflect.DeepEqual(before, repairSnapshot(t, r)) {
				t.Fatal("failure partially committed")
			}
		})
	}
}

func TestAffiliationRepairConcurrentApplyAndGrant(t *testing.T) {
	r := repairFixture(t)
	req := repairRequest()
	preview, err := r.RepairAffiliations(t.Context(), req, "op", "op")
	if err != nil {
		t.Fatal(err)
	}
	req.Apply, req.ExpectedRevision = true, preview.Revision
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); <-start; _, errs[i] = r.RepairAffiliations(t.Context(), req, "op", "op") }(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		errs[2] = r.SetMachineGrant(t.Context(), "current", 8, "unit-alpha", "fixture-site", "op")
	}()
	close(start)
	wg.Wait()
	success, stale := 0, 0
	for _, err := range errs[:2] {
		if err == nil {
			success++
		} else if errors.Is(err, registry.ErrAffiliationRepairStale) {
			stale++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || stale != 1 {
		t.Fatalf("concurrent applies: %v", errs)
	}
	if errs[2] != nil && !errors.Is(errs[2], registry.ErrAffiliationVariant) {
		t.Fatal(errs[2])
	}
	grant, err := r.MachineGrantFor(t.Context(), "current")
	if err != nil {
		t.Fatal(err)
	}
	if (errs[2] == nil && grant.Quota != 8) || (errs[2] != nil && grant.Quota != 6) {
		t.Fatal("repair overwrote quota")
	}
}

func TestAffiliationRepairExactUnicodeSource(t *testing.T) {
	r := affRegistry(t)
	source := "  Unite\u0301 Alpha  "
	_, err := r.DB().ExecContext(t.Context(), `INSERT INTO machine_grants (custodian, quota, affiliation, site, granted_by, granted_at, updated_at) VALUES ('legacy', 2, $1, '', 'op', 1, 1)`, source)
	if err != nil {
		t.Fatal(err)
	}
	req := registry.AffiliationRepairRequest{From: []string{source}, To: "  Unite\u0301-Alpha  ", Reason: "Owner verified"}
	preview, err := r.RepairAffiliations(t.Context(), req, "op", "op")
	if err != nil {
		t.Fatal(err)
	}
	if preview.To != "Unité-Alpha" || preview.Rows[0].Affiliation != source || preview.From[0] != source {
		t.Fatalf("normalization lost exact source: %+v", preview)
	}
	req.Apply, req.ExpectedRevision = true, preview.Revision
	if _, err := r.RepairAffiliations(t.Context(), req, "op", "op"); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := r.DB().GetContext(t.Context(), &stored, `SELECT affiliation FROM machine_grants WHERE custodian='legacy'`); err != nil {
		t.Fatal(err)
	}
	if stored != "Unité-Alpha" {
		t.Fatal(stored)
	}
}

func TestAffiliationRepairGroupMembershipInvalidatesPreview(t *testing.T) {
	for _, mutation := range []string{
		`INSERT INTO machine_grants (custodian, quota, affiliation, site, granted_by, granted_at, updated_at) VALUES ('new-target', 2, 'unit-alpha', '', 'op', 1, 1)`,
		`INSERT INTO slots (slot_id, idx, affiliation) VALUES ('new-target', 99, 'unit-alpha')`,
		`DELETE FROM slots WHERE slot_id='target-slot'`,
	} {
		t.Run(mutation, func(t *testing.T) {
			r := repairFixture(t)
			req := repairRequest()
			preview, err := r.RepairAffiliations(t.Context(), req, "op", "op")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.DB().ExecContext(t.Context(), mutation); err != nil {
				t.Fatal(err)
			}
			before := repairSnapshot(t, r)
			req.Apply, req.ExpectedRevision = true, preview.Revision
			if _, err := r.RepairAffiliations(t.Context(), req, "op", "op"); !errors.Is(err, registry.ErrAffiliationRepairStale) {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, repairSnapshot(t, r)) {
				t.Fatal("stale apply wrote data")
			}
		})
	}
}

func TestAffiliationRepairRefusesEnrollmentWithPreRepairGrant(t *testing.T) {
	r := repairFixture(t)
	ctx := t.Context()
	if _, err := r.DB().ExecContext(ctx, `UPDATE slots SET assigned_to='in-flight-machine' WHERE slot_id='free-slot'`); err != nil {
		t.Fatal(err)
	}
	// The enrollment handler reads this grant before its enrollment transaction.
	// Pause that request after the read, and commit repair before its write.
	grant, err := r.MachineGrantFor(ctx, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	applyRepair(t, r)
	before := repairSnapshot(t, r)
	if err := r.RecordMachineEnrollment(ctx, "free-slot", "legacy", "SERVICES", "supervised", grant.Affiliation, nil); !errors.Is(err, registry.ErrAffiliationVariant) {
		t.Fatalf("stale enrollment: %v", err)
	}
	if !reflect.DeepEqual(before, repairSnapshot(t, r)) {
		t.Fatal("stale enrollment reintroduced a legacy spelling")
	}
	grant, err = r.MachineGrantFor(ctx, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.RecordMachineEnrollment(ctx, "free-slot", "legacy", "SERVICES", "supervised", grant.Affiliation, nil); err != nil {
		t.Fatalf("fresh enrollment: %v", err)
	}
}
