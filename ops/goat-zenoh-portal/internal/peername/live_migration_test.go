package peername

import "testing"

// Real names read off the live ZAGROS NetBird API . Each was minted
// with the degenerate tag==handle. The point of the dual accept is that a
// revoke selector still claims every one of them after mint stopped emitting
// that form; a miss here is a credential that stays live after a cut.
func TestLiveDoubledNamesStillClaimed(t *testing.T) {
	cases := []struct{ name, handle string }{
		{"peer-user371x-user371x-zagros-2026-06-16-JL-2.local", "user371x"},
		{"peer-demo-org-008-demo-org-008-zagros-2026-06-24-ZENOH3", "demo-org-008"},
		{"peer-casey-lane-casey-lane-zagros-2026-06-18-Caseys-MacBook-Air.local", "casey-lane"},
		{"peer-contributora-contributora-zagros-2026-06-14-Laptops-MacBook-Pro.local", "contributora"},
		{"peer-user126x-user126x-zagros-2026-06-16-DESKTOP-627MJQ0", "user126x"},
		{"peer-user340x-user340x-zagros-2026-06-17-ip-172-31-22-233", "user340x"},
		{"peer-user128x-user128x-zagros-2026-06-23-TEIL-S-03", "user128x"},
		{"peer-demo-org-500-demo-org-500-zagros-2026-06-24-piecioshka-mac.local", "demo-org-500"},
	}
	for _, c := range cases {
		if !BelongsTo(c.name, c.handle, c.handle, "zagros") {
			t.Errorf("REVOKE WOULD MISS %s (handle %s)", c.name, c.handle)
		}
		// And the form mint produces from today.
		fresh := Mint(c.handle, c.handle, "zagros", "2026-08-15")
		if !BelongsTo(fresh+"-host", c.handle, c.handle, "zagros") {
			t.Errorf("selector misses the NEW form for %s: %s", c.handle, fresh)
		}
	}
}
