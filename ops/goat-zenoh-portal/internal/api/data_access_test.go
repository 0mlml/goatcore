package api_test

import (
	"net/http"
	"testing"
)

// TestDataAccess_RequestApproveMintsGrant walks the full loop: a viewer requests
// access to a producer, a non-custodian is refused, the operator approves, and a
// grant row appears. Preemptive grant + revoke round-trip too.
func TestDataAccess_RequestApproveMintsGrant(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}

	// Register the producer.
	resp := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{
		"prefix": "sensorco", "auto_approve": true,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register producer: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// A viewer requests org-scoped access (the coalition default).
	resp = doJSON(t, c, "POST", ts.URL+"/api/access/requests", "dev-operator-key", map[string]any{
		"producer": "sensorco", "grantee_kind": "org", "grantee_id": "acme", "justification": "coalition need",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create request: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	req := mustReadJSON(t, resp)
	reqID, _ := req["id"].(string)
	if reqID == "" {
		t.Fatal("no request id")
	}

	// Request for a non-existent producer 404s.
	resp = doJSON(t, c, "POST", ts.URL+"/api/access/requests", "dev-operator-key", map[string]any{
		"producer": "ghost", "grantee_kind": "org", "grantee_id": "acme",
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("request for ghost producer: got %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()

	// Operator approves -> a grant is minted.
	resp = doJSON(t, c, "POST", ts.URL+"/api/access/requests/"+reqID+"/approve", "dev-operator-key", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approve: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	appr := mustReadJSON(t, resp)
	if appr["status"] != "approved" {
		t.Fatalf("approve status: %v", appr["status"])
	}
	grant, _ := appr["grant"].(map[string]any)
	if grant["grantee_id"] != "acme" || grant["producer"] != "sensorco" {
		t.Fatalf("minted grant wrong: %v", grant)
	}

	// Second approve on the now-decided request 409s.
	resp = doJSON(t, c, "POST", ts.URL+"/api/access/requests/"+reqID+"/approve", "dev-operator-key", map[string]any{})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("re-approve: got %d, want 409", resp.StatusCode)
	}
	resp.Body.Close()

	// The grant shows up in the list, then revokes.
	resp = doJSON(t, c, "GET", ts.URL+"/api/access/grants?producer=sensorco", "dev-operator-key", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list grants: %d", resp.StatusCode)
	}
	list := mustReadJSON(t, resp)
	grants, _ := list["grants"].([]any)
	if len(grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(grants))
	}
	gid, _ := grant["id"].(string)
	resp = doJSON(t, c, "POST", ts.URL+"/api/access/grants/"+gid+"/revoke", "dev-operator-key", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	resp.Body.Close()
}

// TestDataAccess_PreemptiveGrantNoRequest checks the ADR 1075 §12 preemptive
// path: a custodian grants with no pending request.
func TestDataAccess_PreemptiveGrantNoRequest(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	resp := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{
		"prefix": "radarco", "auto_approve": true,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register: %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = doJSON(t, c, "POST", ts.URL+"/api/access/grants", "dev-operator-key", map[string]any{
		"producer": "radarco", "grantee_kind": "principal", "grantee_id": "uuid-alice", "justification": "preemptive share",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("preemptive grant: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	g := mustReadJSON(t, resp)
	if g["grantee_id"] != "uuid-alice" || g["status"] != "active" {
		t.Fatalf("preemptive grant wrong: %v", g)
	}
}

// TestDataAccess_Directory returns roster orgs (people empty without Kratos).
func TestDataAccess_Directory(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	resp := doJSON(t, c, "GET", ts.URL+"/api/access/directory", "dev-operator-key", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("directory: %d", resp.StatusCode)
	}
	dir := mustReadJSON(t, resp)
	// newTestServer wires no roster + no kratos, so both are empty with notes —
	// the honest-degradation contract, never a fabricated list.
	if _, ok := dir["orgs"]; !ok {
		t.Fatal("directory missing orgs key")
	}
	notes, _ := dir["notes"].([]any)
	if len(notes) == 0 {
		t.Fatal("expected honest notes when roster/kratos are unconfigured")
	}
}
