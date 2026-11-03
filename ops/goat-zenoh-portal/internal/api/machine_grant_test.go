package api_test

import (
	"net/http"
	"strings"
	"testing"
)

// An operator can seed a brand-new custodian's grant (quota + affiliation), then
// bump the quota alone — and the bumped grant keeps its affiliation so enroll
// still works.
func TestMachineGrant_SeedThenBumpQuota(t *testing.T) {
	ts, _ := newMachineServer(t)
	c := &http.Client{}

	// Seed: new custodian, needs affiliation.
	r := doJSON(t, c, "POST", ts.URL+"/api/machines/grant", "dev-operator-key", map[string]any{
		"custodian": "morgan", "quota": 5, "affiliation": "usareur-af",
	})
	if r.StatusCode != http.StatusOK {
		t.Fatalf("seed grant: %d %s", r.StatusCode, mustReadBody(t, r))
	}
	b := mustReadJSON(t, r)
	q, _ := b["quota"].(map[string]any)
	if q == nil || q["quota"].(float64) != 5 || q["remaining"].(float64) != 5 {
		t.Fatalf("after seed want quota 5 remaining 5, got %v", b["quota"])
	}

	// Bump quota only (no affiliation) — allowed because the grant now exists.
	r = doJSON(t, c, "POST", ts.URL+"/api/machines/grant", "dev-operator-key", map[string]any{
		"custodian": "morgan", "quota": 8,
	})
	if r.StatusCode != http.StatusOK {
		t.Fatalf("bump quota: %d %s", r.StatusCode, mustReadBody(t, r))
	}
	q = mustReadJSON(t, r)["quota"].(map[string]any)
	if q["quota"].(float64) != 8 {
		t.Fatalf("after bump want quota 8, got %v", q["quota"])
	}

	// The bumped grant still carries its affiliation, so an enroll succeeds.
	e := doJSON(t, c, "POST", ts.URL+"/api/machines/enroll", "dev-operator-key", map[string]any{
		"machine_handle": "morgan-1", "node_class": "SENSOR-FEED", "autonomy": "manual", "custodian": "morgan",
	})
	if e.StatusCode != http.StatusCreated {
		t.Fatalf("enroll after bump: %d %s", e.StatusCode, mustReadBody(t, e))
	}
}

// Quota-only against a custodian with no grant yet is refused 400 — you can't
// seed a grant with no affiliation (a machine inherits its custodian's unit).
func TestMachineGrant_QuotaOnlyFirstTimeRefused(t *testing.T) {
	ts, _ := newMachineServer(t)
	c := &http.Client{}
	r := doJSON(t, c, "POST", ts.URL+"/api/machines/grant", "dev-operator-key", map[string]any{
		"custodian": "nobody", "quota": 3,
	})
	if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("quota-only first grant = %d, want 400", r.StatusCode)
	}
}

// A grant affiliation that respells a unit already on file comes back as a
// 400 the caller can act on (the error names the spelling to reuse), not the
// 500 a bare registry failure would produce — and the refused grant does not
// land, so the fleet is not forked.
func TestMachineGrant_AffiliationVariantRefused(t *testing.T) {
	ts, _ := newMachineServer(t)
	c := &http.Client{}

	r := doJSON(t, c, "POST", ts.URL+"/api/machines/grant", "dev-operator-key", map[string]any{
		"custodian": "morgan", "quota": 5, "affiliation": "usareur-af",
	})
	if r.StatusCode != http.StatusOK {
		t.Fatalf("seed grant: %d %s", r.StatusCode, mustReadBody(t, r))
	}

	r = doJSON(t, c, "POST", ts.URL+"/api/machines/grant", "dev-operator-key", map[string]any{
		"custodian": "dana", "quota": 2, "affiliation": "USAREUR AF",
	})
	if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("variant affiliation = %d, want 400", r.StatusCode)
	}
	body := mustReadJSON(t, r)
	e, _ := body["error"].(map[string]any)
	if e == nil || e["code"] != "affiliation_variant" {
		t.Fatalf("want error code affiliation_variant, got %v", body["error"])
	}
	if msg, _ := e["message"].(string); !strings.Contains(msg, "usareur-af") {
		t.Fatalf("refusal does not name the spelling on file: %v", e["message"])
	}

	// Nothing was written for dana, so a later grant with the right spelling
	// is still a first grant rather than an edit of a forked row.
	r = doJSON(t, c, "POST", ts.URL+"/api/machines/grant", "dev-operator-key", map[string]any{
		"custodian": "dana", "quota": 2, "affiliation": "usareur-af",
	})
	if r.StatusCode != http.StatusOK {
		t.Fatalf("grant with the on-file spelling: %d %s", r.StatusCode, mustReadBody(t, r))
	}
}

// A non-operator (vendor bearer) cannot set any quota, even its own.
func TestMachineGrant_NonOperatorRefused(t *testing.T) {
	ts, _ := newMachineServer(t)
	c := &http.Client{}
	_, key := onboardVendorBearer(t, ts, "self-custodian")
	r := doJSON(t, c, "POST", ts.URL+"/api/machines/grant", key, map[string]any{
		"custodian": "self-custodian", "quota": 9, "affiliation": "unit-x",
	})
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("non-operator grant = %d, want 403", r.StatusCode)
	}
}

// A negative quota is rejected.
func TestMachineGrant_NegativeQuotaRefused(t *testing.T) {
	ts, _ := newMachineServer(t)
	c := &http.Client{}
	r := doJSON(t, c, "POST", ts.URL+"/api/machines/grant", "dev-operator-key", map[string]any{
		"custodian": "morgan", "quota": -1, "affiliation": "usareur-af",
	})
	if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("negative quota = %d, want 400", r.StatusCode)
	}
}

// Onboarding a slot-system with a machine_quota + affiliation seeds the
// enrollment grant, so the custodian can immediately enroll — no separate
// operator grant. This is the fix for "machine_grants empty for everyone".
func TestOnboard_SeedsMachineGrant(t *testing.T) {
	ts, _ := newMachineServer(t)
	c := &http.Client{}
	r := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{
		"prefix": "fra-bde", "display_name": "Fra Bde",
		"machine_quota": 3, "affiliation": "usareur-af",
	})
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("onboard: %d %s", r.StatusCode, mustReadBody(t, r))
	}
	anchor, _ := mustReadJSON(t, r)["slot_id"].(string)
	if anchor == "" {
		t.Fatalf("onboard response carried no slot_id")
	}
	// The grant is seeded → custodian (= the slot anchor) shows quota 3.
	q := doJSON(t, c, "GET", ts.URL+"/api/machines?custodian="+anchor, "dev-operator-key", nil)
	qb := mustReadJSON(t, q)
	quota, _ := qb["quota"].(map[string]any)
	if quota == nil || quota["quota"].(float64) != 3 {
		t.Fatalf("onboard did not seed quota 3; got %v", qb["quota"])
	}
	// And an enroll against it works — proving the seeded grant carries the unit.
	e := doJSON(t, c, "POST", ts.URL+"/api/machines/enroll", "dev-operator-key", map[string]any{
		"machine_handle": "fra-bde-1", "node_class": "SENSOR-FEED", "autonomy": "manual", "custodian": anchor,
	})
	if e.StatusCode != http.StatusCreated {
		t.Fatalf("enroll against seeded grant: %d %s", e.StatusCode, mustReadBody(t, e))
	}
}

// A machine_quota with no affiliation is skipped (best-effort) — the onboard
// still succeeds; the grant is just not seeded (a machine can't inherit an
// empty unit). Operator can grant later.
func TestOnboard_QuotaWithoutAffiliationSkipsGrant(t *testing.T) {
	ts, _ := newMachineServer(t)
	c := &http.Client{}
	r := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{
		"prefix": "no-unit", "machine_quota": 5,
	})
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("onboard should still succeed without affiliation: %d %s", r.StatusCode, mustReadBody(t, r))
	}
	anchor := mustReadJSON(t, r)["slot_id"].(string)
	q := doJSON(t, c, "GET", ts.URL+"/api/machines?custodian="+anchor, "dev-operator-key", nil)
	qb := mustReadJSON(t, q)
	if quota, ok := qb["quota"].(map[string]any); ok && quota["quota"].(float64) != 0 {
		t.Fatalf("expected no grant (quota 0/absent), got %v", qb["quota"])
	}
}
