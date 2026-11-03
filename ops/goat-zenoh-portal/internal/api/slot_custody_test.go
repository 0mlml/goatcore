package api_test

// PUT /api/slots/{anchor}/custody takes a caller-stated affiliation and writes
// it through RecordMachineEnrollment, so it is a second door into the column
// the grant guard protects. A spelling variant of a unit on file is refused
// with the same 400 the grant path gives, naming the spelling on file, and the
// slot is left unstamped; the exact spelling is accepted.

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

func TestPutSlotCustody_RefusesAffiliationVariant(t *testing.T) {
	ts, reg := newMachineServer(t)
	ctx := t.Context()
	if err := reg.SetMachineGrant(ctx, "dana-ops", 3, "unit-alpha", "fra", "op"); err != nil {
		t.Fatalf("SetMachineGrant: %v", err)
	}
	slot, err := reg.AllocateSlot(ctx, "dana-1")
	if err != nil {
		t.Fatalf("AllocateSlot: %v", err)
	}
	c := &http.Client{}
	url := ts.URL + "/api/slots/" + slot.SlotID + "/custody"

	resp := doJSON(t, c, "PUT", url, "dev-operator-key", map[string]any{
		"custodian": "dana-ops", "node_class": "SENSOR", "autonomy": "manual", "affiliation": "Unit-Alpha",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("variant custody: status=%d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	b := mustReadJSON(t, resp)
	e, _ := b["error"].(map[string]any)
	if e["code"] != "affiliation_variant" {
		t.Fatalf("error code = %v, want affiliation_variant", e["code"])
	}
	if msg, _ := e["message"].(string); !strings.Contains(msg, `"unit-alpha"`) {
		t.Fatalf("refusal does not name the spelling on file: %q", msg)
	}
	if _, err := reg.MachineByAnchor(ctx, slot.SlotID); !errors.Is(err, registry.ErrNotFound) {
		t.Fatalf("refused custody stamp landed anyway: %v", err)
	}

	resp = doJSON(t, c, "PUT", url, "dev-operator-key", map[string]any{
		"custodian": "dana-ops", "node_class": "SENSOR", "autonomy": "manual", "affiliation": "unit-alpha",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("exact custody: status=%d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	m, err := reg.MachineByAnchor(ctx, slot.SlotID)
	if err != nil {
		t.Fatalf("MachineByAnchor: %v", err)
	}
	if m.Affiliation == nil || *m.Affiliation != "unit-alpha" {
		t.Fatalf("custody stamp affiliation = %v, want unit-alpha", m.Affiliation)
	}
}
