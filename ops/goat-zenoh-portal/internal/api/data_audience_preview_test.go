package api

// data_audience_preview_test.go exercises GET /api/access/audience/preview as an
// HTTP endpoint — auth, argument handling, and the JSON a producer actually
// receives.
//
// The aggregation itself is covered in internal/access; what is covered HERE is
// everything between the derivation and the wire, which the first version of
// this feature shipped without: the handler shared no test with the audience
// endpoint it previews, so nothing checked that the two agree on the same
// roster and the same grants.
//
// The fixture is audienceServer's, deliberately: the preview must be a
// different VIEW of the same derivation, not a different answer, and reusing
// the audience tests' org shape is what makes a disagreement visible.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/access"
)

func getPreview(t *testing.T, h http.Handler, opKey, query string) audiencePreviewResponse {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/access/audience/preview?"+query, nil)
	req.Header.Set("Authorization", "Bearer "+opKey)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview %q: status %d, body %s", query, rec.Code, rec.Body.String())
	}
	var resp audiencePreviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

// Fail-closed, same as the audience read it previews. A producer asking about
// its own stream is still an operator action, and the surface reveals the
// aggregate composition of an organisation.
func TestPreview_RequiresAuth(t *testing.T) {
	h, _, _ := audienceServer(t)
	req := httptest.NewRequest("GET", "/api/access/audience/preview?producer=unit-alice", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestPreview_RequiresAReleaseSet(t *testing.T) {
	h, _, opKey := audienceServer(t)
	req := httptest.NewRequest("GET", "/api/access/audience/preview", nil)
	req.Header.Set("Authorization", "Bearer "+opKey)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 with no release_set, got %d", rec.Code)
	}
}

// THE CASE THE SURFACE EXISTS FOR. One org, three holders, three different
// outcomes — and the counts have to keep them apart, because a producer reading
// "0 excluded" when the real answer is "2 undeclared" concludes the restriction
// is tight when it was never evaluated.
func TestPreview_SeparatesExcludedFromUndeclared(t *testing.T) {
	h, reg, opKey := audienceServer(t)
	grantOrg(t, reg, "unit")
	p := getPreview(t, h, opKey, "producer=partner-radar")

	if !p.Encrypted {
		t.Error("a release-set carrying a grant must report encrypted=true")
	}
	// alice (USA in the USA-owned org) is the only holder admitted. The
	// producer's own slot is deliberately not counted — it is the author.
	if p.Holders != 1 {
		t.Fatalf("holders = %d, want 1 (alice only; the producer is not audience)", p.Holders)
	}
	if p.Refused.Excluded != 1 {
		t.Errorf("excluded = %d, want 1 (chuck, CAN in a USA org)", p.Refused.Excluded)
	}
	if p.Refused.HolderNationalityUndeclared != 1 {
		t.Errorf("holder-undeclared = %d, want 1", p.Refused.HolderNationalityUndeclared)
	}
	if p.Refused.Total != 2 {
		t.Errorf("refused total = %d, want 2", p.Refused.Total)
	}
	if !p.Evaluable {
		t.Error("a preview that excluded someone on declared facts IS evaluable")
	}
}

// An org that declares no owning nation cannot be tested against, so its zero
// means "not answerable" — and must say so rather than reading as a restriction
// that is holding.
func TestPreview_OrgWithNoNationIsNotEvaluable(t *testing.T) {
	h, reg, opKey := audienceServer(t)
	grantOrg(t, reg, "nonation")
	p := getPreview(t, h, opKey, "producer=partner-radar")

	if p.Holders != 0 {
		t.Fatalf("holders = %d, want 0", p.Holders)
	}
	if p.Evaluable {
		t.Fatal("nobody could be tested, so the preview must NOT report itself evaluable")
	}
	if p.Refused.OrgNationUndeclared != 1 {
		t.Errorf("org-nation-undeclared = %d, want 1", p.Refused.OrgNationUndeclared)
	}
	if p.Refused.Excluded != 0 {
		t.Errorf("excluded = %d, want 0 — the rule never ran", p.Refused.Excluded)
	}
	var explained bool
	for _, n := range p.Notes {
		if strings.Contains(n, "NOT EVALUABLE") {
			explained = true
		}
	}
	if !explained {
		t.Errorf("a non-evaluable zero must say so in words too: %v", p.Notes)
	}
}

// A producer with no grant is publishing in clear; the audience question does
// not apply to it, and the surface says that instead of returning a bare zero
// that looks like a tight restriction.
func TestPreview_NoGrantMeansNothingIsEncrypted(t *testing.T) {
	h, _, opKey := audienceServer(t)
	p := getPreview(t, h, opKey, "producer=partner-radar")

	if p.Encrypted {
		t.Error("a release-set with no grant must report encrypted=false")
	}
	if p.Holders != 0 {
		t.Errorf("holders = %d, want 0", p.Holders)
	}
	var explained bool
	for _, n := range p.Notes {
		if strings.Contains(n, "no grant") {
			explained = true
		}
	}
	if !explained {
		t.Errorf("expected a note that nothing here is encrypted: %v", p.Notes)
	}
}

// THE ANTI-DRIFT CHECK, and the reason the preview shares deriveAudience with
// the audience endpoint rather than computing its own answer. Same server, same
// grants, same request: every anchor the audience wraps to, other than the
// producer's own slot, must be one the preview counted.
func TestPreview_AgreesWithTheAudienceItPreviews(t *testing.T) {
	h, reg, opKey := audienceServer(t)
	grantOrg(t, reg, "unit")

	members := getAudience(t, h, opKey, "producer=partner-radar").Members
	p := getPreview(t, h, opKey, "producer=partner-radar")

	wrapped := 0
	for _, m := range members {
		if m != "partner-radar" { // the producer is author, not audience
			wrapped++
		}
	}
	if wrapped != p.Holders {
		t.Fatalf("audience wraps %d holder(s) but preview counts %d — the two have drifted",
			wrapped, p.Holders)
	}
}

// The preview names nobody. Handles, emails and anchors are exactly what must
// not leave this handler: docs/operators/roster.json is public-safe by contract
// and this surface holds the same line.
func TestPreview_NamesNobody(t *testing.T) {
	h, reg, opKey := audienceServer(t)
	grantOrg(t, reg, "unit")

	req := httptest.NewRequest("GET", "/api/access/audience/preview?producer=partner-radar", nil)
	req.Header.Set("Authorization", "Bearer "+opKey)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()

	for _, forbidden := range []string{
		"unit-alice", "unit-chuck", "unit-undeclared", // machine anchors
		"alice", "chuck", // handles
		access.EmailHash("alice@example.org"), // the roster join key
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the preview leaked %q: %s", forbidden, body)
		}
	}
}

// Below the k-anonymity floor the breakdown is withheld while the total stays
// exact — a single-holder cell names a person to anyone who knows the org.
func TestPreview_SmallAudienceWithholdsItsBreakdown(t *testing.T) {
	h, reg, opKey := audienceServer(t)
	grantOrg(t, reg, "unit")
	p := getPreview(t, h, opKey, "producer=partner-radar")

	if p.Holders != 1 {
		t.Fatalf("holders = %d, want 1", p.Holders)
	}
	for _, c := range append(append([]access.AudienceCell{}, p.ByNationality...), p.ByOrg...) {
		if c.Value != "" {
			t.Errorf("cell %q named below the k-floor of %d", c.Value, access.KAnonymityFloor)
		}
	}
}
