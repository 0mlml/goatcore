package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"reflect"

	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

func TestAffiliationRepairAPI(t *testing.T) {
	ts, reg := newMachineServer(t)
	ctx := t.Context()
	if err := reg.SetMachineGrant(ctx, "fixture-custodian", 3, "unit-alpha", "", "fixture-op"); err != nil {
		t.Fatal(err)
	}
	_, err := reg.DB().ExecContext(ctx, `INSERT INTO machine_grants (custodian, quota, affiliation, site, granted_by, granted_at, updated_at) VALUES ('legacy-custodian', 3, 'Unit Alpha', '', 'fixture-op', 1, 1)`)
	if err != nil {
		t.Fatal(err)
	}
	c := ts.Client()
	url := ts.URL + "/api/machines/affiliation/repair"
	payload := map[string]any{"from": []string{"Unit Alpha"}, "to": "unit-alpha", "reason": "Owner verified same organization"}
	_, vendorKey := onboardVendorBearer(t, ts, "repair-reader")
	for _, token := range []string{"", vendorKey} {
		response := doJSON(t, c, "POST", url, token, payload)
		expected := http.StatusUnauthorized
		if token != "" {
			expected = http.StatusForbidden
		}
		if response.StatusCode != expected {
			t.Fatalf("auth: %d", response.StatusCode)
		}
		mustReadBody(t, response)
	}
	blocked := doJSON(t, c, "POST", ts.URL+"/api/machines/enroll", "dev-operator-key", map[string]any{"machine_handle": "repair-machine", "node_class": "SERVICES", "autonomy": "supervised", "custodian": "fixture-custodian"})
	if blocked.StatusCode != http.StatusConflict {
		t.Fatalf("before repair: %d %s", blocked.StatusCode, mustReadBody(t, blocked))
	}
	if !strings.Contains(mustReadBody(t, blocked), "affiliation repair") {
		t.Fatal("missing recovery guidance")
	}
	previewResponse := doJSON(t, c, "POST", url, "dev-operator-key", payload)
	if previewResponse.StatusCode != http.StatusOK {
		t.Fatalf("preview: %d %s", previewResponse.StatusCode, mustReadBody(t, previewResponse))
	}
	preview := mustReadJSON(t, previewResponse)
	if preview["applied"] != false || preview["grants"] != float64(1) {
		t.Fatalf("preview: %v", preview)
	}
	payload["apply"] = true
	invalid := doJSON(t, c, "POST", url, "dev-operator-key", payload)
	if invalid.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing revision: %d", invalid.StatusCode)
	}
	mustReadBody(t, invalid)
	payload["expected_revision"] = preview["revision"]
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	beforeDenied := affiliationRepairAPISnapshot(t, reg)
	for _, credential := range []struct{ token, actor string }{{"", ""}, {vendorKey, ""}, {vendorKey, "forged-operator"}} {
		deniedRequest, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if credential.token != "" {
			deniedRequest.Header.Set("Authorization", "Bearer "+credential.token)
		}
		deniedRequest.Header.Set("Content-Type", "application/json")
		deniedRequest.Header.Set("X-Goat-Operator-Actor", credential.actor)
		denied, err := c.Do(deniedRequest)
		if err != nil {
			t.Fatal(err)
		}
		want := http.StatusForbidden
		if credential.token == "" {
			want = http.StatusUnauthorized
		}
		if denied.StatusCode != want {
			t.Fatalf("apply authorization: got %d want %d", denied.StatusCode, want)
		}
		mustReadBody(t, denied)
		if !reflect.DeepEqual(beforeDenied, affiliationRepairAPISnapshot(t, reg)) {
			t.Fatal("denied apply changed persisted data")
		}
	}
	request, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer dev-operator-key")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Goat-Operator-Actor", "fixture-delegated-human")
	response, err := c.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("apply: %d %s", response.StatusCode, mustReadBody(t, response))
	}
	result := mustReadJSON(t, response)
	if result["applied"] != true || result["audit_id"].(float64) <= 0 {
		t.Fatalf("receipt: %v", result)
	}
	var actor, details string
	if err := reg.DB().QueryRowxContext(ctx, `SELECT actor, details FROM audit_events WHERE action = 'machine-affiliation-repair'`).Scan(&actor, &details); err != nil {
		t.Fatal(err)
	}
	if actor != "fixture-delegated-human" || !strings.Contains(details, `"principal":"operator"`) {
		t.Fatalf("attribution: %s %s", actor, details)
	}
	repeated := doJSON(t, c, "POST", url, "dev-operator-key", payload)
	if repeated.StatusCode != http.StatusConflict || errCode(t, repeated) != "affiliation_repair_stale" {
		t.Fatal("repeat was not a stale-preview conflict")
	}
	enrolled := doJSON(t, c, "POST", ts.URL+"/api/machines/enroll", "dev-operator-key", map[string]any{"machine_handle": "repair-machine", "node_class": "SERVICES", "autonomy": "supervised", "custodian": "fixture-custodian"})
	if enrolled.StatusCode != http.StatusCreated {
		t.Fatalf("after repair: %d %s", enrolled.StatusCode, mustReadBody(t, enrolled))
	}
	mustReadBody(t, enrolled)
}

func affiliationRepairAPISnapshot(t *testing.T, reg *registry.Registry) map[string][]map[string]any {
	t.Helper()
	result := make(map[string][]map[string]any)
	for _, table := range []string{"machine_grants", "slots", "audit_events"} {
		rows, err := reg.DB().QueryxContext(t.Context(), "SELECT * FROM "+table+" ORDER BY 1")
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			row := make(map[string]any)
			if err := rows.MapScan(row); err != nil {
				t.Fatal(err)
			}
			result[table] = append(result[table], row)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return result
}

func TestAffiliationRepairAPIPrincipalActor(t *testing.T) {
	ts, reg := newMachineServer(t)
	if err := reg.SetMachineGrant(t.Context(), "legacy", 1, "Unit Alpha", "", "fixture-op"); err != nil {
		t.Fatal(err)
	}
	url := ts.URL + "/api/machines/affiliation/repair"
	payload := map[string]any{"from": []string{"Unit Alpha"}, "to": "unit-alpha", "reason": "Owner verified same organization"}
	previewResponse := doJSON(t, ts.Client(), "POST", url, "dev-operator-key", payload)
	if previewResponse.StatusCode != http.StatusOK {
		t.Fatalf("preview: %d %s", previewResponse.StatusCode, mustReadBody(t, previewResponse))
	}
	preview := mustReadJSON(t, previewResponse)
	payload["apply"], payload["expected_revision"] = true, preview["revision"]
	response := doJSON(t, ts.Client(), "POST", url, "dev-operator-key", payload)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("apply: %d %s", response.StatusCode, mustReadBody(t, response))
	}
	mustReadBody(t, response)
	var actor, details string
	if err := reg.DB().QueryRowxContext(t.Context(), `SELECT actor, details FROM audit_events WHERE action = 'machine-affiliation-repair'`).Scan(&actor, &details); err != nil {
		t.Fatal(err)
	}
	if actor != "operator" || !strings.Contains(details, `"principal":"operator"`) {
		t.Fatalf("principal attribution: %s %s", actor, details)
	}
}
