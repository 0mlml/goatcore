package api_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

// TestRekeyVendor_SwapsKeyLeavesCert locks the key-only reissue contract
// (goat#1193): POST /api/orgs/{anchor}/rekey mints a FRESH portal api_key
// and swaps the stored hash, while leaving the mTLS cert completely untouched —
// no new bundle, no download link, no slot consumed, no ACL render. This is the
// lighter sibling of /rotate for the recurring "lost/stale portal key, cert is
// fine, still publishing" case.
func TestRekeyVendor_SwapsKeyLeavesCert(t *testing.T) {
	dir := t.TempDir()
	aclPath := filepath.Join(dir, "config.json5")
	const aclMarker = "// pre-baked per-uuid ACL; rekey must not touch this\n"
	if err := os.WriteFile(aclPath, []byte(aclMarker), 0o644); err != nil {
		t.Fatalf("seed acl: %v", err)
	}
	ts, _ := newSlotPoolServer(t, aclPath)
	c := &http.Client{}

	// Onboard one system into a 3-slot pool.
	resp := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key",
		map[string]any{"prefix": "fra-land-bde"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("onboard: status=%d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	onboard := mustReadJSON(t, resp)
	anchor, _ := onboard["slot_id"].(string)
	if anchor == "" {
		t.Fatalf("onboard returned no slot_id")
	}
	oldKey, _ := onboard["api_key"].(string)
	oldToken, _ := onboard["bundle_token"].(string)
	if oldKey == "" || oldToken == "" {
		t.Fatalf("onboard missing api_key/bundle_token: %#v", onboard)
	}

	aliasCount := func() int {
		r := doJSON(t, c, "GET", ts.URL+"/api/aliases", "dev-operator-key", nil)
		return len(mustReadJSON(t, r)["aliases"].([]any))
	}
	if got := aliasCount(); got != 1 {
		t.Fatalf("aliases after onboard = %d, want 1", got)
	}

	// Rekey the anchor.
	resp = doJSON(t, c, "POST", ts.URL+"/api/orgs/"+anchor+"/rekey", "dev-operator-key", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rekey: status=%d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	rk := mustReadJSON(t, resp)

	// Identity unchanged: same anchor echoed back.
	if got, _ := rk["prefix"].(string); got != anchor {
		t.Errorf("rekey prefix=%q, want unchanged anchor %q", got, anchor)
	}

	// A fresh, non-empty portal key.
	newKey, _ := rk["api_key"].(string)
	if newKey == "" || newKey == oldKey {
		t.Errorf("rekey api_key=%q, want a fresh non-empty key (old=%q)", newKey, oldKey)
	}

	// The cert was NOT touched: rekey produces no bundle at all — no URL, no
	// token. This is the whole point (no cert churn, no download step).
	if got, ok := rk["bundle_url"]; ok && got != "" {
		t.Errorf("rekey returned bundle_url=%v, want none (cert untouched)", got)
	}
	if got, ok := rk["bundle_token"]; ok && got != "" {
		t.Errorf("rekey returned bundle_token=%v, want none (cert untouched)", got)
	}

	// The fresh portal key authenticates a vendor-scoped read; the old one no
	// longer does (the hash was swapped in place).
	if r := doJSON(t, c, "GET", ts.URL+"/api/vendors/"+anchor+"/topics", newKey, nil); r.StatusCode != http.StatusOK {
		t.Errorf("read with rekeyed key: status=%d, want 200", r.StatusCode)
	}
	if r := doJSON(t, c, "GET", ts.URL+"/api/vendors/"+anchor+"/topics", oldKey, nil); r.StatusCode == http.StatusOK {
		t.Errorf("read with old key succeeded after rekey; want the old key rejected")
	}

	// No churn: alias count unchanged, ACL byte-identical, and the slot pool was
	// NOT consumed — onboarding two MORE systems fills the remaining 2 slots.
	if got := aliasCount(); got != 1 {
		t.Errorf("aliases after rekey = %d, want 1 (no churn)", got)
	}
	if after, _ := os.ReadFile(aclPath); string(after) != aclMarker {
		t.Errorf("ACL modified by rekey; want untouched. got: %q", string(after))
	}
	for _, h := range []string{"ita-recon-coy", "deu-log-coy"} {
		if r := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{"prefix": h}); r.StatusCode != http.StatusCreated {
			t.Fatalf("post-rekey onboard %s: want 201, got %d %s", h, r.StatusCode, mustReadBody(t, r))
		}
	}
	if r := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{"prefix": "overflow"}); r.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("4th onboard after rekey: want 503 (pool was 3, rekey consumed none), got %d", r.StatusCode)
	}
}

// TestRekeyVendor_PerVendorMode — rekey works in per-vendor mode too (the key
// swap is not slot-pool-specific), keyed on the vendor prefix.
func TestRekeyVendor_PerVendorMode(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}

	resp := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key",
		map[string]any{"prefix": "community", "display_name": "Community", "auto_approve": true})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register: status=%d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	oldKey, _ := mustReadJSON(t, resp)["api_key"].(string)

	resp = doJSON(t, c, "POST", ts.URL+"/api/orgs/community/rekey", "dev-operator-key", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rekey: status=%d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	newKey, _ := mustReadJSON(t, resp)["api_key"].(string)
	if newKey == "" || newKey == oldKey {
		t.Errorf("rekey api_key=%q, want fresh (old=%q)", newKey, oldKey)
	}
	if r := doJSON(t, c, "GET", ts.URL+"/api/vendors/community/topics", newKey, nil); r.StatusCode != http.StatusOK {
		t.Errorf("read with rekeyed key: status=%d, want 200", r.StatusCode)
	}
}

// TestRekeyVendor_UnknownAnchor404 — rekey re-issues a key for an existing
// peer; it never creates. An unknown anchor is a 404.
func TestRekeyVendor_UnknownAnchor404(t *testing.T) {
	dir := t.TempDir()
	aclPath := filepath.Join(dir, "config.json5")
	if err := os.WriteFile(aclPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("seed acl: %v", err)
	}
	ts, _ := newSlotPoolServer(t, aclPath)
	c := &http.Client{}

	unknown := registry.SlotAnchor(slotSalt, 0) // valid shape, never onboarded
	resp := doJSON(t, c, "POST", ts.URL+"/api/orgs/"+unknown+"/rekey", "dev-operator-key", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("rekey unknown anchor: want 404, got %d %s", resp.StatusCode, mustReadBody(t, resp))
	}
}

// TestRekeyVendor_RequiresOperator — rekey is operator-gated; a vendor Bearer
// token (or none) must not be able to reissue a key.
func TestRekeyVendor_RequiresOperator(t *testing.T) {
	dir := t.TempDir()
	aclPath := filepath.Join(dir, "config.json5")
	if err := os.WriteFile(aclPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("seed acl: %v", err)
	}
	ts, _ := newSlotPoolServer(t, aclPath)
	c := &http.Client{}

	resp := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{"prefix": "alpha"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("onboard: %d %s", resp.StatusCode, mustReadBody(t, resp))
	}
	body := mustReadJSON(t, resp)
	anchor, _ := body["slot_id"].(string)
	vendorKey, _ := body["api_key"].(string)

	if r := doJSON(t, c, "POST", ts.URL+"/api/orgs/"+anchor+"/rekey", vendorKey, nil); r.StatusCode == http.StatusOK {
		t.Errorf("rekey with vendor key succeeded; want operator-only rejection")
	}
}
