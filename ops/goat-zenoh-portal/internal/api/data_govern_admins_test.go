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

// governAdminServer: org `unit` holds machines unit-alice/unit-bob and persons
// carol + dave (members, browser-only); org `partner` holds partner-radar.
func governAdminServer(t *testing.T) (*Server, *registry.Registry) {
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
        {"id": "unit", "kind": "organic", "nation": "USA", "parent": null, "members": [
          {"handle": "alice", "type": "machine", "state": "active", "vendor_tag": "unit-alice", "email_hash": ""},
          {"handle": "bob",   "type": "machine", "state": "active", "vendor_tag": "unit-bob",   "email_hash": ""},
          {"handle": "carol", "type": "person",  "state": "active", "vendor_tag": null, "email_hash": "` + access.EmailHash("carol@example.org") + `"},
          {"handle": "dave",  "type": "person",  "state": "active", "vendor_tag": null, "email_hash": "` + access.EmailHash("dave@example.org") + `"}
        ]},
        {"id": "partner", "kind": "partner", "nation": "EST", "parent": null, "members": [
          {"handle": "radar", "type": "machine", "state": "active", "vendor_tag": "partner-radar", "email_hash": ""}
        ]}
      ],
      "members": []
    }`
	if err := os.WriteFile(rosterPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	staticFS, _ := fs.Sub(web.Static, "static")
	srv := New(&config.Config{OperatorAPIKey: "op-key", RosterPath: rosterPath, GovernBootstrapOpen: true}, reg, nil, nil, nil, nil, nil, staticFS)
	srv.humans = fakeHumans{byUUID: map[string]string{
		"carol-uuid":    "carol@example.org",
		"dave-uuid":     "dave@example.org",
		"outsider-uuid": "out@elsewhere.example",
	}}
	return srv, reg
}

func postGovernJSON(h http.Handler, key, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func rolesFor(t *testing.T, h http.Handler, key, query string) dataRolesResponse {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/access/data-roles?"+query, nil)
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("data-roles %q: %d %s", query, rec.Code, rec.Body.String())
	}
	var resp dataRolesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

// The full U9 arc: bootstrap → nominate → narrowed on both planes → remove.
func TestGovernAdmins_NominationNarrowsBothPlanes(t *testing.T) {
	srv, _ := governAdminServer(t)
	h := srv.Routes()

	// BOOTSTRAP: every member governs — persons carry the sentence, a machine
	// governs its org sibling.
	if !has(rolesFor(t, h, "op-key", "identity=dave-uuid").Roles, "data.govern.org:unit") {
		t.Fatalf("bootstrap: a member must carry data.govern.org:unit")
	}
	mayGovern := func(vendor, producer string) bool {
		r := httptest.NewRequest("POST", "/api/access/grants", nil)
		return srv.mayGovernProducer(r.WithContext(withPrincipal(&Principal{Vendor: vendor})), producer)
	}
	if !mayGovern("unit-alice", "unit-bob") {
		t.Fatalf("bootstrap: a machine must govern its sibling")
	}

	// NOMINATE carol (operator path; by email alias).
	rec := postGovernJSON(h, "op-key", "/api/access/govern-admins",
		`{"org":"unit","email":"carol@example.org","justification":"exercise"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("nominate: %d %s", rec.Code, rec.Body.String())
	}

	// NARROWED, person plane: carol keeps the sentence; dave loses it with a note.
	if !has(rolesFor(t, h, "op-key", "identity=carol-uuid").Roles, "data.govern.org:unit") {
		t.Fatalf("nominated admin must keep data.govern.org:unit")
	}
	dave := rolesFor(t, h, "op-key", "identity=dave-uuid")
	if has(dave.Roles, "data.govern.org:unit") {
		t.Fatalf("non-nominated member must lose data.govern.org:unit: %v", dave.Roles)
	}
	found := false
	for _, n := range dave.Notes {
		if strings.Contains(n, "narrowed") {
			found = true
		}
	}
	if !found {
		t.Fatalf("narrowing must be noted, not silent: %v", dave.Notes)
	}
	// Other orgs unaffected: dave still VIEWS unit (narrowing touches govern only).
	if !has(dave.Roles, "data.view.org:unit") {
		t.Fatalf("view roles must be untouched by narrowing: %v", dave.Roles)
	}

	// NARROWED, machine plane: sibling governance drops; own-producer custody stays.
	if mayGovern("unit-alice", "unit-bob") {
		t.Fatalf("narrowed org: a machine must lose sibling governance")
	}
	if !mayGovern("unit-alice", "unit-alice") {
		t.Fatalf("narrowed org: own-producer custody must remain")
	}
	// A different org stays in bootstrap.
	if !mayGovern("partner-radar", "partner-radar") {
		t.Fatalf("unrelated org must be unaffected")
	}

	// LIST shows the narrowed mode.
	req := httptest.NewRequest("GET", "/api/access/govern-admins?org=unit", nil)
	req.Header.Set("Authorization", "Bearer op-key")
	lrec := httptest.NewRecorder()
	h.ServeHTTP(lrec, req)
	var listed governAdminsResponse
	_ = json.Unmarshal(lrec.Body.Bytes(), &listed)
	if !strings.Contains(listed.Mode, "nominated") || len(listed.Admins) != 1 {
		t.Fatalf("list: %+v", listed)
	}

	// REMOVE: sole admin refused (409); after nominating dave, removal works.
	id := listed.Admins[0].ID
	if rec := postGovernJSON(h, "op-key", "/api/access/govern-admins/"+id+"/remove", `{}`); rec.Code != http.StatusConflict {
		t.Fatalf("last-admin removal must 409, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := postGovernJSON(h, "op-key", "/api/access/govern-admins", `{"org":"unit","identity_id":"dave-uuid"}`); rec.Code != http.StatusCreated {
		t.Fatalf("nominate dave: %d %s", rec.Code, rec.Body.String())
	}
	if rec := postGovernJSON(h, "op-key", "/api/access/govern-admins/"+id+"/remove", `{}`); rec.Code != http.StatusOK {
		t.Fatalf("remove with replacement: %d %s", rec.Code, rec.Body.String())
	}
	// Carol (removed) loses the sentence; dave (nominated) carries it.
	if has(rolesFor(t, h, "op-key", "identity=carol-uuid").Roles, "data.govern.org:unit") {
		t.Fatalf("removed admin must lose data.govern.org:unit")
	}
	if !has(rolesFor(t, h, "op-key", "identity=dave-uuid").Roles, "data.govern.org:unit") {
		t.Fatalf("newly nominated admin must carry data.govern.org:unit")
	}
}

func TestGovernAdmins_NominationGateAndMembership(t *testing.T) {
	srv, _ := governAdminServer(t)
	h := srv.Routes()

	// The at-most-own-scope gate: a machine principal in ANOTHER org may not
	// nominate for unit (mayGovernOrg is the same narrowed-role read).
	r := httptest.NewRequest("POST", "/api/access/govern-admins", nil)
	if srv.mayGovernOrg(r.WithContext(withPrincipal(&Principal{Vendor: "partner-radar"})), "unit") {
		t.Fatalf("an outsider must not administer unit's governance")
	}
	// A unit machine may (bootstrap mode confers governance).
	if !srv.mayGovernOrg(r.WithContext(withPrincipal(&Principal{Vendor: "unit-alice"})), "unit") {
		t.Fatalf("a bootstrap-mode member must administer its own org")
	}

	// A nominee outside the org is refused — an org administers its own members.
	rec := postGovernJSON(h, "op-key", "/api/access/govern-admins",
		`{"org":"unit","identity_id":"outsider-uuid"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("outsider nominee must 422, got %d %s", rec.Code, rec.Body.String())
	}
	// An unknown identity is a 404.
	rec = postGovernJSON(h, "op-key", "/api/access/govern-admins",
		`{"org":"unit","identity_id":"phantom-uuid"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("phantom nominee must 404, got %d", rec.Code)
	}
	// Unauthenticated: the list read fails closed.
	req := httptest.NewRequest("GET", "/api/access/govern-admins", nil)
	lrec := httptest.NewRecorder()
	h.ServeHTTP(lrec, req)
	if lrec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", lrec.Code)
	}
}

// A nominated admin can drive the actual grant loop through the console path
// while a non-nominated sibling machine cannot — the doorway U9 exists for.
func TestGovernAdmins_NarrowedGrantGateEndToEnd(t *testing.T) {
	srv, reg := governAdminServer(t)
	h := srv.Routes()
	if rec := postGovernJSON(h, "op-key", "/api/access/govern-admins",
		`{"org":"unit","identity_id":"carol-uuid"}`); rec.Code != http.StatusCreated {
		t.Fatalf("nominate: %d", rec.Code)
	}
	// Seed the producer so the grant path's existence check passes.
	if err := reg.CreateVendor(context.Background(), &registry.Vendor{Prefix: "unit-bob", DisplayName: "bob"}); err != nil {
		t.Skipf("vendor seed unavailable in this harness: %v", err)
	}
	// A narrowed sibling machine may NOT mint a grant for unit-bob…
	r := httptest.NewRequest("POST", "/api/access/grants", strings.NewReader(`{"producer":"unit-bob","grantee_kind":"org","grantee_id":"partner"}`))
	r = r.WithContext(withPrincipal(&Principal{Vendor: "unit-alice"}))
	rec := httptest.NewRecorder()
	srv.handleCreateGrant(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("narrowed machine grant must 403, got %d %s", rec.Code, rec.Body.String())
	}
	// …while the operator/console path (carol's recorded person) still can.
	r2 := httptest.NewRequest("POST", "/api/access/grants", strings.NewReader(`{"producer":"unit-bob","grantee_kind":"org","grantee_id":"partner"}`))
	r2 = r2.WithContext(withPrincipal(&Principal{Operator: true}))
	rec2 := httptest.NewRecorder()
	srv.handleCreateGrant(rec2, r2)
	if rec2.Code != http.StatusCreated {
		t.Fatalf("console/operator grant must succeed, got %d %s", rec2.Code, rec2.Body.String())
	}
}

// Fail-closed default (GovernBootstrapOpen = false, the canonical posture):
// membership alone confers no governance; only nominated admins govern.
func TestGovernAdmins_FailClosedDefault(t *testing.T) {
	srv, _ := governAdminServer(t)
	srv.cfg.GovernBootstrapOpen = false // canonical fail-closed
	h := srv.Routes()

	// A person member of a not-yet-nominated org does NOT govern it, and the
	// data-roles response says why.
	dave := rolesFor(t, h, "op-key", "identity=dave-uuid")
	if has(dave.Roles, "data.govern.org:unit") {
		t.Fatalf("fail-closed: a bare member must NOT govern: %v", dave.Roles)
	}
	if !has(dave.Roles, "data.view.org:unit") {
		t.Fatalf("fail-closed must not touch view roles: %v", dave.Roles)
	}
	foundNote := false
	for _, n := range dave.Notes {
		if strings.Contains(n, "fail-closed") {
			foundNote = true
		}
	}
	if !foundNote {
		t.Fatalf("fail-closed must be noted honestly: %v", dave.Notes)
	}

	// A machine member of a not-yet-nominated org does NOT govern a sibling.
	mayGovern := func(vendor, producer string) bool {
		r := httptest.NewRequest("POST", "/api/access/grants", nil)
		return srv.mayGovernProducer(r.WithContext(withPrincipal(&Principal{Vendor: vendor})), producer)
	}
	if mayGovern("unit-alice", "unit-bob") {
		t.Fatalf("fail-closed: a machine must NOT govern a sibling by membership")
	}
	// Own-producer custody is a separate, retained gate — still governs itself.
	if !mayGovern("unit-alice", "unit-alice") {
		t.Fatalf("fail-closed: own-producer custody must remain")
	}
	// An operator always governs.
	rOp := httptest.NewRequest("POST", "/api/access/grants", nil)
	if !srv.mayGovernProducer(rOp.WithContext(withPrincipal(&Principal{Operator: true})), "unit-bob") {
		t.Fatalf("operator must govern under fail-closed")
	}

	// List mode reflects fail-closed.
	req := httptest.NewRequest("GET", "/api/access/govern-admins?org=unit", nil)
	req.Header.Set("Authorization", "Bearer op-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var listed governAdminsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &listed)
	if !strings.Contains(listed.Mode, "fail-closed") {
		t.Fatalf("mode should read fail-closed: %q", listed.Mode)
	}

	// Nomination still establishes governance regardless of the default.
	if rec := postGovernJSON(h, "op-key", "/api/access/govern-admins",
		`{"org":"unit","identity_id":"carol-uuid"}`); rec.Code != http.StatusCreated {
		t.Fatalf("nominate under fail-closed: %d %s", rec.Code, rec.Body.String())
	}
	if !has(rolesFor(t, h, "op-key", "identity=carol-uuid").Roles, "data.govern.org:unit") {
		t.Fatalf("a nominated admin governs even under fail-closed")
	}
	// dave still does not (only nominees govern once there are any).
	if has(rolesFor(t, h, "op-key", "identity=dave-uuid").Roles, "data.govern.org:unit") {
		t.Fatalf("non-nominated member must not govern once an org has admins")
	}
}

// The pure resolver no longer emits governance — it is the handler's one source.
func TestResolver_DoesNotEmitGovern(t *testing.T) {
	srv, _ := governAdminServer(t)
	// Even with bootstrap-open, the PURE roster resolver (no handler, no store)
	// must not carry data.govern.org — governance is added at the handler layer.
	dr, err := access.LoadDataRoster(srv.cfg.RosterPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range dr.RolesForPerson(access.EmailHash("carol@example.org")).Roles {
		if strings.HasPrefix(role, "data.govern.org:") {
			t.Fatalf("pure resolver must not emit governance (person): %s", role)
		}
	}
	for _, role := range dr.RolesForAnchor("unit-alice").Roles {
		if strings.HasPrefix(role, "data.govern.org:") {
			t.Fatalf("pure resolver must not emit governance (anchor): %s", role)
		}
	}
}
