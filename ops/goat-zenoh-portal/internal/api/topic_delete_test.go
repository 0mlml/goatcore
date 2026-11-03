package api_test

import (
	"net/http"
	"testing"
)

// TestTopicDeleteVsRetire exercises the two DELETE-verb semantics on
// /api/vendors/{v}/topics/{id}:
//
//   - bare DELETE → retire (tombstone, slot stays occupied)
//   - DELETE ?purge=true → hard-delete (slot freed for reuse)
//
// The triggering motivation is the DRS team's "topics stay hidden, can't
// reuse names" report — retire-only blocked their iteration
// loop. Both verbs now coexist; retire preserves audit, purge frees names.
func TestTopicDeleteVsRetire(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}

	resp := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key",
		map[string]any{"prefix": "drs", "auto_approve": true})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register vendor: status %d", resp.StatusCode)
	}
	apiKey, _ := mustReadJSON(t, resp)["api_key"].(string)

	create := func(key string) int64 {
		t.Helper()
		r := doJSON(t, c, "POST", ts.URL+"/api/vendors/drs/topics", apiKey, map[string]any{
			"key_pattern": key, "format": "json",
		})
		if r.StatusCode != http.StatusCreated {
			t.Fatalf("create %s: status %d", key, r.StatusCode)
		}
		body := mustReadJSON(t, r)
		id, _ := body["id"].(float64)
		return int64(id)
	}

	// Path A — retire (default). Slot stays occupied; re-create refused.
	idA := create("drs/radar1/RADAR-EXAMPLE/status")
	r := doJSON(t, c, "DELETE",
		ts.URL+"/api/vendors/drs/topics/"+i64s(idA), apiKey, nil)
	if r.StatusCode != http.StatusNoContent {
		t.Fatalf("retire DELETE: status %d", r.StatusCode)
	}
	r = doJSON(t, c, "POST", ts.URL+"/api/vendors/drs/topics", apiKey, map[string]any{
		"key_pattern": "drs/radar1/RADAR-EXAMPLE/status", "format": "json",
	})
	if r.StatusCode != http.StatusConflict {
		t.Errorf("re-create after retire: expected 409, got %d", r.StatusCode)
	}

	// Path B — purge. Same row, freed slot, re-create succeeds with a
	// fresh id.
	r2 := doJSON(t, c, "DELETE",
		ts.URL+"/api/vendors/drs/topics/"+i64s(idA)+"?purge=true", apiKey, nil)
	if r2.StatusCode != http.StatusNoContent {
		t.Fatalf("purge DELETE: status %d", r2.StatusCode)
	}
	idB := create("drs/radar1/RADAR-EXAMPLE/status")
	if idB == idA {
		t.Errorf("re-create after purge: expected fresh id, got reused %d", idA)
	}

	// Purge an already-purged id returns 404 (the GetTopic pre-check refuses).
	r3 := doJSON(t, c, "DELETE",
		ts.URL+"/api/vendors/drs/topics/"+i64s(idA)+"?purge=true", apiKey, nil)
	if r3.StatusCode != http.StatusNotFound {
		t.Errorf("purge missing: expected 404, got %d", r3.StatusCode)
	}
}

// i64s avoids strconv import pollution for one call site.
func i64s(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
