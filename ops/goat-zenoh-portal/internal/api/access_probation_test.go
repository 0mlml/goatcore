package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func getProb(t *testing.T, h http.Handler, key, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", path, nil)
	if key != "" {
		r.Header.Set("Authorization", "Bearer "+key)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func countProbation(t *testing.T, w *httptest.ResponseRecorder) int {
	t.Helper()
	var list struct {
		Probation []json.RawMessage `json:"probation"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	return len(list.Probation)
}

// Reuses onboardTestServer (operator key) + postJSON from the onboard tests.

func TestProbationHoldListDecideFlow(t *testing.T) {
	h, key := onboardTestServer(t)

	// Hold a stray.
	w := postJSON(t, h, key, "/api/access/probation",
		`{"ref":"kid-oliver","label":"oliver@mil.example","reason":"stray:untracked"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("hold status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var held struct {
		ID, Ref, Disposition string
		CureBy               int64 `json:"cure_by"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &held)
	if held.ID == "" || held.Disposition != "probation" || held.CureBy == 0 {
		t.Fatalf("hold response missing fields: %s", w.Body.String())
	}

	// Double-hold → 409.
	if w := postJSON(t, h, key, "/api/access/probation", `{"ref":"kid-oliver"}`); w.Code != http.StatusConflict {
		t.Errorf("double-hold status = %d, want 409", w.Code)
	}

	// List shows it.
	lw := getProb(t, h, key, "/api/access/probation")
	if lw.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", lw.Code, lw.Body.String())
	}
	var list struct {
		Probation []struct{ ID, Ref string } `json:"probation"`
	}
	_ = json.Unmarshal(lw.Body.Bytes(), &list)
	if len(list.Probation) != 1 || list.Probation[0].Ref != "kid-oliver" {
		t.Fatalf("list = %s", lw.Body.String())
	}

	// Decide → purged, and it returns the follow-up command.
	dw := postJSON(t, h, key, "/api/access/probation/"+held.ID+"/decide", `{"disposition":"purged","note":"personal email"}`)
	if dw.Code != http.StatusOK {
		t.Fatalf("decide status = %d, want 200: %s", dw.Code, dw.Body.String())
	}
	var dec struct{ Disposition, Next string }
	_ = json.Unmarshal(dw.Body.Bytes(), &dec)
	if dec.Disposition != "purged" || dec.Next == "" {
		t.Errorf("decide response = %s", dw.Body.String())
	}

	// Re-decide → 409.
	if w := postJSON(t, h, key, "/api/access/probation/"+held.ID+"/decide", `{"disposition":"repaired"}`); w.Code != http.StatusConflict {
		t.Errorf("re-decide status = %d, want 409", w.Code)
	}

	// Open list now empty; ?all=true still shows the decided row.
	if lw := getProb(t, h, key, "/api/access/probation"); countProbation(t, lw) != 0 {
		t.Error("open list should be empty after decide")
	}
	if lw := getProb(t, h, key, "/api/access/probation?all=true"); countProbation(t, lw) != 1 {
		t.Error("history should retain the decided row")
	}
}

func TestProbationRejectsBadInputHTTP(t *testing.T) {
	h, key := onboardTestServer(t)
	if w := postJSON(t, h, key, "/api/access/probation", `{"reason":"no ref"}`); w.Code != http.StatusBadRequest {
		t.Errorf("missing ref = %d, want 400", w.Code)
	}
	if w := postJSON(t, h, key, "/api/access/probation", `{"ref":"x"}`); w.Code != http.StatusOK {
		t.Fatalf("valid hold = %d: %s", w.Code, w.Body.String())
	}
	// Unknown id → 404.
	if w := postJSON(t, h, key, "/api/access/probation/prob-nope/decide", `{"disposition":"purged"}`); w.Code != http.StatusNotFound {
		t.Errorf("decide unknown = %d, want 404", w.Code)
	}
}

func TestProbationRequiresOperator(t *testing.T) {
	h, _ := onboardTestServer(t)
	// No/blank bearer → not authorized (requireOperator), never 200.
	if w := postJSON(t, h, "", "/api/access/probation", `{"ref":"x"}`); w.Code == http.StatusOK {
		t.Errorf("unauthenticated hold returned 200 — must be gated")
	}
}
