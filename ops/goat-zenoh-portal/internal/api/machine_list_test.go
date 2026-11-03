package api_test

import (
	"net/http"
	"testing"
)

// machineHandles pulls the system_handle list out of a GET /api/machines body.
func machineHandles(t *testing.T, resp *http.Response) []string {
	t.Helper()
	b := mustReadJSON(t, resp)
	arr, _ := b["machines"].([]any)
	out := make([]string, 0, len(arr))
	for _, m := range arr {
		mm, _ := m.(map[string]any)
		if h, ok := mm["system_handle"].(string); ok {
			out = append(out, h)
		}
	}
	return out
}

func TestMachineList_OperatorFleetAndFilter(t *testing.T) {
	ts, reg := newMachineServer(t)
	ctx := t.Context()
	_ = reg.SetMachineGrant(ctx, "alice", 3, "usareur-af", "", "op")
	c := &http.Client{}

	// enroll two machines under alice (operator on behalf).
	for _, h := range []string{"alice-1", "alice-2"} {
		r := doJSON(t, c, "POST", ts.URL+"/api/machines/enroll", "dev-operator-key", map[string]any{
			"machine_handle": h, "node_class": "SENSOR-FEED", "autonomy": "manual", "custodian": "alice",
		})
		if r.StatusCode != http.StatusCreated {
			t.Fatalf("enroll %s: %d %s", h, r.StatusCode, mustReadBody(t, r))
		}
	}

	// operator fleet view (no filter) → both machines.
	fleet := doJSON(t, c, "GET", ts.URL+"/api/machines", "dev-operator-key", nil)
	if fleet.StatusCode != http.StatusOK {
		t.Fatalf("fleet: %d %s", fleet.StatusCode, mustReadBody(t, fleet))
	}
	if got := machineHandles(t, fleet); len(got) != 2 {
		t.Fatalf("fleet handles = %v, want 2", got)
	}

	// operator custodian filter → alice's + her quota.
	filtered := doJSON(t, c, "GET", ts.URL+"/api/machines?custodian=alice", "dev-operator-key", nil)
	if filtered.StatusCode != http.StatusOK {
		t.Fatalf("filtered: %d", filtered.StatusCode)
	}
	b := mustReadJSON(t, filtered)
	if arr, _ := b["machines"].([]any); len(arr) != 2 {
		t.Fatalf("filtered machines = %d, want 2", len(arr))
	}
	q, ok := b["quota"].(map[string]any)
	if !ok {
		t.Fatalf("single-custodian view must carry quota; body=%v", b)
	}
	if used, _ := q["used"].(float64); used != 2 {
		t.Fatalf("quota.used = %v, want 2", q["used"])
	}
}

func TestMachineList_VendorSeesOwnOnly(t *testing.T) {
	ts, reg := newMachineServer(t)
	ctx := t.Context()
	prefix, key := onboardVendorBearer(t, ts, "dev-custodian")
	_ = reg.SetMachineGrant(ctx, prefix, 2, "unit-s", "", "op")
	// also give another custodian a machine, to prove isolation.
	_ = reg.SetMachineGrant(ctx, "someone-else", 2, "unit-o", "", "op")
	c := &http.Client{}

	// vendor enrolls its own machine (self-service).
	r := doJSON(t, c, "POST", ts.URL+"/api/machines/enroll", key, map[string]any{
		"machine_handle": "proc-1", "node_class": "FUSION", "autonomy": "autonomous",
	})
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("self enroll: %d %s", r.StatusCode, mustReadBody(t, r))
	}

	// vendor GET → only its own machine (never the whole fleet).
	own := doJSON(t, c, "GET", ts.URL+"/api/machines", key, nil)
	if own.StatusCode != http.StatusOK {
		t.Fatalf("own list: %d %s", own.StatusCode, mustReadBody(t, own))
	}
	got := machineHandles(t, own)
	if len(got) != 1 || got[0] != "proc-1" {
		t.Fatalf("vendor own list = %v, want [proc-1]", got)
	}

	// naming another custodian as a non-operator → 403.
	other := doJSON(t, c, "GET", ts.URL+"/api/machines?custodian=someone-else", key, nil)
	if other.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-custodian list = %d, want 403", other.StatusCode)
	}
}
