package api

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/access"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/config"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/pgtest"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/web"
)

// fakeHumans is an in-memory humanResolver: byID over a UUID→email map, byEmail
// the case-insensitive inverse. It lets the handler tests exercise the human
// paths without a live Kratos.
type fakeHumans struct{ byUUID map[string]string } // uuid -> email

func (f fakeHumans) byID(_ context.Context, id string) (identityRef, error) {
	if e, ok := f.byUUID[id]; ok {
		return identityRef{ID: id, Email: e}, nil
	}
	return identityRef{}, errHumanNotFound
}
func (f fakeHumans) byEmail(_ context.Context, email string) (identityRef, error) {
	for id, e := range f.byUUID {
		if access.EmailHash(e) == access.EmailHash(email) { // case/space-insensitive, same key the roster uses
			return identityRef{ID: id, Email: e}, nil
		}
	}
	return identityRef{}, errHumanNotFound
}
func (f fakeHumans) listAll(_ context.Context) ([]identityRef, error) {
	out := make([]identityRef, 0, len(f.byUUID))
	for id, e := range f.byUUID {
		out = append(out, identityRef{ID: id, Email: e})
	}
	return out, nil
}

// dataRolesTestServer builds a server whose roster.json carries a two-deep org
// (unit → unit-producer) plus a person, and wires a fake human resolver so the
// identity/email paths resolve. Returns the routes, the operator key, and the
// console service token.
func dataRolesTestServer(t *testing.T) (http.Handler, string, string) {
	t.Helper()
	dir := t.TempDir()
	reg, err := registry.Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open registry: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })

	rosterPath := filepath.Join(dir, "roster.json")
	// alice@example.org (uuid alice-uuid) is a member of org `unit` carrying producer
	// `unit-alice`; org `unit` has a child org `unit-sub` with producer
	// `unit-sub-bob`, so viewing `unit` rolls up to include the child's producer.
	body := `{
      "generated_at": "2026-07-09T00:00:00Z",
      "source_digest": "abc123",
      "orgs": [
        {"id": "unit", "kind": "organic", "nation": "USA", "parent": null, "members": [
          {"handle": "alice", "type": "person", "state": "active", "vendor_tag": "unit-alice", "email_hash": "` + access.EmailHash("alice@example.org") + `"}
        ]},
        {"id": "unit-sub", "kind": "organic", "nation": "USA", "parent": "unit", "members": [
          {"handle": "bob", "type": "person", "state": "active", "vendor_tag": "unit-sub-bob", "email_hash": "` + access.EmailHash("bob@example.org") + `"}
        ]}
      ],
      "members": []
    }`
	if err := os.WriteFile(rosterPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	staticFS, err := fs.Sub(web.Static, "static")
	if err != nil {
		t.Fatalf("static fs: %v", err)
	}
	srv := New(&config.Config{OperatorAPIKey: "test-op-key", NotifyToken: "svc-token", RosterPath: rosterPath}, reg, nil, nil, nil, nil, nil, staticFS)
	srv.humans = fakeHumans{byUUID: map[string]string{
		"alice-uuid": "alice@example.org",
		"bob-uuid":   "bob@example.org",
	}}
	return srv.Routes(), "test-op-key", "svc-token"
}

func getDataRoles(t *testing.T, h http.Handler, bearer, query string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", "/api/access/data-roles?"+query, nil)
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func decodeDataRoles(t *testing.T, w *httptest.ResponseRecorder) dataRolesResponse {
	t.Helper()
	var resp dataRolesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (%s)", err, w.Body.String())
	}
	return resp
}

func hasStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestDataRoles_UUIDDirect(t *testing.T) {
	h, opKey, _ := dataRolesTestServer(t)
	w := getDataRoles(t, h, opKey, "identity=alice-uuid")
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d; %s", w.Code, w.Body.String())
	}
	resp := decodeDataRoles(t, w)
	if resp.Principal.ID != "alice-uuid" || resp.Principal.Kind != "person" {
		t.Errorf("principal = %+v, want alice-uuid/person", resp.Principal)
	}
	if !hasStr(resp.Roles, "data.view.org:unit") || !hasStr(resp.Roles, "data.view.producer:unit-alice") {
		t.Errorf("roles missing org/custody: %v", resp.Roles)
	}
	// Org rollup makes the child org's producer real on the seed model.
	if !hasStr(resp.Producers, "unit-sub-bob") || !hasStr(resp.Producers, "unit-alice") {
		t.Errorf("producers missing rollup: %v", resp.Producers)
	}
	if resp.DerivedAt == "" {
		t.Errorf("derived_at not stamped")
	}
}

func TestDataRoles_EmailAliasResolvesToSamePrincipal(t *testing.T) {
	h, opKey, _ := dataRolesTestServer(t)
	// Email is an alias — case-insensitive — and must resolve to the identity
	// UUID as the principal id, yielding the identical role set as identity=.
	byID := decodeDataRoles(t, getDataRoles(t, h, opKey, "identity=alice-uuid"))
	byEmail := decodeDataRoles(t, getDataRoles(t, h, opKey, "email=Alice@Example.org"))
	if byEmail.Principal.ID != "alice-uuid" || byEmail.Principal.Kind != "person" {
		t.Fatalf("email principal = %+v, want alice-uuid keyed on UUID", byEmail.Principal)
	}
	if len(byEmail.Roles) != len(byID.Roles) {
		t.Errorf("email path roles %v != uuid path roles %v", byEmail.Roles, byID.Roles)
	}
}

func TestDataRoles_ConsoleServiceTokenAccepted(t *testing.T) {
	h, _, svcToken := dataRolesTestServer(t)
	w := getDataRoles(t, h, svcToken, "identity=alice-uuid")
	if w.Code != http.StatusOK {
		t.Fatalf("console token should be accepted, got %d; %s", w.Code, w.Body.String())
	}
}

func TestDataRoles_AnchorPath(t *testing.T) {
	h, opKey, _ := dataRolesTestServer(t)
	w := getDataRoles(t, h, opKey, "anchor=unit-alice")
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d; %s", w.Code, w.Body.String())
	}
	resp := decodeDataRoles(t, w)
	if resp.Principal.Kind != "machine" || resp.Principal.ID != "unit-alice" {
		t.Errorf("principal = %+v, want unit-alice/machine", resp.Principal)
	}
	for _, want := range []string{"data.write.producer:unit-alice", "data.view.producer:unit-alice", "data.view.org:unit"} {
		if !hasStr(resp.Roles, want) {
			t.Errorf("roles missing %q: %v", want, resp.Roles)
		}
	}
}

func TestDataRoles_FailClosed(t *testing.T) {
	h, opKey, _ := dataRolesTestServer(t)
	cases := []struct {
		name  string
		token string
		query string
		want  int
	}{
		{"bad token", "wrong-token", "identity=alice-uuid", http.StatusUnauthorized},
		{"no token", "", "identity=alice-uuid", http.StatusUnauthorized},
		{"no params", opKey, "", http.StatusBadRequest},
		{"two params", opKey, "identity=alice-uuid&anchor=unit-alice", http.StatusBadRequest},
		{"unknown identity", opKey, "identity=ghost-uuid", http.StatusNotFound},
		{"unknown anchor", opKey, "anchor=ghost-producer", http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := getDataRoles(t, h, c.token, c.query)
			if w.Code != c.want {
				t.Errorf("code = %d, want %d; %s", w.Code, c.want, w.Body.String())
			}
		})
	}
}
