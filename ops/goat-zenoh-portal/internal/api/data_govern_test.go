package api

import (
	"context"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/config"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/pgtest"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/web"
)

// governServer builds a Server with a roster where org `unit` owns two producers
// (unit-alice, unit-bob) and org `partner` owns partner-radar — so we can test
// that a member of `unit` governs sharing for a SIBLING producer in `unit` but
// not for a producer in `partner` (ADR 1102 org data-governance authority).
func governServer(t *testing.T) *Server {
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
          {"handle": "bob",   "type": "machine", "state": "active", "vendor_tag": "unit-bob",   "email_hash": ""}
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
	return New(&config.Config{OperatorAPIKey: "op", RosterPath: rosterPath, GovernBootstrapOpen: true}, reg, nil, nil, nil, nil, nil, staticFS)
}

func withPrincipal(p *Principal) context.Context {
	return context.WithValue(context.Background(), principalKey, p)
}

func TestMayGovernProducer_OrgScopedAuthority(t *testing.T) {
	s := governServer(t)
	mk := func(p *Principal, producer string) bool {
		r := httptest.NewRequest("POST", "/api/access/grants", nil)
		return s.mayGovernProducer(r.WithContext(withPrincipal(p)), producer)
	}

	// Own producer — the custody path (bootstrapped self-governance).
	if !mk(&Principal{Vendor: "unit-alice"}, "unit-alice") {
		t.Fatal("a producer must govern its own sharing")
	}
	// Sibling producer in the SAME org — the org data-governance authority.
	if !mk(&Principal{Vendor: "unit-alice"}, "unit-bob") {
		t.Fatal("a unit member must govern a sibling producer its org owns")
	}
	// A producer in a DIFFERENT org — denied.
	if mk(&Principal{Vendor: "unit-alice"}, "partner-radar") {
		t.Fatal("a unit member must NOT govern another org's producer")
	}
	// An operator governs anything.
	if !mk(&Principal{Operator: true}, "partner-radar") {
		t.Fatal("an operator must govern any producer")
	}
	// A principal with no producer identity has no org authority.
	if mk(&Principal{}, "unit-bob") {
		t.Fatal("a principal with no producer must not govern via the org path")
	}
	// nil principal (unauthenticated) — denied.
	r := httptest.NewRequest("POST", "/api/access/grants", nil)
	if s.mayGovernProducer(r, "unit-alice") {
		t.Fatal("an unauthenticated caller must not govern")
	}
}
