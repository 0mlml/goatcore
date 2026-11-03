package api

import (
	"context"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/access"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/config"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/pgtest"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/web"
)

// dataRolesGrantServer mirrors dataRolesTestServer but also returns the registry
// so a test can seed grant rows and observe them join into the resolver output.
func dataRolesGrantServer(t *testing.T) (http.Handler, *registry.Registry, string) {
	t.Helper()
	dir := t.TempDir()
	reg, err := registry.Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open registry: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })
	rosterPath := filepath.Join(dir, "roster.json")
	body := `{
      "generated_at": "2026-07-09T00:00:00Z",
      "orgs": [
        {"id": "unit", "kind": "organic", "nation": "USA", "parent": null, "members": [
          {"handle": "alice", "type": "person", "state": "active", "vendor_tag": "unit-alice", "email_hash": "` + access.EmailHash("alice@example.org") + `"}
        ]}
      ],
      "members": []
    }`
	if err := os.WriteFile(rosterPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	staticFS, _ := fs.Sub(web.Static, "static")
	srv := New(&config.Config{OperatorAPIKey: "test-op-key", RosterPath: rosterPath}, reg, nil, nil, nil, nil, nil, staticFS)
	srv.humans = fakeHumans{byUUID: map[string]string{"alice-uuid": "alice@example.org"}}
	return srv.Routes(), reg, "test-op-key"
}

func TestDataRoles_PrincipalGrantJoins(t *testing.T) {
	h, reg, opKey := dataRolesGrantServer(t)
	ctx := context.Background()
	// A preemptive principal grant to alice for a producer she has no registry
	// relation to.
	if err := reg.CreateGrant(ctx, &registry.Grant{
		GranteeKind: registry.GranteePrincipal, GranteeID: "alice-uuid",
		Producer: "partner-eo", GrantedBy: "operator", Justification: "coalition",
		EffectiveFrom: 1,
	}); err != nil {
		t.Fatalf("create grant: %v", err)
	}
	resp := decodeDataRoles(t, getDataRoles(t, h, opKey, "identity=alice-uuid"))
	if !hasStr(resp.Roles, "data.view.producer:partner-eo") {
		t.Fatalf("principal grant did not join: %v", resp.Roles)
	}
	if !hasStr(resp.Producers, "partner-eo") {
		t.Fatalf("granted producer missing from set: %v", resp.Producers)
	}
}

func TestDataRoles_OrgGrantJoinsForMember(t *testing.T) {
	h, reg, opKey := dataRolesGrantServer(t)
	ctx := context.Background()
	// An org grant to `unit` (alice's org) opens partner-radar for every member.
	if err := reg.CreateGrant(ctx, &registry.Grant{
		GranteeKind: registry.GranteeOrg, GranteeID: "unit",
		Producer: "partner-radar", GrantedBy: "operator", Justification: "exercise",
		EffectiveFrom: 1,
	}); err != nil {
		t.Fatalf("create org grant: %v", err)
	}
	resp := decodeDataRoles(t, getDataRoles(t, h, opKey, "identity=alice-uuid"))
	if !hasStr(resp.Roles, "data.view.producer:partner-radar") {
		t.Fatalf("org grant did not join for member: %v", resp.Roles)
	}
}

func TestDataRoles_RevokedGrantDoesNotJoin(t *testing.T) {
	h, reg, opKey := dataRolesGrantServer(t)
	ctx := context.Background()
	g := &registry.Grant{
		GranteeKind: registry.GranteePrincipal, GranteeID: "alice-uuid",
		Producer: "partner-eo", GrantedBy: "operator", EffectiveFrom: 1,
	}
	if err := reg.CreateGrant(ctx, g); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := reg.RevokeGrant(ctx, g.ID, "operator"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	resp := decodeDataRoles(t, getDataRoles(t, h, opKey, "identity=alice-uuid"))
	if hasStr(resp.Roles, "data.view.producer:partner-eo") {
		t.Fatalf("revoked grant should not join: %v", resp.Roles)
	}
}

func TestDataRoles_ExpiredGrantDoesNotJoin(t *testing.T) {
	h, reg, opKey := dataRolesGrantServer(t)
	ctx := context.Background()
	// A grant whose window already closed (effective_to in the past).
	past := int64(1000)
	if err := reg.CreateGrant(ctx, &registry.Grant{
		GranteeKind: registry.GranteePrincipal, GranteeID: "alice-uuid",
		Producer: "partner-eo", GrantedBy: "operator", EffectiveFrom: 1, EffectiveTo: &past,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	resp := decodeDataRoles(t, getDataRoles(t, h, opKey, "identity=alice-uuid"))
	if hasStr(resp.Roles, "data.view.producer:partner-eo") {
		t.Fatalf("expired grant should not join: %v", resp.Roles)
	}
}
