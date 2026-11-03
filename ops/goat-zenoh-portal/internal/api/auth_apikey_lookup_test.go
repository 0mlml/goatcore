package api

import (
	"io/fs"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/config"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/pgtest"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/web"
)

// newAuthTestServer returns a Server (not its routes — these tests call
// resolvePrincipal directly, which is the unit the property belongs to) plus
// its registry.
func newAuthTestServer(t *testing.T) (*Server, *registry.Registry) {
	t.Helper()
	reg, err := registry.Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open registry: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })
	staticFS, err := fs.Sub(web.Static, "static")
	if err != nil {
		t.Fatalf("static fs: %v", err)
	}
	return New(&config.Config{OperatorAPIKey: "test-op-key"}, reg, nil, nil, nil, nil, nil, staticFS), reg
}

// F-371. The portal used to authenticate a vendor by walking every row in the
// vendors table and running bcrypt against each api_key_hash. At 164 rows on
// ZAGROS that cost 11.7 CPU-seconds per request — including for a bearer token
// that was pure junk, so any caller could spend the portal's CPU without
// holding a credential at all.
//
// These tests assert the property that fixed it, and they assert it by
// COUNTING bcrypt comparisons rather than by timing anything: the cost of
// authentication must not scale with the number of vendors. A future change
// that reintroduces a walk fails here even on a fast machine.

// legacyVendor seeds a row exactly as the pre-F-371 portal would have written
// it: a bcrypt hash and NO digest. This is the shape every already-onboarded
// identity has at the moment the fix deploys.
func legacyVendor(t *testing.T, reg *registry.Registry, prefix string) string {
	t.Helper()
	key, err := generateAPIKey(prefix)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	hash, _, err := hashAPIKey(key)
	if err != nil {
		t.Fatalf("hash key: %v", err)
	}
	if err := reg.CreateVendor(t.Context(), &registry.Vendor{
		Prefix:      prefix,
		APIKeyHash:  hash,
		DisplayName: prefix,
	}); err != nil {
		t.Fatalf("create %s: %v", prefix, err)
	}
	return key
}

// comparesDuring returns how many bcrypt comparisons fn caused.
func comparesDuring(fn func()) int64 {
	before := APIKeyBcryptCompares()
	fn()
	return APIKeyBcryptCompares() - before
}

// A token that is not shaped like one this portal mints must be rejected
// without touching bcrypt at all — this is the leg that closes the
// unauthenticated amplifier.
func TestResolvePrincipal_MalformedTokenCostsNoBcrypt(t *testing.T) {
	s, reg := newAuthTestServer(t)
	for i := range 8 {
		legacyVendor(t, reg, string(rune('a'+i))+"-org")
	}

	for _, tok := range []string{
		"definitely-not-a-real-key",
		"",
		"vp_",
		"vp_org_short",
		"vp_org_" + "z0z0" /* non-hex */ + "00000000000000000000000000000000000000000000000000000000000",
		"vp_UPPER_" + hex64,
		"Bearer vp_a-org_" + hex64,
	} {
		var p *Principal
		n := comparesDuring(func() {
			p, _ = s.resolvePrincipal(t.Context(), tok)
		})
		if p != nil {
			t.Errorf("token %q authenticated; want rejection", tok)
		}
		if n != 0 {
			t.Errorf("token %q cost %d bcrypt compare(s); want 0", tok, n)
		}
	}
}

// A well-formed token naming a vendor that does not exist is also free: the
// row lookup misses and there is nothing to compare against.
func TestResolvePrincipal_UnknownVendorCostsNoBcrypt(t *testing.T) {
	s, reg := newAuthTestServer(t)
	for i := range 8 {
		legacyVendor(t, reg, string(rune('a'+i))+"-org")
	}

	var p *Principal
	n := comparesDuring(func() {
		p, _ = s.resolvePrincipal(t.Context(), "vp_nosuchorg_"+hex64)
	})
	if p != nil {
		t.Fatal("unknown vendor authenticated")
	}
	if n != 0 {
		t.Errorf("unknown-vendor token cost %d bcrypt compare(s); want 0", n)
	}
}

// THE REGRESSION GUARD. A legacy row authenticates at a cost of exactly one
// bcrypt no matter how many other vendors exist, and the row it names is the
// only one consulted — the walk is gone.
func TestResolvePrincipal_LegacyAuthCostIsFlatInVendorCount(t *testing.T) {
	s, reg := newAuthTestServer(t)
	// 40 rows, and we authenticate the LAST one alphabetically — the worst
	// case for a walk, which would have cost 40 bcrypts.
	var last string
	for i := range 40 {
		last = "org-" + string(rune('a'+i/26)) + string(rune('a'+i%26))
		legacyVendor(t, reg, last)
	}
	key := legacyVendor(t, reg, "zzz-final")

	var p *Principal
	n := comparesDuring(func() {
		p, _ = s.resolvePrincipal(t.Context(), key)
	})
	if p == nil || p.Vendor != "zzz-final" {
		t.Fatalf("legacy key resolved to %+v; want vendor zzz-final", p)
	}
	if n != 1 {
		t.Errorf("legacy auth cost %d bcrypt compare(s) with 41 vendors; want exactly 1", n)
	}
}

// A wrong secret for a REAL vendor also costs at most one compare. Before the
// fix this was the full-table case; it is the one an attacker who knows a
// valid anchor would have reached for.
func TestResolvePrincipal_WrongSecretForRealVendorCostsOne(t *testing.T) {
	s, reg := newAuthTestServer(t)
	for i := range 10 {
		legacyVendor(t, reg, "org-"+string(rune('a'+i)))
	}

	var p *Principal
	n := comparesDuring(func() {
		p, _ = s.resolvePrincipal(t.Context(), "vp_org-a_"+hex64)
	})
	if p != nil {
		t.Fatal("wrong secret authenticated")
	}
	if n > 1 {
		t.Errorf("wrong secret cost %d bcrypt compare(s); want at most 1", n)
	}
}

// After a legacy row authenticates once it backfills its digest, and every
// later request on that key is an index hit costing no bcrypt at all.
func TestResolvePrincipal_LegacyRowBackfillsAndStaysFast(t *testing.T) {
	s, reg := newAuthTestServer(t)
	key := legacyVendor(t, reg, "backfill-org")

	if n := comparesDuring(func() { _, _ = s.resolvePrincipal(t.Context(), key) }); n != 1 {
		t.Fatalf("first auth cost %d compare(s); want 1", n)
	}

	v, err := reg.GetVendor(t.Context(), "backfill-org")
	if err != nil {
		t.Fatalf("get vendor: %v", err)
	}
	if v.APIKeySHA256 != apiKeyDigest(key) {
		t.Fatalf("digest not backfilled: got %q", v.APIKeySHA256)
	}

	for i := range 3 {
		var p *Principal
		n := comparesDuring(func() { p, _ = s.resolvePrincipal(t.Context(), key) })
		if p == nil || p.Vendor != "backfill-org" {
			t.Fatalf("auth %d resolved to %+v", i, p)
		}
		if n != 0 {
			t.Errorf("auth %d after backfill cost %d bcrypt compare(s); want 0", i, n)
		}
	}

	// And the convergence signal notices.
	n, err := reg.CountVendorsAwaitingAPIKeyDigest(t.Context())
	if err != nil {
		t.Fatalf("count awaiting: %v", err)
	}
	if n != 0 {
		t.Errorf("awaiting-digest count = %d after backfill; want 0", n)
	}
}

// A freshly minted row carries its digest from the start and never touches
// bcrypt on the authentication path.
func TestResolvePrincipal_FreshMintNeverHashes(t *testing.T) {
	s, reg := newAuthTestServer(t)
	key := seedPrincipal(t, reg, "fresh-org", false, "")

	var p *Principal
	n := comparesDuring(func() { p, _ = s.resolvePrincipal(t.Context(), key) })
	if p == nil || p.Vendor != "fresh-org" {
		t.Fatalf("fresh key resolved to %+v", p)
	}
	if n != 0 {
		t.Errorf("fresh-mint auth cost %d bcrypt compare(s); want 0", n)
	}
}

// Rotation must invalidate the old token. Writing only the bcrypt hash would
// have left the previous key's digest indexed and still resolving — a revoked
// credential that keeps working.
func TestUpdateVendorAPIKey_RotationRetiresTheOldToken(t *testing.T) {
	s, reg := newAuthTestServer(t)
	oldKey := seedPrincipal(t, reg, "rotate-org", false, "")

	newKey, err := generateAPIKey("rotate-org")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	hash, digest, err := hashAPIKey(newKey)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := reg.UpdateVendorAPIKey(t.Context(), "rotate-org", hash, digest); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	if p, _ := s.resolvePrincipal(t.Context(), newKey); p == nil || p.Vendor != "rotate-org" {
		t.Errorf("new key resolved to %+v; want rotate-org", p)
	}
	if p, _ := s.resolvePrincipal(t.Context(), oldKey); p != nil {
		t.Errorf("retired key still authenticates as %+v", p)
	}
}

// A row with no credential at all (the deferred-credential onboarding state)
// authenticates to nothing, and costs nothing to refuse.
func TestResolvePrincipal_DeferredCredentialRowRefuses(t *testing.T) {
	s, reg := newAuthTestServer(t)
	if err := reg.CreateVendor(t.Context(), &registry.Vendor{
		Prefix: "deferred-org", APIKeyHash: "", DisplayName: "deferred",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	var p *Principal
	n := comparesDuring(func() {
		p, _ = s.resolvePrincipal(t.Context(), "vp_deferred-org_"+hex64)
	})
	if p != nil {
		t.Fatal("deferred-credential row authenticated")
	}
	if n != 0 {
		t.Errorf("deferred row cost %d bcrypt compare(s); want 0", n)
	}
}

// 64 hex chars — the secret half of a well-formed token, for cases where the
// secret is meant to be wrong.
const hex64 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
