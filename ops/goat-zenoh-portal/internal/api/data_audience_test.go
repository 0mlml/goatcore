package api

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/access"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/config"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/pgtest"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/web"
)

// audienceServer builds a portal with a two-org roster: `unit` (parent) with a
// member producer `unit-alice`, and `partner` with `partner-radar`. The audience
// of a producer is derived from its in-effect grant rows.
func audienceServer(t *testing.T) (http.Handler, *registry.Registry, string) {
	t.Helper()
	dir := t.TempDir()
	reg, err := registry.Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open registry: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })
	rosterPath := filepath.Join(dir, "roster.json")
	body := `{
      "generated_at": "2026-07-29T00:00:00Z",
      "orgs": [
        {"id": "unit", "kind": "organic", "nation": "USA", "composition": "national", "parent": null, "members": [
          {"handle": "alice", "type": "person", "state": "active", "vendor_tag": "unit-alice", "nationality": "USA", "email_hash": "` + access.EmailHash("alice@example.org") + `"},
          {"handle": "chuck", "type": "person", "state": "active", "vendor_tag": "unit-chuck", "nationality": "CAN", "email_hash": "` + access.EmailHash("chuck@ca.example") + `"},
          {"handle": "undeclared", "type": "person", "state": "active", "vendor_tag": "unit-undeclared", "email_hash": "` + access.EmailHash("u@example.org") + `"}
        ]},
        {"id": "nato-cmd", "kind": "partner", "nation": "NATO", "composition": "multinational", "parent": null, "members": [
          {"handle": "bel", "type": "person", "state": "active", "vendor_tag": "nato-bel", "nationality": "BEL", "email_hash": "` + access.EmailHash("b@be.example") + `"},
          {"handle": "ukr", "type": "person", "state": "active", "vendor_tag": "nato-ukr", "nationality": "UKR", "email_hash": "` + access.EmailHash("u@ua.example") + `"}
        ]},
        {"id": "nonation", "kind": "organic", "parent": null, "members": [
          {"handle": "nobody", "type": "person", "state": "active", "vendor_tag": "nonation-x", "nationality": "USA", "email_hash": "` + access.EmailHash("n@example.org") + `"}
        ]},
        {"id": "partner", "kind": "partner", "nation": "EST", "parent": null, "members": [
          {"handle": "raadar", "type": "machine", "state": "active", "vendor_tag": "partner-radar", "email_hash": ""}
        ]}
      ],
      "members": []
    }`
	if err := os.WriteFile(rosterPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	staticFS, _ := fs.Sub(web.Static, "static")
	srv := New(&config.Config{OperatorAPIKey: "test-op-key", RosterPath: rosterPath}, reg, nil, nil, nil, nil, nil, staticFS)
	return srv.Routes(), reg, "test-op-key"
}

func getAudience(t *testing.T, h http.Handler, opKey, query string) audienceResponse {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/access/audience?"+query, nil)
	req.Header.Set("Authorization", "Bearer "+opKey)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("audience %q: status %d, body %s", query, rec.Code, rec.Body.String())
	}
	var resp audienceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

func has(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// The producer is always in its own audience (producer-as-member), even with no
// grants — so it can unwrap its own DEK slot.
func TestAudience_ProducerIsAlwaysItsOwnMember(t *testing.T) {
	h, _, opKey := audienceServer(t)
	resp := getAudience(t, h, opKey, "producer=unit-alice")
	if !has(resp.Members, "unit-alice") {
		t.Fatalf("producer not in its own audience: %v", resp.Members)
	}
	if len(resp.Members) != 1 {
		t.Fatalf("no grants → audience should be just the producer, got %v", resp.Members)
	}
}

// An org grant expands to the producer anchors under the grantee org.
func TestAudience_OrgGrantExpandsToMemberAnchors(t *testing.T) {
	h, reg, opKey := audienceServer(t)
	// partner-radar shares to org `unit` → unit's producers (unit-alice) join the
	// audience alongside the producer itself.
	if err := reg.CreateGrant(context.Background(), &registry.Grant{
		GranteeKind: registry.GranteeOrg, GranteeID: "unit",
		Producer: "partner-radar", GrantedBy: "operator", Justification: "exercise",
		EffectiveFrom: 1,
	}); err != nil {
		t.Fatalf("create org grant: %v", err)
	}
	resp := getAudience(t, h, opKey, "producer=partner-radar")
	if !has(resp.Members, "partner-radar") {
		t.Fatalf("producer missing: %v", resp.Members)
	}
	if !has(resp.Members, "unit-alice") {
		t.Fatalf("org grant did not expand to member anchor: %v", resp.Members)
	}
}

// A principal grantee with no machine anchor is a browser-only reader: noted,
// not wrapped.
func TestAudience_PrincipalWithoutAnchorIsNotedNotWrapped(t *testing.T) {
	h, reg, opKey := audienceServer(t)
	if err := reg.CreateGrant(context.Background(), &registry.Grant{
		GranteeKind: registry.GranteePrincipal, GranteeID: "some-human-uuid",
		Producer: "partner-radar", GrantedBy: "operator", EffectiveFrom: 1,
	}); err != nil {
		t.Fatalf("create principal grant: %v", err)
	}
	resp := getAudience(t, h, opKey, "producer=partner-radar")
	if has(resp.Members, "some-human-uuid") {
		t.Fatalf("browser-only principal must not be a wrap target: %v", resp.Members)
	}
	if len(resp.Notes) == 0 {
		t.Fatalf("expected a note explaining the human-plane reader")
	}
}

// A revoked grant drops its grantee from the audience.
func TestAudience_RevokedGrantDropsMember(t *testing.T) {
	h, reg, opKey := audienceServer(t)
	ctx := context.Background()
	g := &registry.Grant{
		GranteeKind: registry.GranteeOrg, GranteeID: "unit",
		Producer: "partner-radar", GrantedBy: "operator", EffectiveFrom: 1,
	}
	if err := reg.CreateGrant(ctx, g); err != nil {
		t.Fatalf("create: %v", err)
	}
	if has(getAudience(t, h, opKey, "producer=partner-radar").Members, "unit-alice") == false {
		t.Fatalf("precondition: member should be present before revoke")
	}
	if err := reg.RevokeGrant(ctx, g.ID, "operator"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if has(getAudience(t, h, opKey, "producer=partner-radar").Members, "unit-alice") {
		t.Fatalf("revoked grant still in audience")
	}
}

// Unauthenticated callers get nothing (fail-closed).
func TestAudience_RequiresAuth(t *testing.T) {
	h, _, _ := audienceServer(t)
	req := httptest.NewRequest("GET", "/api/access/audience?producer=unit-alice", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

// grant-keyd parses release_sets as a list, so no grants must be [], never null.
func TestReleaseSets_NoGrantsIsAnEmptyList(t *testing.T) {
	h, _, token := audienceServer(t)
	req := httptest.NewRequest("GET", "/api/access/release-sets", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"release_sets":[]`) {
		t.Fatalf("want an empty list, got %s", rec.Body.String())
	}
}

// grantOrg is the shared setup for the nationality-intersection cases below.
func grantOrg(t *testing.T, reg *registry.Registry, org string) {
	t.Helper()
	if err := reg.CreateGrant(context.Background(), &registry.Grant{
		GranteeKind: registry.GranteeOrg, GranteeID: org,
		Producer: "partner-radar", GrantedBy: "operator", Justification: "exercise",
		EffectiveFrom: 1,
	}); err != nil {
		t.Fatalf("create org grant for %s: %v", org, err)
	}
}

// THE RULE (ADR 1171, ratified): a member of the grantee org earns a
// wrap slot only if their OWN nationality is one of the nations that own it.
// This is the case the whole model exists for — two people at the same desk in
// the same org, separated by citizenship and nothing else.
func TestAudience_NationalityIntersection_NationalOrg(t *testing.T) {
	h, reg, opKey := audienceServer(t)
	grantOrg(t, reg, "unit")
	resp := getAudience(t, h, opKey, "producer=partner-radar")

	if !has(resp.Members, "unit-alice") {
		t.Errorf("a USA holder in the USA-owned org must be wrapped: %v", resp.Members)
	}
	if has(resp.Members, "unit-chuck") {
		t.Errorf("a CAN holder in the USA-owned org must NOT be wrapped: %v", resp.Members)
	}
	if has(resp.Members, "unit-undeclared") {
		t.Errorf("an undeclared nationality is not a match — must not be wrapped: %v", resp.Members)
	}
	// A silent shrink is the failure mode this fabric already has: the
	// producer has to be able to see WHY the audience is smaller than the org.
	var sawChuck, sawUndeclared bool
	for _, n := range resp.Notes {
		if strings.Contains(n, "unit-chuck") && strings.Contains(n, "CAN is not USA") {
			sawChuck = true
		}
		if strings.Contains(n, "unit-undeclared") && strings.Contains(n, "declares no nationality") {
			sawUndeclared = true
		}
	}
	if !sawChuck {
		t.Errorf("no note explaining the refused CAN holder: %v", resp.Notes)
	}
	if !sawUndeclared {
		t.Errorf("no note explaining the undeclared holder: %v", resp.Notes)
	}
}

// A NATO-constituted org's material reaches the citizens of NATO nations within
// it — that is what "owned by NATO" means. A non-member's citizen inside the
// same org does not, and falls out of the same rule rather than a second one.
func TestAudience_NationalityIntersection_MultinationalOrg(t *testing.T) {
	h, reg, opKey := audienceServer(t)
	grantOrg(t, reg, "nato-cmd")
	resp := getAudience(t, h, opKey, "producer=partner-radar")

	// Without a coalition framework configured, a multinational org cannot be
	// expanded — and refuses, rather than wrapping to everyone in it.
	if has(resp.Members, "nato-bel") || has(resp.Members, "nato-ukr") {
		t.Errorf("a multinational org with no framework loaded must wrap to nobody: %v", resp.Members)
	}
	var explained bool
	for _, n := range resp.Notes {
		if strings.Contains(n, "multinational") && strings.Contains(n, "NATO") {
			explained = true
		}
	}
	if !explained {
		t.Errorf("the refusal must say the framework is missing: %v", resp.Notes)
	}
}

// Fail-closed on the ORG side: an org that declares no owning nation cannot be
// matched against, so nobody in it is wrapped — never everybody.
func TestAudience_NationalityIntersection_OrgWithNoNationWrapsNobody(t *testing.T) {
	h, reg, opKey := audienceServer(t)
	grantOrg(t, reg, "nonation")
	resp := getAudience(t, h, opKey, "producer=partner-radar")

	if has(resp.Members, "nonation-x") {
		t.Errorf("an org declaring no nation must wrap to nobody: %v", resp.Members)
	}
	var explained bool
	for _, n := range resp.Notes {
		if strings.Contains(n, "declares no owning nation") {
			explained = true
		}
	}
	if !explained {
		t.Errorf("expected a note naming the undeclared org nation: %v", resp.Notes)
	}
	// The producer itself is always in its own audience — the intersection
	// narrows the grantee org, it never removes the publisher.
	if !has(resp.Members, "partner-radar") {
		t.Errorf("the producer must remain in its own audience: %v", resp.Members)
	}
}

// An org grant must name an org the roster declares. A member anchor in the org
// slot is refused with the org that declares it; the org id itself is accepted.
func TestCreateGrant_RefusesAnOrgTheRosterLacks(t *testing.T) {
	h, reg, opKey := audienceServer(t)
	post := func(path string, body map[string]any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", path, strings.NewReader(string(b)))
		req.Header.Set("Authorization", "Bearer "+opKey)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	if err := reg.CreateVendor(context.Background(), &registry.Vendor{Prefix: "partner-radar"}); err != nil {
		t.Fatalf("register producer: %v", err)
	}
	grant := func(org string) *httptest.ResponseRecorder {
		return post("/api/access/grants", map[string]any{
			"producer": "partner-radar", "grantee_kind": "org", "grantee_id": org, "justification": "share",
		})
	}
	rec := grant("unit-alice")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "unknown_org") ||
		!strings.Contains(rec.Body.String(), "declared under org unit") {
		t.Fatalf("grant to a member anchor: %d %s", rec.Code, rec.Body.String())
	}
	if rec := grant("no-such-org"); rec.Code != http.StatusBadRequest {
		t.Fatalf("grant to an undeclared org: %d %s", rec.Code, rec.Body.String())
	}
	if rec := grant("unit"); rec.Code != http.StatusCreated {
		t.Fatalf("grant to a declared org: %d %s", rec.Code, rec.Body.String())
	}
}
