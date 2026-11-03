package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/orgstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/pgtest"
)

// orgReq fires one authenticated org-registry request through the full route
// stack (gate included) and returns the recorder.
func orgReq(t *testing.T, h http.Handler, method, path, key string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	req := httptest.NewRequestWithContext(context.Background(), method, path, &buf)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestOrgRegistryAPI(t *testing.T) {
	s := newOnboardTestServer(t)
	orgs, err := orgstore.Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open orgstore: %v", err)
	}
	t.Cleanup(func() { _ = orgs.Close() })
	s.Orgs = orgs
	h := s.Routes()

	// Unauthenticated + wrong key are refused at the gate.
	if rr := orgReq(t, h, "GET", "/api/org-registry/orgs", "", nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: got %d", rr.Code)
	}
	if rr := orgReq(t, h, "GET", "/api/org-registry/orgs", "wrong-key", nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("bad key: got %d", rr.Code)
	}

	// Create the chain root, then a child attached in the same act.
	rr := orgReq(t, h, "POST", "/api/org-registry/orgs", "runner-key", map[string]any{
		"id": "org125x", "kind": "partner", "nation": "SVN",
		"display_name": "org125x",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create root: got %d body %s", rr.Code, rr.Body.String())
	}
	rr = orgReq(t, h, "POST", "/api/org-registry/orgs", "runner-key", map[string]any{
		"id": "org130x", "kind": "partner", "nation": "SVN",
		"parent": "org125x", "justification": "cohort chain fill-in",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create child: got %d body %s", rr.Code, rr.Body.String())
	}
	var child orgstore.OrgView
	if err := json.Unmarshal(rr.Body.Bytes(), &child); err != nil || child.Parent != "org125x" {
		t.Fatalf("child parent: %+v err %v", child, err)
	}

	// A cycle through the API is refused with the typed verdict.
	rr = orgReq(t, h, "POST", "/api/org-registry/orgs/org125x/parent", "runner-key",
		map[string]any{"parent": "org130x"})
	if rr.Code != http.StatusConflict {
		t.Fatalf("cycle: got %d body %s", rr.Code, rr.Body.String())
	}

	// Alias add + one-namespace conflict.
	rr = orgReq(t, h, "POST", "/api/org-registry/orgs/org125x/aliases", "runner-key",
		map[string]any{"alias": "fortele-armate-romane", "kind": "name-variant"})
	if rr.Code != http.StatusOK {
		t.Fatalf("alias: got %d body %s", rr.Code, rr.Body.String())
	}
	rr = orgReq(t, h, "POST", "/api/org-registry/orgs", "runner-key", map[string]any{"id": "fortele-armate-romane"})
	if rr.Code != http.StatusConflict {
		t.Fatalf("org over alias: got %d", rr.Code)
	}

	// Export carries both orgs, the parent edge, and the alias.
	rr = orgReq(t, h, "GET", "/api/org-registry/export", "runner-key", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("export: got %d", rr.Code)
	}
	var cat orgstore.Catalog
	if err := json.Unmarshal(rr.Body.Bytes(), &cat); err != nil {
		t.Fatalf("export decode: %v", err)
	}
	if len(cat.Orgs) != 2 || cat.Orgs[0].ID != "org125x" ||
		len(cat.Orgs[0].Aliases) != 1 || cat.Orgs[1].Parent != "org125x" {
		t.Fatalf("export shape: %+v", cat.Orgs)
	}

	// Clearing the parent makes the child a root again.
	rr = orgReq(t, h, "POST", "/api/org-registry/orgs/org130x/parent", "runner-key",
		map[string]any{"parent": "", "justification": "detach"})
	if rr.Code != http.StatusOK {
		t.Fatalf("clear parent: got %d", rr.Code)
	}
	var cleared orgstore.OrgView
	_ = json.Unmarshal(rr.Body.Bytes(), &cleared)
	if cleared.Parent != "" {
		t.Fatalf("after clear: parent %q", cleared.Parent)
	}
}

// POST /api/org-registry/orgs/{id}/update corrects facts in place, audited —
// the machine-realm door for the `origin` kind repair.
func TestOrgUpdateRouteCorrectsFacts(t *testing.T) {
	s := newOnboardTestServer(t)
	orgs, err := orgstore.Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open orgstore: %v", err)
	}
	t.Cleanup(func() { _ = orgs.Close() })
	s.Orgs = orgs
	h := s.Routes()

	rr := orgReq(t, h, "POST", "/api/org-registry/orgs", "runner-key", map[string]any{
		"id": "origin", "kind": "partner", "nation": "LVA",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: got %d body %s", rr.Code, rr.Body.String())
	}

	rr = orgReq(t, h, "POST", "/api/org-registry/orgs/origin/update", "runner-key", map[string]any{
		"kind": "vendor", "justification": "commercial company; kind captured wrong at intake",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("update: got %d body %s", rr.Code, rr.Body.String())
	}
	v, err := orgs.GetOrg("origin")
	if err != nil || v.Kind != "vendor" || v.Nation != "LVA" {
		t.Fatalf("after update: %+v err %v", v, err)
	}

	// Refusals surface as client errors, not silence.
	if rr := orgReq(t, h, "POST", "/api/org-registry/orgs/origin/update", "runner-key",
		map[string]any{"kind": "battalion"}); rr.Code != http.StatusBadRequest {
		t.Fatalf("bad kind: got %d body %s", rr.Code, rr.Body.String())
	}
	if rr := orgReq(t, h, "POST", "/api/org-registry/orgs/no-such/update", "runner-key",
		map[string]any{"kind": "vendor"}); rr.Code != http.StatusNotFound {
		t.Fatalf("unknown org: got %d", rr.Code)
	}
	if rr := orgReq(t, h, "POST", "/api/org-registry/orgs/origin/update", "wrong-key",
		map[string]any{"kind": "vendor"}); rr.Code != http.StatusUnauthorized {
		t.Fatalf("bad key: got %d", rr.Code)
	}
}

func TestOrgRegistryOperatorOnlyAndUnconfigured(t *testing.T) {
	s := newOnboardTestServer(t)
	orgs, err := orgstore.Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open orgstore: %v", err)
	}
	t.Cleanup(func() { _ = orgs.Close() })
	s.Orgs = orgs

	// A NON-operator principal (a participant sponsor) may read but not
	// mutate. The gate resolves participants via portal delegation, which
	// this test doesn't stand up — so inject the resolved principal directly
	// (same package) and call the handler the way the gate would.
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/org-registry/orgs", bytes.NewBufferString(`{"id":"sneaky-org"}`))
	ctx := context.WithValue(req.Context(), ctxKeyOnboardPrincipal, onboardPrincipal{Handle: "partner-sponsor", IsOperator: false})
	rr := httptest.NewRecorder()
	s.apiOrgCreate(rr, req.WithContext(ctx))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("participant mutation: got %d want 403", rr.Code)
	}

	// Nil store fails closed with an explicit verdict, not a panic.
	s.Orgs = nil
	req2 := httptest.NewRequestWithContext(context.Background(), "GET", "/api/org-registry/orgs", nil)
	ctx2 := context.WithValue(req2.Context(), ctxKeyOnboardPrincipal, onboardPrincipal{Handle: "op", IsOperator: true})
	rr2 := httptest.NewRecorder()
	s.apiOrgList(rr2, req2.WithContext(ctx2))
	if rr2.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil store: got %d want 503", rr2.Code)
	}
}
