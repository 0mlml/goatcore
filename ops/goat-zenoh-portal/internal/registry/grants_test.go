package registry

import (
	"context"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/pgtest"
)

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	reg, err := Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open registry: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })
	return reg
}

func i64(v int64) *int64 { return &v }

func TestGrantCreateGetList(t *testing.T) {
	reg := testRegistry(t)
	ctx := context.Background()
	g := &Grant{
		GranteeKind: GranteePrincipal, GranteeID: "uuid-alice",
		Producer: "org1-producer", GrantedBy: "operator",
		Justification: "coalition liaison", EffectiveFrom: 100,
	}
	if err := reg.CreateGrant(ctx, g); err != nil {
		t.Fatalf("create grant: %v", err)
	}
	if g.ID == "" {
		t.Fatal("expected an assigned grant id")
	}
	got, err := reg.GetGrant(ctx, g.ID)
	if err != nil {
		t.Fatalf("get grant: %v", err)
	}
	if got.GranteeID != "uuid-alice" || got.Producer != "org1-producer" || got.Status != GrantActive {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	rows, err := reg.ListGrants(ctx, "org1-producer", "", "", false)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list by producer: rows=%d err=%v", len(rows), err)
	}
	none, _ := reg.ListGrants(ctx, "other-producer", "", "", false)
	if len(none) != 0 {
		t.Fatalf("expected no rows for other producer, got %d", len(none))
	}
}

func TestGrantValidation(t *testing.T) {
	reg := testRegistry(t)
	ctx := context.Background()
	cases := []struct {
		name string
		g    *Grant
	}{
		{"bad kind", &Grant{GranteeKind: "team", GranteeID: "x", Producer: "p", EffectiveFrom: 1}},
		{"empty grantee", &Grant{GranteeKind: GranteeOrg, GranteeID: "", Producer: "p", EffectiveFrom: 1}},
		{"empty producer", &Grant{GranteeKind: GranteeOrg, GranteeID: "o", Producer: "", EffectiveFrom: 1}},
		{"window inverted", &Grant{GranteeKind: GranteeOrg, GranteeID: "o", Producer: "p", EffectiveFrom: 10, EffectiveTo: i64(5)}},
		// ADR 1105 co-publish (write) grant validation
		{"bad action", &Grant{GranteeKind: GranteePrincipal, GranteeID: "x", Producer: "multipub/aa/bb", Action: "publish", EffectiveFrom: 1}},
		{"write to org grantee", &Grant{GranteeKind: GranteeOrg, GranteeID: "o", Producer: "multipub/aa/bb", Action: GrantActionWrite, EffectiveFrom: 1}},
		// The guardrail: a write grant target must be a multipub-topic key-base,
		// never a single-owner root or any other key.
		{"write to single-owner anchor", &Grant{GranteeKind: GranteePrincipal, GranteeID: "x", Producer: "1a2b3c4d", Action: GrantActionWrite, EffectiveFrom: 1}},
		{"write to non-multipub prefix", &Grant{GranteeKind: GranteePrincipal, GranteeID: "x", Producer: "ops/foo/bar", Action: GrantActionWrite, EffectiveFrom: 1}},
		{"write to malformed multipub key", &Grant{GranteeKind: GranteePrincipal, GranteeID: "x", Producer: "multipub/aa", Action: GrantActionWrite, EffectiveFrom: 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := reg.CreateGrant(ctx, c.g); err == nil {
				t.Fatalf("expected validation error for %s", c.name)
			}
		})
	}
}

func TestCoPublishWriteGrant(t *testing.T) {
	reg := testRegistry(t)
	ctx := context.Background()

	// Legacy row: no action set → backfills to view, never write.
	legacy := &Grant{GranteeKind: GranteePrincipal, GranteeID: "uuid-alice", Producer: "acme-sensor", GrantedBy: "op", EffectiveFrom: 1}
	if err := reg.CreateGrant(ctx, legacy); err != nil {
		t.Fatalf("create legacy grant: %v", err)
	}
	if got, _ := reg.GetGrant(ctx, legacy.ID); got.Action != GrantActionView || got.IsWrite() {
		t.Fatalf("legacy grant must default to view, got action=%q isWrite=%v", got.Action, got.IsWrite())
	}

	// A co-publish write grant targets a multipub-topic KEY-BASE; CoPublishKeyBase
	// is exactly the producer field (the whole multi-publisher topic is the unit).
	keyBase := MultipubTopicKeyBase("aa11bb22", "cc33dd44")
	cp := &Grant{GranteeKind: GranteePrincipal, GranteeID: "co-writer-x", Producer: keyBase, Action: GrantActionWrite, GrantedBy: "admin", Justification: "shared org feed", EffectiveFrom: 1}
	if err := reg.CreateGrant(ctx, cp); err != nil {
		t.Fatalf("create co-publish grant: %v", err)
	}
	if base := cp.CoPublishKeyBase(); base != keyBase {
		t.Fatalf("co-publish key base = %q, want %q", base, keyBase)
	}

	// ActiveWriteGrants returns only the write grant, never the legacy view one.
	writes, err := reg.ActiveWriteGrants(ctx)
	if err != nil {
		t.Fatalf("ActiveWriteGrants: %v", err)
	}
	if len(writes) != 1 || !writes[0].IsWrite() {
		t.Fatalf("expected exactly 1 active write grant, got %d", len(writes))
	}

	// The legacy view grant stays out of the encryption sweep candidate list is
	// unaffected; the write grant must NOT appear there.
	prods, err := reg.ProducersWithViewGrants(ctx)
	if err != nil {
		t.Fatalf("ProducersWithViewGrants: %v", err)
	}
	for _, p := range prods {
		if p == keyBase {
			t.Fatalf("a co-publish (write) grant leaked into the encryption-sweep producer list: %q", p)
		}
	}
}

// Revoking a producer's last grant must leave it in the sweep list, so grant-keyd
// rotates to a producer-only generation instead of leaving the revoked reader's
// generation current.
func TestRevokedGrantKeepsProducerInSweep(t *testing.T) {
	reg := testRegistry(t)
	ctx := context.Background()
	g := &Grant{GranteeKind: GranteeOrg, GranteeID: "partner", Producer: "52bd565e", GrantedBy: "admin", Justification: "share", EffectiveFrom: 1}
	if err := reg.CreateGrant(ctx, g); err != nil {
		t.Fatalf("create grant: %v", err)
	}
	if err := reg.RevokeGrant(ctx, g.ID, "admin"); err != nil {
		t.Fatalf("revoke grant: %v", err)
	}
	prods, err := reg.ProducersWithViewGrants(ctx)
	if err != nil {
		t.Fatalf("ProducersWithViewGrants: %v", err)
	}
	if len(prods) != 1 || prods[0] != "52bd565e" {
		t.Fatalf("sweep list after revoking the last grant = %v, want [52bd565e]", prods)
	}
}

func TestMultipubTopicKeyBaseGuardrail(t *testing.T) {
	ok := []string{"multipub/aa/bb", "multipub/aa11bb22/cc33dd44"}
	for _, k := range ok {
		if !IsMultipubTopicKeyBase(k) {
			t.Fatalf("expected %q to be a valid multipub-topic key-base", k)
		}
	}
	// A single-owner root, wrong prefix, wrong arity, or wildcards must all fail —
	// this is the guardrail's structural floor.
	bad := []string{"1a2b3c4d", "multipub/aa", "multipub/aa/bb/cc", "ops/x/y", "multipub//bb", "multipub/aa/**", "release/set/x"}
	for _, k := range bad {
		if IsMultipubTopicKeyBase(k) {
			t.Fatalf("expected %q to be REJECTED as a multipub-topic key-base", k)
		}
	}
}

func TestMultipubTopicCreateAndResolve(t *testing.T) {
	reg := testRegistry(t)
	ctx := context.Background()

	st, err := reg.CreateMultipubTopic(ctx, "acme", "Shared status feed", "redundant collectors", "admin", true)
	if err != nil {
		t.Fatalf("create multi-publisher topic: %v", err)
	}
	if st.Org != "acme" || st.PoolHandle == "" || st.TopicID == "" {
		t.Fatalf("multi-publisher topic not fully populated: %+v", st)
	}
	if !IsMultipubTopicKeyBase(st.KeyBase()) {
		t.Fatalf("multi-publisher topic key-base is not well-formed: %q", st.KeyBase())
	}

	// The pool handle is stable across a second topic for the same org.
	st2, err := reg.CreateMultipubTopic(ctx, "acme", "Second feed", "", "admin", true)
	if err != nil {
		t.Fatalf("create second multi-publisher topic: %v", err)
	}
	if st2.PoolHandle != st.PoolHandle {
		t.Fatalf("pool handle must be stable per org: %q vs %q", st2.PoolHandle, st.PoolHandle)
	}

	// Resolve by key-base round-trips; a forged pool under a real topic_id does not.
	got, err := reg.GetMultipubTopicByKeyBase(ctx, st.KeyBase())
	if err != nil || got.TopicID != st.TopicID {
		t.Fatalf("resolve by key-base failed: got=%+v err=%v", got, err)
	}
	if _, err := reg.GetMultipubTopicByKeyBase(ctx, MultipubTopicKeyBase("deadbeef", st.TopicID)); err == nil {
		t.Fatalf("a forged pool handle must not resolve a real topic_id")
	}
	if _, err := reg.GetMultipubTopicByKeyBase(ctx, "1a2b3c4d"); err == nil {
		t.Fatalf("a single-owner root must not resolve as a multi-publisher topic")
	}
}

func TestGrantInEffectAt(t *testing.T) {
	open := Grant{Status: GrantActive, EffectiveFrom: 100}
	bounded := Grant{Status: GrantActive, EffectiveFrom: 100, EffectiveTo: i64(200)}
	revoked := Grant{Status: GrantRevoked, EffectiveFrom: 100}
	cases := []struct {
		name string
		g    Grant
		t    int64
		want bool
	}{
		{"open before start", open, 50, false},
		{"open at start", open, 100, true},
		{"open far future", open, 1 << 40, true},
		{"bounded inside", bounded, 150, true},
		{"bounded at end (exclusive)", bounded, 200, false},
		{"bounded after end", bounded, 250, false},
		{"revoked ignores window", revoked, 150, false},
	}
	for _, c := range cases {
		if got := c.g.InEffectAt(c.t); got != c.want {
			t.Errorf("%s: InEffectAt(%d)=%v want %v", c.name, c.t, got, c.want)
		}
	}
}

func TestGrantRevokeIsEndDateNeverDelete(t *testing.T) {
	reg := testRegistry(t)
	ctx := context.Background()
	g := &Grant{GranteeKind: GranteeOrg, GranteeID: "acme", Producer: "acme-sensor", GrantedBy: "operator", EffectiveFrom: 100}
	if err := reg.CreateGrant(ctx, g); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := reg.RevokeGrant(ctx, g.ID, "operator"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	got, err := reg.GetGrant(ctx, g.ID)
	if err != nil {
		t.Fatalf("row must survive revoke (deprecate-never-delete): %v", err)
	}
	if got.Status != GrantRevoked || got.RevokedAt == nil || got.RevokedBy == nil {
		t.Fatalf("revoke should end-date, not delete: %+v", got)
	}
	// Idempotent second revoke keeps the first stamp.
	first := *got.RevokedAt
	if err := reg.RevokeGrant(ctx, g.ID, "someone-else"); err != nil {
		t.Fatalf("second revoke: %v", err)
	}
	again, _ := reg.GetGrant(ctx, g.ID)
	if *again.RevokedAt != first || *again.RevokedBy != "operator" {
		t.Fatalf("revoke should be idempotent, keep original stamp: %+v", again)
	}
	// Not in effect after revoke regardless of window.
	if again.InEffectAt(150) {
		t.Fatal("revoked grant must not be in effect")
	}
	if err := reg.RevokeGrant(ctx, "grant-nope", "operator"); err == nil {
		t.Fatal("expected ErrNotFound revoking unknown id")
	}
}

func TestActiveGrantsExcludesRevoked(t *testing.T) {
	reg := testRegistry(t)
	ctx := context.Background()
	live := &Grant{GranteeKind: GranteeOrg, GranteeID: "acme", Producer: "acme-sensor", GrantedBy: "op", EffectiveFrom: 1}
	dead := &Grant{GranteeKind: GranteeOrg, GranteeID: "acme", Producer: "acme-radar", GrantedBy: "op", EffectiveFrom: 1}
	if err := reg.CreateGrant(ctx, live); err != nil {
		t.Fatal(err)
	}
	if err := reg.CreateGrant(ctx, dead); err != nil {
		t.Fatal(err)
	}
	if err := reg.RevokeGrant(ctx, dead.ID, "op"); err != nil {
		t.Fatal(err)
	}
	active, err := reg.ActiveGrants(ctx)
	if err != nil {
		t.Fatalf("active grants: %v", err)
	}
	if len(active) != 1 || active[0].ID != live.ID {
		t.Fatalf("ActiveGrants should return only the live grant, got %d", len(active))
	}
}
